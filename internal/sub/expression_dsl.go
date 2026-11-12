package sub

import (
	"errors"
	"strings"

	js "github.com/shusfun/weir/internal/sub/jscompat"
)

// 移植自 functions/utils/expression-dsl.js：受限的声明式表达式，不执行任何脚本。

var (
	reDSLIdentifier = js.MustRegExp(`^[a-zA-Z_][a-zA-Z0-9_]*$`, "")
	reDSLString     = js.MustRegExp(`^(?:'([^'\\]*(?:\\.[^'\\]*)*)'|"([^"\\]*(?:\\.[^"\\]*)*)")$`, "")
	reDSLNumber     = js.MustRegExp(`^-?\d+(?:\.\d+)?$`, "")
	reDSLEscape     = js.MustRegExp(`\\(['"\\nrt])`, "g")
	reDSLCall       = js.MustRegExp(`^([a-zA-Z_][a-zA-Z0-9_]*)\((.*)\)$`, "s")
	reDSLOr         = js.MustRegExp(`\s+\|\|\s+`, "")
	reDSLAnd        = js.MustRegExp(`\s+&&\s+`, "")
	reDSLCompare    = js.MustRegExp(`^(.+?)\s*(===|!==|>=|<=|>|<)\s*(.+)$`, "s")
	reDSLTemplate   = js.MustRegExp(`\{([^{}]+)\}`, "g")
)

func dslUnquote(value any) string {
	text := js.Trim(strOr(value))
	m := reDSLString.MatchFirst(text)
	if m == nil {
		return text
	}
	raw := js.ToString(js.Coalesce(m.Group(1), m.Group(2), ""))
	return reDSLEscape.ReplaceFunc(raw, func(mm *js.Match) string {
		switch mm.Str(1) {
		case "n":
			return "\n"
		case "r":
			return "\r"
		case "t":
			return "\t"
		}
		return mm.Str(1)
	})
}

func dslSplitArgs(input string) []string {
	var args []string
	var current strings.Builder
	quote := rune(0)
	escaped := false
	depth := 0
	for _, ch := range input {
		if escaped {
			current.WriteRune(ch)
			escaped = false
			continue
		}
		if ch == '\\' {
			current.WriteRune(ch)
			escaped = true
			continue
		}
		if quote != 0 {
			current.WriteRune(ch)
			if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '\'' || ch == '"' {
			quote = ch
			current.WriteRune(ch)
			continue
		}
		if ch == '(' {
			depth++
		}
		if ch == ')' {
			depth = max(0, depth-1)
		}
		if ch == ',' && depth == 0 {
			args = append(args, js.Trim(current.String()))
			current.Reset()
			continue
		}
		current.WriteRune(ch)
	}
	if js.Trim(current.String()) != "" || input == "" {
		args = append(args, js.Trim(current.String()))
	}
	return args
}

// dslGetField 对应 getField：只读取自有属性，标识符非法时返回空串。
func dslGetField(ctx *js.Object, name any) any {
	if !reDSLIdentifier.Test(strOr(name)) {
		return ""
	}
	key := js.ToString(name)
	if ctx.Has(key) {
		return ctx.Get(key)
	}
	return ""
}

type dslEvaluation struct {
	err   error
	depth int
}

func (e *dslEvaluation) match(value, pattern, flags any) bool {
	re, err := js.NewConfigRegExp(strOr(pattern), js.ToString(js.Or(flags, "i")))
	if err != nil {
		e.err = errors.New("dsl_invalid_regex")
		return false
	}
	return re.Test(strOr(value))
}

func (e *dslEvaluation) replace(value, pattern, replacement, flags any) string {
	re, err := js.NewConfigRegExp(strOr(pattern), js.ToString(js.Or(flags, "g")))
	if err != nil {
		e.err = errors.New("dsl_invalid_regex")
		return ""
	}
	out, err := re.ReplaceBounded(strOr(value), strOr(replacement), 64<<10)
	if err != nil {
		e.err = errors.New("dsl_output_too_large")
		return ""
	}
	return out
}

func dslFallback(values []any) any {
	for _, v := range values {
		if !js.IsNullish(v) && js.Trim(js.ToString(v)) != "" {
			return v
		}
	}
	return ""
}

func argAt(args []any, i int) any {
	if i < len(args) {
		return args[i]
	}
	return nil
}

func (e *dslEvaluation) value(expr any, ctx *js.Object) any {
	if e.err != nil {
		return ""
	}
	if e.depth >= 32 || len(strOr(expr)) > 64<<10 {
		e.err = errors.New("dsl_expression_limit")
		return ""
	}
	e.depth++
	defer func() { e.depth-- }()
	text := js.Trim(strOr(expr))
	if text == "" {
		return ""
	}
	if reDSLString.Test(text) {
		return dslUnquote(text)
	}
	if reDSLNumber.Test(text) {
		return js.StringToNumber(text)
	}
	switch text {
	case "true":
		return true
	case "false":
		return false
	case "null":
		return js.Null
	}
	if m := reDSLCall.MatchFirst(text); m != nil {
		fn := m.Str(1)
		var args []any
		for _, a := range dslSplitArgs(m.Str(2)) {
			args = append(args, e.value(a, ctx))
		}
		switch fn {
		case "upper":
			return js.ToUpper(strOr(argAt(args, 0)))
		case "lower":
			return js.ToLower(strOr(argAt(args, 0)))
		case "title":
			return js.CharAtUpperFirst(strOr(argAt(args, 0)))
		case "trim":
			return js.Trim(strOr(argAt(args, 0)))
		case "replace":
			return e.replace(argAt(args, 0), argAt(args, 1), js.Or(argAt(args, 2), ""), argAt(args, 3))
		case "contains":
			return strings.Contains(js.ToLower(strOr(argAt(args, 0))), js.ToLower(strOr(argAt(args, 1))))
		case "match":
			return e.match(argAt(args, 0), argAt(args, 1), js.Or(argAt(args, 2), "i"))
		case "fallback":
			return dslFallback(args)
		case "pick":
			falsy := js.Coalesce(argAt(args, 2), "")
			if js.Truthy(argAt(args, 0)) {
				return argAt(args, 1)
			}
			return falsy
		}
		return ""
	}
	return dslGetField(ctx, text)
}

// EvaluateDslExpression 对应 evaluateDslExpression。
func EvaluateDslExpression(expression any, ctx *js.Object) bool {
	value, _ := EvaluateDslExpressionChecked(expression, ctx)
	return value
}

func EvaluateDslExpressionChecked(expression any, ctx *js.Object) (bool, error) {
	e := &dslEvaluation{}
	value := e.evaluate(expression, ctx)
	if e.err != nil {
		return false, e.err
	}
	return value, nil
}

func (e *dslEvaluation) evaluate(expression any, ctx *js.Object) bool {
	if e.err != nil {
		return false
	}
	if e.depth >= 32 || len(strOr(expression)) > 64<<10 {
		e.err = errors.New("dsl_expression_limit")
		return false
	}
	e.depth++
	defer func() { e.depth-- }()
	if ctx == nil {
		ctx = js.NewObject()
	}
	expr := js.Trim(strOr(expression))
	if expr == "" {
		return true
	}
	if parts := dslSplitBoolean(expr, "||"); len(parts) > 1 {
		for _, p := range parts {
			if e.evaluate(p, ctx) {
				return true
			}
		}
		return false
	}
	if parts := dslSplitBoolean(expr, "&&"); len(parts) > 1 {
		for _, p := range parts {
			if !e.evaluate(p, ctx) {
				return false
			}
		}
		return true
	}
	if m := reDSLCompare.MatchFirst(expr); m != nil {
		left := e.value(m.Str(1), ctx)
		right := e.value(m.Str(3), ctx)
		switch m.Str(2) {
		case "===":
			return js.ToString(left) == js.ToString(right)
		case "!==":
			return js.ToString(left) != js.ToString(right)
		case ">=":
			return js.ToNumber(left) >= js.ToNumber(right)
		case "<=":
			return js.ToNumber(left) <= js.ToNumber(right)
		case ">":
			return js.ToNumber(left) > js.ToNumber(right)
		case "<":
			return js.ToNumber(left) < js.ToNumber(right)
		}
		return false
	}
	return js.Truthy(e.value(expr, ctx))
}

// RenderDslTemplate 对应 renderDslTemplate。
func RenderDslTemplate(template any, ctx *js.Object) string {
	value, _ := RenderDslTemplateChecked(template, ctx)
	return value
}

func RenderDslTemplateChecked(template any, ctx *js.Object) (string, error) {
	e := &dslEvaluation{}
	value := e.render(template, ctx)
	if e.err != nil {
		return "", e.err
	}
	return value, nil
}

func (e *dslEvaluation) render(template any, ctx *js.Object) string {
	if len(strOr(template)) > 64<<10 {
		e.err = errors.New("dsl_template_limit")
		return ""
	}
	if ctx == nil {
		ctx = js.NewObject()
	}
	text := strOr(template)
	var out strings.Builder
	write := func(value string) bool {
		if len(value) > (64<<10)-out.Len() {
			e.err = errors.New("dsl_output_too_large")
			return false
		}
		out.WriteString(value)
		return true
	}
	pos := 0
	for _, m := range reDSLTemplate.FindAll(text) {
		if !write(text[pos:m.Index]) {
			return ""
		}
		v := e.value(m.Str(1), ctx)
		if e.err != nil {
			return ""
		}
		if !js.IsNullish(v) && !write(js.ToString(v)) {
			return ""
		}
		pos = m.End
	}
	if !write(text[pos:]) {
		return ""
	}
	return js.Trim(out.String())
}

// MatchesDslCondition 对应 matchesDslCondition；condition 可以是表达式字符串或 {field, op, value, flags}。
func MatchesDslCondition(record *js.Object, condition any) bool {
	value, _ := MatchesDslConditionChecked(record, condition)
	return value
}

func MatchesDslConditionChecked(record *js.Object, condition any) (bool, error) {
	e := &dslEvaluation{}
	value := e.condition(record, condition)
	if e.err != nil {
		return false, e.err
	}
	return value, nil
}

func (e *dslEvaluation) condition(record *js.Object, condition any) bool {
	if s, ok := condition.(string); ok {
		return e.evaluate(s, record)
	}
	cond, ok := condition.(*js.Object)
	if !ok {
		// 数组同样是 object，但其属性均为 undefined
		if _, isArr := condition.(*js.Array); !isArr {
			return true
		}
		cond = js.NewObject()
	}
	actual := dslGetField(record, js.Or(cond.Get("field"), "name"))
	expected := js.Coalesce(cond.Get("value"), "")
	op := js.ToLower(js.ToString(js.Or(cond.Get("op"), "contains")))
	switch op {
	case "eq", "equals":
		return js.ToString(actual) == js.ToString(expected)
	case "ne", "not_equals":
		return js.ToString(actual) != js.ToString(expected)
	case "contains":
		return strings.Contains(js.ToLower(strOr(actual)), js.ToLower(strOr(expected)))
	case "not_contains":
		return !strings.Contains(js.ToLower(strOr(actual)), js.ToLower(strOr(expected)))
	case "match", "regex":
		return e.match(actual, expected, js.Or(cond.Get("flags"), "i"))
	case "in":
		arr, isArr := expected.(*js.Array)
		if !isArr {
			return false
		}
		a := js.ToString(actual)
		for _, e := range arr.Items {
			if js.ToString(e) == a {
				return true
			}
		}
		return false
	}
	return false
}

func dslSplitBoolean(text, token string) []string {
	var parts []string
	quote := byte(0)
	escaped, depth, start := false, 0, 0
	for i := 0; i < len(text); i++ {
		ch := text[i]
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '\'' || ch == '"' {
			quote = ch
			continue
		}
		if ch == '(' {
			depth++
			continue
		}
		if ch == ')' {
			depth--
			continue
		}
		if depth == 0 && i > 0 && i+len(token) < len(text) && dslSpace(text[i-1]) && dslSpace(text[i+len(token)]) && strings.HasPrefix(text[i:], token) {
			parts = append(parts, text[start:i])
			i += len(token) - 1
			start = i + 1
		}
	}
	return append(parts, text[start:])
}

func dslSpace(ch byte) bool { return ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' }
