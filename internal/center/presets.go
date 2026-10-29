package center

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/shusfun/weir/internal/kernelconf"
)

// Preset 是建节点方案：来自内核驱动（kernelconf.Preset），加上它属于哪个内核。
// 管理员只选方案，伪装与传输参数按驱动里的 settings 固定生成；高级字段也由驱动声明。
type Preset struct {
	kernelconf.Preset
	Kernel string `json:"kernel"`
}

// PresetField 是高级字段的说明（与驱动一致）。
type PresetField = kernelconf.PresetField

// presetDriver 是某内核建节点时用的驱动：明确设置的默认版本 → 已下载的最新版本 → min_version 最高的驱动。
func (s *Server) presetDriver(ctx context.Context, kernel string) (kernelconf.Driver, bool) {
	drivers := s.drivers(ctx)
	version := s.explicitDefault(ctx, kernel)
	if version == "" {
		version = s.newestAnyArch(kernel)
	}
	if version != "" {
		if d, _, err := drivers.Select(kernel, version); err == nil {
			return d, true
		}
	}
	return drivers.Newest(kernel)
}

// presets 列出全部方案（两个内核的，按驱动里的 order 排序）。
func (s *Server) presets(ctx context.Context) []Preset {
	var out []Preset
	for _, kernel := range kernelconf.Kernels {
		d, ok := s.presetDriver(ctx, kernel)
		if !ok {
			continue
		}
		for _, p := range d.SortedPresets() {
			out = append(out, Preset{Preset: p, Kernel: kernel})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Order < out[j].Order })
	return out
}

func (s *Server) presetByID(ctx context.Context, id string) (Preset, bool) {
	for _, p := range s.presets(ctx) {
		if p.ID == id {
			return p, true
		}
	}
	return Preset{}, false
}

func (p Preset) advancedAllowed(key string) bool { return p.Advanced(key) }

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

// presetSettings 按驱动里的 settings 生成节点创建时固定下来的参数（设置键与驱动模板里的入站字段一致）。
// 值里的 "$gen:名称" 换成生成的密钥或随机值；REALITY 公私钥同一次生成。REALITY 目标、证书等由调用方填入。
func presetSettings(p Preset) (map[string]any, error) {
	st := map[string]any{}
	var realityPriv, realityPub string
	gen := func(name string) (string, error) {
		switch name {
		case "reality.private", "reality.public":
			if realityPriv == "" {
				var err error
				if realityPriv, realityPub, err = realityKeyPair(); err != nil {
					return "", err
				}
			}
			if name == "reality.private" {
				return realityPriv, nil
			}
			return realityPub, nil
		case "hex8":
			return randomHex(4), nil
		case "hex12":
			return randomHex(6), nil
		case "hex24":
			return randomHex(12), nil
		case "ss2022":
			return ss2022Key(), nil
		case "wg.private":
			priv, _, err := wgKeyPair()
			return priv, err
		}
		return "", fmt.Errorf("unknown generator %s", name)
	}
	for k, v := range p.Settings {
		text, ok := v.(string)
		if !ok {
			st[k] = v
			continue
		}
		var err error
		st[k] = kernelconf.GenPattern.ReplaceAllStringFunc(text, func(m string) string {
			out, e := gen(strings.TrimPrefix(m, "$gen:"))
			if e != nil {
				err = e
			}
			return out
		})
		if err != nil {
			return nil, err
		}
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
