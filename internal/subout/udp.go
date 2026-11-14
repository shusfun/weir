package subout

import (
	"encoding/json"
	"gopkg.in/yaml.v3"
	"strings"
)

func applyClashUDP(proxies []map[string]any, items []Item) {
	for i, item := range items {
		if i < len(proxies) {
			if item.UDP != nil {
				proxies[i]["udp"] = *item.UDP
			} else {
				delete(proxies[i], "udp")
			}
		}
	}
}

func applySingboxUDP(out []map[string]any, names []string, items []Item) {
	for i, item := range items {
		if item.UDP == nil || *item.UDP || i >= len(names) {
			continue
		}
		for _, o := range out {
			if o["tag"] == names[i] {
				switch o["type"] {
				case "vless", "vmess", "trojan", "shadowsocks", "socks", "hysteria2", "tuic", "anytls":
					o["network"] = "tcp"
				}
			}
		}
	}
}

func applyINIUDP(lines []string, items []Item) {
	for i, item := range items {
		if i >= len(lines) || item.UDP == nil || *item.UDP {
			continue
		}
		if strings.Contains(lines[i], "udp-relay=") {
			lines[i] = strings.ReplaceAll(lines[i], "udp-relay=true", "udp-relay=false")
		} else {
			lines[i] += ", udp-relay=false"
		}
	}
}

// Raw output also needs the metadata that share links cannot encode.
func applyUDPBody(body []byte, items []Item, target string) ([]byte, error) {
	switch target {
	case "clash", "mihomo":
		var doc map[string]any
		if err := yaml.Unmarshal(body, &doc); err != nil {
			return nil, err
		}
		if proxies, ok := doc["proxies"].([]any); ok {
			for i, p := range proxies {
				if i < len(items) {
					if items[i].UDP != nil {
						p.(map[string]any)["udp"] = *items[i].UDP
					} else {
						delete(p.(map[string]any), "udp")
					}
				}
			}
		}
		return yaml.Marshal(doc)
	case "singbox", "sing-box":
		var doc map[string]any
		if err := json.Unmarshal(body, &doc); err != nil {
			return nil, err
		}
		out, _, names, err := singboxNodes(linksOf(items), map[string]bool{"proxy": true})
		if err != nil {
			return nil, err
		}
		applySingboxUDP(out, names, items)
		// Match by endpoint identity instead of relying on raw selector positions.
		for _, o := range doc["outbounds"].([]any) {
			p := o.(map[string]any)
			for _, patched := range out {
				if p["tag"] == patched["tag"] && patched["network"] == "tcp" {
					p["network"] = "tcp"
				}
			}
		}
		return json.MarshalIndent(doc, "", "  ")
	case "surge", "loon", "quanx":
		lines, _, _, err := iniNodes(linksOf(items), target, map[string]bool{"PROXY": true, "DIRECT": true, "REJECT": true, "direct": true, "reject": true})
		if err != nil {
			return nil, err
		}
		patched := append([]string(nil), lines...)
		applyINIUDP(patched, items)
		text := string(body)
		for i, line := range lines {
			text = strings.Replace(text, line, patched[i], 1)
		}
		return []byte(text), nil
	}
	return body, nil
}
