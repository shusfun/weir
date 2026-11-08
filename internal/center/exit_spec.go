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

// 出口的统一表示：链式代理（管理员建节点的出口、用户自建）与分流出口都先解析成 resolvedExit，
// 再按入口内核检查能不能连、由入口 Gate 实测、编译进 Gate 配置。
// 每个成员同时保留一条节点链接，实测时据此生成 mihomo 客户端。

// proxyKinds 是填地址的代理出口；link 是单条节点链接。
var proxyKinds = map[string]string{"socks5": "socks", "http": "http", "https": "https"}

type resolvedExit struct {
	Outbound   contracts.Outbound // 单个出口，或 Protocol 为 group 的一组
	Names      []string           // 成员名称（订阅源节点）
	Links      []string           // 每个成员的节点链接，与成员一一对应
	Skipped    []string           // 订阅源里匹配但不能作出口的节点及原因
	TestNodeID int                // 另一节点使用短时内部授权完成协议实测
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

// proxyLink 是代理出口对应的节点链接（实测时生成客户端用）。
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

// linkOutbound 把一条节点链接转成出口；表达不了的参数明确报错，不悄悄丢掉。
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

// parseExitLink 解析单条节点链接作出口。订阅地址不接受：出口只能是一个确定的节点。
func parseExitLink(raw string) (contracts.Outbound, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 4096 || strings.ContainsAny(raw, "\r\n") {
		return contracts.Outbound{}, "", errors.New("请粘贴一条节点链接")
	}
	if subscriptionURL(raw) {
		return contracts.Outbound{}, "", errors.New("这是订阅地址，不能作出口；请粘贴其中一个节点的链接")
	}
	n, err := subscription.ParseLink(raw)
	if err != nil {
		return contracts.Outbound{}, "", errors.New("无法识别的节点链接（支持 vless / vmess / trojan / ss / hysteria2 / tuic / anytls / socks5 / http）")
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

// clientOutbound 与订阅复用同一个 mihomo 单代理条目。
func clientOutbound(link string) (map[string]any, error) { return subout.ClashProxy(link) }

// exitProbe 是一次实测的结果；保存在 exit-probe:<所属对象>，链路页据此显示实际出口 IP 与延迟。
type exitProbe struct {
	OK        bool      `json:"ok"`
	Mode      string    `json:"mode"` // proxy：按协议完成真实请求
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

// probeExit 不把端口可达当作协议实测成功。
func (s *Server) probeExit(ctx context.Context, entry *ent.Gate, ex resolvedExit, publicOnly bool) (*exitProbe, error) {
	kernels, profiles, err := s.gateKernels(ctx, entry, map[string]bool{"mihomo": true})
	if err != nil {
		return nil, err
	}
	if ex.TestNodeID != 0 {
		n, err := s.Store.Ent.Node.Get(ctx, ex.TestNodeID)
		if err != nil {
			return nil, err
		}
		credential, revoke, err := s.issueProbeCredential(ctx, n)
		if err != nil {
			return nil, err
		}
		defer revoke()
		g, err := s.waitProbeApplied(ctx, n)
		if err != nil {
			return nil, err
		}
		var cert *ent.Certificate
		if n.CertID != nil {
			cert, err = s.Store.Ent.Certificate.Get(ctx, *n.CertID)
			if err != nil {
				return nil, err
			}
		}
		link, err := shareLink(n, g.PublicHost, credential, "exit-test", cert)
		if err != nil {
			return nil, err
		}
		ex.Links = []string{link}
	}
	members := ex.members()
	var last error
	for i, m := range members {
		if i >= 5 {
			break
		}
		if i >= len(ex.Links) || ex.Links[i] == "" {
			return nil, errors.New("出口缺少可实测的客户端配置")
		}
		client, err := clientOutbound(ex.Links[i])
		if err != nil {
			return nil, fmt.Errorf("出口客户端配置无法生成：%w", err)
		}
		payload := map[string]any{"address": m.Address, "port": m.Port, "public_only": publicOnly, "target": s.probeTarget(ctx)}
		raw, _ := json.Marshal(map[string]any{"kernels": kernels, "drivers": profiles, "client": client})
		var config map[string]any
		_ = json.Unmarshal(raw, &config)
		for k, v := range config {
			payload[k] = v
		}
		jobStep(ctx, fmt.Sprintf("正在经出口实测（第 %d/%d 个成员）", i+1, min(len(members), 5)), 45*time.Second)
		res, err := s.gateProbe(ctx, entry, contracts.ActionChainProbe, payload, 45*time.Second)
		if err != nil {
			last = err
			continue
		}
		p := &exitProbe{OK: true, Mode: "proxy", At: time.Now().UTC()}
		if v, ok := res["latency_ms"].(float64); ok {
			p.LatencyMS = int64(v)
		}
		if v, ok := res["exit_ip"].(string); ok {
			p.ExitIP = v
		}
		return p, nil
	}
	return nil, fmt.Errorf("chain_probe_failed: %w", last)
}
