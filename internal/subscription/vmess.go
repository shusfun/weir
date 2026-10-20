package subscription

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// VMess v2 sharing format keeps the original payload so names can be changed
// without discarding transport fields. Unsupported conversion fields are explicit.
func parseVMessLink(raw string) (ParsedLink, error) {
	payload := raw[len("vmess://"):]
	decoded, ok := tryDecodeBase64(payload)
	if !ok {
		return ParsedLink{}, errors.New("invalid vmess encoding")
	}
	var values map[string]json.RawMessage
	if json.Unmarshal([]byte(decoded), &values) != nil {
		return ParsedLink{}, errors.New("invalid vmess object")
	}
	get := func(key string) string {
		value := values[key]
		var s string
		if json.Unmarshal(value, &s) == nil {
			return s
		}
		return string(value)
	}
	port, err := strconv.Atoi(get("port"))
	if err != nil || port < 1 || port > 65535 || get("add") == "" || get("id") == "" {
		return ParsedLink{}, errors.New("invalid vmess endpoint or credential")
	}
	q := url.Values{}
	for key, target := range map[string]string{"net": "type", "tls": "security", "sni": "sni", "host": "host", "path": "path", "alpn": "alpn", "fp": "fp", "allowInsecure": "allowInsecure"} {
		if value := get(key); value != "" {
			q.Set(target, value)
		}
	}
	settings, err := settingsFromQuery(q, get("add"))
	if err != nil {
		return ParsedLink{}, err
	}
	settings.Method = get("scy")
	if settings.Method == "" {
		settings.Method = "auto"
	}
	switch settings.Method {
	case "auto", "aes-128-gcm", "chacha20-poly1305", "none", "zero":
	default:
		settings.VmessUnsupported = true
	}
	if aid := get("aid"); aid != "" && aid != "0" {
		settings.VmessUnsupported = true
	}
	if kind := get("type"); kind != "" && kind != "none" {
		settings.VmessUnsupported = true
	}
	if settings.Network == "grpc" {
		settings.ServiceName = get("path")
		settings.Path = ""
	}
	if settings.Security != "" && settings.Security != "none" && settings.Security != "tls" {
		settings.VmessUnsupported = true
	}
	for key, value := range values {
		switch key {
		case "v", "ps", "add", "port", "id", "aid", "scy", "net", "type", "host", "path", "tls", "sni", "alpn", "fp", "allowInsecure":
		default:
			if string(value) != `""` && string(value) != "null" {
				settings.VmessUnsupported = true
			}
		}
	}
	b, err := json.Marshal(settings)
	if err != nil {
		return ParsedLink{}, fmt.Errorf("vmess settings: %w", err)
	}
	node := ParsedLink{Protocol: "vmess", Remark: get("ps"), Address: strings.TrimSpace(get("add")), Port: port, Username: get("id"), RawLink: raw, SettingsJSON: string(b)}
	node.Identity = StableIdentity(raw, node)
	return node, nil
}
