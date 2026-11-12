package sub

import (
	"fmt"
	"strings"

	js "github.com/shusfun/weir/internal/sub/jscompat"
)

// ValidateFilterRules shares the same RE2 parser used by the processing chain.
func ValidateFilterRules(text string) error {
	if len(text) > 64<<10 {
		return fmt.Errorf("filter_rules_too_large")
	}
	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(line), "keep:") {
			line = strings.TrimSpace(line[5:])
		}
		if line == "" || line == "---" || strings.HasPrefix(strings.ToLower(line), "proto:") {
			continue
		}
		if _, err := js.NewConfigRegExp(line, "i"); err != nil {
			return fmt.Errorf("filter line %d: invalid RE2 pattern", i+1)
		}
	}
	return nil
}

// ValidateOperatorPatterns checks nested filter and rename declarations,
// including disabled entries so enabling them cannot expose a latent error.
func ValidateOperatorPatterns(value any) error {
	return validateOperatorPatterns(value, "operators", 0)
}

func validateOperatorPatterns(value any, path string, depth int) error {
	if depth > 16 {
		return fmt.Errorf("operator nesting exceeds limit")
	}
	switch node := value.(type) {
	case []any:
		for i, child := range node {
			if err := validateOperatorPatterns(child, fmt.Sprintf("%s[%d]", path, i), depth+1); err != nil {
				return err
			}
		}
	case map[string]any:
		if raw, exists := node["pattern"]; exists {
			pattern, ok := raw.(string)
			if !ok {
				return fmt.Errorf("%s.pattern: expected text", path)
			}
			flags := ""
			if raw, exists := node["flags"]; exists {
				flags, ok = raw.(string)
				if !ok {
					return fmt.Errorf("%s.flags: expected text", path)
				}
			}
			if _, err := js.NewConfigRegExp(pattern, flags); err != nil {
				return fmt.Errorf("%s.pattern: invalid RE2 pattern or flags", path)
			}
		}
		for key, child := range node {
			if key == "rules" {
				switch rules := child.(type) {
				case string:
					if err := validateRegexRules(rules); err != nil {
						return fmt.Errorf("%s.rules: %w", path, err)
					}
				case []any:
					for i, rule := range rules {
						if text, ok := rule.(string); ok {
							if err := validateRegexRules(text); err != nil {
								return fmt.Errorf("%s.rules[%d]: %w", path, i, err)
							}
						}
					}
				}
			}
			if err := validateOperatorPatterns(child, path+"."+key, depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateRegexRules(text string) error {
	if len(text) > 64<<10 {
		return fmt.Errorf("regex_rules_too_large")
	}
	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if _, err := js.NewConfigRegExp(line, "i"); err != nil {
			return fmt.Errorf("line %d: invalid RE2 pattern", i+1)
		}
	}
	return nil
}

// ValidateTemplateModelPatterns also checks groups with no current proxy names.
func ValidateTemplateModelPatterns(model *js.Object) error {
	for i, group := range groupObjects(model) {
		filters, ok := group.Get("filters").(*js.Array)
		if !ok {
			continue
		}
		for j, filter := range filters.Items {
			pattern, ok := filter.(string)
			if !ok {
				return fmt.Errorf("template_group[%d].filter[%d]: expected text", i, j)
			}
			if _, err := js.NewConfigRegExp(pattern, "i"); err != nil {
				return fmt.Errorf("template_group[%d].filter[%d]: invalid RE2 pattern", i, j)
			}
		}
	}
	return nil
}
