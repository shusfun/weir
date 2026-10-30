package subout

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/shusfun/weir/packages/contracts"
)

// urlTestSec 是 url-test 组测延迟的间隔；与节点列表刷新（Plan.IntervalSec）无关。
const urlTestSec = 300

// RenderPlan 按计划输出带分组与分流的订阅；plan 为空时与 Render 相同。
func RenderPlan(items []Item, target string, plan *Plan) ([]byte, string, error) {
	if plan == nil {
		return Render(linksOf(items), target)
	}
	items = itemsForTarget(items, target)
	switch target {
	case "clash", "mihomo":
		b, err := clashPlan(items, plan)
		return b, "application/yaml; charset=utf-8", err
	case "singbox", "sing-box":
		b, err := singboxPlan(items, plan)
		return b, "application/json", err
	case "surge", "loon", "quanx":
		b, err := iniPlan(items, target, plan)
		return b, "text/plain; charset=utf-8", err
	case "egern":
		b, err := egernPlan(items, plan)
		return b, "application/yaml; charset=utf-8", err
	}
	// 通用链接列表（v2rayN 等）表达不了分组：按计划里的顺序给出节点
	return Render(linksOf(items), target)
}

// ClashProvider 输出 Clash proxy-providers 的一份节点列表：key 为 auto（自动选择）或某个类型。
func ClashProvider(items []Item, plan *Plan, key string) ([]byte, error) {
	var picked []string
	for _, it := range itemsForTarget(items, "clash") {
		if key == AutoProvider && it.Auto || it.Type == key {
			picked = append(picked, it.Link)
		}
	}
	if key == AutoProvider && len(picked) == 0 {
		picked = linksOf(items)
	}
	proxies, _, err := clashProxies(picked, map[string]bool{})
	if err != nil {
		return nil, err
	}
	return yaml.Marshal(map[string]any{"proxies": proxies})
}

func itemsForTarget(items []Item, target string) []Item {
	keep := map[string]bool{}
	for _, l := range forTarget(linksOf(items), target) {
		keep[l] = true
	}
	out := make([]Item, 0, len(items))
	for _, it := range items {
		if keep[it.Link] {
			out = append(out, it)
		}
	}
	return out
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
	case "domain_suffix":
		return "DOMAIN-SUFFIX," + value + "," + policy
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
	var groups []any
	if plan.Providers != nil {
		// 节点放进 proxy-providers：每个类型一份、自动选择一份；节点选择组直接列出各组，不再逐个列节点
		providers := map[string]any{}
		add := func(key string) {
			providers["p-"+key] = map[string]any{"type": "http", "url": plan.Providers(key), "interval": plan.interval(),
				"health-check": map[string]any{"enable": false}}
		}
		for _, g := range lay.Groups {
			switch {
			case g.Name == GroupSelect:
				var sel []string
				for _, m := range g.Members {
					if m == GroupAuto || m == "DIRECT" || isGroupName(lay, m) {
						sel = append(sel, m)
					}
				}
				groups = append(groups, map[string]any{"name": g.Name, "type": "select", "proxies": sel})
			case g.Name == GroupAuto:
				add(AutoProvider)
				groups = append(groups, map[string]any{"name": g.Name, "type": "url-test", "use": []string{"p-" + AutoProvider}, "url": plan.testURL(), "interval": urlTestSec, "tolerance": 50})
			default:
				add(g.Type)
				groups = append(groups, map[string]any{"name": g.Name, "type": "url-test", "use": []string{"p-" + g.Type}, "url": plan.testURL(), "interval": urlTestSec, "tolerance": 50})
			}
		}
		cfg["proxy-providers"] = providers
	} else {
		cfg["proxies"] = proxies
		for _, g := range lay.Groups {
			m := map[string]any{"name": g.Name, "type": g.Kind, "proxies": g.Members}
			if g.Kind == "url-test" {
				m["url"], m["interval"], m["tolerance"] = plan.testURL(), urlTestSec, 50
			}
			groups = append(groups, m)
		}
	}
	cfg["proxy-groups"] = groups
	rules := []string{}
	for _, c := range privateCIDRs {
		rules = append(rules, clashRule("ip_cidr", c, "DIRECT"))
	}
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
		if g.Kind == "url-test" {
			groups = append(groups, map[string]any{"type": "urltest", "tag": g.Name, "outbounds": members, "url": plan.testURL(), "interval": strconv.Itoa(urlTestSec) + "s", "tolerance": 50})
		} else {
			groups = append(groups, map[string]any{"type": "selector", "tag": g.Name, "outbounds": members, "default": members[0]})
		}
	}
	out = append(append(groups, out...), map[string]any{"type": "direct", "tag": "direct"})
	// sing-box 新版本已不支持内置的 GeoIP 库，规则集又要从外网下载（下载失败会让客户端启动失败），
	// 所以「国内直连」只按域名后缀匹配，GeoIP 规则在这里跳过。
	rules := []map[string]any{{"action": "sniff"}, {"ip_cidr": privateCIDRs, "outbound": "direct"}}
	for _, r := range lay.Rules {
		var rule map[string]any
		switch r.Kind {
		case "domain_suffix":
			rule = map[string]any{"domain_suffix": r.Values}
		case "ip_cidr":
			rule = map[string]any{"ip_cidr": r.Values}
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
		final = GroupSelect
	}
	cfg := map[string]any{"outbounds": out, "route": map[string]any{"rules": rules, "final": final, "auto_detect_interface": true}}
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
		return quanxPlan(lines, lay, plan), nil
	}
	var b strings.Builder
	if target == "surge" && plan.ManagedURL != "" {
		fmt.Fprintf(&b, "#!MANAGED-CONFIG %s interval=%d strict=false\n\n", plan.ManagedURL, max(plan.UpdateSec, 3600))
	}
	b.WriteString("[General]\n\n[Proxy]\n" + strings.Join(lines, "\n") + "\n\n[Proxy Group]\n")
	for _, g := range lay.Groups {
		if g.Kind == "url-test" {
			fmt.Fprintf(&b, "%s = url-test, %s, url=%s, interval=%d, tolerance=50\n", g.Name, strings.Join(g.Members, ", "), plan.testURL(), urlTestSec)
		} else {
			fmt.Fprintf(&b, "%s = select, %s\n", g.Name, strings.Join(g.Members, ", "))
		}
	}
	b.WriteString("\n[Rule]\n")
	for _, c := range privateCIDRs {
		fmt.Fprintf(&b, "IP-CIDR, %s, DIRECT, no-resolve\n", c)
	}
	for _, r := range lay.Rules {
		for _, v := range r.Values {
			switch r.Kind {
			case "domain_suffix":
				fmt.Fprintf(&b, "DOMAIN-SUFFIX, %s, %s\n", v, r.Policy)
			case "ip_cidr":
				fmt.Fprintf(&b, "IP-CIDR, %s, %s, no-resolve\n", v, r.Policy)
			case "geoip":
				fmt.Fprintf(&b, "GEOIP, %s, %s\n", strings.ToUpper(v), r.Policy)
			}
		}
	}
	fmt.Fprintf(&b, "FINAL, %s\n\n%s", lay.Final, strings.Join(sections, "\n\n"))
	return []byte(b.String()), nil
}

func quanxPlan(lines []string, lay layout, plan *Plan) []byte {
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
	b.WriteString("[server_local]\n" + strings.Join(lines, "\n") + "\n\n[policy]\n")
	for _, g := range lay.Groups {
		members := make([]string, 0, len(g.Members))
		for _, m := range g.Members {
			members = append(members, policy(m))
		}
		if g.Kind == "url-test" {
			fmt.Fprintf(&b, "url-latency-benchmark=%s, %s, check-interval=%d, tolerance=50\n", g.Name, strings.Join(members, ", "), urlTestSec)
		} else {
			fmt.Fprintf(&b, "static=%s, %s\n", g.Name, strings.Join(members, ", "))
		}
	}
	b.WriteString("\n[filter_local]\n")
	for _, c := range privateCIDRs {
		fmt.Fprintf(&b, "ip-cidr, %s, direct\n", c)
	}
	for _, r := range lay.Rules {
		for _, v := range r.Values {
			switch r.Kind {
			case "domain_suffix":
				fmt.Fprintf(&b, "host-suffix, %s, %s\n", v, policy(r.Policy))
			case "ip_cidr":
				fmt.Fprintf(&b, "ip-cidr, %s, %s\n", v, policy(r.Policy))
			case "geoip":
				fmt.Fprintf(&b, "geoip, %s, %s\n", strings.ToLower(v), policy(r.Policy))
			}
		}
	}
	fmt.Fprintf(&b, "final, %s\n", policy(lay.Final))
	return []byte(b.String())
}

// ---- Egern ----

func egernPlan(items []Item, plan *Plan) ([]byte, error) {
	proxies, names, err := egernNodes(linksOf(items), reservedNames(plan))
	if err != nil {
		return nil, err
	}
	lay := plan.layout(names, items)
	var groups []any
	for _, g := range lay.Groups {
		if g.Kind == "url-test" {
			groups = append(groups, map[string]any{"auto_test": map[string]any{"name": g.Name, "policies": g.Members, "interval": urlTestSec, "tolerance": 50}})
		} else {
			groups = append(groups, map[string]any{"select": map[string]any{"name": g.Name, "policies": g.Members}})
		}
	}
	rules := []any{}
	for _, c := range privateCIDRs {
		rules = append(rules, map[string]any{"ip_cidr": map[string]any{"match": c, "policy": "DIRECT", "no_resolve": true}})
	}
	for _, r := range lay.Rules {
		for _, v := range r.Values {
			switch r.Kind {
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
	return yaml.Marshal(map[string]any{"general": map[string]any{"dns_server": []string{"223.5.5.5", "1.1.1.1"}, "ipv6": false}, "proxies": proxies, "policy_groups": groups, "rules": rules})
}

// Layout 给 Weir 自己的格式用：按节点名排出各组与规则（names 与 items 一一对应，由调用方保证唯一）。
func (p *Plan) Layout(names []string, items []Item) ([]contracts.WeirGroup, []contracts.WeirRule) {
	lay := p.layout(names, items)
	groups := make([]contracts.WeirGroup, 0, len(lay.Groups))
	for _, g := range lay.Groups {
		groups = append(groups, contracts.WeirGroup{Name: g.Name, Kind: g.Kind, Members: g.Members})
	}
	rules := []contracts.WeirRule{{Kind: "ip_cidr", Values: privateCIDRs, Policy: "DIRECT"}}
	for _, r := range lay.Rules {
		rules = append(rules, contracts.WeirRule{Kind: r.Kind, Values: r.Values, Policy: r.Policy})
	}
	return groups, append(rules, contracts.WeirRule{Kind: "final", Policy: lay.Final})
}
