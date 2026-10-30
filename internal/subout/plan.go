package subout

import (
	"strings"
)

// Plan 是带分组与分流的订阅：每个节点属于一个类型（协议方案），按类型各成一组、另有「自动选择」与「节点选择」，
// 再按网站类别把流量分到某一组。没有 Plan 时各格式照旧只输出一个手动选择组。
// 类型与类别由中心定（代理页），这里只负责按各客户端的写法输出。
type Plan struct {
	Types       []TypeGroup // 显示顺序
	Rules       []Rule      // 按顺序匹配；之后补一条「其余流量 → Final」
	Final       string      // auto / select / direct / 类型 key
	TestURL     string
	IntervalSec int
	// Providers 不为空时 Clash 的节点放进 proxy-providers（按类型各一份、自动选择一份），
	// 客户端按 IntervalSec 只刷新节点列表，分组与规则不变。
	Providers func(key string) string
	// ManagedURL 不为空时 Surge 配置首行写 #!MANAGED-CONFIG，按 UpdateSec 自动更新整份配置。
	ManagedURL string
	UpdateSec  int
}

type TypeGroup struct{ Key, Label string }

// Item 是一个节点：分享链接、所属类型、是否放进「自动选择」组。
type Item struct {
	Link string
	Type string
	Auto bool
}

// Rule：Kind 是 domain_suffix / ip_cidr / geoip；Target 同 Plan.Final。
type Rule struct {
	Kind   string
	Values []string
	Target string
}

const (
	GroupSelect = "节点选择"
	GroupAuto   = "自动选择"
	// AutoProvider 是 Clash 里「自动选择」那份节点列表的 key（其余 key 为类型）。
	AutoProvider = "auto"
)

// CleanLabel 去掉各格式里有特殊含义的字符（INI 的逗号、等号，YAML 的井号等），用于组名。
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
	for i, l := range links {
		out[i] = Item{Link: l, Auto: true}
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

// group 是生成后的一组：Type 为空时是节点选择或自动选择。
type group struct {
	Name    string
	Kind    string // select / url-test
	Members []string
	Type    string
}

type layoutRule struct {
	Kind   string
	Values []string
	Policy string // 组名，或 DIRECT / REJECT
}

type layout struct {
	Groups []group
	Rules  []layoutRule
	Final  string
}

// reserved 是不能被节点名占用的名称：组名与内置策略。
func (p *Plan) reserved(used map[string]bool) {
	used[GroupSelect], used[GroupAuto], used["DIRECT"], used["REJECT"] = true, true, true, true
	for _, t := range p.Types {
		used[t.Label] = true
	}
}

// layout 按最终的节点名（与 items 一一对应）排出各组与规则。没有成员的类型不成组，指向它的规则改走节点选择。
func (p *Plan) layout(names []string, items []Item) layout {
	members := map[string][]string{}
	var auto []string
	for i, it := range items {
		members[it.Type] = append(members[it.Type], names[i])
		if it.Auto {
			auto = append(auto, names[i])
		}
	}
	if len(auto) == 0 {
		auto = append([]string(nil), names...)
	}
	labels := map[string]string{}
	var typed []group
	for _, t := range p.Types {
		if len(members[t.Key]) == 0 {
			continue
		}
		labels[t.Key] = t.Label
		typed = append(typed, group{Name: t.Label, Kind: "url-test", Members: members[t.Key], Type: t.Key})
	}
	sel := []string{}
	if len(auto) > 0 {
		sel = append(sel, GroupAuto)
	}
	for _, g := range typed {
		sel = append(sel, g.Name)
	}
	sel = append(append(sel, names...), "DIRECT")
	out := layout{Groups: []group{{Name: GroupSelect, Kind: "select", Members: sel}}}
	if len(auto) > 0 {
		out.Groups = append(out.Groups, group{Name: GroupAuto, Kind: "url-test", Members: auto})
	}
	out.Groups = append(out.Groups, typed...)
	policy := func(target string) string {
		switch target {
		case "auto":
			if len(auto) > 0 {
				return GroupAuto
			}
		case "direct":
			return "DIRECT"
		case "reject":
			return "REJECT"
		case "select", "":
		default:
			if l, ok := labels[target]; ok {
				return l
			}
		}
		return GroupSelect
	}
	for _, r := range p.Rules {
		if len(r.Values) == 0 {
			continue
		}
		out.Rules = append(out.Rules, layoutRule{Kind: r.Kind, Values: r.Values, Policy: policy(r.Target)})
	}
	out.Final = policy(p.Final)
	return out
}

func (p *Plan) testURL() string {
	if p.TestURL != "" {
		return p.TestURL
	}
	return "http://www.gstatic.com/generate_204"
}

func (p *Plan) interval() int {
	if p.IntervalSec > 0 {
		return p.IntervalSec
	}
	return 1800
}
