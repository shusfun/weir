package subout

import (
	"net/netip"
	"strings"
)

// Match 是一个目标按订阅规则算出的走向。Maybe 是排在命中规则之前、中心判断不了的规则：
// 目标是域名时 IP 规则要看客户端解析的结果，GeoIP 要看客户端的数据库。
type Match struct {
	EntryID string      `json:"entry_id,omitempty"`
	Label   string      `json:"label"`
	Kind    string      `json:"kind"`
	Value   string      `json:"value,omitempty"`
	Policy  string      `json:"policy"`
	Maybe   []MaybeRule `json:"maybe,omitempty"`
}

type MaybeRule struct {
	EntryID string `json:"entry_id,omitempty"`
	Label   string `json:"label"`
	Kind    string `json:"kind"`
	Policy  string `json:"policy"`
}

// Match 按与渲染相同的顺序计算 target（已规范的小写域名或 IP）的走向：内网 → 规则 → 其余流量。names 与 items 一一对应。
func (p *Plan) Match(target string, names []string, items []Item) Match {
	addr, err := netip.ParseAddr(target)
	isIP := err == nil
	if isIP {
		addr = addr.Unmap()
		for _, c := range privateCIDRs {
			if netip.MustParsePrefix(c).Contains(addr) {
				return Match{Label: "内网地址", Kind: "ip_cidr", Value: c, Policy: "DIRECT"}
			}
		}
	}
	lay := p.layout(names, items)
	var maybe []MaybeRule
	for _, r := range lay.Rules {
		switch {
		case r.Kind == "geoip" || r.Kind == "ip_cidr" && !isIP:
			maybe = append(maybe, MaybeRule{EntryID: r.ID, Label: r.Label, Kind: r.Kind, Policy: r.Policy})
		default:
			if v, ok := matchRule(r, target, addr, isIP); ok {
				return Match{EntryID: r.ID, Label: r.Label, Kind: r.Kind, Value: v, Policy: r.Policy, Maybe: maybe}
			}
		}
	}
	return Match{Label: "其余流量", Kind: "final", Policy: lay.Final, Maybe: maybe}
}

func matchRule(r layoutRule, target string, addr netip.Addr, isIP bool) (string, bool) {
	for _, v := range r.Values {
		switch r.Kind {
		case "ip_cidr":
			if pre, err := netip.ParsePrefix(v); err == nil && isIP && pre.Contains(addr) {
				return v, true
			}
		case "domain":
			if !isIP && target == v {
				return v, true
			}
		case "domain_suffix":
			if !isIP && (target == v || strings.HasSuffix(target, "."+v)) {
				return v, true
			}
		case "domain_keyword":
			if !isIP && strings.Contains(target, v) {
				return v, true
			}
		}
	}
	return "", false
}
