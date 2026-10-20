// Declarative DSL adapted from MiSub 4952c16 functions/utils/expression-dsl.js (MIT).
// No JavaScript evaluator, filesystem, network, or process primitives exist here.
package subscriptions

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

var identifier = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
var comparison = regexp.MustCompile(`(?s)^(.+?)\s*(===|!==|>=|<=|>|<)\s*(.+)$`)
var placeholders = regexp.MustCompile(`\{([^{}]+)\}`)

func truth(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != ""
	case float64:
		return x != 0
	case int:
		return x != 0
	default:
		return true
	}
}
func str(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}
func asNumber(v any) float64 {
	n, e := strconv.ParseFloat(str(v), 64)
	if e != nil {
		return 0
	}
	return n
}
func Evaluate(expression string, ctx map[string]any) (bool, error) {
	return evaluate(strings.TrimSpace(expression), ctx, 0)
}
func evaluate(expr string, ctx map[string]any, depth int) (bool, error) {
	if len(expr) > 4096 || depth > 32 {
		return false, errors.New("DSL complexity limit")
	}
	if expr == "" {
		return true, nil
	}
	for _, op := range []string{" || ", " && "} {
		parts := strings.Split(expr, op)
		if len(parts) > 1 {
			value := op == " && "
			for _, p := range parts {
				v, e := evaluate(strings.TrimSpace(p), ctx, depth+1)
				if e != nil {
					return false, e
				}
				if op == " || " {
					value = value || v
				} else {
					value = value && v
				}
			}
			return value, nil
		}
	}
	if m := comparison.FindStringSubmatch(expr); m != nil {
		a, e := evalValue(m[1], ctx, depth+1)
		if e != nil {
			return false, e
		}
		b, e := evalValue(m[3], ctx, depth+1)
		if e != nil {
			return false, e
		}
		switch m[2] {
		case "===":
			return str(a) == str(b), nil
		case "!==":
			return str(a) != str(b), nil
		case ">=":
			return asNumber(a) >= asNumber(b), nil
		case "<=":
			return asNumber(a) <= asNumber(b), nil
		case ">":
			return asNumber(a) > asNumber(b), nil
		case "<":
			return asNumber(a) < asNumber(b), nil
		}
	}
	v, e := evalValue(expr, ctx, depth+1)
	return truth(v), e
}
func splitArgs(s string) ([]string, error) {
	var result []string
	start, depth := 0, 0
	var quote rune
	escaped := false
	for i, c := range s {
		if escaped {
			escaped = false
			continue
		}
		if c == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
			continue
		}
		switch c {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				result = append(result, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
		if depth < 0 {
			return nil, errors.New("unbalanced DSL")
		}
	}
	if quote != 0 || depth != 0 {
		return nil, errors.New("unbalanced DSL")
	}
	if strings.TrimSpace(s) != "" {
		result = append(result, strings.TrimSpace(s[start:]))
	}
	return result, nil
}
func evalValue(expr string, ctx map[string]any, depth int) (any, error) {
	if depth > 32 || len(expr) > 4096 {
		return nil, errors.New("DSL complexity limit")
	}
	s := strings.TrimSpace(expr)
	if s == "" {
		return "", nil
	}
	if len(s) >= 2 && ((s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'')) {
		raw := s[1 : len(s)-1]
		raw = strings.NewReplacer(`\'`, `'`, `\"`, `"`, `\n`, "\n", `\r`, "\r", `\t`, "\t", `\\`, `\`).Replace(raw)
		return raw, nil
	}
	if s == "true" {
		return true, nil
	}
	if s == "false" {
		return false, nil
	}
	if s == "null" {
		return nil, nil
	}
	if n, e := strconv.ParseFloat(s, 64); e == nil {
		return n, nil
	}
	if at := strings.IndexByte(s, '('); at > 0 && strings.HasSuffix(s, ")") {
		name := s[:at]
		if !identifier.MatchString(name) {
			return nil, errors.New("invalid DSL function")
		}
		raw, e := splitArgs(s[at+1 : len(s)-1])
		if e != nil {
			return nil, e
		}
		if len(raw) > 20 {
			return nil, errors.New("too many DSL arguments")
		}
		args := make([]any, len(raw))
		for i, v := range raw {
			args[i], e = evalValue(v, ctx, depth+1)
			if e != nil {
				return nil, e
			}
		}
		get := func(i int) string {
			if i >= len(args) {
				return ""
			}
			return str(args[i])
		}
		switch name {
		case "upper":
			return strings.ToUpper(get(0)), nil
		case "lower":
			return strings.ToLower(get(0)), nil
		case "title":
			v := get(0)
			if v == "" {
				return "", nil
			}
			r, n := utf8.DecodeRuneInString(v)
			return string(unicode.ToUpper(r)) + v[n:], nil
		case "trim":
			return strings.TrimSpace(get(0)), nil
		case "contains":
			return strings.Contains(strings.ToLower(get(0)), strings.ToLower(get(1))), nil
		case "match", "replace":
			flags := get(2)
			pattern := get(1)
			if name == "replace" {
				flags = get(3)
			}
			if name == "match" && flags == "" {
				flags = "i"
			}
			re, e := safeRegex(pattern, flags)
			if e != nil {
				return nil, e
			}
			if name == "match" {
				return re.MatchString(get(0)), nil
			}
			replacement := get(2)
			if !strings.Contains(flags, "g") && flags != "" {
				loc := re.FindStringIndex(get(0))
				if loc == nil {
					return get(0), nil
				}
				return get(0)[:loc[0]] + re.ReplaceAllString(get(0)[loc[0]:loc[1]], replacement) + get(0)[loc[1]:], nil
			}
			return re.ReplaceAllString(get(0), replacement), nil
		case "fallback":
			for _, v := range args {
				if strings.TrimSpace(str(v)) != "" {
					return v, nil
				}
			}
			return "", nil
		case "pick":
			if len(args) < 2 {
				return nil, errors.New("pick requires two arguments")
			}
			if truth(args[0]) {
				return args[1], nil
			}
			if len(args) > 2 {
				return args[2], nil
			}
			return "", nil
		default:
			return nil, fmt.Errorf("unsupported DSL function %s", name)
		}
	}
	if !identifier.MatchString(s) {
		return nil, errors.New("unsupported DSL expression")
	}
	return ctx[s], nil
}
func safeRegex(pattern, flags string) (*regexp.Regexp, error) {
	if len(pattern) > 1024 {
		return nil, errors.New("regex too large")
	}
	prefix := ""
	for _, f := range flags {
		switch f {
		case 'i':
			prefix += "i"
		case 'm':
			prefix += "m"
		case 's':
			prefix += "s"
		case 'g':
		default:
			return nil, errors.New("unsupported regex flag")
		}
	}
	if prefix != "" {
		pattern = "(?" + prefix + ")" + pattern
	}
	return regexp.Compile(pattern)
}
func Render(template string, ctx map[string]any) (string, error) {
	if len(template) > 8192 {
		return "", errors.New("template too large")
	}
	var first error
	value := placeholders.ReplaceAllStringFunc(template, func(v string) string {
		x, e := evalValue(v[1:len(v)-1], ctx, 0)
		if e != nil && first == nil {
			first = e
		}
		return str(x)
	})
	return strings.TrimSpace(value), first
}
func MatchCondition(ctx map[string]any, raw json.RawMessage) (bool, error) {
	var expr string
	if json.Unmarshal(raw, &expr) == nil {
		return Evaluate(expr, ctx)
	}
	var c struct {
		Action, Field, Op, Flags string
		Value                    any
	}
	if e := decodeOperator(raw, &c); e != nil {
		return false, e
	}
	if c.Field == "" {
		c.Field = "name"
	}
	switch c.Field {
	case "name", "originalName", "cleanName", "multiplier", "group", "provider", "protocol", "server", "port", "index", "globalIndex", "target", "region", "regionZh", "emoji":
	default:
		return false, errors.New("unsupported DSL field")
	}
	if c.Op == "" {
		c.Op = "contains"
	}
	a, b := str(ctx[c.Field]), str(c.Value)
	switch strings.ToLower(c.Op) {
	case "eq", "equals":
		return a == b, nil
	case "ne", "not_equals":
		return a != b, nil
	case "contains":
		return strings.Contains(strings.ToLower(a), strings.ToLower(b)), nil
	case "not_contains":
		return !strings.Contains(strings.ToLower(a), strings.ToLower(b)), nil
	case "match", "regex":
		if c.Flags == "" {
			c.Flags = "i"
		}
		re, e := safeRegex(b, c.Flags)
		if e != nil {
			return false, e
		}
		return re.MatchString(a), nil
	case "in":
		list, ok := c.Value.([]any)
		if !ok {
			return false, errors.New("in requires array")
		}
		for _, v := range list {
			if str(v) == a {
				return true, nil
			}
		}
		return false, nil
	default:
		return false, errors.New("unsupported DSL condition")
	}
}
