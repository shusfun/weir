package kernelconf

import (
	"bytes"
	"encoding/json"
)

// withDNS 在内核配置里加入 DoH 优先、普通 DNS 兜底。
// directIPv4 为真时直连只走 IPv4。已有 dns 字段时不覆盖。
// version 是正在生成的内核版本。sing-box 1.14 起用 evaluate 在 DoH 失败后改走 UDP；
// 1.12 没有这条规则动作，默认服务器仍是 DoH，不能把 final 指到 udp。
func withDNS(body []byte, kernel, version string, directIPv4 bool) []byte {
	if len(body) == 0 {
		return body
	}
	var doc map[string]any
	if json.Unmarshal(body, &doc) != nil {
		return body
	}
	if _, ok := doc["dns"]; ok {
		return body
	}
	switch kernel {
	case "xray":
		// 串行：先等 DoH，失败再试下一个。并行时 UDP 几乎总是先返回，DoH 优先就看不到。
		// domains 让 DoH 成为优先服务器；没匹配到的 UDP 只在优先服务器失败后作后备。
		doh := func(url string) map[string]any {
			return map[string]any{
				"address": url, "domains": []any{"regexp:.*"}, "timeoutMs": 4000,
			}
		}
		servers := []any{
			doh("https://1.1.1.1/dns-query"),
			doh("https://8.8.8.8/dns-query"),
			"1.1.1.1",
			"8.8.8.8",
		}
		strategy := "UseIP"
		if directIPv4 {
			strategy = "UseIPv4"
		}
		doc["dns"] = map[string]any{
			"servers": servers, "queryStrategy": strategy, "enableParallelQuery": false,
		}
		// 直连出站走内核 DNS。REALITY 伪装目标由 reality 模块用系统解析器拨号，不读这里的 domainStrategy。
		routing, _ := doc["routing"].(map[string]any)
		if routing == nil {
			routing = map[string]any{}
			doc["routing"] = routing
		}
		routing["domainStrategy"] = strategy
		if outs, ok := doc["outbounds"].([]any); ok {
			for _, item := range outs {
				ob, _ := item.(map[string]any)
				if ob["protocol"] != "freedom" {
					continue
				}
				settings, _ := ob["settings"].(map[string]any)
				if settings == nil {
					settings = map[string]any{}
					ob["settings"] = settings
				}
				settings["domainStrategy"] = strategy
			}
		}
	case "sing-box":
		strategy := "prefer_ipv4"
		if directIPv4 {
			strategy = "ipv4_only"
		}
		servers := []any{
			map[string]any{"type": "https", "tag": "cloudflare", "server": "1.1.1.1", "server_port": 443, "path": "/dns-query"},
			map[string]any{"type": "https", "tag": "google", "server": "8.8.8.8", "server_port": 443, "path": "/dns-query"},
			map[string]any{"type": "udp", "tag": "udp", "server": "1.1.1.1"},
		}
		dns := map[string]any{
			"servers":  servers,
			"final":    "cloudflare",
			"strategy": strategy,
		}
		if CompareVersions(version, "1.14.0") >= 0 {
			// evaluate 失败时 respond 不匹配，继续下一条；route 会停在那个服务器上，失败就不再落到 UDP。
			dns["rules"] = []any{
				map[string]any{"action": "evaluate", "server": "cloudflare", "tag": "doh"},
				map[string]any{"action": "respond", "match_response": "doh"},
				map[string]any{"action": "evaluate", "server": "google", "tag": "doh2"},
				map[string]any{"action": "respond", "match_response": "doh2"},
			}
			dns["final"] = "udp"
			// 不把 default_domain_resolver 钉到某一个 DoH。1.14 指定服务器后出站拨号跳过上面的规则，
			// DoH 失败就不会再试 UDP。不指定时，多个 DNS 服务器的出站解析走规则，final 才是兜底。
		}
		doc["dns"] = dns
	default:
		return body
	}
	out, err := json.Marshal(doc)
	if err != nil {
		return body
	}
	if bytes.Equal(bytes.TrimSpace(body), bytes.TrimSpace(out)) {
		return body
	}
	return out
}
