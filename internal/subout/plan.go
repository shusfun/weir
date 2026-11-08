package subout

import (
	"fmt"
	"slices"
	"strings"

	"github.com/shusfun/weir/packages/contracts"
)

// Plan 让全部格式、匹配预览与结构化订阅使用同一份分组和规则。
type Plan struct {
	Groups      []contracts.ProxyGroup
	HiddenLines []string
	Rules       []Rule
	TestURL     string
	IntervalSec int
	Providers   func(key string) string
	ManagedURL  string
	UpdateSec   int
	Custom      []Rule
}

const CustomRuleSetSec = 300
const CustomRuleSetName = "weir-custom"
const CustomLabel = "自定义清单"
const GroupAuto = "自动选择"
const GroupProxy = "PROXY"
const SingleProvider = "single"
const AutoProvider = "auto"
const AllProvider = "all"

// Ref 对应节点默认线路或用户自建链式线路；名称变化不改变引用。
type Item struct {
	Link   string
	Type   string
	Ref    string
	Auto   bool
	Direct bool
}

type Rule struct {
	ID     string
	Kind   string
	Values []string
	Target string
	Label  string
}

func CleanLabel(s string) string {
	s = strings.Map(func(r rune) rune {
		switch r {
		case ',', '=', ';', '#', '[', ']', '"', '\r', '\n', '\t':
			return ' '
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

func itemsOf(links []string) []Item {
	out := make([]Item, len(links))
	for i, link := range links {
		out[i] = Item{Link: link, Auto: true, Direct: true}
	}
	return out
}

func linksOf(items []Item) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Link
	}
	return out
}

type group struct {
	Ref, Name, Kind, Provider, TestURL string
	Members, MemberRefs                []string
	IntervalSec                        int
	Root                               bool
	Hidden                             bool
	ToleranceMS                        int
}

type layoutRule struct {
	ID     string
	Kind   string
	Values []string
	Policy string
	Label  string
}

type layout struct {
	Groups []group
	Rules  []layoutRule
	Final  string
}

func (p *Plan) reserved(used map[string]bool) {
	for _, name := range []string{GroupProxy, GroupAuto, "DIRECT", "REJECT", "direct", "block", "proxy", "reject"} {
		used[name] = true
	}
	for _, g := range p.Groups {
		used[CleanLabel(g.Name)] = true
	}
	for _, name := range p.automaticGroupNames(nil) {
		used[name] = true
	}
}

func (p *Plan) automaticGroupNames(nodes []string) map[int]string {
	used := map[string]bool{GroupProxy: true, GroupAuto: true, "DIRECT": true, "REJECT": true}
	for _, name := range nodes {
		used[name] = true
	}
	for _, g := range p.Groups {
		used[CleanLabel(g.Name)] = true
	}
	names := map[int]string{}
	for _, g := range p.Groups {
		if g.Kind == "select" {
			continue
		}
		suffix := " · 自动选择"
		if g.Kind == "fallback" {
			suffix = " · 主备切换"
		}
		base := CleanLabel(g.Name) + suffix
		name := base
		for i := 2; used[name]; i++ {
			name = fmt.Sprintf("%s (%d)", base, i)
		}
		used[name], names[g.ID] = true, name
	}
	return names
}

func (p *Plan) layout(names []string, items []Item) layout {
	refs := map[string]string{}
	var auto, autoRefs, direct, directRefs []string
	for i, item := range items {
		if item.Ref != "" {
			refs[item.Ref] = names[i]
		}
		if item.Direct {
			direct = append(direct, names[i])
			directRefs = append(directRefs, item.Ref)
		}
		if item.Direct && item.Auto {
			auto = append(auto, names[i])
			autoRefs = append(autoRefs, item.Ref)
		}
	}
	if len(auto) == 0 {
		auto, autoRefs = direct, directRefs
	}
	var custom, automatic []group
	automaticNames := p.automaticGroupNames(names)
	grouped := map[string]bool{}
	for _, g := range p.Groups {
		members, memberRefs := []string{}, []string{}
		seen := map[string]bool{}
		for _, ref := range g.Members {
			if name, ok := refs[ref]; ok && (strings.HasPrefix(ref, "node:") || strings.HasPrefix(ref, "chain:")) && !seen[ref] {
				members, memberRefs = append(members, name), append(memberRefs, ref)
				seen[ref] = true
				grouped[ref] = true
			}
		}
		if len(members) == 0 {
			continue
		}
		ref := fmt.Sprintf("group:%d", g.ID)
		name := CleanLabel(g.Name)
		provider := fmt.Sprintf("group-%d", g.ID)
		selection := group{Ref: ref, Name: name, Kind: "select", Members: members, MemberRefs: memberRefs, Provider: provider}
		if g.Kind != "select" {
			// 可见入口始终可手选节点，原自动策略作为默认成员保留。
			innerRef, innerName := ref+":auto", automaticNames[g.ID]
			automatic = append(automatic, group{Ref: innerRef, Name: innerName, Kind: g.Kind, Members: members, MemberRefs: memberRefs, Provider: provider, Hidden: true, TestURL: p.testURL(), IntervalSec: g.IntervalSeconds, ToleranceMS: g.ToleranceMS})
			selection.Members = append([]string{innerName}, members...)
			selection.MemberRefs = append([]string{innerRef}, memberRefs...)
		}
		custom = append(custom, selection)
	}
	policy := func(target string) string {
		if target == "direct" {
			return "DIRECT"
		}
		return GroupProxy
	}
	out := layout{Final: GroupProxy}
	root := group{Ref: "proxy", Name: GroupProxy, Kind: "select", Provider: SingleProvider, Root: true, Members: []string{}, MemberRefs: []string{}}
	if len(auto) > 0 {
		out.Groups = append(out.Groups, group{Ref: "auto", Name: GroupAuto, Kind: "url-test", Hidden: true, Members: auto, MemberRefs: autoRefs, Provider: AutoProvider, TestURL: p.testURL(), IntervalSec: 60, ToleranceMS: 50})
		root.Members, root.MemberRefs = append(root.Members, GroupAuto), append(root.MemberRefs, "auto")
	}
	out.Groups = append(out.Groups, custom...)
	out.Groups = append(out.Groups, automatic...)
	for _, g := range custom {
		root.Members, root.MemberRefs = append(root.Members, g.Name), append(root.MemberRefs, g.Ref)
	}
	for i, it := range items {
		if !grouped[it.Ref] && !slices.Contains(p.HiddenLines, it.Ref) {
			root.Members, root.MemberRefs = append(root.Members, names[i]), append(root.MemberRefs, it.Ref)
		}
	}
	out.Groups = append([]group{root}, out.Groups...)
	for _, r := range p.Custom {
		if len(r.Values) > 0 {
			out.Rules = append(out.Rules, layoutRule{ID: r.ID, Kind: r.Kind, Values: r.Values, Policy: policy(r.Target), Label: CustomLabel})
		}
	}
	out.Rules = append(out.Rules, layoutRule{Kind: "ip_cidr", Values: privateCIDRs, Policy: "DIRECT", Label: "局域网与本机"})
	for _, r := range p.Rules {
		if len(r.Values) > 0 {
			out.Rules = append(out.Rules, layoutRule{ID: r.ID, Kind: r.Kind, Values: r.Values, Policy: policy(r.Target), Label: r.Label})
		}
	}
	return out
}

func (p *Plan) testURL() string {
	if p.TestURL != "" {
		return p.TestURL
	}
	return contracts.GroupProbeURL
}
func (p *Plan) interval() int {
	if p.IntervalSec > 0 {
		return p.IntervalSec
	}
	return 1800
}
func (g group) testURL() string {
	if g.TestURL != "" {
		return g.TestURL
	}
	return contracts.GroupProbeURL
}
func (g group) interval() int {
	if g.IntervalSec > 0 {
		return g.IntervalSec
	}
	return 60
}
