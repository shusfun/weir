package control

import (
	"fmt"
	"regexp"
	"strconv"
)

// MiSub's pinned template renderers map remote RULE-SET references to Clash
// rule-providers and sing-box remote rule sets. The center emits declarations;
// it does not fetch files or execute template contents.
type outputRuleSet struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	Format   string `json:"format"`
	Behavior string `json:"behavior,omitempty"`
	Interval int    `json:"interval,omitempty"`
}

var ruleSetName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

func validateOutputRuleSets(t outputTemplate) error {
	if len(t.RuleSets) > 64 {
		return fmt.Errorf("too many rule sets")
	}
	seen := map[string]bool{}
	for _, set := range t.RuleSets {
		if !ruleSetName.MatchString(set.Name) || seen[set.Name] || set.Interval != 0 && (set.Interval < 3600 || set.Interval > 604800) {
			return fmt.Errorf("invalid rule set name or interval")
		}
		seen[set.Name] = true
		u, err := parseTemplateDNS(set.URL)
		if err != nil || u.Scheme != "https" {
			return fmt.Errorf("rule set requires credential-free HTTPS URL")
		}
		if t.Target == "clash" {
			if set.Format != "yaml" && set.Format != "text" || set.Behavior != "domain" && set.Behavior != "ipcidr" && set.Behavior != "classical" {
				return fmt.Errorf("unsupported Clash rule set format or behavior")
			}
		} else if set.Format != "source" && set.Format != "binary" || set.Behavior != "" {
			return fmt.Errorf("unsupported sing-box rule set format or behavior")
		}
	}
	return nil
}

func ruleSetInterval(set outputRuleSet) int {
	if set.Interval == 0 {
		return 86400
	}
	return set.Interval
}

func clashRuleProviders(sets []outputRuleSet) map[string]any {
	out := map[string]any{}
	for _, set := range sets {
		ext := "yaml"
		if set.Format == "text" {
			ext = "txt"
		}
		out[set.Name] = map[string]any{"type": "http", "url": set.URL, "format": set.Format, "behavior": set.Behavior, "interval": ruleSetInterval(set), "path": "./rule-providers/" + set.Name + "." + ext}
	}
	return out
}

func singboxRuleSets(sets []outputRuleSet) []any {
	out := []any{}
	for _, set := range sets {
		out = append(out, map[string]any{"type": "remote", "tag": set.Name, "format": set.Format, "url": set.URL, "download_detour": "direct", "update_interval": strconv.Itoa(ruleSetInterval(set)) + "s"})
	}
	return out
}
