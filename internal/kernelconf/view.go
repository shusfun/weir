package kernelconf

import (
	"crypto/ecdh"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/shusfun/weir/internal/db"
	"github.com/shusfun/weir/packages/contracts"
)

// 模板变量：引擎把 GateConfig 整理成与内核无关的视图交给驱动模板。标签与统计名属于统计接口的约定
// （Gate 按它们读流量），由引擎按内核类别算好；凭据只在这里注入，模板只能引用，不能生成。
//
//	ib   入站：节点参数（键名与节点 settings 一致）+ tag、stats_tag、protocol / network / security（已小写）、
//	     clients（已启用的凭据）；Xray 另有 ss_password
//	c    凭据：id、stats_name、credential、password、route_user（Xray）、tuic_uuid、wg_public、wg_address（sing-box）
//	ob   出口：id、tag、protocol（已规范）、address、port、username、password、group（成员标签，非组为 null）、s（出口设置）
//	r    规则：outbound、balancer（指向组）、inbound、user、domain、ip、protocol、reject（sing-box）
//	stats 统计端口；groups 组（tag、members）；probe 组探测（url、interval）

// view 是渲染一个内核时的全部变量。
type view struct {
	Inbounds  []map[string]any
	Outbounds []map[string]any
	Routes    []map[string]any
	Groups    []map[string]any
}

func (v view) vars(stats int) map[string]any {
	return map[string]any{
		"inbounds":  toAny(v.Inbounds),
		"outbounds": toAny(v.Outbounds),
		"routes":    toAny(v.Routes),
		"groups":    toAny(v.Groups),
		"stats":     stats,
		"probe":     map[string]any{"url": contracts.GroupProbeURL, "interval": contracts.GroupProbeEvery},
	}
}

func toAny(list []map[string]any) []any {
	out := make([]any, len(list))
	for i, m := range list {
		out[i] = m
	}
	return out
}

// inboundMap 把入站字段按 JSON 键名展开，再加上派生字段。
func inboundMap(in db.Inbound) map[string]any {
	b, _ := json.Marshal(in)
	m := map[string]any{}
	_ = json.Unmarshal(b, &m)
	delete(m, "clients")
	m["protocol"] = strings.ToLower(strings.TrimSpace(in.Protocol))
	m["network"] = strings.ToLower(strings.TrimSpace(in.Network))
	m["security"] = strings.ToLower(strings.TrimSpace(in.Security))
	m["port"] = in.Port
	m["id"] = in.ID
	return m
}

func enabled(clients []db.Client) []db.Client {
	out := []db.Client{}
	for _, c := range clients {
		if c.Enabled {
			out = append(out, c)
		}
	}
	return out
}

func statsName(c db.Client) string {
	if s := strings.TrimSpace(c.StatsKey); s != "" {
		return s
	}
	return strings.TrimSpace(c.Email)
}

// routeUser 是 Xray 分流规则 user 字段：SOCKS / HTTP 入站按登录用户名，其他协议按凭据的统计名。
func routeUser(protocol string, c db.Client) string {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case "socks", "http":
		return strings.TrimSpace(c.CredentialIDValue())
	}
	return statsName(c)
}

// ssPassword 是 Xray 单用户 Shadowsocks 2022 的服务端密钥：由入站 UUID 派生（非 2022 方法直接用 UUID）。
func ssPassword(method, seed string) string {
	if !strings.HasPrefix(method, "2022-blake3") {
		return seed
	}
	size := 16
	if strings.Contains(method, "aes-256") {
		size = 32
	}
	seed = strings.TrimSpace(seed)
	if seed == "" {
		return seed
	}
	sum := sha256.Sum256([]byte(method + ":" + seed))
	return base64.StdEncoding.EncodeToString(sum[:size])
}

// tuicUUID：TUIC 要求 uuid 是 UUID 格式；凭据 ID 不是时按它稳定派生一个。
func tuicUUID(seed string) string {
	seed = strings.TrimSpace(seed)
	if db.IsUUID(seed) {
		return seed
	}
	sum := sha256.Sum256([]byte(seed))
	u := sum[:16]
	u[6] = (u[6] & 0x0f) | 0x40
	u[8] = (u[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", u[0:4], u[4:6], u[6:8], u[8:10], u[10:])
}

// wgPublic 由 WireGuard 私钥（标准 base64）算出公钥；无效时为空。
func wgPublic(priv string) string {
	raw, err := base64.StdEncoding.DecodeString(priv)
	if err != nil {
		return ""
	}
	key, err := ecdh.X25519().NewPrivateKey(raw)
	if err != nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(key.PublicKey().Bytes())
}

// outboundSettings 是出口设置的视图：按协议补齐默认（HTTPS 默认 TLS 与 http/1.1；Hysteria2 等必须 TLS）。
func outboundSettings(ob db.Outbound, kernel string) (map[string]any, error) {
	protocol := db.NormalizeOutboundProtocol(ob.Protocol)
	s := db.ParseOutboundSettings(ob.SettingsJSON)
	switch protocol {
	case "http", "https":
		https := protocol == "https"
		if kernel == db.CoreXray {
			https = ob.Protocol == "https"
		}
		if https && s.Security == "" {
			s.Security, s.TLS = "tls", true
		}
		if (s.Security == "tls" || s.TLS) && len(s.ALPN) == 0 {
			s.ALPN = []string{"http/1.1"}
		}
	case "hysteria2", "tuic", "shadowtls", "anytls":
		managed, err := db.ManagedOutboundSettings(ob)
		if err != nil {
			return nil, err
		}
		s = managed
		if !s.TLS {
			s.TLS, s.Security = true, "tls"
		}
	}
	security := strings.ToLower(strings.TrimSpace(s.Security))
	if security == "" {
		if s.Reality {
			security = "reality"
		} else if s.TLS {
			security = "tls"
		}
	}
	alpn := []any{}
	for _, a := range s.ALPN {
		alpn = append(alpn, a)
	}
	return map[string]any{
		"security": security, "network": strings.ToLower(strings.TrimSpace(s.Network)),
		"sni": s.SNI, "fingerprint": s.Fingerprint, "alpn": alpn, "public_key": s.PublicKey, "short_id": s.ShortID,
		"path": s.Path, "host": s.Host, "service_name": s.ServiceName, "allow_insecure": s.AllowInsecure,
		"trusted_certificate": s.TrustedCertificate, "spki": s.SPKI, "flow": strings.TrimSpace(s.Flow), "method": s.Method,
		"obfs": s.Obfs, "obfs_password": s.ObfsPassword, "congestion_control": s.CongestionControl, "zero_rtt": s.ZeroRTT,
		"shadowtls_version": s.ShadowTLSVersion,
	}, nil
}

// buildView 为一个内核整理视图。inbounds 只含这个内核的入站（sing-box 已去掉没有凭据的）。
func buildView(kernel string, inbounds []db.Inbound, outbounds []db.Outbound, rules []db.RoutingRule, groups []group) (view, error) {
	var v view
	groupOf := map[int64][]int64{}
	for _, g := range groups {
		groupOf[g.id] = g.members
	}
	for _, in := range inbounds {
		if !in.Enabled || db.InboundCore(in) != kernel {
			continue
		}
		m := inboundMap(in)
		m["tag"] = db.GeneratedInboundTag(in)
		m["stats_tag"] = statsTag(in)
		if kernel == db.CoreXray {
			method := in.SSMethod
			if method == "" {
				method = "2022-blake3-aes-128-gcm"
			}
			m["ss_method"], m["ss_password"] = method, ssPassword(method, in.UUID)
		}
		clients := []any{}
		for _, c := range enabled(in.Clients) {
			cm := map[string]any{"id": c.ID, "stats_name": statsName(c), "credential": c.CredentialIDValue(), "password": c.PasswordValue()}
			if kernel == db.CoreXray {
				cm["route_user"] = routeUser(in.Protocol, c)
			} else {
				cm["tuic_uuid"] = tuicUUID(c.CredentialIDValue())
				cm["wg_public"] = wgPublic(c.PasswordValue())
				cm["wg_address"] = contracts.WGClientAddress(c.ID)
			}
			clients = append(clients, cm)
		}
		m["clients"] = clients
		v.Inbounds = append(v.Inbounds, m)
	}
	removed := func(ob db.Outbound) bool {
		return kernel == db.CoreSingbox && db.NormalizeOutboundProtocol(ob.Protocol) == "dns"
	}
	for _, ob := range outbounds {
		if !ob.Enabled || !db.OutboundSupportsCore(ob, kernel) || removed(ob) {
			continue
		}
		if err := db.ValidateOutboundProfile(ob); err != nil {
			return v, err
		}
		s, err := outboundSettings(ob, kernel)
		if err != nil {
			return v, err
		}
		m := map[string]any{"id": ob.ID, "tag": db.GeneratedOutboundTag(kernel, ob.ID, ob.Tag), "protocol": db.NormalizeOutboundProtocol(ob.Protocol),
			"address": ob.Address, "port": ob.Port, "username": ob.Username, "password": ob.Password, "s": s, "group": nil}
		if members, ok := groupOf[ob.ID]; ok {
			m["group"] = toStrings(tags(kernel, members))
		}
		v.Outbounds = append(v.Outbounds, m)
	}
	for _, g := range groups {
		v.Groups = append(v.Groups, map[string]any{"tag": db.GeneratedOutboundTag(kernel, g.id, ""), "members": toStrings(tags(kernel, g.members))})
	}
	routes, err := routeViews(kernel, inbounds, outbounds, rules, groupOf)
	v.Routes = routes
	return v, err
}

func toStrings(list []string) []any {
	out := make([]any, len(list))
	for i, s := range list {
		out[i] = s
	}
	return out
}

func tags(kernel string, ids []int64) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = db.GeneratedOutboundTag(kernel, id, "")
	}
	return out
}

// routeViews 解析分流规则：指向的出口、入站与用户；引用不到的规则跳过（该入站或用户此刻不在这个内核上）。
func routeViews(kernel string, inbounds []db.Inbound, outbounds []db.Outbound, rules []db.RoutingRule, groupOf map[int64][]int64) ([]map[string]any, error) {
	if len(rules) == 0 {
		return nil, nil
	}
	byID, alias := map[int64]string{}, map[string]string{}
	clientUser := map[int64]string{}
	for _, in := range inbounds {
		if !in.Enabled || db.InboundCore(in) != kernel {
			continue
		}
		tag := db.GeneratedInboundTag(in)
		if kernel == db.CoreSingbox {
			tag = statsTag(in)
			alias[db.GeneratedInboundTag(in)] = tag
		} else {
			alias[tag] = tag
		}
		byID[in.ID] = tag
		if r := strings.TrimSpace(in.Remark); r != "" {
			alias[r] = tag
		}
		for _, c := range enabled(in.Clients) {
			clientUser[c.ID] = routeUser(in.Protocol, c)
		}
	}
	var out []map[string]any
	for _, rule := range rules {
		if !rule.Enabled || !db.RoutingRuleAppliesToCore(rule, inbounds, kernel) {
			continue
		}
		ob, ok := db.ResolveRuleOutbound(rule, outbounds)
		if !ok {
			return nil, fmt.Errorf("routing rule %d targets missing outbound profile", rule.ID)
		}
		if !db.OutboundSupportsCore(ob, kernel) {
			name := "xray"
			if kernel == db.CoreSingbox {
				name = "sing-box"
			}
			return nil, fmt.Errorf("routing rule %d targets outbound %q that does not support %s", rule.ID, ob.Tag, name)
		}
		r := map[string]any{"outbound": db.GeneratedOutboundTag(kernel, ob.ID, rule.OutboundTag), "balancer": false, "reject": false,
			"domain": rule.Domain, "ip": rule.IP, "protocol": rule.Protocol, "inbound": nil, "user": nil}
		if _, isGroup := groupOf[ob.ID]; isGroup {
			r["balancer"] = true
		}
		if kernel == db.CoreSingbox && db.NormalizeOutboundProtocol(ob.Protocol) == "dns" {
			return nil, fmt.Errorf("unsupported route action")
		}
		if rule.ClientID > 0 {
			if kernel == db.CoreSingbox {
				r["user"] = fmt.Sprintf("grant-%d", rule.ClientID)
			} else {
				u, ok := clientUser[rule.ClientID]
				if !ok || strings.TrimSpace(u) == "" {
					continue
				}
				r["user"] = u
			}
		}
		if rule.InboundID > 0 {
			tag, ok := byID[rule.InboundID]
			if !ok {
				continue
			}
			r["inbound"] = tag
		} else if t := strings.TrimSpace(rule.InboundTag); t != "" {
			tag, ok := alias[t]
			if !ok {
				continue
			}
			r["inbound"] = tag
		}
		out = append(out, r)
	}
	return out, nil
}

// statsTag 是 sing-box 入站的统计标签（Gate 按它读按入站的流量）；与改为驱动之前一致。
func statsTag(in db.Inbound) string {
	switch p := strings.ToLower(strings.TrimSpace(in.Protocol)); p {
	case "hysteria2":
		return fmt.Sprintf("hy2-inbound-%d", in.ID)
	case "tuic", "shadowtls":
		return fmt.Sprintf("%s-inbound-%d", p, in.ID)
	default:
		return fmt.Sprintf("inbound-%d-%s", in.ID, p)
	}
}
