package sub

import (
	"math"
	"strings"

	js "github.com/shusfun/weir/internal/sub/jscompat"
)

// 移植自 functions/modules/subscription/user-agent-utils.js

var (
	reBrowserMozilla = js.MustRegExp(`Mozilla`, "i")
	reBrowserEngines = js.MustRegExp(`Chrome|Safari|Edge|Opera|Firefox|Via|UCBrowser|Quark|MQQBrowser|Konqueror`, "i")
	reProxyOrBot     = js.MustRegExp(`clash|flclash|v2ray|surge|loon|shadowrocket|quantumult|stash|shadowsocks|mihomo|mihomo\.party|meta|nekobox|nekoray|sfi|sfa|sfra|sing-box|surfboard|hiddify|egern|yuetu|月兔|dio|dart|flutter|http-client|okhttp|axios|postman|curl|wget|go-http-client|python|java`, "i")
	reHiddify        = js.MustRegExp(`hiddify`, "i")
	reSurgeVersion   = js.MustRegExp(`surge(?:\s*-?\s*mac)?\/(\d+)`, "")
	reMetaKeyword    = js.MustRegExp(`mihomo|mihomo\.party|meta|verge|flclash|stash|nekoray|nekobox|party|surfboard`, "i")
	reDigitsOnly     = js.MustRegExp(`^\d+$`, "")
)

// IsBrowserAgent 对应 isBrowserAgent：浏览器访问（用于伪装页判断），排除代理客户端与脚本工具。
func IsBrowserAgent(userAgent string) bool {
	if userAgent == "" {
		return false
	}
	isBrowser := reBrowserMozilla.Test(userAgent) && reBrowserEngines.Test(userAgent)
	return isBrowser && !reProxyOrBot.Test(userAgent)
}

// IsHiddifyAgent 对应 isHiddifyAgent。
func IsHiddifyAgent(userAgent string) bool { return reHiddify.Test(userAgent) }

var targetUAMapping = [][2]string{
	{"hiddify", "singbox"},
	{"flclash", "clash"}, {"flyclash", "clash"}, {"butterfly", "clash"}, {"mihomo", "clash"}, {"clash.meta", "clash"},
	{"clash-verge", "clash"}, {"mihomo.party", "clash"}, {"mihomo-party", "clash"}, {"clash-party", "clash"},
	{"clash party", "clash"}, {"meta", "clash"},
	{"stash", "clash"}, {"nekoray", "clash"}, {"nekobox", "clash"}, {"surfboard", "clash"}, {"cfw", "clash"},
	{"clashforwindows", "clash"}, {"egern", "egern"}, {"yuetu", "clash"}, {"月兔", "clash"}, {"sing-box", "singbox"},
	{"singbox", "singbox"}, {"shadowrocket", "base64"}, {"v2rayn", "base64"}, {"v2rayng", "base64"}, {"loon", "loon"},
	{"quantumult x", "quanx"}, {"quantumult%20x", "quanx"}, {"quantumult-x", "quanx"}, {"quantumult", "quanx"},
	{"clash", "clash"},
}

// DetermineTargetFormat 对应 determineTargetFormat：先看 URL 参数，再按 User-Agent 推断客户端格式。
func DetermineTargetFormat(userAgent string, searchParams *js.URLSearchParams) string {
	targetFormat, _ := searchParams.Get("target")
	if targetFormat == "" {
		for _, f := range []string{"clash", "singbox", "surge", "loon", "base64", "v2ray", "trojan", "quanx", "egern", "nodes"} {
			if searchParams.Has(f) {
				if f == "v2ray" || f == "trojan" {
					targetFormat = "base64"
				} else {
					targetFormat = f
				}
				break
			}
		}
	}
	if targetFormat != "" {
		normalized := js.ToLower(targetFormat)
		if normalized == "singbox" || normalized == "sing-box" {
			return "singbox"
		}
		if normalized == "surge" {
			ver, _ := searchParams.Get("ver")
			safeVer := 4.0
			if ver != "" && reDigitsOnly.Test(ver) {
				safeVer = js.ParseInt(ver, 10)
			}
			return "surge&ver=" + js.NumberToString(safeVer)
		}
		if strings.HasPrefix(normalized, "surge&ver=") {
			return normalized
		}
		return targetFormat
	}
	ua := js.ToLower(userAgent)
	if strings.Contains(ua, "surge") {
		if m := reSurgeVersion.MatchFirst(ua); m != nil {
			version := js.ParseInt(m.Str(1), 10)
			if strings.Contains(ua, "surge/") && !strings.Contains(ua, "mac") {
				return "surge&ver=4"
			}
			return "surge&ver=" + js.NumberToString(math.Max(2, version))
		}
		return "surge&ver=4"
	}
	for _, m := range targetUAMapping {
		if strings.Contains(ua, m[0]) {
			return m[1]
		}
	}
	return "base64"
}

// IsMetaCore 对应 isMetaCore：判断客户端是否为 Mihomo（Clash Meta）内核，决定是否启用 dialer-proxy 等语法。
func IsMetaCore(userAgent string, searchParams *js.URLSearchParams) bool {
	if searchParams != nil {
		if m, _ := searchParams.Get("meta"); m == "1" || m == "true" {
			return true
		}
	}
	if userAgent == "" {
		return false
	}
	ua := js.ToLower(userAgent)
	isMetaKeyword := reMetaKeyword.Test(ua)
	isModernClash := strings.Contains(ua, "clash") && !strings.Contains(ua, "clash-for-windows") && !strings.Contains(ua, "clash.for.windows")
	return isMetaKeyword || isModernClash
}
