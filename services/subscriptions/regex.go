package subscriptions

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
)

type regexRule struct{ Pattern, Flags, Replacement string }

func regexRename(name string, rules []json.RawMessage, ctx map[string]any) (string, error) {
	for _, raw := range rules {
		var rule regexRule
		if json.Unmarshal(raw, &rule.Pattern) != nil {
			if err := decodeOperator(raw, &rule); err != nil {
				return "", err
			}
		}
		if rule.Pattern == "" {
			continue
		}
		if rule.Flags == "" {
			rule.Flags = "gi"
		}
		re, err := safeRegex(rule.Pattern, rule.Flags)
		if err != nil {
			return "", err
		}
		replacement := rule.Replacement
		if strings.Contains(replacement, "{") {
			replacement, err = RenderOperatorTemplate(replacement, ctx)
			if err != nil {
				return "", err
			}
		}
		matches := re.FindAllStringSubmatchIndex(name, -1)
		if len(matches) == 0 {
			continue
		}
		first := matches[0]
		for i := 1; i < len(first)/2; i++ {
			value := ""
			if first[2*i] >= 0 {
				value = name[first[2*i]:first[2*i+1]]
			}
			ctx["g"+strconv.Itoa(i)] = value
			if n := re.SubexpNames()[i]; n != "" {
				ctx["g_"+n] = value
			}
		}
		if !strings.Contains(rule.Flags, "g") {
			matches = matches[:1]
		}
		var result strings.Builder
		pos := 0
		for _, m := range matches {
			result.WriteString(name[pos:m[0]])
			result.WriteString(jsReplacement(replacement, name, m, re.SubexpNames()))
			if result.Len() > 8192 {
				return "", errors.New("regex replacement size limit")
			}
			pos = m[1]
		}
		result.WriteString(name[pos:])
		name = strings.TrimSpace(result.String())
	}
	return name, nil
}

// JS replacement tokens differ from Go's $name expansion: preserve literal
// unknown tokens, $$, $&, prefix/suffix and numbered/named capture semantics.
func jsReplacement(repl, input string, m []int, names []string) string {
	var out strings.Builder
	capture := func(n int) string {
		if n < 0 || 2*n+1 >= len(m) || m[2*n] < 0 {
			return ""
		}
		return input[m[2*n]:m[2*n+1]]
	}
	for i := 0; i < len(repl); i++ {
		if repl[i] != '$' || i+1 == len(repl) {
			out.WriteByte(repl[i])
			continue
		}
		switch repl[i+1] {
		case '$':
			out.WriteByte('$')
			i++
		case '&':
			out.WriteString(capture(0))
			i++
		case '`':
			out.WriteString(input[:m[0]])
			i++
		case '\'':
			out.WriteString(input[m[1]:])
			i++
		case '<':
			end := strings.IndexByte(repl[i+2:], '>')
			found := false
			if end >= 0 {
				key := repl[i+2 : i+2+end]
				for j, n := range names {
					if n != "" && n == key {
						out.WriteString(capture(j))
						i += end + 3
						found = true
						break
					}
				}
			}
			if !found {
				out.WriteByte('$')
			}
		default:
			n := int(repl[i+1] - '0')
			digits := 1
			if n >= 0 && n <= 9 && i+2 < len(repl) && repl[i+2] >= '0' && repl[i+2] <= '9' {
				two := n*10 + int(repl[i+2]-'0')
				if two > 0 && two < len(m)/2 {
					n = two
					digits = 2
				}
			}
			if n > 0 && n < len(m)/2 {
				out.WriteString(capture(n))
				i += digits
			} else {
				out.WriteByte('$')
			}
		}
	}
	return out.String()
}
