package sub

import (
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

func dslSafeMatch(value, pattern, flags any) bool {
	re, err := js.NewRegExp(strOr(pattern), js.ToString(js.Or(flags, "i")))
	if err != nil {
		return false
	}
	return re.Test(strOr(value))
}

func dslSafeReplace(value, pattern, replacement, flags any) string {
	re, err := js.NewRegExp(strOr(pattern), js.ToString(js.Or(flags, "g")))
	if err != nil {
		return strOr(value)
	}
	return re.Replace(strOr(value), strOr(replacement))
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

func dslEvalValue(expr any, ctx *js.Object) any {
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
			args = append(args, dslEvalValue(a, ctx))
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
			return dslSafeReplace(argAt(args, 0), argAt(args, 1), js.Or(argAt(args, 2), ""), argAt(args, 3))
		case "contains":
			return strings.Contains(js.ToLower(strOr(argAt(args, 0))), js.ToLower(strOr(argAt(args, 1))))
		case "match":
			return dslSafeMatch(argAt(args, 0), argAt(args, 1), js.Or(argAt(args, 2), "i"))
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
	if ctx == nil {
		ctx = js.NewObject()
	}
	expr := js.Trim(strOr(expression))
	if expr == "" {
		return true
	}
	if parts := reDSLOr.Split(expr, -1); len(parts) > 1 {
		for _, p := range parts {
			if EvaluateDslExpression(p, ctx) {
				return true
			}
		}
		return false
	}
	if parts := reDSLAnd.Split(expr, -1); len(parts) > 1 {
		for _, p := range parts {
			if !EvaluateDslExpression(p, ctx) {
				return false
			}
		}
		return true
	}
	if m := reDSLCompare.MatchFirst(expr); m != nil {
		left := dslEvalValue(m.Str(1), ctx)
		right := dslEvalValue(m.Str(3), ctx)
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
	return js.Truthy(dslEvalValue(expr, ctx))
}

// RenderDslTemplate 对应 renderDslTemplate。
func RenderDslTemplate(template any, ctx *js.Object) string {
	if ctx == nil {
		ctx = js.NewObject()
	}
	out := reDSLTemplate.ReplaceFunc(strOr(template), func(m *js.Match) string {
		v := dslEvalValue(m.Str(1), ctx)
		if js.IsNullish(v) {
			return ""
		}
		return js.ToString(v)
	})
	return js.Trim(out)
}

// MatchesDslCondition 对应 matchesDslCondition；condition 可以是表达式字符串或 {field, op, value, flags}。
func MatchesDslCondition(record *js.Object, condition any) bool {
	if s, ok := condition.(string); ok {
		return EvaluateDslExpression(s, record)
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
		return dslSafeMatch(actual, expected, js.Or(cond.Get("flags"), "i"))
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
