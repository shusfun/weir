package subscription

import (
	"errors"
	"regexp"
	"strings"
)

// MiSub 4952c16 node-cleaner.js parseFilterRuleText/buildRuleSet semantics:
// before --- excludes, after --- and keep: include; protocol/name rules OR.
// RE2 and strict errors replace JS regex and silent invalid-regex skipping.
type sourceRuleSet struct {
	protocols map[string]bool
	name      *regexp.Regexp
}
type sourceFilter struct{ include, exclude sourceRuleSet }

func compileSourceFilter(text string) (sourceFilter, error) {
	f := sourceFilter{include: sourceRuleSet{protocols: map[string]bool{}}, exclude: sourceRuleSet{protocols: map[string]bool{}}}
	if len(text) > 16384 {
		return f, errors.New("source filter size limit")
	}
	lines := strings.Split(text, "\n")
	if len(lines) > 100 {
		return f, errors.New("source filter rule limit")
	}
	include := false
	patterns := map[bool][]string{}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if line == "---" {
			include = true
			continue
		}
		keep := include
		if strings.HasPrefix(strings.ToLower(line), "keep:") {
			keep = true
			line = strings.TrimSpace(line[5:])
		}
		if line == "" {
			continue
		}
		set := &f.exclude
		if keep {
			set = &f.include
		}
		if strings.HasPrefix(strings.ToLower(line), "proto:") {
			for _, p := range strings.Split(line[6:], ",") {
				p = strings.ToLower(strings.TrimSpace(p))
				if p == "" {
					continue
				}
				switch p {
				case "ss", "vmess", "vless", "trojan", "hy2", "hysteria2", "tuic", "anytls", "http", "https", "socks", "socks5", "socks5-tls":
				default:
					return f, errors.New("unsupported source filter protocol")
				}
				set.protocols[p] = true
			}
		} else {
			patterns[keep] = append(patterns[keep], line)
		}
	}
	for _, keep := range []bool{false, true} {
		if len(patterns[keep]) == 0 {
			continue
		}
		pattern, err := regexp.Compile("(?i)(" + strings.Join(patterns[keep], "|") + ")")
		if err != nil {
			return f, errors.New("invalid source filter regex")
		}
		if keep {
			f.include.name = pattern
		} else {
			f.exclude.name = pattern
		}
	}
	return f, nil
}

func ValidateSourceFilters(include, exclude string) error {
	for _, text := range []string{include, exclude} {
		if _, err := compileSourceFilter(text); err != nil {
			return err
		}
	}
	return nil
}

func FilterSourceLinks(links []string, include, exclude string) ([]string, error) {
	if len(links) > 10000 {
		return nil, errors.New("source filter node limit")
	}
	filters := []sourceFilter{}
	for _, text := range []string{include, exclude} {
		f, err := compileSourceFilter(text)
		if err != nil {
			return nil, err
		}
		filters = append(filters, f)
	}
	out := make([]string, 0, len(links))
	for _, link := range links {
		n, err := ParseLink(link)
		if err != nil {
			return nil, errors.New("invalid source filter node")
		}
		protocol, _, _ := strings.Cut(link, "://")
		keep := true
		for _, f := range filters {
			match := func(r sourceRuleSet) bool {
				return r.protocols[strings.ToLower(protocol)] || r.name != nil && r.name.MatchString(n.Remark)
			}
			if (len(f.include.protocols) > 0 || f.include.name != nil) && !match(f.include) || match(f.exclude) {
				keep = false
				break
			}
		}
		if keep {
			out = append(out, link)
		}
	}
	return out, nil
}
