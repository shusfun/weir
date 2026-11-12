package kernelconf

import (
	"encoding/json"

	"github.com/shusfun/weir/internal/db"
	"github.com/shusfun/weir/packages/contracts"
)

// rejectInboundTag 必须与模板写进内核的入站标签一致。
// Xray 模板用 GeneratedInboundTag（inbound-<id>-协议），不是节点业务标签 node-<id>。
// sing-box 模板用统计标签；Shadowsocks 两者相同，Hysteria2 / TUIC 不同。
func rejectInboundTag(kernel string, in contracts.Inbound) string {
	ib := db.Inbound{ID: in.ID, Protocol: in.Protocol}
	if kernel == "sing-box" {
		return statsTag(ib)
	}
	return db.GeneratedInboundTag(ib)
}

// rejectUDP 让不支持 UDP 的入站在服务端直接拒绝 UDP，客户端才能退回 TCP。
// 订阅里不写 udp=false。标签必须与模板里的入站标签一致，否则规则匹配不到，流量被静默丢掉。
func rejectUDP(body []byte, kernel string, tags []string) []byte {
	if len(body) == 0 || len(tags) == 0 {
		return body
	}
	var doc map[string]any
	if json.Unmarshal(body, &doc) != nil {
		return body
	}
	switch kernel {
	case "xray":
		routing, _ := doc["routing"].(map[string]any)
		if routing == nil {
			routing = map[string]any{}
			doc["routing"] = routing
		}
		rules, _ := routing["rules"].([]any)
		// blackhole 会立刻结束 VLESS/mux 的 UDP 会话，客户端控制连接随之关闭。
		// 指到未监听端口会先重试约 1 秒，客户端这段时间看不到拒绝。
		routing["rules"] = append([]any{map[string]any{"type": "field", "network": "udp", "inboundTag": tags, "outboundTag": "udp-reject"}}, rules...)
		outbounds, _ := doc["outbounds"].([]any)
		found := false
		for _, item := range outbounds {
			ob, _ := item.(map[string]any)
			if ob["tag"] == "udp-reject" {
				found = true
			}
		}
		if !found {
			doc["outbounds"] = append(outbounds, map[string]any{
				"tag": "udp-reject", "protocol": "blackhole", "settings": map[string]any{},
			})
		}
	case "sing-box":
		route, _ := doc["route"].(map[string]any)
		if route == nil {
			route = map[string]any{}
			doc["route"] = route
		}
		rules, _ := route["rules"].([]any)
		route["rules"] = append([]any{map[string]any{"network": "udp", "inbound": tags, "action": "reject"}}, rules...)
	default:
		return body
	}
	// Shadowsocks 的 UDP 是独立套接字，路由拒绝不会回错误帧，客户端只能等到读超时。
	// 不监听 UDP 时，内核回 ICMP 端口不可达，本地客户端会马上关掉这次关联。
	// VLESS / Trojan 的 UDP 在 TCP 连接里，仍靠上面的路由拒绝关掉这条流。
	restrictShadowsocksUDP(doc, kernel, tags)
	out, err := json.Marshal(doc)
	if err != nil {
		return body
	}
	return out
}

func restrictShadowsocksUDP(doc map[string]any, kernel string, tags []string) {
	want := map[string]bool{}
	for _, tag := range tags {
		want[tag] = true
	}
	inbounds, _ := doc["inbounds"].([]any)
	for _, item := range inbounds {
		ib, _ := item.(map[string]any)
		tag, _ := ib["tag"].(string)
		if !want[tag] {
			continue
		}
		switch kernel {
		case "xray":
			if ib["protocol"] != "shadowsocks" {
				continue
			}
			settings, _ := ib["settings"].(map[string]any)
			if settings == nil {
				settings = map[string]any{}
				ib["settings"] = settings
			}
			settings["network"] = "tcp"
		case "sing-box":
			if ib["type"] != "shadowsocks" {
				continue
			}
			ib["network"] = "tcp"
		}
	}
}

func outboundUDP(m contracts.Outbound) (supported, known bool) {
	if m.Settings != nil {
		if v, ok := m.Settings["udp"].(bool); ok {
			return v, true
		}
	}
	switch m.Protocol {
	case "freedom":
		return true, true
	case "http", "https", "blackhole":
		return false, true
	default:
		return false, false
	}
}

// selectGroupUDP 让分组的 UDP 只走已知支持的成员。成员都支持或都未知时不改配置。
func selectGroupUDP(body []byte, kernel string, groups []group) []byte {
	type picked struct {
		tag string
		ids []int64
	}
	var want []picked
	for _, g := range groups {
		if len(g.udp) == 0 || len(g.udp) == len(g.members) {
			continue
		}
		want = append(want, picked{tag: db.GeneratedOutboundTag(kernel, g.id, ""), ids: g.udp})
	}
	if len(body) == 0 || len(want) == 0 {
		return body
	}
	var doc map[string]any
	if json.Unmarshal(body, &doc) != nil {
		return body
	}
	changed := false
	for _, g := range want {
		tags := make([]any, len(g.ids))
		for i, id := range g.ids {
			tags[i] = db.GeneratedOutboundTag(kernel, id, "")
		}
		udpTag := g.tag + "-udp"
		switch kernel {
		case "xray":
			routing, _ := doc["routing"].(map[string]any)
			if routing == nil {
				continue
			}
			balancers, _ := routing["balancers"].([]any)
			var base map[string]any
			for _, item := range balancers {
				b, _ := item.(map[string]any)
				if b["tag"] == g.tag {
					base = b
				}
			}
			if base == nil {
				continue
			}
			clone := map[string]any{}
			for k, v := range base {
				clone[k] = v
			}
			clone["tag"] = udpTag
			clone["selector"] = tags
			routing["balancers"] = append(balancers, clone)
			rules, _ := routing["rules"].([]any)
			routing["rules"] = prependUDPRules(rules, "balancerTag", g.tag, udpTag)
			changed = true
		case "sing-box":
			outs, _ := doc["outbounds"].([]any)
			var base map[string]any
			for _, item := range outs {
				o, _ := item.(map[string]any)
				if o["tag"] == g.tag && o["type"] == "urltest" {
					base = o
				}
			}
			if base == nil {
				continue
			}
			clone := map[string]any{}
			for k, v := range base {
				clone[k] = v
			}
			clone["tag"] = udpTag
			clone["outbounds"] = tags
			doc["outbounds"] = append(outs, clone)
			route, _ := doc["route"].(map[string]any)
			if route == nil {
				route = map[string]any{}
				doc["route"] = route
			}
			rules, _ := route["rules"].([]any)
			route["rules"] = prependUDPRules(rules, "outbound", g.tag, udpTag)
			changed = true
		}
	}
	if !changed {
		return body
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return body
	}
	return out
}

func prependUDPRules(rules []any, key, from, to string) []any {
	var extra []any
	for _, item := range rules {
		rule, _ := item.(map[string]any)
		if rule[key] != from {
			continue
		}
		if net, ok := rule["network"].(string); ok && net != "" && net != "udp" {
			continue
		}
		clone := map[string]any{}
		for k, v := range rule {
			clone[k] = v
		}
		clone["network"] = "udp"
		clone[key] = to
		extra = append(extra, clone)
	}
	if len(extra) == 0 {
		return rules
	}
	return append(extra, rules...)
}

func udpRejectTags(cfgInboundSettings func(id int64) map[string]any, inbounds []int64, tags []string) []string {
	var out []string
	for i, id := range inbounds {
		settings := cfgInboundSettings(id)
		if settings == nil || settings["reject_udp"] != true {
			continue
		}
		if i < len(tags) {
			out = append(out, tags[i])
		}
	}
	return out
}
