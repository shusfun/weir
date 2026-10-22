// Package kernelconf 把中心下发的 GateConfig 生成为 Xray / sing-box 的配置文件。
// 中心和 Gate 使用同一份代码：中心用于预览与检查，Gate 用于实际应用（可离线重建）。
package kernelconf

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/imzyb/MiGate/internal/db"
	"github.com/imzyb/MiGate/internal/singbox"
	"github.com/imzyb/MiGate/internal/xray"
	"github.com/imzyb/MiGate/packages/contracts"
)

// Options 控制生成过程。Exhausted 是本 Gate 上额度已用尽的用户键集合。
type Options struct {
	CertDir   string
	Now       time.Time
	Exhausted map[string]bool
}

// Output 是生成结果；某个内核没有需要运行的入站时对应字段为 nil。
type Output struct {
	Xray    []byte
	Singbox []byte
}

func profileFor(cfg contracts.GateConfig, kernel string) (*Profile, error) {
	text, ok := cfg.Profiles[kernel]
	if !ok || text == "" {
		return nil, nil
	}
	p, err := ParseProfile(text)
	if err != nil {
		return nil, err
	}
	if p.Kernel != kernel {
		return nil, fmt.Errorf("profile for %s declares kernel %s", kernel, p.Kernel)
	}
	return &p, nil
}

// Build 生成两个内核的配置。版本档案存在时先校验每个入站组合，再删除档案列出的字段。
func Build(cfg contracts.GateConfig, opt Options) (Output, error) {
	if opt.Now.IsZero() {
		opt.Now = time.Now()
	}
	var out Output
	inbounds, tagToID, keyToCred, err := toInbounds(cfg, opt)
	if err != nil {
		return out, err
	}
	outbounds, outByTag, err := toOutbounds(cfg)
	if err != nil {
		return out, err
	}
	routes, err := toRoutes(cfg, tagToID, keyToCred, outByTag)
	if err != nil {
		return out, err
	}
	var xrayIn, singIn []db.Inbound
	for _, in := range inbounds {
		switch in.Core {
		case "xray":
			xrayIn = append(xrayIn, in)
		case "sing-box":
			// sing-box 不接受没有用户的入站；该入站此刻没有有效凭据时跳过。
			if len(in.Clients) > 0 {
				singIn = append(singIn, in)
			}
		default:
			return out, fmt.Errorf("inbound %d: unknown kernel %q", in.ID, in.Core)
		}
	}
	for kernel, list := range map[string][]db.Inbound{"xray": xrayIn, "sing-box": singIn} {
		p, err := profileFor(cfg, kernel)
		if err != nil {
			return out, err
		}
		if p == nil {
			continue
		}
		for _, in := range list {
			if err := p.Allows(in.Protocol, in.Network, in.Security); err != nil {
				return out, fmt.Errorf("inbound %d: %w", in.ID, err)
			}
		}
	}
	if len(xrayIn) > 0 {
		if cfg.Ports.XrayStats <= 0 {
			return out, errors.New("xray stats port not allocated")
		}
		conf, err := xray.BuildConfigWithOutbounds(xrayIn, outbounds, routes)
		if err != nil {
			return out, err
		}
		for i := range conf.Inbounds {
			if conf.Inbounds[i].Tag == "api" {
				conf.Inbounds[i].Port = cfg.Ports.XrayStats
			}
		}
		if out.Xray, err = finish(conf, cfg, "xray"); err != nil {
			return out, err
		}
	}
	if len(singIn) > 0 {
		if cfg.Ports.SingboxStats <= 0 {
			return out, errors.New("sing-box stats port not allocated")
		}
		conf, err := singbox.BuildConfigWithOutboundsOptions(singIn, outbounds, routes, singbox.BuildOptions{EnableV2RayAPIStats: true})
		if err != nil {
			return out, err
		}
		if conf.Experimental == nil || conf.Experimental.V2RayAPI == nil {
			return out, errors.New("sing-box stats api missing")
		}
		conf.Experimental.V2RayAPI.Listen = fmt.Sprintf("127.0.0.1:%d", cfg.Ports.SingboxStats)
		for i := range conf.Inbounds {
			if t := conf.Inbounds[i].TLS; t != nil && t.Enabled && t.CertificatePath == singbox.CertFile {
				return out, errors.New("sing-box TLS inbound requires a managed certificate")
			}
		}
		if out.Singbox, err = finish(conf, cfg, "sing-box"); err != nil {
			return out, err
		}
	}
	return out, nil
}

// finish 转成通用 JSON 后按档案删除字段，再输出缩进格式。
func finish(conf any, cfg contracts.GateConfig, kernel string) ([]byte, error) {
	b, err := json.Marshal(conf)
	if err != nil {
		return nil, err
	}
	p, err := profileFor(cfg, kernel)
	if err != nil {
		return nil, err
	}
	if p != nil && len(p.Strip) > 0 {
		var doc any
		if err = json.Unmarshal(b, &doc); err != nil {
			return nil, err
		}
		for _, path := range p.Strip {
			strip(doc, splitPath(path))
		}
		return json.MarshalIndent(doc, "", "  ")
	}
	var doc any
	_ = json.Unmarshal(b, &doc)
	return json.MarshalIndent(doc, "", "  ")
}

func splitPath(path string) []string {
	var parts []string
	start := 0
	for i := 0; i <= len(path); i++ {
		if i == len(path) || path[i] == '.' {
			if i > start {
				parts = append(parts, path[start:i])
			}
			start = i + 1
		}
	}
	return parts
}
