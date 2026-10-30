package center

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/subout"
	"github.com/shusfun/weir/packages/contracts"
)

// 代理页：用户已授权的节点按「类型」（协议方案）归类。每个类型可以不放进订阅、可以加一个统一的出口；
// 网站按类别分到某一组（自动选择、节点选择、某个类型、直连、拒绝）；自动选择可以交给客户端测延迟，
// 也可以由中心按各节点自己统计的下载速率排序、只放排在前面的几个（见 rate_rank.go）。设置存在用户上，对他的全部链接生效。
// 分流与自动选择最终都写进订阅，由客户端执行：代理流量不经过中心，中心只能决定订阅里写什么。

// RuleCategory 是可以分流的网站类别；Default 是用户没设置时的目标。
type RuleCategory struct {
	Key     string   `json:"key"`
	Label   string   `json:"label"`
	Hint    string   `json:"hint"`
	Default string   `json:"default"`
	Domains []string `json:"-"`
	CIDRs   []string `json:"-"`
	GeoIP   []string `json:"-"`
}

// ruleCategories 的顺序就是匹配顺序：AI 与流媒体里有 Google 的域名（Gemini、YouTube），要排在 Google 前面。
// final 是其余全部流量，没有域名。
var ruleCategories = []RuleCategory{
	{Key: "ai", Label: "AI 服务", Hint: "ChatGPT、Claude、Gemini、Copilot 等", Default: "auto", Domains: aiDomains()},
	{Key: "streaming", Label: "流媒体", Hint: "Netflix、YouTube、Disney+、Spotify 等", Default: "auto", Domains: []string{
		"netflix.com", "netflix.net", "nflxvideo.net", "nflximg.net", "nflxext.com", "nflxso.net",
		"youtube.com", "googlevideo.com", "ytimg.com", "youtu.be", "youtube-nocookie.com",
		"disneyplus.com", "disney-plus.net", "dssott.com", "bamgrid.com",
		"hbomax.com", "max.com", "primevideo.com", "aiv-cdn.net", "spotify.com", "scdn.co", "twitch.tv", "ttvnw.net", "tiktok.com", "tiktokcdn.com"}},
	{Key: "google", Label: "Google", Hint: "搜索、Gmail、Drive 等 Google 服务", Default: "auto", Domains: []string{
		"google.com", "googleapis.com", "gstatic.com", "googleusercontent.com", "ggpht.com", "gmail.com", "google.com.hk", "withgoogle.com", "android.com", "gvt1.com", "gvt2.com"}},
	{Key: "telegram", Label: "Telegram", Default: "auto", Domains: []string{"telegram.org", "t.me", "telegram.me", "telesco.pe", "tdesktop.com"},
		CIDRs: []string{"91.108.4.0/22", "91.108.8.0/22", "91.108.12.0/22", "91.108.16.0/22", "91.108.56.0/22", "149.154.160.0/20", "2001:b28:f23d::/48", "2001:67c:4e8::/48"}},
	{Key: "cn", Label: "国内网站", Hint: "国内常用网站与国内 IP；sing-box 只按域名匹配", Default: "direct", Domains: []string{
		"cn", "baidu.com", "bdstatic.com", "qq.com", "gtimg.cn", "weixin.qq.com", "taobao.com", "tmall.com", "alipay.com", "alicdn.com", "aliyun.com",
		"jd.com", "360buyimg.com", "bilibili.com", "hdslb.com", "weibo.com", "sinaimg.cn", "163.com", "126.net", "douyin.com", "zhihu.com", "xiaohongshu.com", "meituan.com", "mi.com"},
		GeoIP: []string{"cn"}},
	{Key: "final", Label: "其余流量", Hint: "上面没有匹配到的全部流量", Default: "select"},
}

func aiDomains() []string {
	return []string{"openai.com", "chatgpt.com", "oaistatic.com", "oaiusercontent.com", "anthropic.com", "claude.ai", "claude.com", "claudeusercontent.com",
		"gemini.google.com", "aistudio.google.com", "generativelanguage.googleapis.com", "copilot.microsoft.com", "githubcopilot.com",
		"x.ai", "grok.com", "perplexity.ai", "mistral.ai", "poe.com", "huggingface.co", "openrouter.ai"}
}

func ruleCategory(key string) (RuleCategory, bool) {
	for _, c := range ruleCategories {
		if c.Key == key {
			return c, true
		}
	}
	return RuleCategory{}, false
}

// loadPolicy 读用户的代理设置（缺省为默认值）。
func loadPolicy(u *ent.User) contracts.ProxyPolicy {
	var p contracts.ProxyPolicy
	if len(u.ProxyPolicy) > 0 {
		b, _ := json.Marshal(u.ProxyPolicy)
		_ = json.Unmarshal(b, &p)
	}
	return p.Normalized()
}

func policyMap(p contracts.ProxyPolicy) map[string]any {
	var m map[string]any
	b, _ := json.Marshal(p)
	_ = json.Unmarshal(b, &m)
	return m
}

// ---- 类型 ----

// nodeType 是节点的类型：用建节点时的方案；没有方案（手填字段的旧节点）时按协议、安全层与传输归类。
type nodeType struct {
	Key, Label string
	Order      int
}

var protocolLabels = map[string]string{"vless": "VLESS", "vmess": "VMess", "trojan": "Trojan", "shadowsocks": "Shadowsocks", "hysteria2": "Hysteria2",
	"tuic": "TUIC", "wireguard": "WireGuard", "shadowtls": "ShadowTLS", "socks": "SOCKS5", "http": "HTTP"}

// typeIndex 在一次请求里缓存方案表。
type typeIndex map[string]Preset

func (s *Server) typeIndex(ctx context.Context) typeIndex {
	out := typeIndex{}
	for _, p := range s.presets(ctx) {
		out[p.ID] = p
	}
	return out
}

func (ti typeIndex) of(n *ent.Node) nodeType {
	if p, ok := ti[n.Preset]; ok && n.Preset != "" {
		return nodeType{Key: p.ID, Label: p.Name, Order: p.Order}
	}
	label := firstNonEmpty(protocolLabels[n.Protocol], n.Protocol)
	key := n.Protocol
	switch {
	case n.Security == "reality":
		label, key = label+" REALITY", key+"-reality"
	case n.Network == "ws":
		label, key = label+" WS", key+"-ws"
	case n.Security == "tls" && n.Protocol != "hysteria2" && n.Protocol != "tuic":
		label, key = label+" TLS", key+"-tls"
	}
	return nodeType{Key: key, Label: label, Order: 1000}
}

// ---- 节点状况与排序 ----

// nodeHealth：ok 可用；down 已知不可用（VPS 离线、配置应用失败、外部连不上、出口实测失败）；unknown 没有足够的观测。
// 只决定能不能用；排先后看速率（见 rate_rank.go）。
type nodeHealth struct {
	Status string `json:"status"`
	Why    string `json:"why,omitempty"`
	CPU    *float64
}

func hostCPU(g *ent.Gate) *float64 {
	if g == nil || g.Host == nil {
		return nil
	}
	if v, ok := g.Host["cpu_percent"].(float64); ok {
		return &v
	}
	return nil
}

func (s *Server) healthOf(ctx context.Context, n *ent.Node, g *ent.Gate) nodeHealth {
	switch {
	case g == nil || g.Status != "active":
		return nodeHealth{Status: "down", Why: "VPS 已撤销"}
	case g.LastSeenAt == nil || time.Since(*g.LastSeenAt) >= 90*time.Second:
		return nodeHealth{Status: "down", Why: "VPS 离线"}
	case g.FailedRevision >= g.ConfigRevision && g.ConfigRevision > 0:
		return nodeHealth{Status: "down", Why: "VPS 配置应用失败"}
	case !n.Enabled:
		return nodeHealth{Status: "down", Why: "节点已停用"}
	}
	h := nodeHealth{Status: "unknown", CPU: hostCPU(g)}
	r, ok := s.loadReach(ctx, n.ID)
	switch {
	case !ok:
		h.Why = "还没有外部连接结果"
	case r.Status == "failed":
		return nodeHealth{Status: "down", Why: r.Why}
	case r.Status == "ok":
		h.Status = "ok"
	default:
		h.Why = r.Why
	}
	return h
}

// subPlan 把代理设置转成订阅的输出计划。
func (s *Server) subPlan(ctx context.Context, p planned) (*subout.Plan, []subout.Item) {
	ti := s.typeIndex(ctx)
	seen := map[string]nodeType{}
	items := make([]subout.Item, len(p.Entries))
	for i, e := range p.Entries {
		items[i] = subout.Item{Link: e.URI, Type: e.Type, Auto: p.Ranked[i].Auto}
		if _, ok := seen[e.Type]; !ok {
			if n, err := s.Store.Ent.Node.Get(ctx, e.NodeID); err == nil {
				seen[e.Type] = ti.of(n)
			}
		}
	}
	types := make([]nodeType, 0, len(seen))
	for _, t := range seen {
		types = append(types, t)
	}
	sort.SliceStable(types, func(i, j int) bool {
		if types[i].Order != types[j].Order {
			return types[i].Order < types[j].Order
		}
		return types[i].Key < types[j].Key
	})
	plan := &subout.Plan{IntervalSec: p.Policy.Auto.RefreshMinutes * 60, UpdateSec: p.Policy.Auto.RefreshMinutes * 60}
	used := map[string]bool{subout.GroupSelect: true, subout.GroupAuto: true}
	for _, t := range types {
		label := subout.CleanLabel(t.Label)
		for n := 2; used[label]; n++ {
			label = subout.CleanLabel(t.Label) + " " + itoa(n)
		}
		used[label] = true
		plan.Types = append(plan.Types, subout.TypeGroup{Key: t.Key, Label: label})
	}
	for _, c := range ruleCategories {
		target := firstNonEmpty(p.Policy.Rules[c.Key], c.Default)
		if c.Key == "final" {
			plan.Final = target
			continue
		}
		if len(c.Domains) > 0 {
			plan.Rules = append(plan.Rules, subout.Rule{Kind: "domain_suffix", Values: c.Domains, Target: target})
		}
		if len(c.CIDRs) > 0 {
			plan.Rules = append(plan.Rules, subout.Rule{Kind: "ip_cidr", Values: c.CIDRs, Target: target})
		}
		for _, g := range c.GeoIP {
			plan.Rules = append(plan.Rules, subout.Rule{Kind: "geoip", Values: []string{g}, Target: target})
		}
	}
	return plan, items
}
