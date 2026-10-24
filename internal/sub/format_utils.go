package sub

import (
	"strings"

	js "github.com/shusfun/weir/internal/sub/jscompat"
)

// 移植自 functions/utils/format-utils.js

// ClashFix 对应 clashFix（上游已不再注入 remote-dns-resolve，保持原样返回）。
func ClashFix(content string) string { return content }

var reB64Strict = js.MustRegExp(`^[A-Za-z0-9+\/=]+$`, "")

// IsValidBase64 对应 isValidBase64。
func IsValidBase64(v any) bool {
	s, ok := v.(string)
	if !ok || s == "" {
		return false
	}
	clean := reAnySpace.Replace(s, "")
	if clean == "" {
		return false
	}
	normalized := urlSafeToStd(clean)
	if pad := len(normalized) % 4; pad != 0 {
		normalized += strings.Repeat("=", 4-pad)
	}
	return reB64Strict.Test(normalized) && len(normalized) > 20
}

var reClashQuery = js.MustRegExp(`[?&](?:clash(?:=|&|$)|target=clash(?:&|$))`, "i")

// GetProcessedUserAgent 对应 getProcessedUserAgent：拉取上游订阅时使用的 User-Agent。
func GetProcessedUserAgent(originalUserAgent string, rawURL string) string {
	if originalUserAgent == "" {
		return originalUserAgent
	}
	if u, err := js.ParseURL(rawURL); err == nil {
		sp := u.SearchParams()
		if t, ok := sp.Get("target"); sp.Has("clash") || (ok && js.ToLower(t) == "clash") {
			return "clash-verge/v2.4.3"
		}
	} else if reClashQuery.Test(rawURL) {
		return "clash-verge/v2.4.3"
	}
	return "v2rayN/7.23"
}

var formatUAMapping = [][2]string{
	{"flyclash", "clash"}, {"mihomo", "clash"}, {"clash.meta", "clash"}, {"clash-verge", "clash"}, {"meta", "clash"},
	{"stash", "clash"}, {"egern", "clash"}, {"nekoray", "clash"}, {"sing-box", "singbox"}, {"shadowrocket", "clash"},
	{"v2rayn", "base64"}, {"v2rayng", "base64"}, {"surge", "surge"}, {"loon", "loon"}, {"quantumult x", "quanx"},
	{"quantumult%20x", "quanx"}, {"quantumult-x", "quanx"}, {"quantumult", "quanx"}, {"clash", "clash"},
}

// DetermineFormatByUserAgent 对应 format-utils.js 的 determineFormatByUserAgent。
func DetermineFormatByUserAgent(userAgent string) string {
	ua := js.ToLower(userAgent)
	for _, m := range formatUAMapping {
		if strings.Contains(ua, m[0]) {
			return m[1]
		}
	}
	return "base64"
}

// DetermineFormatByURL 对应 determineFormatByUrl；返回 nil 表示 null。
func DetermineFormatByURL(sp *js.URLSearchParams) any {
	if t, ok := sp.Get("target"); ok && t != "" {
		return t
	}
	var target any = sp.GetValue("target")
	for _, f := range []string{"clash", "singbox", "surge", "loon", "quanx", "base64", "v2ray", "trojan", "egern"} {
		if sp.Has(f) {
			if f == "v2ray" || f == "trojan" {
				return "base64"
			}
			return f
		}
	}
	return target
}
