package subout

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
)

func digest(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }

// Formats 是支持的订阅目标格式。
var Formats = []string{"base64", "clash", "singbox", "surge", "loon", "quanx", "egern"}

// Render 把链接渲染成目标格式，返回内容与 Content-Type。
func Render(links []string, target string) ([]byte, string, error) {
	switch target {
	case "base64", "":
		return []byte(base64.StdEncoding.EncodeToString([]byte(strings.Join(links, "\n")))), "text/plain; charset=utf-8", nil
	case "clash", "mihomo":
		b, err := clashOutput(links)
		return b, "application/yaml; charset=utf-8", err
	case "singbox", "sing-box":
		b, err := singboxOutput(links)
		return b, "application/json", err
	case "surge", "loon", "quanx":
		b, err := iniOutput(links, target)
		return b, "text/plain; charset=utf-8", err
	case "egern":
		b, err := egernOutput(links)
		return b, "application/yaml; charset=utf-8", err
	}
	return nil, "", fmt.Errorf("unsupported target %q", target)
}

// TargetForUA 按客户端 User-Agent 推断格式；无法识别时为 base64（通用分享链接列表）。
func TargetForUA(ua string) string {
	ua = strings.ToLower(ua)
	switch {
	case strings.Contains(ua, "sing-box") || strings.Contains(ua, "singbox") || strings.Contains(ua, "hiddify") || strings.Contains(ua, "sfa") || strings.Contains(ua, "sfi") || strings.Contains(ua, "sfm"):
		return "singbox"
	case strings.Contains(ua, "clash") || strings.Contains(ua, "mihomo") || strings.Contains(ua, "stash") || strings.Contains(ua, "flclash") || strings.Contains(ua, "nyanpasu") || strings.Contains(ua, "verge"):
		return "clash"
	case strings.Contains(ua, "surge"):
		return "surge"
	case strings.Contains(ua, "loon"):
		return "loon"
	case strings.Contains(ua, "quantumult"):
		return "quanx"
	case strings.Contains(ua, "egern"):
		return "egern"
	}
	return "base64"
}
