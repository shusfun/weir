package control

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"regexp"
	"strings"

	"github.com/imzyb/MiGate/ent"
	"github.com/imzyb/MiGate/ent/resource"
	"gopkg.in/yaml.v3"
)

// Structured models follow MiSub's pinned unified template model. Node material
// always comes from the caller's authorized links, never from a saved template.
type outputTemplate struct {
	Name            string          `json:"name"`
	Target          string          `json:"target"`
	Groups          []outputGroup   `json:"groups"`
	Rules           []outputRule    `json:"rules"`
	DNS             []string        `json:"dns"`
	DNSBootstrap    []string        `json:"dns_bootstrap,omitempty"`
	RuleSets        []outputRuleSet `json:"rule_sets,omitempty"`
	AutoGroups      string          `json:"auto_groups,omitempty"`
	regionOverrides []regionOverride
}
type outputGroup struct {
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Members []string `json:"members"`
	Filter  string   `json:"filter,omitempty"`
}
type outputRule struct {
	Type   string `json:"type"`
	Value  string `json:"value,omitempty"`
	Policy string `json:"policy"`
}

func decodeOutputTemplate(data map[string]any) (outputTemplate, error) {
	var t outputTemplate
	b, err := json.Marshal(data)
	if err != nil || len(b) > 128<<10 {
		return t, fmt.Errorf("template size invalid")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err = d.Decode(&t); err != nil {
		return t, fmt.Errorf("unsupported template field")
	}
	if t.Target == "mihomo" {
		t.Target = "clash"
	}
	if t.AutoGroups != "" && t.AutoGroups != "region" && t.AutoGroups != "protocol" {
		return t, fmt.Errorf("unsupported automatic grouping")
	}
	if t.Target != "clash" && t.Target != "singbox" || len(t.Name) > 200 || len(t.Groups) == 0 || len(t.Groups) > 64 || len(t.Rules) == 0 || len(t.Rules) > 1000 || len(t.DNS) > 16 {
		return t, fmt.Errorf("template limits or target invalid")
	}
	names := map[string]bool{"DIRECT": true, "REJECT": true, "direct": true}
	for _, g := range t.Groups {
		if g.Name == "" || g.Name == "$nodes" || g.Name == "$auto" || strings.ContainsAny(g.Name, "\r\n,\t") || len(g.Name) > 200 || names[g.Name] || len(g.Members) == 0 || len(g.Members) > 256 || len(g.Filter) > 2048 {
			return t, fmt.Errorf("invalid group")
		}
		names[g.Name] = true
		if g.Type != "select" && g.Type != "url-test" && !(t.Target == "clash" && (g.Type == "fallback" || g.Type == "load-balance")) {
			return t, fmt.Errorf("unsupported group type")
		}
		if _, err = regexp.Compile(g.Filter); err != nil {
			return t, fmt.Errorf("invalid group filter")
		}
	}
	for _, g := range t.Groups {
		for _, m := range g.Members {
			if m == "$auto" {
				if t.AutoGroups == "" || g.Type != "select" {
					return t, fmt.Errorf("automatic group member requires preset and selector")
				}
				continue
			}
			if m != "$nodes" && !names[m] {
				return t, fmt.Errorf("unknown member")
			}
			if m == "direct" && t.Target == "clash" || m == "REJECT" && (g.Type != "select" || t.Target == "singbox") || (m == "DIRECT" || m == "direct") && g.Type != "select" {
				return t, fmt.Errorf("unsupported automatic group member")
			}
		}
	}
	// Cycles are invalid even if a core accepts the JSON syntax.
	visiting, done := map[string]bool{}, map[string]bool{}
	var visit func(string) error
	visit = func(name string) error {
		if done[name] {
			return nil
		}
		if visiting[name] {
			return fmt.Errorf("cyclic group")
		}
		visiting[name] = true
		for _, g := range t.Groups {
			if g.Name == name {
				for _, m := range g.Members {
					if m != "$nodes" && m != "$auto" {
						if err := visit(m); err != nil {
							return err
						}
					}
				}
			}
		}
		visiting[name] = false
		done[name] = true
		return nil
	}
	for _, g := range t.Groups {
		if err = visit(g.Name); err != nil {
			return t, err
		}
	}
	for i, r := range t.Rules {
		if !names[r.Policy] || r.Policy == "direct" && t.Target == "clash" {
			return t, fmt.Errorf("unknown rule policy")
		}
		switch r.Type {
		case "RULE-SET":
			found := false
			for _, set := range t.RuleSets {
				found = found || set.Name == r.Value
			}
			if !found {
				return t, fmt.Errorf("unknown rule set")
			}
		case "FINAL":
			if i != len(t.Rules)-1 || r.Value != "" || t.Target == "singbox" && r.Policy == "REJECT" {
				return t, fmt.Errorf("FINAL must be last")
			}
		case "DOMAIN", "DOMAIN-SUFFIX", "DOMAIN-KEYWORD":
			if r.Value == "" || len(r.Value) > 253 || strings.ContainsAny(r.Value, "\r\n, \t") {
				return t, fmt.Errorf("invalid domain rule")
			}
		case "IP-CIDR", "IP-CIDR6":
			prefix, e := netip.ParsePrefix(r.Value)
			if e != nil || (r.Type == "IP-CIDR6") != prefix.Addr().Is6() {
				return t, fmt.Errorf("invalid IP rule")
			}
		default:
			return t, fmt.Errorf("unsupported rule type")
		}
	}
	if t.Rules[len(t.Rules)-1].Type != "FINAL" {
		return t, fmt.Errorf("final policy required")
	}
	if err := validateTemplateDNS(t.DNS, t.DNSBootstrap); err != nil {
		return t, err
	}
	if err := validateOutputRuleSets(t); err != nil {
		return t, err
	}
	return t, nil
}

func renderOutputTemplate(links []string, t outputTemplate) ([]byte, error) {
	buckets, err := templateAutoBuckets(links, t)
	if err != nil {
		return nil, err
	}
	var cfg map[string]any
	var proxies []any
	if t.Target == "clash" {
		b, err := clashOutput(links)
		if err != nil {
			return nil, err
		}
		if err = yaml.Unmarshal(b, &cfg); err != nil {
			return nil, err
		}
		proxies = cfg["proxies"].([]any)
	} else {
		b, err := singboxOutput(links)
		if err != nil {
			return nil, err
		}
		if err = json.Unmarshal(b, &cfg); err != nil {
			return nil, err
		}
		// Preserve input order across modern endpoint and outbound collections.
		outbounds := cfg["outbounds"].([]any)
		byTag := map[string]any{}
		for _, raw := range outbounds[1:] {
			p := raw.(map[string]any)
			byTag[text(p["tag"])] = p
		}
		if endpoints, ok := cfg["endpoints"].([]any); ok {
			for _, raw := range endpoints {
				p := raw.(map[string]any)
				byTag[text(p["tag"])] = p
			}
		}
		for _, tag := range outbounds[0].(map[string]any)["outbounds"].([]any) {
			p, ok := byTag[text(tag)]
			if !ok {
				return nil, fmt.Errorf("missing node output")
			}
			proxies = append(proxies, p)
		}
	}
	// Re-assign names against all groups, including malicious/conflicting labels.
	used := map[string]bool{"DIRECT": true, "REJECT": true, "direct": true, "$nodes": true, "$auto": true}
	for _, g := range t.Groups {
		used[g.Name] = true
	}
	for _, bucket := range buckets {
		used[bucket.name] = true
	}
	nameKey := "tag"
	if t.Target == "clash" {
		nameKey = "name"
	}
	names := []string{}
	for _, raw := range proxies {
		p := raw.(map[string]any)
		name := uniqueNodeName(text(p[nameKey]), used)
		p[nameKey] = name
		names = append(names, name)
	}
	// Expand explicit references before rendering; preserve node and bucket order.
	expanded := make([]outputGroup, 0, len(t.Groups)+len(buckets))
	for _, g := range t.Groups {
		members := []string{}
		for _, m := range g.Members {
			if m == "$auto" {
				for _, b := range buckets {
					members = append(members, b.name)
				}
			} else {
				members = append(members, m)
			}
		}
		g.Members = members
		expanded = append(expanded, g)
	}
	for _, bucket := range buckets {
		members := []string{}
		for _, index := range bucket.nodes {
			if index >= len(names) {
				return nil, fmt.Errorf("automatic node mapping mismatch")
			}
			members = append(members, names[index])
		}
		expanded = append(expanded, outputGroup{Name: bucket.name, Type: "url-test", Members: members})
	}
	t.Groups = expanded
	groups := []any{}
	for _, g := range t.Groups {
		members := []string{}
		seen := map[string]bool{}
		re, _ := regexp.Compile(g.Filter)
		appendMember := func(name string) {
			if !seen[name] {
				members = append(members, name)
				seen[name] = true
			}
		}
		for _, m := range g.Members {
			if m == "$nodes" {
				for _, name := range names {
					if re.MatchString(name) {
						appendMember(name)
					}
				}
			} else {
				if t.Target == "singbox" && m == "DIRECT" {
					m = "direct"
				}
				appendMember(m)
			}
		}
		if len(members) == 0 {
			return nil, fmt.Errorf("group has no eligible nodes")
		}
		if t.Target == "clash" {
			p := map[string]any{"name": g.Name, "type": g.Type, "proxies": members}
			if g.Type != "select" {
				p["url"] = "https://www.gstatic.com/generate_204"
				p["interval"] = 300
			}
			groups = append(groups, p)
		} else {
			kind := "selector"
			if g.Type == "url-test" {
				kind = "urltest"
			}
			p := map[string]any{"tag": g.Name, "type": kind, "outbounds": members}
			if kind == "urltest" {
				p["url"] = "https://www.gstatic.com/generate_204"
				p["interval"] = "5m"
			}
			groups = append(groups, p)
		}
	}
	if t.Target == "clash" {
		cfg["proxies"] = proxies
		cfg["proxy-groups"] = groups
		if len(t.RuleSets) > 0 {
			cfg["rule-providers"] = clashRuleProviders(t.RuleSets)
		}
		rules := []string{}
		for _, r := range t.Rules {
			if r.Type == "FINAL" {
				rules = append(rules, "MATCH,"+r.Policy)
			} else {
				rules = append(rules, r.Type+","+r.Value+","+r.Policy)
			}
		}
		cfg["rules"] = rules
		if len(t.DNS) > 0 {
			dns := map[string]any{"enable": true, "nameserver": t.DNS}
			if len(t.DNSBootstrap) > 0 {
				dns["default-nameserver"] = t.DNSBootstrap
			}
			cfg["dns"] = dns
		}
		return yaml.Marshal(cfg)
	}
	all := append([]any{}, groups...)
	endpoints := []any{}
	for _, raw := range proxies {
		if raw.(map[string]any)["type"] == "wireguard" {
			endpoints = append(endpoints, raw)
		} else {
			all = append(all, raw)
		}
	}
	if len(endpoints) > 0 {
		cfg["endpoints"] = endpoints
	} else {
		delete(cfg, "endpoints")
	}
	all = append(all, map[string]any{"type": "direct", "tag": "direct"})
	cfg["outbounds"] = all
	rules := []any{}
	final := ""
	for _, r := range t.Rules {
		policy := r.Policy
		if policy == "DIRECT" {
			policy = "direct"
		}
		if r.Type == "FINAL" {
			if policy == "REJECT" {
				return nil, fmt.Errorf("reject final unsupported")
			}
			final = policy
			continue
		}
		key := map[string]string{"DOMAIN": "domain", "DOMAIN-SUFFIX": "domain_suffix", "DOMAIN-KEYWORD": "domain_keyword", "IP-CIDR": "ip_cidr", "IP-CIDR6": "ip_cidr", "RULE-SET": "rule_set"}[r.Type]
		p := map[string]any{key: []string{r.Value}}
		if policy == "REJECT" {
			p["action"] = "reject"
		} else {
			p["action"] = "route"
			p["outbound"] = policy
		}
		rules = append(rules, p)
	}
	route := map[string]any{"rules": rules, "final": final}
	if len(t.RuleSets) > 0 {
		route["rule_set"] = singboxRuleSets(t.RuleSets)
	}
	cfg["route"] = route
	if len(t.DNS) > 0 {
		servers := []any{}
		for i, address := range t.DNS {
			servers = append(servers, templateDNSServer(address, fmt.Sprintf("dns-%d", i), len(t.DNSBootstrap) > 0))
		}
		for i, address := range t.DNSBootstrap {
			servers = append(servers, map[string]any{"type": "udp", "tag": fmt.Sprintf("dns-bootstrap-%d", i), "server": address})
		}
		cfg["dns"] = map[string]any{"servers": servers, "final": "dns-0"}
		route["default_domain_resolver"] = "dns-0"
	}
	return json.MarshalIndent(cfg, "", "  ")
}

func (s *Server) profileTemplate(ctx context.Context, data map[string]any, target string) (*outputTemplate, error) {
	if target == "mihomo" {
		target = "clash"
	}
	var selected *outputTemplate
	if ids, ok := data["template_ids"].([]any); ok {
		for _, id := range ids {
			r, err := s.Store.Resource.Query().Where(resource.IDEQ(int(number(id))), resource.KindEQ("templates")).Only(ctx)
			if err != nil {
				return nil, fmt.Errorf("template missing")
			}
			t, err := decodeOutputTemplate(r.Data)
			if err != nil {
				return nil, err
			}
			if t.Target == target {
				if selected != nil {
					return nil, fmt.Errorf("ambiguous templates")
				}
				selected = &t
			}
		}
	}
	if selected != nil && selected.AutoGroups == "region" {
		n, e := s.readSubscriptionNaming(ctx)
		if e != nil {
			return nil, e
		}
		selected.regionOverrides = n.RegionOverrides
	}
	return selected, nil
}

func (s *Server) userOutputTemplate(ctx context.Context, userID int, target string) (*outputTemplate, error) {
	if target != "clash" && target != "mihomo" && target != "singbox" {
		return nil, nil
	}
	profiles, err := s.Store.Resource.Query().Where(resource.KindEQ("profiles")).Order(ent.Asc("id")).All(ctx)
	if err != nil {
		return nil, err
	}
	var selected *outputTemplate
	for _, p := range profiles {
		if p.Data["enabled"] == false {
			continue
		}
		assigned := false
		if ids, ok := p.Data["user_ids"].([]any); ok {
			for _, id := range ids {
				assigned = assigned || int(number(id)) == userID
			}
		}
		if !assigned {
			continue
		}
		t, err := s.profileTemplate(ctx, p.Data, target)
		if err != nil {
			return nil, err
		}
		if t != nil {
			if selected != nil {
				return nil, fmt.Errorf("ambiguous assigned templates")
			}
			selected = t
		}
	}
	return selected, nil
}
