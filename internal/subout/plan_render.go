package subout

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/shusfun/weir/internal/sub"
	"github.com/shusfun/weir/packages/contracts"
)

// urlTestSec 是 url-test 组测延迟的间隔；与节点列表刷新（Plan.IntervalSec）无关。
const urlTestSec = 60

// RenderPlan 按计划输出带分组与分流的订阅；plan 为空时与 Render 相同。
func RenderPlan(items []Item, target string, plan *Plan) ([]byte, string, error) {
	body, contentType, _, err := RenderPlanDetailed(items, target, plan)
	return body, contentType, err
}

// RenderPlanDetailed renders the compatible node subset and returns safe omission details.
func RenderPlanDetailed(items []Item, target string, plan *Plan) ([]byte, string, CompatibilityReport, error) {
	report := emptyReport(target)
	if !knownTarget(target) {
		return nil, "", report, fmt.Errorf("unsupported target %q", target)
	}
	items, report = compatibleItems(items, target)
	if plan != nil {
		report.ExcludedNodes = append(report.ExcludedNodes, plan.ExcludedNodes...)
	}
	filtered := FilterPlanForCompatibility(plan, &report)
	if len(items) == 0 {
		return nil, "", report, fmt.Errorf("no compatible nodes for %s", report.Target)
	}
	if plan == nil || target == "" || target == "base64" {
		b, contentType, err := renderRaw(linksOf(items), target)
		return b, contentType, report, err
	}
	names := make([]string, len(items))
	for i := range names {
		names[i] = fmt.Sprintf("node-%d", i)
	}
	if len(filtered.layout(names, items).Groups[0].Members) == 0 {
		return nil, "", report, fmt.Errorf("%s：PROXY 没有可输出成员", target)
	}
	switch target {
	case "clash", "mihomo":
		b, err := clashPlan(items, filtered)
		return b, "application/yaml; charset=utf-8", report, err
	case "singbox", "sing-box":
		b, err := singboxPlan(items, filtered)
		return b, "application/json", report, err
	case "surge", "loon", "quanx":
		b, err := iniPlan(items, target, filtered)
		return b, "text/plain; charset=utf-8", report, err
	case "egern":
		b, err := egernPlan(items, filtered)
		return b, "application/yaml; charset=utf-8", report, err
	}
	// 通用链接列表（v2rayN 等）表达不了分组：按计划里的顺序给出节点
	b, contentType, err := renderRaw(linksOf(items), target)
	return b, contentType, report, err
}

// ClashProvider 先为全部节点统一分配名称，再按用途截取，避免子集里的重名编号漂移。
func ClashProvider(items []Item, plan *Plan, key string) ([]byte, error) {
	var report CompatibilityReport
	items, report = compatibleItems(items, "clash")
	if plan != nil {
		report.ExcludedNodes = append(report.ExcludedNodes, plan.ExcludedNodes...)
	}
	plan = FilterPlanForCompatibility(plan, &report)
	if plan == nil {
		plan = &Plan{}
	}
	proxies, names, err := clashProxies(linksOf(items), reservedNames(plan))
	if err != nil {
		return nil, err
	}
	lay := plan.layout(names, items)
	if len(lay.Groups[0].Members) == 0 {
		return nil, fmt.Errorf("PROXY 没有可输出成员")
	}
	var wanted []string
	if key == AllProvider {
		wanted = names
	} else if key == SingleProvider {
		for _, name := range lay.Groups[0].Members {
			if slices.Contains(names, name) {
				wanted = append(wanted, name)
			}
		}
	} else {
		for _, g := range lay.Groups {
			if !g.Root && g.Provider == key {
				wanted = g.Members
				break
			}
		}
	}
	if len(wanted) == 0 {
		return nil, fmt.Errorf("节点列表 %s 没有可用成员", key)
	}
	picked := make([]map[string]any, 0, len(wanted))
	for _, name := range wanted {
		for _, proxy := range proxies {
			if proxy["name"] == name {
				picked = append(picked, proxy)
			}
		}
	}
	return yaml.Marshal(map[string]any{"proxies": picked})
}

func itemsForTarget(items []Item, target string) []Item {
	kept, _ := compatibleItems(items, target)
	return kept
}

func compatibleItems(items []Item, target string) ([]Item, CompatibilityReport) {
	report := emptyReport(target)
	kept := make([]Item, 0, len(items))
	for i, item := range items {
		if err := validateTargetLink(item.Link, target); err != nil {
			ref := item.Ref
			if ref == "" {
				ref = fmt.Sprintf("node:%d", i+1)
			}
			report.ExcludedNodes = append(report.ExcludedNodes, exclusionFor(item.Link, ref, err))
			continue
		}
		kept = append(kept, item)
	}
	report.IncludedNodes = len(kept)
	return kept, report
}

// FilterPlanForCompatibility removes references recorded in report from a plan and records their impacts.
func FilterPlanForCompatibility(plan *Plan, report *CompatibilityReport) *Plan {
	report.AffectedGroups = []GroupImpact{}
	report.AffectedRules = []RuleImpact{}
	if plan == nil {
		return nil
	}
	filtered := *plan
	if report.Target == "base64" || report.Target == "" {
		return &filtered
	}
	excluded := make(map[string]bool, len(report.ExcludedNodes))
	for _, node := range report.ExcludedNodes {
		excluded[node.Ref] = true
	}
	filtered.Groups = make([]contracts.ProxyGroup, 0, len(plan.Groups))
	for _, group := range plan.Groups {
		members := make([]string, 0, len(group.Members))
		removed := false
		for _, ref := range group.Members {
			if excluded[ref] {
				removed = true
				continue
			}
			members = append(members, ref)
		}
		if removed {
			removedGroup := len(members) == 0
			reason := "已移除不兼容节点"
			if removedGroup {
				reason = "组内没有可导入节点，已移除该组"
			}
			report.AffectedGroups = append(report.AffectedGroups, GroupImpact{ID: group.ID, Name: group.Name, Removed: removedGroup, Reason: reason})
		}
		if len(members) == 0 {
			excluded[fmt.Sprintf("group:%d", group.ID)] = true
			excluded[fmt.Sprintf("group:%d:auto", group.ID)] = true
			continue
		}
		group.Members = members
		filtered.Groups = append(filtered.Groups, group)
	}
	filtered.HiddenLines = make([]string, 0, len(plan.HiddenLines))
	for _, ref := range plan.HiddenLines {
		if !excluded[ref] {
			filtered.HiddenLines = append(filtered.HiddenLines, ref)
		}
	}
	filterRules := func(rules []Rule) []Rule {
		out := make([]Rule, 0, len(rules))
		for _, rule := range rules {
			if excluded[rule.Target] {
				name := rule.Label
				if name == "" {
					name = rule.ID
				}
				report.AffectedRules = append(report.AffectedRules, RuleImpact{ID: rule.ID, Name: name, Reason: "引用不可用节点或分组，已移除该规则"})
				continue
			}
			out = append(out, rule)
		}
		return out
	}
	filtered.Custom = filterRules(plan.Custom)
	filtered.Rules = filterRules(plan.Rules)
	return &filtered
}

func reservedNames(plan *Plan, extra ...string) map[string]bool {
	used := map[string]bool{}
	plan.reserved(used)
	for _, e := range extra {
		used[e] = true
	}
	return used
}

var privateCIDRs = []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "127.0.0.0/8", "100.64.0.0/10", "169.254.0.0/16"}

// ---- Clash / mihomo ----

func clashRule(kind, value, policy string) string {
	switch kind {
	case "domain":
		return "DOMAIN," + value + "," + policy
	case "domain_suffix":
		return "DOMAIN-SUFFIX," + value + "," + policy
	case "domain_keyword":
		return "DOMAIN-KEYWORD," + value + "," + policy
	case "ip_cidr":
		return "IP-CIDR," + value + "," + policy + ",no-resolve"
	case "geoip":
		return "GEOIP," + strings.ToUpper(value) + "," + policy
	}
	return ""
}

func clashPlan(items []Item, plan *Plan) ([]byte, error) {
	proxies, names, err := clashProxies(linksOf(items), reservedNames(plan))
	if err != nil {
		return nil, err
	}
	lay := plan.layout(names, items)
	cfg := map[string]any{"mixed-port": 7890, "mode": "rule"}
	if len(lay.Groups[0].Members) == 0 {
		return nil, fmt.Errorf("PROXY 没有可输出成员")
	}
	var groups []any
	providers := map[string]any{}
	for _, g := range lay.Groups {
		m := map[string]any{"name": g.Name, "type": g.Kind, "hidden": g.Hidden}
		if plan.Providers == nil {
			m["proxies"] = g.Members
		} else {
			var fixed, nodes []string
			for _, name := range g.Members {
				if isGroupName(lay, name) {
					fixed = append(fixed, name)
				} else {
					nodes = append(nodes, name)
				}
			}
			if len(fixed) > 0 {
				m["proxies"] = fixed
			}
			if len(nodes) > 0 {
				key := "p-" + g.Provider
				health := map[string]any{"enable": false}
				if g.Kind != "select" {
					health = map[string]any{"enable": true, "url": g.testURL(), "interval": g.interval(), "lazy": false}
				}
				// 可见选择组与内部自动组共用列表，健康检查由自动组决定。
				if providers[key] == nil || g.Kind != "select" {
					providers[key] = map[string]any{"type": "http", "url": plan.Providers(g.Provider), "interval": plan.interval(), "health-check": health}
				}
				m["use"] = []string{key}
			}
		}
		if g.Kind != "select" {
			m["url"], m["interval"], m["lazy"] = g.testURL(), g.interval(), false
			if g.Kind == "url-test" {
				m["tolerance"] = g.ToleranceMS
			}
		}
		groups = append(groups, m)
	}
	if plan.Providers == nil {
		cfg["proxies"] = proxies
	} else {
		cfg["proxy-providers"] = providers
	}
	cfg["proxy-groups"] = groups
	rules := []string{}
	for _, r := range lay.Rules {
		for _, v := range r.Values {
			if line := clashRule(r.Kind, v, r.Policy); line != "" {
				rules = append(rules, line)
			}
		}
	}
	cfg["rules"] = append(rules, "MATCH,"+lay.Final)
	return yaml.Marshal(cfg)
}

func isGroupName(lay layout, name string) bool {
	for _, g := range lay.Groups {
		if g.Name == name {
			return true
		}
	}
	return false
}

// ---- sing-box ----

func singboxPlan(items []Item, plan *Plan) ([]byte, error) {
	out, endpoints, names, err := singboxNodes(linksOf(items), reservedNames(plan, "direct", "block", "proxy"))
	if err != nil {
		return nil, err
	}
	lay := plan.layout(names, items)
	tag := func(policy string) string {
		if policy == "DIRECT" {
			return "direct"
		}
		return policy
	}
	var groups []map[string]any
	for _, g := range lay.Groups {
		members := make([]string, 0, len(g.Members))
		for _, m := range g.Members {
			members = append(members, tag(m))
		}
		if g.Kind == "url-test" || g.Kind == "fallback" {
			groups = append(groups, map[string]any{"type": "urltest", "tag": g.Name, "outbounds": members, "url": g.testURL(), "interval": strconv.Itoa(g.interval()) + "s", "idle_timeout": strconv.Itoa(max(1800, g.interval())) + "s", "tolerance": g.ToleranceMS})
		} else {
			groups = append(groups, map[string]any{"type": "selector", "tag": g.Name, "outbounds": members, "default": members[0]})
		}
	}
	out = append(append(groups, out...), map[string]any{"type": "direct", "tag": "direct"})
	rules := []map[string]any{{"action": "sniff"}}
	sets := []map[string]any{}
	for _, r := range lay.Rules {
		var rule map[string]any
		switch r.Kind {
		case "domain":
			rule = map[string]any{"domain": r.Values}
		case "domain_suffix":
			rule = map[string]any{"domain_suffix": r.Values}
		case "domain_keyword":
			rule = map[string]any{"domain_keyword": r.Values}
		case "ip_cidr":
			rule = map[string]any{"ip_cidr": r.Values}
		case "geoip":
			for _, country := range r.Values {
				if country != "cn" {
					return nil, fmt.Errorf("sing-box：不支持 GeoIP %s", country)
				}
			}
			rule = map[string]any{"rule_set": []string{"geoip-cn"}}
			if len(sets) == 0 {
				sets = append(sets, map[string]any{"type": "remote", "tag": "geoip-cn", "format": "binary", "url": sub.RemoteSources["geoip-cn"].Singbox, "download_detour": GroupProxy})
			}
		default:
			continue
		}
		if r.Policy == "REJECT" {
			rule["action"] = "reject"
		} else {
			rule["outbound"] = tag(r.Policy)
		}
		rules = append(rules, rule)
	}
	final := tag(lay.Final)
	if final == "REJECT" {
		final = GroupAuto
	}
	cfg := map[string]any{"outbounds": out, "route": map[string]any{"rules": rules, "final": final, "auto_detect_interface": true}}
	if len(sets) > 0 {
		cfg["route"].(map[string]any)["rule_set"] = sets
	}
	if len(endpoints) > 0 {
		cfg["endpoints"] = endpoints
	}
	return json.MarshalIndent(cfg, "", "  ")
}

// ---- Surge / Loon / QuanX ----

func iniPlan(items []Item, target string, plan *Plan) ([]byte, error) {
	lines, names, sections, err := iniNodes(linksOf(items), target, reservedNames(plan, "direct", "reject"))
	if err != nil {
		return nil, err
	}
	lay := plan.layout(names, items)
	if target == "quanx" {
		return quanxPlan(lines, lay, plan)
	}
	var b strings.Builder
	if target == "surge" && plan.ManagedURL != "" {
		fmt.Fprintf(&b, "#!MANAGED-CONFIG %s interval=%d strict=false\n\n", plan.ManagedURL, max(plan.UpdateSec, 3600))
	}
	b.WriteString("[General]\nproxy-test-url = " + plan.testURL() + "\n\n[Proxy]\n" + strings.Join(lines, "\n") + "\n\n[Proxy Group]\n")
	for _, g := range lay.Groups {
		fmt.Fprintf(&b, "%s = %s, %s", g.Name, g.Kind, strings.Join(g.Members, ", "))
		if g.Kind != "select" {
			if target != "surge" {
				fmt.Fprintf(&b, ", url=%s", g.testURL())
			}
			fmt.Fprintf(&b, ", interval=%d", g.interval())
			if g.Kind == "url-test" {
				fmt.Fprintf(&b, ", tolerance=%d", g.ToleranceMS)
			}
		}
		if target == "surge" && g.Hidden {
			b.WriteString(", hidden=true")
		}
		b.WriteByte('\n')
	}
	b.WriteString("\n[Rule]\n")
	for _, r := range lay.Rules {
		for _, v := range r.Values {
			switch r.Kind {
			case "domain":
				fmt.Fprintf(&b, "DOMAIN, %s, %s\n", v, r.Policy)
			case "domain_suffix":
				fmt.Fprintf(&b, "DOMAIN-SUFFIX, %s, %s\n", v, r.Policy)
			case "domain_keyword":
				fmt.Fprintf(&b, "DOMAIN-KEYWORD, %s, %s\n", v, r.Policy)
			case "ip_cidr":
				kw := "IP-CIDR"
				if strings.Contains(v, ":") {
					kw = "IP-CIDR6"
				}
				fmt.Fprintf(&b, "%s, %s, %s, no-resolve\n", kw, v, r.Policy)
			case "geoip":
				fmt.Fprintf(&b, "GEOIP, %s, %s\n", strings.ToUpper(v), r.Policy)
			}
		}
	}
	fmt.Fprintf(&b, "FINAL, %s\n\n%s", lay.Final, strings.Join(sections, "\n\n"))
	return []byte(b.String()), nil
}

func quanxPlan(lines []string, lay layout, plan *Plan) ([]byte, error) {
	probe, err := sharedProbeURL(lay, "QuanX")
	if err != nil {
		return nil, err
	}
	policy := func(p string) string {
		switch p {
		case "DIRECT":
			return "direct"
		case "REJECT":
			return "reject"
		}
		return p
	}
	var b strings.Builder
	b.WriteString("[general]\nserver_check_url = " + probe + "\n\n[server_local]\n" + strings.Join(lines, "\n") + "\n\n[policy]\n")
	for _, g := range lay.Groups {
		members := make([]string, 0, len(g.Members))
		for _, m := range g.Members {
			members = append(members, policy(m))
		}
		if g.Kind == "url-test" {
			fmt.Fprintf(&b, "url-latency-benchmark=%s, %s, check-interval=%d, tolerance=%d\n", g.Name, strings.Join(members, ", "), g.interval(), g.ToleranceMS)
		} else if g.Kind == "fallback" {
			fmt.Fprintf(&b, "available=%s, %s, check-interval=%d\n", g.Name, strings.Join(members, ", "), g.interval())
		} else {
			fmt.Fprintf(&b, "static=%s, %s\n", g.Name, strings.Join(members, ", "))
		}
	}
	b.WriteString("\n[filter_local]\n")
	for _, r := range lay.Rules {
		for _, v := range r.Values {
			switch r.Kind {
			case "domain":
				fmt.Fprintf(&b, "host, %s, %s\n", v, policy(r.Policy))
			case "domain_suffix":
				fmt.Fprintf(&b, "host-suffix, %s, %s\n", v, policy(r.Policy))
			case "domain_keyword":
				fmt.Fprintf(&b, "host-keyword, %s, %s\n", v, policy(r.Policy))
			case "ip_cidr":
				kw := "ip-cidr"
				if strings.Contains(v, ":") {
					kw = "ip6-cidr"
				}
				fmt.Fprintf(&b, "%s, %s, %s\n", kw, v, policy(r.Policy))
			case "geoip":
				fmt.Fprintf(&b, "geoip, %s, %s\n", strings.ToLower(v), policy(r.Policy))
			}
		}
	}
	fmt.Fprintf(&b, "final, %s\n", policy(lay.Final))
	return []byte(b.String()), nil
}

// ---- Egern ----

func egernPlan(items []Item, plan *Plan) ([]byte, error) {
	proxies, names, err := egernNodes(linksOf(items), reservedNames(plan))
	if err != nil {
		return nil, err
	}
	lay := plan.layout(names, items)
	probe, err := sharedProbeURL(lay, "Egern")
	if err != nil {
		return nil, err
	}
	var groups []any
	for _, g := range lay.Groups {
		kind := g.Kind
		if kind == "url-test" {
			kind = "auto_test"
		}
		body := map[string]any{"name": g.Name, "policies": g.Members, "hidden": g.Hidden}
		if kind != "select" {
			body["interval"] = g.interval()
			if kind == "auto_test" {
				body["tolerance"] = g.ToleranceMS
			}
		}
		groups = append(groups, map[string]any{kind: body})
	}
	rules := []any{}
	for _, r := range lay.Rules {
		for _, v := range r.Values {
			switch r.Kind {
			case "domain":
				rules = append(rules, map[string]any{"domain": map[string]any{"match": v, "policy": r.Policy}})
			case "domain_keyword":
				rules = append(rules, map[string]any{"domain_keyword": map[string]any{"match": v, "policy": r.Policy}})
			case "domain_suffix":
				rules = append(rules, map[string]any{"domain_suffix": map[string]any{"match": v, "policy": r.Policy}})
			case "ip_cidr":
				rules = append(rules, map[string]any{"ip_cidr": map[string]any{"match": v, "policy": r.Policy, "no_resolve": true}})
			case "geoip":
				rules = append(rules, map[string]any{"geoip": map[string]any{"match": strings.ToUpper(v), "policy": r.Policy}})
			}
		}
	}
	rules = append(rules, map[string]any{"default": map[string]any{"policy": lay.Final}})
	return yaml.Marshal(map[string]any{"general": map[string]any{"http_test_url": probe, "dns_server": []string{"223.5.5.5", "1.1.1.1"}, "ipv6": false}, "proxies": proxies, "policy_groups": groups, "rules": rules})
}

// Layout 给 Weir 自己的格式用：按节点名排出各组与规则（names 与 items 一一对应，由调用方保证唯一）。
func (p *Plan) Layout(names []string, items []Item) ([]contracts.WeirGroup, []contracts.WeirRule) {
	lay := p.layout(names, items)
	groups := make([]contracts.WeirGroup, 0, len(lay.Groups))
	for _, g := range lay.Groups {
		groups = append(groups, contracts.WeirGroup{Ref: g.Ref, Name: g.Name, Kind: g.Kind, Members: g.Members, MemberRefs: g.MemberRefs, ProbeURL: g.TestURL, IntervalSeconds: g.IntervalSec, Hidden: g.Hidden, ToleranceMS: g.ToleranceMS})
	}
	rules := []contracts.WeirRule{}
	for _, r := range lay.Rules {
		rules = append(rules, contracts.WeirRule{Kind: r.Kind, Values: r.Values, Policy: r.Policy})
	}
	return groups, append(rules, contracts.WeirRule{Kind: "final", Policy: lay.Final})
}

// 这两种格式只提供全局探测地址，不能把多个不同地址静默合并。
func sharedProbeURL(lay layout, format string) (string, error) {
	probe := ""
	for _, g := range lay.Groups {
		if g.Kind == "select" {
			continue
		}
		if probe != "" && probe != g.testURL() {
			return "", fmt.Errorf("%s：分组「%s」的探测地址与其他组不同，该格式只能设置一个探测地址", format, g.Name)
		}
		probe = g.testURL()
	}
	return probe, nil
}
