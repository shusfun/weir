package center

import (
	"context"
	"encoding/json"
	"time"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/subout"
	"github.com/shusfun/weir/packages/contracts"
)

// 用户策略管理客户端分组、单独显示和全局清单；速度仅用于展示。
// 清单预设只提供快捷添加内容，不决定订阅走向。
type RuleCategory struct {
	Key     string   `json:"key"`
	Label   string   `json:"label"`
	Hint    string   `json:"hint"`
	Domains []string `json:"-"`
	CIDRs   []string `json:"-"`
	GeoIP   []string `json:"-"`
}

var ruleCategories = []RuleCategory{
	{Key: "ai", Label: "AI 服务", Hint: "ChatGPT、Claude、Gemini、Copilot 等", Domains: aiDomains()},
	{Key: "streaming", Label: "流媒体", Hint: "Netflix、YouTube、Disney+、Spotify 等", Domains: []string{
		"netflix.com", "netflix.net", "nflxvideo.net", "nflximg.net", "nflxext.com", "nflxso.net",
		"youtube.com", "googlevideo.com", "ytimg.com", "youtu.be", "youtube-nocookie.com",
		"disneyplus.com", "disney-plus.net", "dssott.com", "bamgrid.com",
		"hbomax.com", "max.com", "primevideo.com", "aiv-cdn.net", "spotify.com", "scdn.co", "twitch.tv", "ttvnw.net", "tiktok.com", "tiktokcdn.com"}},
	{Key: "google", Label: "Google", Hint: "搜索、Gmail、Drive 等 Google 服务", Domains: []string{
		"google.com", "googleapis.com", "gstatic.com", "googleusercontent.com", "ggpht.com", "gmail.com", "google.com.hk", "withgoogle.com", "android.com", "gvt1.com", "gvt2.com"}},
	{Key: "telegram", Label: "Telegram", Domains: []string{"telegram.org", "t.me", "telegram.me", "telesco.pe", "tdesktop.com"},
		CIDRs: []string{"91.108.4.0/22", "91.108.8.0/22", "91.108.12.0/22", "91.108.16.0/22", "91.108.56.0/22", "149.154.160.0/20", "2001:b28:f23d::/48", "2001:67c:4e8::/48"}},
	{Key: "cn", Label: "国内网站", Hint: "国内常用网站与国内 IP", Domains: []string{
		"cn", "baidu.com", "bdstatic.com", "qq.com", "gtimg.cn", "weixin.qq.com", "taobao.com", "tmall.com", "alipay.com", "alicdn.com", "aliyun.com",
		"jd.com", "360buyimg.com", "bilibili.com", "hdslb.com", "weibo.com", "sinaimg.cn", "163.com", "126.net", "douyin.com", "zhihu.com", "xiaohongshu.com", "meituan.com", "mi.com"},
		GeoIP: []string{"cn"}},
}

func aiDomains() []string {
	return []string{"openai.com", "chatgpt.com", "oaistatic.com", "oaiusercontent.com", "anthropic.com", "claude.ai", "claude.com", "claudeusercontent.com",
		"gemini.google.com", "aistudio.google.com", "generativelanguage.googleapis.com", "copilot.microsoft.com", "githubcopilot.com",
		"x.ai", "grok.com", "perplexity.ai", "mistral.ai", "poe.com", "huggingface.co", "openrouter.ai"}
}

// loadPolicy 读用户的代理设置（缺省为默认值）。
func loadPolicy(u *ent.User) contracts.ProxyPolicy {
	var p contracts.ProxyPolicy
	if len(u.ProxyPolicy) > 0 {
		b, _ := json.Marshal(u.ProxyPolicy)
		_ = json.Unmarshal(b, &p)
	}
	return migratePolicyJSON(p.Normalized(), u.ProxyPolicy)
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
	if state, why := s.nodeReadiness(ctx, n, g); state != "ready" {
		h.Why = why
		return h
	}
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
	p.Policy = migrateProxyTargets(p.Policy)
	items := make([]subout.Item, len(p.Entries))
	for i, e := range p.Entries {
		items[i] = subout.Item{Link: e.URI, Type: e.Type, Ref: e.Ref, Auto: p.Ranked[i].Auto, Direct: !e.Chained, UDP: e.UDP}
	}
	plan := &subout.Plan{Groups: p.Policy.Groups, HiddenLines: p.Policy.HiddenLines, IntervalSec: p.Policy.Auto.RefreshMinutes * 60, UpdateSec: p.Policy.Auto.RefreshMinutes * 60, TestURL: subout.SubscriptionProbeURL}
	available := make(map[string]bool, len(p.Entries))
	for _, e := range p.Entries {
		available[e.Ref] = true
	}
	for _, e := range p.Source {
		if !available[e.Ref] {
			plan.ExcludedNodes = append(plan.ExcludedNodes, subout.NodeExclusion{
				Ref: e.Ref, Name: e.Name, Protocol: e.Protocol,
				ReasonCode: "node_unavailable", Reason: "节点当前不可用或已在代理设置中停用",
			})
		}
	}
	custom := p.Policy.Custom
	for _, e := range custom.Entries {
		if e.Enabled {
			plan.Custom = append(plan.Custom, subout.Rule{ID: e.ID, Kind: e.Kind, Values: []string{e.Value}, Target: e.Target})
		}
	}
	for _, c := range ruleCategories {
		if c.Key != "cn" {
			continue
		}
		target := "direct"
		if len(c.Domains) > 0 {
			plan.Rules = append(plan.Rules, subout.Rule{Kind: "domain_suffix", Values: c.Domains, Target: target, Label: c.Label})
		}
		if len(c.CIDRs) > 0 {
			plan.Rules = append(plan.Rules, subout.Rule{Kind: "ip_cidr", Values: c.CIDRs, Target: target, Label: c.Label})
		}
		for _, g := range c.GeoIP {
			plan.Rules = append(plan.Rules, subout.Rule{Kind: "geoip", Values: []string{g}, Target: target, Label: c.Label})
		}
	}
	return plan, items
}
