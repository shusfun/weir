package center

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"

	"github.com/google/uuid"
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
}

var presets = []Preset{
	{ID: "vless-reality", Name: "VLESS + REALITY", Summary: "无需域名和证书，借用大站 TLS 握手，抗探测最好（推荐）", Kernel: "xray", Protocol: "vless", Network: "tcp", Security: "reality", Cert: "none", ChainExit: true},
	{ID: "vless-ws-tls", Name: "VLESS + WebSocket + TLS", Summary: "需要域名与证书，可套 CDN", Kernel: "xray", Protocol: "vless", Network: "ws", Security: "tls", Cert: "required", ChainExit: true},
	{ID: "vmess-ws-tls", Name: "VMess + WebSocket + TLS", Summary: "老客户端兼容最好，需要域名与证书", Kernel: "xray", Protocol: "vmess", Network: "ws", Security: "tls", Cert: "required", ChainExit: true},
	{ID: "trojan-tls", Name: "Trojan + TLS", Summary: "需要域名与证书", Kernel: "xray", Protocol: "trojan", Network: "tcp", Security: "tls", Cert: "required", ChainExit: true},
	{ID: "ss2022", Name: "Shadowsocks 2022", Summary: "轻量、无需证书，适合内网中转或对抗性弱的线路", Kernel: "sing-box", Protocol: "shadowsocks", Network: "tcp", Security: "none", Cert: "none"},
	{ID: "hysteria2", Name: "Hysteria2", Summary: "基于 QUIC，高丢包线路速度好；未配证书时自动自签并在订阅中固定指纹", Kernel: "sing-box", Protocol: "hysteria2", Network: "udp", Security: "tls", Cert: "self_signed_ok"},
	{ID: "tuic", Name: "TUIC v5", Summary: "基于 QUIC，延迟低；未配证书时自动自签并在订阅中固定指纹", Kernel: "sing-box", Protocol: "tuic", Network: "udp", Security: "tls", Cert: "self_signed_ok"},
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
	case "shadowsocks":
		return "", ss2022Key()
	default:
		return "", randomHex(16)
	}
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
