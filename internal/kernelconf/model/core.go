package model

import (
	"fmt"
	"strings"
)

const (
	CoreXray    = "xray"
	CoreSingbox = "sing-box"
)

var sharedOutboundProtocols = map[string]bool{
	"vmess":       true,
	"socks":       true,
	"socks5":      true,
	"http":        true,
	"https":       true,
	"vless":       true,
	"trojan":      true,
	"shadowsocks": true,
}

var singboxOnlyOutboundProtocols = map[string]bool{
	"anytls":    true,
	"hysteria2": true,
	"tuic":      true,
	"shadowtls": true,
}

var builtinOutboundProtocols = map[string]bool{
	"direct":    true,
	"block":     true,
	"dns":       true,
	"freedom":   true,
	"blackhole": true,
}

func NormalizeCore(core string) string {
	switch strings.ToLower(strings.TrimSpace(core)) {
	case CoreSingbox, "singbox":
		return CoreSingbox
	default:
		return CoreXray
	}
}

// singboxInbounds 是只能由 sing-box 承载的入站协议，其余入站由 Xray 承载。
var singboxInbounds = map[string]bool{"hysteria2": true, "tuic": true, "wireguard": true, "shadowtls": true}

func InferInboundCore(protocol string) string {
	if singboxInbounds[strings.ToLower(strings.TrimSpace(protocol))] {
		return CoreSingbox
	}
	return CoreXray
}

func InboundCore(inbound Inbound) string {
	if strings.TrimSpace(inbound.Core) == "" {
		return InferInboundCore(inbound.Protocol)
	}
	return NormalizeCore(inbound.Core)
}

func NormalizeOutboundProtocol(protocol string) string {
	protocol = strings.ToLower(strings.TrimSpace(protocol))
	switch protocol {
	case "socks5":
		return "socks"
	case "direct":
		return "freedom"
	case "block":
		return "blackhole"
	default:
		return protocol
	}
}

func OutboundProtocolSupportedCores(protocol string) []string {
	normalized := NormalizeOutboundProtocol(protocol)
	switch {
	case sharedOutboundProtocols[normalized], builtinOutboundProtocols[normalized]:
		return []string{CoreXray, CoreSingbox}
	case singboxOnlyOutboundProtocols[normalized]:
		return []string{CoreSingbox}
	default:
		return []string{}
	}
}

func SupportsCore(cores []string, core string) bool {
	want := NormalizeCore(core)
	for _, item := range cores {
		if NormalizeCore(item) == want {
			return true
		}
	}
	return false
}

func OutboundSupportsCore(outbound Outbound, core string) bool {
	return SupportsCore(OutboundProtocolSupportedCores(outbound.Protocol), core)
}

func ResolveRuleOutbound(rule RoutingRule, outbounds []Outbound) (Outbound, bool) {
	if rule.OutboundID > 0 {
		for _, outbound := range outbounds {
			if outbound.ID == rule.OutboundID {
				return outbound, true
			}
		}
	}
	return Outbound{}, false
}

func ValidateOutboundProfile(outbound Outbound) error {
	protocol := NormalizeOutboundProtocol(outbound.Protocol)
	if outboundProtocolNeedsEndpoint(protocol) {
		if strings.TrimSpace(outbound.Address) == "" {
			return fmt.Errorf("%s outbound requires address", protocol)
		}
		if outbound.Port <= 0 || outbound.Port > 65535 {
			return fmt.Errorf("%s outbound requires valid port", protocol)
		}
	}
	switch protocol {
	case "vless", "vmess":
		if !IsUUID(outbound.Username) {
			return fmt.Errorf("vless outbound requires valid uuid in username")
		}
	case "trojan", "anytls":
		if strings.TrimSpace(outbound.Password) == "" {
			return fmt.Errorf("trojan outbound requires password")
		}
	case "shadowsocks":
		if strings.TrimSpace(outbound.Username) == "" {
			return fmt.Errorf("shadowsocks outbound requires method in username")
		}
		if strings.TrimSpace(outbound.Password) == "" {
			return fmt.Errorf("shadowsocks outbound requires password")
		}
	case "hysteria2":
		if strings.TrimSpace(outbound.Password) == "" {
			return fmt.Errorf("hysteria2 outbound requires password")
		}
	case "tuic":
		if !IsUUID(outbound.Username) {
			return fmt.Errorf("tuic outbound requires valid uuid in username")
		}
		if strings.TrimSpace(outbound.Password) == "" {
			return fmt.Errorf("tuic outbound requires password")
		}
	case "shadowtls":
		if strings.TrimSpace(outbound.Password) == "" {
			return fmt.Errorf("shadowtls outbound requires password")
		}
	}
	return nil
}

func GeneratedOutboundTag(core string, profileID int64, fallback string) string {
	if profileID <= 0 {
		return strings.TrimSpace(fallback)
	}
	switch NormalizeCore(core) {
	case CoreSingbox:
		return fmt.Sprintf("singbox-out-%d", profileID)
	default:
		return fmt.Sprintf("xray-out-%d", profileID)
	}
}

func RoutingRuleAppliesToCore(rule RoutingRule, inbounds []Inbound, core string) bool {
	want := NormalizeCore(core)
	if rule.ClientID > 0 {
		for _, inbound := range inbounds {
			for _, client := range inbound.Clients {
				if client.ID == rule.ClientID {
					return inbound.Enabled && InboundCore(inbound) == want
				}
			}
		}
		return false
	}
	if rule.InboundID > 0 {
		for _, inbound := range inbounds {
			if inbound.ID == rule.InboundID {
				return inbound.Enabled && InboundCore(inbound) == want
			}
		}
		return false
	}
	inboundTag := strings.TrimSpace(rule.InboundTag)
	if inboundTag == "" {
		for _, inbound := range inbounds {
			if inbound.Enabled && InboundCore(inbound) == want {
				return true
			}
		}
		return len(inbounds) == 0
	}
	for _, inbound := range inbounds {
		if GeneratedInboundTag(inbound) == inboundTag || strings.TrimSpace(inbound.Remark) == inboundTag {
			return inbound.Enabled && InboundCore(inbound) == want
		}
	}
	return false
}

func GeneratedInboundTag(inbound Inbound) string {
	return fmt.Sprintf("inbound-%d-%s", inbound.ID, strings.ToLower(strings.TrimSpace(inbound.Protocol)))
}

func IsUUID(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != 36 {
		return false
	}
	for i, r := range value {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
				return false
			}
		}
	}
	return true
}

func outboundProtocolNeedsEndpoint(protocol string) bool {
	switch protocol {
	case "socks", "http", "https", "vless", "trojan", "shadowsocks", "hysteria2", "tuic", "shadowtls":
		return true
	default:
		return false
	}
}
