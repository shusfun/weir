package center

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/shusfun/weir/internal/db"
	"github.com/shusfun/weir/internal/kernelconf"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/subout"
	"github.com/shusfun/weir/internal/subscription"
	"github.com/shusfun/weir/packages/contracts"
)

// 出口的统一表示：链式代理（管理员第 ③ 步、用户自建）与分流出口都先解析成 resolvedExit，
// 再按入口内核检查能不能连、由入口 Gate 实测、编译进 Gate 配置。
// 每个成员同时保留一条分享链接，实测时据此生成 sing-box 客户端。

// proxyKinds 是填地址的代理出口；link 是单条分享链接。
var proxyKinds = map[string]string{"socks5": "socks", "http": "http", "https": "https"}

type resolvedExit struct {
	Outbound contracts.Outbound // 单个出口，或 Protocol 为 group 的一组
	Names    []string           // 成员名称（订阅源节点）
	Links    []string           // 每个成员的分享链接，与成员一一对应
	Skipped  []string           // 订阅源里匹配但不能作出口的节点及原因
	TCPOnly  bool               // 只能测端口（另一台 VPS 的节点：内部凭据保存后才生成）
}

func (r resolvedExit) members() []contracts.Outbound {
	if r.Outbound.Protocol == "group" {
		return r.Outbound.Members
	}
	return []contracts.Outbound{r.Outbound}
}

// validHostPort 校验代理地址：主机名或 IP（IPv6 可带方括号），端口 1–65535。
func validHostPort(host string, port int) bool {
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if host == "" || len(host) > 255 || strings.ContainsAny(host, "/ @?#") || port < 1 || port > 65535 {
		return false
	}
	return net.ParseIP(host) != nil || strings.Contains(host, ".") && validHost(host)
}

// proxyExit 生成 SOCKS5 / HTTP / HTTPS 代理出口（HTTPS 是经 TLS 连接的 HTTP 代理，证书按地址校验）。
func proxyExit(kind, address string, port int, username, password string) contracts.Outbound {
	o := contracts.Outbound{Protocol: proxyKinds[kind], Address: strings.Trim(strings.TrimSpace(address), "[]"), Port: port, Username: username, Password: password}
	if kind == "https" {
		o.Settings = map[string]any{"security": "tls", "tls": true, "sni": o.Address}
	}
	return o
}

// proxyLink 是代理出口对应的分享链接（实测时生成客户端用）。
func proxyLink(kind, address string, port int, username, password string) string {
	scheme := map[string]string{"socks5": "socks5", "http": "http", "https": "https"}[kind]
	u := url.URL{Scheme: scheme, Host: net.JoinHostPort(strings.Trim(strings.TrimSpace(address), "[]"), strconv.Itoa(port))}
	if username != "" || password != "" {
		u.User = url.UserPassword(username, password)
	}
	return u.String()
}

// subscriptionURL 判断粘贴的是不是订阅地址：http(s) 地址带路径或查询参数（代理地址只有主机和端口）。
func subscriptionURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	return (u.Path != "" && u.Path != "/") || u.RawQuery != ""
}

// linkOutbound 把一条分享链接转成出口；表达不了的参数明确报错，不悄悄丢掉。
func linkOutbound(n subscription.ParsedLink) (contracts.Outbound, error) {
	var s subscription.Settings
	if n.SettingsJSON != "" {
		if err := json.Unmarshal([]byte(n.SettingsJSON), &s); err != nil {
			return contracts.Outbound{}, errors.New("节点参数无法解析")
		}
	}
	switch {
	case len(s.UnsupportedFields) > 0:
		return contracts.Outbound{}, fmt.Errorf("含有出口不支持的参数 %s", strings.Join(s.UnsupportedFields, "、"))
	case s.ShadowTLS != nil:
		return contracts.Outbound{}, errors.New("ShadowTLS 节点不能作出口")
	case s.Snell != nil || s.WireGuard != nil || n.Protocol == "snell" || n.Protocol == "wireguard":
		return contracts.Outbound{}, fmt.Errorf("%s 节点不能作出口", n.Protocol)
	case s.VmessUnsupported:
		return contracts.Outbound{}, errors.New("VMess 参数不受支持（如 alterId 不为 0）")
	case s.AllowInsecure:
		return contracts.Outbound{}, errors.New("这个节点要求跳过证书校验，出口不跳过证书校验")
	case s.CertificatePin != "" || s.SPKI != "":
		return contracts.Outbound{}, errors.New("这个节点用证书指纹代替证书校验，出口不支持")
	}
	o := contracts.Outbound{Address: n.Address, Port: n.Port, Username: n.Username, Password: n.Password}
	switch n.Protocol {
	case "vless", "vmess", "trojan", "shadowsocks", "hysteria2", "tuic", "anytls":
		o.Protocol = n.Protocol
	case "socks5":
		o.Protocol = "socks"
	case "http":
		o.Protocol = "http"
		if s.TLS || s.Security == "tls" {
			o.Protocol = "https"
		}
	default:
		return contracts.Outbound{}, fmt.Errorf("%s 节点不能作出口", n.Protocol)
	}
	if o.Protocol == "shadowsocks" {
		o.Username, s.Method = strings.TrimSpace(s.Method), ""
		if o.Username == "" {
			o.Username = strings.TrimSpace(n.Username)
		}
	}
	set := db.OutboundSettings{Obfs: s.Obfs, ObfsPassword: s.ObfsPassword, CongestionControl: s.CongestionControl, ZeroRTT: s.ZeroRTT,
		Security: s.Security, Network: s.Network, TLS: s.TLS, Reality: s.Reality, SNI: s.SNI, Host: s.Host, Path: s.Path, Flow: s.Flow,
		Fingerprint: s.Fingerprint, PublicKey: s.PublicKey, ShortID: s.ShortID, ALPN: s.ALPN, Method: s.Method, ServiceName: s.ServiceName, Mode: s.Mode}
	if set.Security == "none" {
		set.Security = ""
	}
	raw, _ := json.Marshal(set)
	managed, err := db.ManagedOutboundSettings(db.Outbound{Protocol: o.Protocol, SettingsJSON: string(raw)})
	if err != nil {
		return contracts.Outbound{}, fmt.Errorf("参数不能作出口：%v", err)
	}
	if o.Protocol == "hysteria2" || o.Protocol == "tuic" || o.Protocol == "anytls" {
		managed.Network = "" // 这些协议没有传输层选项，网络由协议决定
	}
	b, _ := json.Marshal(managed)
	_ = json.Unmarshal(b, &o.Settings)
	return o, nil
}

// parseExitLink 解析单条分享链接作出口。订阅地址不接受：出口只能是一个确定的节点。
func parseExitLink(raw string) (contracts.Outbound, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 4096 || strings.ContainsAny(raw, "\r\n") {
		return contracts.Outbound{}, "", errors.New("请粘贴一条分享链接")
	}
	if subscriptionURL(raw) {
		return contracts.Outbound{}, "", errors.New("这是订阅地址，不能作出口；请粘贴其中一个节点的分享链接")
	}
	n, err := subscription.ParseLink(raw)
	if err != nil {
		return contracts.Outbound{}, "", errors.New("无法识别的分享链接（支持 vless / vmess / trojan / ss / hysteria2 / tuic / anytls / socks5 / http）")
	}
	o, err := linkOutbound(n)
	if err != nil {
		return contracts.Outbound{}, "", err
	}
	return o, n.Remark, nil
}

// checkOutboundKernel 确认入口用的内核能作客户端连接这些出口（协议有出口模板、传输在 outbound_networks 里）。
func (s *Server) checkOutboundKernel(ctx context.Context, entry *ent.Gate, kernel string, members []contracts.Outbound) error {
	if kernel == "" {
		return nil
	}
	_, profiles, err := s.gateKernels(ctx, entry, map[string]bool{kernel: true})
	if err != nil {
		return fmt.Errorf("无法确认入口内核能否连接这个出口：%v", err)
	}
	d, err := kernelconf.ParseDriver(profiles[kernel])
	if err != nil {
		return err
	}
	for _, m := range members {
		network, _ := m.Settings["network"].(string)
		if err := d.OutboundAllows(db.NormalizeOutboundProtocol(m.Protocol), network); err != nil {
			return err
		}
	}
	return nil
}

// probeTarget 是实测时经出口访问的地址：中心的回显接口（顺带拿到实际出口 IP）；没记录公共地址时用通用连通检查。
func (s *Server) probeTarget(ctx context.Context) string {
	if o := s.centerOrigin(ctx); o != "" {
		return strings.TrimRight(o, "/") + "/api/v1/ip"
	}
	return "https://www.gstatic.com/generate_204"
}

// clientOutbound 把分享链接生成 sing-box 客户端出站（与订阅里的写法一致）；生成不了时返回错误，调用方退回只测端口。
func clientOutbound(link string) (map[string]any, error) {
	body, _, err := subout.Render([]string{link}, "singbox")
	if err != nil {
		return nil, err
	}
	var doc struct {
		Outbounds []map[string]any `json:"outbounds"`
	}
	if err = json.Unmarshal(body, &doc); err != nil {
		return nil, err
	}
	for _, o := range doc.Outbounds {
		switch o["type"] {
		case "selector", "urltest", "direct", "block", "dns":
			continue
		}
		return o, nil
	}
	return nil, errors.New("客户端配置为空")
}

// exitProbe 是一次实测的结果；保存在 exit-probe:<所属对象>，链路页据此显示实际出口 IP 与延迟。
type exitProbe struct {
	OK        bool      `json:"ok"`
	Mode      string    `json:"mode"` // proxy：按协议真连；tcp：只测了端口
	LatencyMS int64     `json:"latency_ms,omitempty"`
	ExitIP    string    `json:"exit_ip,omitempty"`
	Note      string    `json:"note,omitempty"`
	Error     string    `json:"error,omitempty"`
	At        time.Time `json:"at"`
}

func exitProbeKey(owner string) string { return "exit-probe:" + owner }

func (s *Server) saveExitProbe(ctx context.Context, owner string, p *exitProbe) {
	if p == nil || owner == "" {
		return
	}
	b, _ := json.Marshal(p)
	_ = s.Store.KV.Set(ctx, exitProbeKey(owner), string(b), 0)
}

func (s *Server) loadExitProbe(ctx context.Context, owner string) (exitProbe, bool) {
	raw, ok, _ := s.Store.KV.Get(ctx, exitProbeKey(owner))
	var p exitProbe
	if !ok || json.Unmarshal([]byte(raw), &p) != nil {
		return exitProbe{}, false
	}
	return p, true
}

// probeExit 由入口 Gate 实测出口：有分享链接时用 sing-box 客户端按协议真连一次，经它访问中心拿到实际出口 IP；
// sing-box 表达不了这个出口或入口 VPS 拿不到 sing-box 时退回只测端口，并在结果里写明。一组至少一个成员通过（最多试 5 个）。
func (s *Server) probeExit(ctx context.Context, entry *ent.Gate, ex resolvedExit, publicOnly bool) (*exitProbe, error) {
	members := ex.members()
	var last error
	for i, m := range members {
		if i >= 5 {
			break
		}
		payload := map[string]any{"kind": "node", "address": m.Address, "port": m.Port, "public_only": publicOnly}
		note := ""
		if !ex.TCPOnly && i < len(ex.Links) && ex.Links[i] != "" {
			client, cerr := clientOutbound(ex.Links[i])
			kernels, profiles, kerr := s.gateKernels(ctx, entry, map[string]bool{"sing-box": true})
			switch {
			case cerr != nil:
				note = "实测客户端表达不了这个出口，只测了端口"
			case kerr != nil:
				note = "入口 VPS 暂时拿不到 sing-box，只测了端口"
			default:
				var cfg map[string]any
				raw, _ := json.Marshal(map[string]any{"kernels": kernels, "drivers": map[string]string{"sing-box": profiles["sing-box"]}, "client": client})
				_ = json.Unmarshal(raw, &cfg)
				for k, v := range cfg {
					payload[k] = v
				}
				payload["target"] = s.probeTarget(ctx)
			}
		} else if ex.TCPOnly {
			note = "另一台 VPS 的节点：只测了端口，这个节点本身已在建节点时实测"
		}
		res, err := s.gateProbe(ctx, entry, contracts.ActionChainProbe, payload, 45*time.Second)
		if err != nil {
			last = err
			continue
		}
		p := &exitProbe{OK: true, Mode: "tcp", Note: note, At: time.Now().UTC()}
		if v, ok := res["mode"].(string); ok && v != "" {
			p.Mode = v
		}
		if v, ok := res["latency_ms"].(float64); ok {
			p.LatencyMS = int64(v)
		}
		if v, ok := res["exit_ip"].(string); ok {
			p.ExitIP = v
		}
		if p.Mode == "tcp" && p.Note == "" {
			p.Note = "只测了端口"
		}
		return p, nil
	}
	return nil, fmt.Errorf("chain_probe_failed: %w", last)
}
