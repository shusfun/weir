package subscriptions

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// MiSub renderTemplate is distinct from its expression DSL renderer.
var operatorPlaceholder = regexp.MustCompile(`\{([a-zA-Z0-9_]+)(?::([a-zA-Z0-9]+))?\}`)

func RenderOperatorTemplate(template string, ctx map[string]any) (string, error) {
	if len(template) > 8192 {
		return "", errors.New("template too large")
	}
	var first error
	result := operatorPlaceholder.ReplaceAllStringFunc(template, func(token string) string {
		parts := operatorPlaceholder.FindStringSubmatch(token)
		key, modifier := parts[1], parts[2]
		value, ok := ctx[key]
		if !ok {
			return ""
		}
		s := str(value)
		switch modifier {
		case "UPPER":
			return strings.ToUpper(s)
		case "lower":
			return strings.ToLower(s)
		case "Title":
			r := []rune(s)
			if len(r) > 0 {
				r[0] = unicode.ToUpper(r[0])
			}
			return string(r)
		case "zh":
			if key == "region" {
				return str(ctx["regionZh"])
			}
		default:
			if key == "index" && modifier != "" {
				if width, err := strconv.Atoi(modifier); err == nil {
					if width > 1024 {
						first = errors.New("index padding limit")
						return ""
					}
					if width > len(s) {
						return strings.Repeat("0", width-len(s)) + s
					}
				}
			}
		}
		return s
	})
	return strings.TrimSpace(result), first
}
