package subscriptions

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// MiSub normalizeRules accepts newline text, string arrays and pattern objects.
func filterRules(raw json.RawMessage) ([]*regexp.Regexp, error) {
	var values []json.RawMessage
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var value string
	if json.Unmarshal(raw, &value) == nil {
		values = []json.RawMessage{raw}
	} else if err := json.Unmarshal(raw, &values); err != nil {
		return nil, err
	}
	if len(values) > 100 {
		return nil, fmt.Errorf("filter rule limit")
	}
	result := []*regexp.Regexp{}
	for _, item := range values {
		var rule regexRule
		if json.Unmarshal(item, &value) == nil {
			for _, line := range strings.Split(value, "\n") {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				re, err := safeRegex(line, "i")
				if err != nil {
					return nil, err
				}
				result = append(result, re)
			}
		} else {
			if err := decodeOperator(item, &rule); err != nil {
				return nil, err
			}
			if rule.Pattern == "" {
				continue
			}
			if rule.Flags == "" {
				rule.Flags = "i"
			}
			re, err := safeRegex(rule.Pattern, rule.Flags)
			if err != nil {
				return nil, err
			}
			result = append(result, re)
		}
		if len(result) > 100 {
			return nil, fmt.Errorf("filter rule limit")
		}
	}
	return result, nil
}
