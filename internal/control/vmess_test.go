package control

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	pipeline "github.com/imzyb/MiGate/services/subscriptions"
	"gopkg.in/yaml.v3"
)

func TestVMessConversionPreservesTransport(t *testing.T) {
	values := map[string]any{"v": "2", "ps": "HK 原名", "add": "example.com", "port": "443", "id": "11111111-1111-4111-8111-111111111111", "aid": 0, "scy": "aes-128-gcm", "net": "ws", "type": "none", "path": "/edge", "host": "edge.example.com", "tls": "tls", "sni": "tls.example.com", "alpn": "h2,http/1.1"}
	payload, _ := json.Marshal(values)
	link := "vmess://" + base64.RawURLEncoding.EncodeToString(payload)
	links, err := pipeline.Run([]string{link}, []pipeline.Operator{{Type: "rename", Params: json.RawMessage(`{"template":{"enabled":true,"template":"{region:zh}-{index:02}"}}`)}}, "clash")
	if err != nil {
		t.Fatal(err)
	}
	clash, err := clashOutput(links)
	if err != nil {
		t.Fatal(err)
	}
	var c struct {
		Proxies []map[string]any `yaml:"proxies"`
	}
	if yaml.Unmarshal(clash, &c) != nil || len(c.Proxies) != 1 {
		t.Fatal("invalid clash")
	}
	p := c.Proxies[0]
	if p["name"] != "香港-01" || p["cipher"] != "aes-128-gcm" || p["servername"] != "tls.example.com" || p["network"] != "ws" {
		t.Fatal("VMess fields lost", p)
	}
	sb, err := singboxOutput(links)
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		Outbounds []map[string]any `json:"outbounds"`
	}
	if json.Unmarshal(sb, &s) != nil || s.Outbounds[1]["security"] != "aes-128-gcm" {
		t.Fatal("singbox cipher lost")
	}
	values["aid"] = 64
	payload, _ = json.Marshal(values)
	legacy := []string{"vmess://" + base64.StdEncoding.EncodeToString(payload)}
	if _, err = clashOutput(legacy); err == nil {
		t.Fatal("legacy alterId silently dropped")
	}
	if _, err = singboxOutput(legacy); err == nil {
		t.Fatal("legacy alterId silently dropped")
	}
}
