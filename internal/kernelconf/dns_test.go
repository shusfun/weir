package kernelconf

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSingBoxDNSPrefersDoH(t *testing.T) {
	out := withDNS([]byte(`{"outbounds":[]}`), "sing-box", "1.12.0", true)
	var doc map[string]any
	if json.Unmarshal(out, &doc) != nil {
		t.Fatal("dns config is not json")
	}
	dns, _ := doc["dns"].(map[string]any)
	if dns["final"] == "udp" {
		t.Fatal("sing-box final still points at udp")
	}
	if dns["final"] != "cloudflare" {
		t.Fatalf("final = %v", dns["final"])
	}
	raw, _ := json.Marshal(dns)
	if !strings.Contains(string(raw), `"type":"https"`) {
		t.Fatal("DoH server missing")
	}
}

func TestXrayDirectUsesKernelDNS(t *testing.T) {
	in := []byte(`{"routing":{"domainStrategy":"AsIs"},"outbounds":[{"protocol":"freedom","tag":"direct","settings":{}}]}`)
	out := withDNS(in, "xray", "26.3.27", true)
	var doc map[string]any
	if json.Unmarshal(out, &doc) != nil {
		t.Fatal(string(out))
	}
	routing, _ := doc["routing"].(map[string]any)
	if routing["domainStrategy"] != "UseIPv4" {
		t.Fatalf("routing domainStrategy = %v", routing["domainStrategy"])
	}
	outs, _ := doc["outbounds"].([]any)
	ob, _ := outs[0].(map[string]any)
	settings, _ := ob["settings"].(map[string]any)
	if settings["domainStrategy"] != "UseIPv4" {
		t.Fatalf("freedom domainStrategy = %v", settings["domainStrategy"])
	}
	dns, _ := doc["dns"].(map[string]any)
	if dns["enableParallelQuery"] != false {
		t.Fatalf("parallel query would let UDP win before DoH: %v", dns["enableParallelQuery"])
	}
	servers, _ := dns["servers"].([]any)
	first, _ := servers[0].(map[string]any)
	if first["address"] != "https://1.1.1.1/dns-query" {
		t.Fatalf("first DNS server = %v", first)
	}
	if _, ok := servers[len(servers)-1].(string); !ok {
		t.Fatalf("UDP fallback missing: %v", servers)
	}
}

func TestSingBox114FallsBackAfterDoH(t *testing.T) {
	out := withDNS([]byte(`{"outbounds":[]}`), "sing-box", "1.14.4", true)
	var doc map[string]any
	if json.Unmarshal(out, &doc) != nil {
		t.Fatal(string(out))
	}
	dns, _ := doc["dns"].(map[string]any)
	rules, _ := dns["rules"].([]any)
	if len(rules) < 2 {
		t.Fatal("1.14 config has no DoH failure fallback rules")
	}
	for _, item := range rules {
		rule, _ := item.(map[string]any)
		if rule["action"] == "route" {
			t.Fatalf("route stops at one server and hides UDP fallback: %v", rule)
		}
	}
	last, _ := rules[len(rules)-1].(map[string]any)
	if last["action"] != "respond" || last["match_response"] != "doh2" {
		t.Fatalf("last DoH rule = %v", last)
	}
	if dns["final"] != "udp" {
		t.Fatalf("final = %v", dns["final"])
	}
	route, _ := doc["route"].(map[string]any)
	if route["default_domain_resolver"] != nil {
		t.Fatalf("pinned resolver skips DNS rules: %v", route["default_domain_resolver"])
	}
}
