package center

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/credential"
	"github.com/shusfun/weir/internal/store/ent/link"
	"github.com/shusfun/weir/internal/subout"
	"github.com/shusfun/weir/packages/contracts"
)

func (s *Server) subRoutes(r *gin.Engine, ready *gin.RouterGroup) {
	r.GET("/sub/:token", s.serveSub)
	r.GET("/sub/:token/provider/:key", s.serveProvider)
	ready.GET("/sub-info/:token", s.subInfo)
}

// Entry 是订阅里的一个节点：自有节点或用户自建链式代理，各带本链接独立的凭据。
type Entry struct {
	Name     string `json:"name"`
	NodeID   int    `json:"node_id"`
	ChainID  int    `json:"chain_id,omitempty"`
	Protocol string `json:"protocol"`
	Type     string `json:"type"` // 代理页的类型（协议方案）
	Host     string `json:"host"`
	Port     int    `json:"port"`
	UUID     string `json:"uuid,omitempty"`
	Password string `json:"password,omitempty"`
	URI      string `json:"uri"`
	Error    string `json:"error,omitempty"`
}

// certPins 返回证书指纹（Clash / Hysteria2 链接用 DER 十六进制）与 SPKI 摘要（sing-box 用 base64）。
func certPins(certPEM string) (string, string) {
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		return "", ""
	}
	x, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", ""
	}
	der := sha256.Sum256(block.Bytes)
	spki := sha256.Sum256(x.RawSubjectPublicKeyInfo)
	return fmt.Sprintf("%x", der), base64.StdEncoding.EncodeToString(spki[:])
}

// shareLink 生成节点的标准分享链接；参数与入站配置一致，自签证书时附带指纹固定。
func shareLink(n *ent.Node, host string, cred *ent.Credential, name string, cert *ent.Certificate) (string, error) {
	if host == "" {
		return "", fmt.Errorf("VPS 没有公网地址")
	}
	st := n.Settings
	addr := net.JoinHostPort(host, strconv.Itoa(n.Port))
	q := url.Values{}
	frag := "#" + url.PathEscape(name)
	pin, spki := "", ""
	if cert != nil && cert.Source == "self_signed" {
		pin, spki = certPins(cert.CertPem)
	}
	transport := func() {
		q.Set("type", n.Network)
		if n.Network == "ws" {
			q.Set("path", nodeSetting(st, "ws_path"))
			if h := nodeSetting(st, "ws_host"); h != "" {
				q.Set("host", h)
			}
		}
		if n.Network == "grpc" {
			q.Set("serviceName", nodeSetting(st, "grpc_service_name"))
		}
		if n.Network == "xhttp" {
			q.Set("path", firstNonEmpty(nodeSetting(st, "xhttp_path"), "/"))
			q.Set("mode", firstNonEmpty(nodeSetting(st, "xhttp_mode"), "stream-one"))
		}
		switch n.Security {
		case "reality":
			q.Set("security", "reality")
			q.Set("pbk", nodeSetting(st, "reality_public_key"))
			q.Set("sid", firstOf(nodeSetting(st, "reality_short_id")))
			q.Set("sni", firstOf(nodeSetting(st, "reality_server_names")))
			q.Set("fp", firstNonEmpty(nodeSetting(st, "tls_fingerprint"), "chrome"))
		case "tls":
			q.Set("security", "tls")
			q.Set("sni", nodeSetting(st, "tls_sni"))
			if a := nodeSetting(st, "tls_alpn"); a != "" {
				q.Set("alpn", a)
			}
		default:
			q.Set("security", "none")
		}
	}
	switch n.Protocol {
	case "vless":
		transport()
		if n.Security == "reality" && n.Network == "tcp" {
			q.Set("flow", "xtls-rprx-vision")
		}
		q.Set("encryption", "none")
		return "vless://" + cred.UUID + "@" + addr + "?" + q.Encode() + frag, nil
	case "trojan":
		transport()
		return "trojan://" + url.PathEscape(cred.Password) + "@" + addr + "?" + q.Encode() + frag, nil
	case "vmess":
		doc := map[string]string{"v": "2", "ps": name, "add": host, "port": strconv.Itoa(n.Port), "id": cred.UUID, "aid": "0", "scy": "auto", "net": n.Network, "type": "none",
			"path": nodeSetting(st, "ws_path"), "host": nodeSetting(st, "ws_host"), "tls": "", "sni": "", "alpn": ""}
		if n.Security == "tls" {
			doc["tls"], doc["sni"], doc["alpn"] = "tls", nodeSetting(st, "tls_sni"), nodeSetting(st, "tls_alpn")
		}
		b, _ := json.Marshal(doc)
		return "vmess://" + base64.StdEncoding.EncodeToString(b), nil
	case "shadowsocks":
		secret := nodeSetting(st, "ss_password") + ":" + cred.Password
		return "ss://" + url.UserPassword(nodeSetting(st, "ss_method"), secret).String() + "@" + addr + frag, nil
	case "hysteria2":
		q.Set("sni", nodeSetting(st, "tls_sni"))
		if o := nodeSetting(st, "hy2_obfs"); o != "" {
			q.Set("obfs", o)
			q.Set("obfs-password", nodeSetting(st, "hy2_obfs_password"))
		}
		if pin != "" {
			q.Set("pinSHA256", pin)
			q.Set("spki", spki)
		}
		return "hysteria2://" + url.PathEscape(cred.Password) + "@" + addr + "?" + q.Encode() + frag, nil
	case "socks", "http":
		// 本地代理：用户名密码认证、不加密。
		scheme := "socks5"
		if n.Protocol == "http" {
			scheme = "http"
		}
		return (&url.URL{Scheme: scheme, User: url.UserPassword(cred.UUID, cred.Password), Host: addr, Fragment: name}).String(), nil
	case "shadowtls":
		// 没有通用分享链接：按 SIP003 写成带 shadow-tls 插件的 Shadowsocks 2022，Clash / sing-box 订阅据此生成。
		q.Set("plugin", "shadow-tls;host="+firstNonEmpty(nodeSetting(st, "tls_sni"), "www.microsoft.com")+";password="+cred.Password+";version=3")
		secret := nodeSetting(st, "ss_password") + ":" + cred.Password
		return "ss://" + url.UserPassword(firstNonEmpty(nodeSetting(st, "ss_method"), ss2022Method), secret).String() + "@" + addr + "?" + q.Encode() + frag, nil
	case "wireguard":
		pub := wgPublicKey(nodeSetting(st, "wg_private_key"))
		if pub == "" {
			return "", fmt.Errorf("WireGuard 节点缺少服务端密钥")
		}
		q.Set("publickey", pub)
		q.Set("address", contracts.WGClientAddress(int64(cred.ID)))
		if m := numberOf(st["wg_mtu"]); m > 0 {
			q.Set("mtu", strconv.Itoa(m))
		}
		return (&url.URL{Scheme: "wireguard", User: url.User(cred.Password), Host: addr, RawQuery: q.Encode(), Fragment: name}).String(), nil
	case "tuic":
		q.Set("sni", nodeSetting(st, "tls_sni"))
		q.Set("alpn", firstNonEmpty(nodeSetting(st, "tls_alpn"), "h3"))
		q.Set("congestion_control", firstNonEmpty(nodeSetting(st, "tuic_congestion_control"), "bbr"))
		if pin != "" {
			q.Set("pinSHA256", pin)
			q.Set("spki", spki)
		}
		return "tuic://" + cred.UUID + ":" + url.PathEscape(cred.Password) + "@" + addr + "?" + q.Encode() + frag, nil
	}
	return "", fmt.Errorf("协议 %s 暂不能生成分享链接", n.Protocol)
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

// linkEntries 列出某条链接在订阅里输出的节点（停用的节点、链式代理不输出）。
// 代理页的设置在这里生效：关掉的类型不输出；类型加了出口时，同一入口只输出经出口的那条，不再输出直连。
func (s *Server) linkEntries(ctx context.Context, u *ent.User, l *ent.Link) ([]Entry, error) {
	creds, err := s.Store.Ent.Credential.Query().Where(credential.LinkIDEQ(l.ID), credential.InternalEQ(false)).Order(ent.Asc(credential.FieldNodeID), ent.Asc(credential.FieldID)).All(ctx)
	if err != nil {
		return nil, err
	}
	pol := loadPolicy(u)
	disabled := map[string]bool{}
	for _, t := range pol.Disabled {
		disabled[t] = true
	}
	ti := s.typeIndex(ctx)
	viaExit := map[int]bool{} // 入口节点 → 有类型出口
	if u.AllowChain {
		for _, c := range creds {
			if c.ChainID == nil {
				continue
			}
			if ch, err := s.Store.Ent.Chain.Get(ctx, *c.ChainID); err == nil && ch.Enabled && ch.PolicyType != "" {
				viaExit[c.NodeID] = true
			}
		}
	}
	out := make([]Entry, 0, len(creds))
	for _, c := range creds {
		n, err := s.Store.Ent.Node.Get(ctx, c.NodeID)
		if err != nil || !n.Enabled {
			continue
		}
		typ := ti.of(n).Key
		if disabled[typ] || c.ChainID == nil && viaExit[c.NodeID] {
			continue
		}
		g, err := s.Store.Ent.Gate.Get(ctx, n.GateID)
		if err != nil || g.Status != "active" {
			continue
		}
		name := n.Name
		e := Entry{NodeID: n.ID, Protocol: n.Protocol, Type: typ, Host: g.PublicHost, Port: n.Port, UUID: c.UUID, Password: c.Password}
		if c.ChainID != nil {
			ch, err := s.Store.Ent.Chain.Get(ctx, *c.ChainID)
			if err != nil || !ch.Enabled || !u.AllowChain {
				continue
			}
			e.ChainID, name = ch.ID, ch.Name
		}
		var cert *ent.Certificate
		e.Name = name
		if n.CertID != nil {
			// 证书已过期或状态不明的节点不写进订阅（连接会失败，也不能显示为正常）。
			if cert, err = s.Store.Ent.Certificate.Get(ctx, *n.CertID); err != nil {
				e.Error = "证书状态未知"
				out = append(out, e)
				continue
			}
			if ok, why := certUsable(cert, time.Now()); !ok {
				e.Error = why
				out = append(out, e)
				continue
			}
		}
		if e.URI, err = shareLink(n, g.PublicHost, c, name, cert); err != nil {
			e.Error = err.Error()
		}
		out = append(out, e)
	}
	return out, nil
}

func (s *Server) linkByToken(ctx context.Context, token string) (*ent.Link, *ent.User, bool) {
	if len(token) < 16 || len(token) > 128 {
		return nil, nil, false
	}
	l, err := s.Store.Ent.Link.Query().Where(link.TokenEQ(token)).Only(ctx)
	if err != nil || !l.Enabled {
		return nil, nil, false
	}
	u, err := s.Store.Ent.User.Get(ctx, l.UserID)
	if err != nil || !u.Enabled {
		return nil, nil, false
	}
	return l, u, true
}

// disguised：订阅地址打开了伪装页开关。
func (s *Server) disguised(ctx context.Context) bool {
	g, err := s.subscriptionSettings(ctx)
	return err == nil && g.Disguise
}

// disguisePage 是浏览器访问订阅地址时看到的伪装页：普通的空站点，不出现 Weir、订阅或任何用量信息。
func disguisePage(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Data(200, "text/html; charset=utf-8", []byte(`<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Welcome</title>`+
		`<style>body{margin:0;min-height:100vh;display:grid;place-items:center;font:15px/1.6 system-ui,sans-serif;color:#555;background:#fafafa}</style></head>`+
		`<body><main><h1 style="font-weight:500;color:#333">It works.</h1><p>This site is under construction.</p></main></body></html>`))
	c.Abort()
}

func wantsBrowserPage(c *gin.Context) bool {
	ua := c.GetHeader("User-Agent")
	return c.Query("target") == "" && strings.Contains(c.GetHeader("Accept"), "text/html") &&
		strings.Contains(ua, "Mozilla") && subout.TargetForUA(ua) == "base64"
}

// serveSub 输出订阅：客户端按 User-Agent 自动选格式（?target= 可指定）；浏览器打开时显示一键导入页。
// 不在生效时间内（链接的开始 / 结束与用户到期取交集）时拿不到节点；超额由 Gate 执行，这里仍返回节点。
// 节点按代理页的设置分组、分流；target=weir 是 Weir 自己的结构化格式。每次拉取记一条，供「订阅管理」统计。
func (s *Server) serveSub(c *gin.Context) {
	if !s.Config.Installed || s.Store == nil {
		failure(c, 503, "setup_required")
		return
	}
	ctx := c.Request.Context()
	l, u, ok := s.linkByToken(ctx, c.Param("token"))
	if !ok {
		failure(c, 404, "subscription_not_found")
		return
	}
	if wantsBrowserPage(c) {
		if s.disguised(ctx) {
			disguisePage(c)
			return
		}
		s.spa(c)
		return
	}
	target := c.Query("target")
	if target == "" {
		target = subout.TargetForUA(c.GetHeader("User-Agent"))
	}
	if state := windowState(u, l, time.Now()); state != "active" {
		s.recordPull(c, l, target, state)
		failure(c, 403, "subscription_"+state)
		return
	}
	entries, err := s.linkEntries(ctx, u, l)
	if err != nil {
		s.recordPull(c, l, target, "failed")
		failInternal(c, 503, "subscription_unavailable", err)
		return
	}
	p := s.planEntries(ctx, u, entries, true)
	var body []byte
	contentType := "application/json"
	if target == "weir" {
		body, err = json.Marshal(s.weirSubscription(ctx, u, l, p))
	} else {
		plan, items := s.subPlan(ctx, p)
		origin := s.siteOrigin(c)
		switch target {
		case "clash", "mihomo":
			plan.Providers = func(key string) string { return origin + "/sub/" + l.Token + "/provider/" + url.PathEscape(key) }
		case "surge":
			plan.ManagedURL = origin + "/sub/" + l.Token + "?target=surge"
		}
		body, contentType, err = subout.RenderPlan(items, target, plan)
	}
	if err != nil {
		s.recordPull(c, l, target, "failed")
		failure(c, 422, "format_conversion_failed")
		return
	}
	_, end := linkWindow(u, l)
	expire := int64(0)
	if end != nil {
		expire = end.Unix()
	}
	c.Header("Subscription-Userinfo", fmt.Sprintf("upload=0; download=%d; total=%d; expire=%d", u.Used, u.Quota, expire))
	// 整份配置按小时更新（客户端只认小时）；节点列表另按代理页的刷新间隔经 proxy-providers 更新
	c.Header("Profile-Update-Interval", strconv.Itoa(max(1, (p.Policy.Auto.RefreshMinutes+59)/60)))
	c.Header("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape("Weir-"+firstNonEmpty(l.Name, "订阅")))
	_ = l.Update().SetLastUsedAt(time.Now()).Exec(ctx)
	s.recordPull(c, l, target, "ok")
	s.recordSubCheck("link-"+itoa(l.ID), firstNonEmpty(l.Name, "订阅")+" · "+u.Email, target, body)
	c.Data(200, contentType, body)
}

// serveProvider 是 Clash proxy-providers 的一份节点列表（key 为 auto 或某个类型），客户端按代理页的刷新间隔拉取。
// 生效时间与主订阅相同；不单独计一次拉取（刷新频繁，统计里只看整份配置的拉取）。
func (s *Server) serveProvider(c *gin.Context) {
	if !s.Config.Installed || s.Store == nil {
		failure(c, 503, "setup_required")
		return
	}
	ctx := c.Request.Context()
	l, u, ok := s.linkByToken(ctx, c.Param("token"))
	if !ok {
		failure(c, 404, "subscription_not_found")
		return
	}
	if state := windowState(u, l, time.Now()); state != "active" {
		failure(c, 403, "subscription_"+state)
		return
	}
	entries, err := s.linkEntries(ctx, u, l)
	if err != nil {
		failInternal(c, 503, "subscription_unavailable", err)
		return
	}
	plan, items := s.subPlan(ctx, s.planEntries(ctx, u, entries, false))
	key := c.Param("key")
	found := key == subout.AutoProvider
	for _, t := range plan.Types {
		found = found || t.Key == key
	}
	if !found {
		// 类型已不存在时返回 404：客户端保留上一次的列表，而不是换成空列表
		failure(c, 404, "provider_not_found")
		return
	}
	body, err := subout.ClashProvider(items, plan, key)
	if err != nil {
		failure(c, 422, "format_conversion_failed")
		return
	}
	c.Data(200, "application/yaml; charset=utf-8", body)
}

// weirSubscription 是 target=weir 的内容：节点仍是分享链接，带名次、状况与分组、规则。
func (s *Server) weirSubscription(ctx context.Context, u *ent.User, l *ent.Link, p planned) contracts.WeirSubscription {
	plan, items := s.subPlan(ctx, p)
	start, end := linkWindow(u, l)
	out := contracts.WeirSubscription{Version: contracts.WeirSubscriptionVersion, Name: firstNonEmpty(l.Name, "订阅"), Generated: time.Now().UTC(),
		Usage: contracts.WeirUsage{Used: u.Used, Quota: u.Quota}, Window: contracts.WeirWindow{StartsAt: start, EndsAt: end},
		Refresh: p.Policy.Auto.RefreshMinutes, Auto: p.Policy.Auto, Types: map[string]string{}}
	used := map[string]bool{subout.GroupSelect: true, subout.GroupAuto: true, "DIRECT": true, "REJECT": true}
	for _, t := range plan.Types {
		used[t.Label] = true
		out.Types[t.Key] = t.Label
	}
	names := make([]string, len(items))
	for i, e := range p.Entries {
		name := e.Name
		for n := 2; used[name]; n++ {
			name = fmt.Sprintf("%s (%d)", e.Name, n)
		}
		used[name], names[i] = true, name
		r := p.Ranked[i]
		health := map[string]string{"ok": "ok", "down": "degraded"}[r.Health.Status]
		node := contracts.WeirNode{Name: name, Type: e.Type, URI: e.URI, Rank: r.Rank, Health: firstNonEmpty(health, "unknown"), Auto: r.Auto, Exploring: r.Exploring}
		if r.Rate.known() {
			node.RateBps, node.RateMinutes, node.RateFrom = int64(r.Rate.P90), r.Rate.Minutes, r.Rate.From
		}
		out.Nodes = append(out.Nodes, node)
	}
	out.Groups, out.Rules = plan.Layout(names, items)
	return out
}

// subInfo 供浏览器订阅页使用：只返回本链接的用量、到期与节点名称，不含凭据。
func (s *Server) subInfo(c *gin.Context) {
	ctx := c.Request.Context()
	// 伪装页打开时，匿名的订阅信息接口与不存在的订阅一样返回 404；登录后在「我的订阅」查看。
	if s.disguised(ctx) {
		failure(c, 404, "subscription_not_found")
		return
	}
	l, u, ok := s.linkByToken(ctx, c.Param("token"))
	if !ok {
		failure(c, 404, "subscription_not_found")
		return
	}
	entries, err := s.linkEntries(ctx, u, l)
	if err != nil {
		failInternal(c, 503, "subscription_unavailable", err)
		return
	}
	nodes := make([]gin.H, 0, len(entries))
	for _, e := range entries {
		nodes = append(nodes, gin.H{"name": e.Name, "protocol": e.Protocol, "chain": e.ChainID != 0, "available": e.URI != ""})
	}
	start, end := linkWindow(u, l)
	c.JSON(200, gin.H{"name": l.Name, "quota": u.Quota, "used": u.Used, "expires_at": end, "starts_at": start, "state": windowState(u, l, time.Now()), "next_reset": resetHint(u),
		"formats": subout.Formats, "url": s.siteOrigin(c) + "/sub/" + l.Token, "nodes": nodes})
}
