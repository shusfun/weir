package center

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"

	"github.com/google/uuid"

	"github.com/shusfun/weir/packages/contracts"
)

// Preset 是内置的「最稳定」节点方案：管理员只选协议，伪装与传输参数由这里固定生成。
type Preset struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Summary  string `json:"summary"`
	Kernel   string `json:"kernel"`
	Protocol string `json:"protocol"`
	Network  string `json:"network"`
	Security string `json:"security"`
	// Cert 为 "none" / "required"（需要真实证书）/ "self_signed_ok"（客户端以指纹固定自签证书）。
	Cert string `json:"cert"`
	// ChainExit 表示该协议可作为其他节点的链式代理出口（中心能为它生成客户端出口配置）。
	ChainExit bool `json:"chain_exit"`
	// Fields 是第 ① 步展开「高级」后可以改的字段（默认值由方案生成）；不在这里的字段不能改。
	Fields []PresetField `json:"fields"`
	// Note 是选这个方案时要知道的限制（如 WireGuard 只给不限额度的用户）。
	Note string `json:"note,omitempty"`
}

// PresetField 是高级字段的说明：中文名、类型、单位、可选值。键名与节点 settings 一致。
type PresetField struct {
	Key     string      `json:"key"`
	Label   string      `json:"label"`
	Kind    string      `json:"kind"` // text / number / select / bool
	Unit    string      `json:"unit,omitempty"`
	Hint    string      `json:"hint,omitempty"`
	Options [][2]string `json:"options,omitempty"` // [值, 显示名]
}

var (
	fpField   = PresetField{Key: "tls_fingerprint", Label: "TLS 指纹", Kind: "select", Options: [][2]string{{"chrome", "Chrome"}, {"firefox", "Firefox"}, {"safari", "Safari"}, {"ios", "iOS"}, {"edge", "Edge"}, {"random", "随机"}}}
	alpnField = PresetField{Key: "tls_alpn", Label: "ALPN", Kind: "text", Hint: "多个用逗号分隔，例如 h2,http/1.1"}
	wsFields  = []PresetField{{Key: "ws_path", Label: "WebSocket 路径", Kind: "text", Hint: "以 / 开头"}, {Key: "ws_host", Label: "WebSocket Host", Kind: "text", Hint: "留空时用连接地址"}, alpnField, fpField}
)

var presets = []Preset{
	{ID: "vless-reality", Name: "VLESS + REALITY", Summary: "无需域名和证书，借用大站 TLS 握手，抗探测最好（推荐）", Kernel: "xray", Protocol: "vless", Network: "tcp", Security: "reality", Cert: "none", ChainExit: true,
		Fields: []PresetField{fpField, {Key: "reality_short_id", Label: "Short ID", Kind: "text", Hint: "0–16 位十六进制"},
			{Key: "reality_dest", Label: "REALITY 目标", Kind: "text", Hint: "地址:端口；一般留空，由 VPS 实测选择（第 ④ 步也可以只填域名）"},
			{Key: "reality_server_names", Label: "REALITY SNI", Kind: "text", Hint: "填了 REALITY 目标时一起填，多个用逗号分隔"}}},
	{ID: "vless-ws-tls", Name: "VLESS + WebSocket + TLS", Summary: "用 VPS 公网 IP 的证书，不需要加解析；可套 CDN（需自备域名证书）", Kernel: "xray", Protocol: "vless", Network: "ws", Security: "tls", Cert: "required", ChainExit: true, Fields: wsFields},
	{ID: "vmess-ws-tls", Name: "VMess + WebSocket + TLS", Summary: "老客户端兼容最好；用 VPS 公网 IP 的证书", Kernel: "xray", Protocol: "vmess", Network: "ws", Security: "tls", Cert: "required", ChainExit: true, Fields: wsFields},
	{ID: "trojan-tls", Name: "Trojan + TLS", Summary: "用 VPS 公网 IP 的证书，不需要加解析", Kernel: "xray", Protocol: "trojan", Network: "tcp", Security: "tls", Cert: "required", ChainExit: true, Fields: []PresetField{alpnField, fpField}},
	{ID: "ss2022", Name: "Shadowsocks 2022", Summary: "轻量、无需证书，适合内网中转或对抗性弱的线路", Kernel: "sing-box", Protocol: "shadowsocks", Network: "tcp", Security: "none", Cert: "none"},
	{ID: "hysteria2", Name: "Hysteria2", Summary: "基于 QUIC，高丢包线路速度好；同一台 VPS 共用一张自签证书，订阅中固定指纹", Kernel: "sing-box", Protocol: "hysteria2", Network: "udp", Security: "tls", Cert: "self_signed_ok",
		Fields: []PresetField{{Key: "hy2_up_mbps", Label: "上行带宽", Kind: "number", Unit: "Mbps", Hint: "留空不限"}, {Key: "hy2_down_mbps", Label: "下行带宽", Kind: "number", Unit: "Mbps", Hint: "留空不限"},
			{Key: "hy2_obfs", Label: "混淆", Kind: "select", Options: [][2]string{{"salamander", "Salamander"}, {"", "不混淆"}}}}},
	{ID: "tuic", Name: "TUIC v5", Summary: "基于 QUIC，延迟低；同一台 VPS 共用一张自签证书，订阅中固定指纹", Kernel: "sing-box", Protocol: "tuic", Network: "udp", Security: "tls", Cert: "self_signed_ok",
		Fields: []PresetField{{Key: "tuic_congestion_control", Label: "拥塞控制", Kind: "select", Options: [][2]string{{"bbr", "BBR"}, {"cubic", "CUBIC"}, {"new_reno", "NewReno"}}}, {Key: "tuic_zero_rtt", Label: "0-RTT 握手", Kind: "bool", Hint: "更快，但有重放风险"}}},
	{ID: "shadowtls", Name: "ShadowTLS v3 + Shadowsocks 2022", Summary: "借用大站 TLS 握手伪装，无需证书；Clash / sing-box 订阅可用，没有通用分享链接", Kernel: "sing-box", Protocol: "shadowtls", Network: "tcp", Security: "none", Cert: "none",
		Fields: []PresetField{{Key: "tls_sni", Label: "握手站点", Kind: "text", Hint: "支持 TLS 1.3 的大站，例如 www.microsoft.com"}},
		Note:   "只有 Clash（mihomo）与 sing-box 订阅里会出现；Surge、Loon、QuanX、Egern 订阅不包含这个节点。"},
	{ID: "wireguard", Name: "WireGuard", Summary: "内核级 VPN，适合整机接入；UDP", Kernel: "sing-box", Protocol: "wireguard", Network: "udp", Security: "none", Cert: "none",
		Fields: []PresetField{{Key: "wg_mtu", Label: "MTU", Kind: "number", Hint: "默认 1408"}},
		Note:   "只对不限额度的用户生效：WireGuard 没有按用户的流量统计，无法执行额度，有额度的用户不会拿到这个节点（用户详情与「我的订阅」里会写明原因）。"},
	{ID: "socks-local", Name: "本地 SOCKS5", Summary: "带用户名密码的 SOCKS5（不加密），给只支持 SOCKS 的程序用", Kernel: "xray", Protocol: "socks", Network: "tcp", Security: "none", Cert: "none",
		Note: "SOCKS5 不加密，流量在公网上是明文，只适合可信网络或再套一层加密。"},
	{ID: "http-local", Name: "本地 HTTP 代理", Summary: "带用户名密码的 HTTP 代理（不加密），给只支持 HTTP 代理的程序用", Kernel: "xray", Protocol: "http", Network: "tcp", Security: "none", Cert: "none",
		Note: "HTTP 代理不加密，用户名密码与流量在公网上都是明文，只适合可信网络。"},
}

// advancedAllowed：建节点时可以覆盖的字段（预设的高级字段）。
func (p Preset) advancedAllowed(key string) bool {
	for _, f := range p.Fields {
		if f.Key == key {
			return true
		}
	}
	return false
}

func presetByID(id string) (Preset, bool) {
	for _, p := range presets {
		if p.ID == id {
			return p, true
		}
	}
	return Preset{}, false
}

// realityTargets 是 REALITY 伪装候选：Gate 实测 TLS 1.3 + h2 + 不跨域跳转后取延迟最低者。
var realityTargets = []string{
	"www.microsoft.com", "www.apple.com", "itunes.apple.com", "www.amazon.com", "aws.amazon.com",
	"www.cloudflare.com", "www.nvidia.com", "www.amd.com", "www.samsung.com", "www.tesla.com",
	"gateway.icloud.com", "dl.google.com", "www.yahoo.com", "www.lovelive-anime.jp",
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ss2022Method 固定使用 128 位方法：密钥 16 字节，客户端支持最广。
const ss2022Method = "2022-blake3-aes-128-gcm"

func ss2022Key() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return base64.StdEncoding.EncodeToString(b)
}

// presetSettings 生成节点创建时固定下来的参数（设置键与构建器的入站字段一致）。
// REALITY 目标、证书 ID 等需要外部信息的部分由调用方填入。
func presetSettings(p Preset) (map[string]any, error) {
	st := map[string]any{}
	switch p.ID {
	case "vless-reality":
		priv, pub, err := realityKeyPair()
		if err != nil {
			return nil, err
		}
		st["reality_private_key"], st["reality_public_key"] = priv, pub
		st["reality_short_id"] = randomHex(4)
		st["tls_fingerprint"] = "chrome"
	case "vless-ws-tls", "vmess-ws-tls":
		st["ws_path"] = "/" + randomHex(6)
		st["tls_alpn"] = "http/1.1"
	case "trojan-tls":
		st["tls_alpn"] = "h2,http/1.1"
	case "ss2022":
		st["ss_method"], st["ss_password"] = ss2022Method, ss2022Key()
	case "hysteria2":
		st["hy2_obfs"], st["hy2_obfs_password"] = "salamander", randomHex(12)
	case "tuic":
		st["tuic_congestion_control"], st["tls_alpn"] = "bbr", "h3"
	case "shadowtls":
		// ShadowTLS 只负责握手伪装，真正的加密由它后面的 Shadowsocks 2022 完成。
		st["shadowtls_version"], st["tls_sni"] = 3, realityTargets[0]
		st["ss_method"], st["ss_password"] = ss2022Method, ss2022Key()
	case "wireguard":
		priv, _, err := wgKeyPair()
		if err != nil {
			return nil, err
		}
		st["wg_private_key"], st["wg_mtu"], st["wg_address"] = priv, 1408, contracts.WGServerAddress
	case "socks-local", "http-local":
	default:
		return nil, fmt.Errorf("unknown preset %s", p.ID)
	}
	return st, nil
}

// newSecret 为某协议生成一份客户端凭据（UUID / 密码）。
func newSecret(protocol string) (id, password string) {
	switch protocol {
	case "vless", "vmess":
		return uuid.NewString(), ""
	case "tuic":
		return uuid.NewString(), randomHex(12)
	case "shadowsocks", "shadowtls":
		return "", ss2022Key()
	case "socks", "http":
		return "u" + randomHex(5), randomHex(12)
	case "wireguard":
		priv, _, _ := wgKeyPair()
		return "", priv
	default:
		return "", randomHex(16)
	}
}

// wgKeyPair 生成 WireGuard 密钥对（标准 base64，与 wg genkey 一致）。
func wgKeyPair() (string, string, error) {
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	return base64.StdEncoding.EncodeToString(key.Bytes()), base64.StdEncoding.EncodeToString(key.PublicKey().Bytes()), nil
}

// wgPublicKey 由私钥算出公钥；私钥无效时返回空。
func wgPublicKey(priv string) string {
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

// realityKeyPair 生成 REALITY 使用的 X25519 密钥对（base64url 无填充，与 Xray 一致）。
func realityKeyPair() (string, string, error) {
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	enc := base64.RawURLEncoding
	return enc.EncodeToString(key.Bytes()), enc.EncodeToString(key.PublicKey().Bytes()), nil
}
