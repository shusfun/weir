package subout

import (
	"encoding/json"
	"github.com/shusfun/weir/packages/contracts"
	"gopkg.in/yaml.v3"
	"strings"
	"testing"
)

func TestCompatibilityEmptyLinksAndArrayContract(t *testing.T) {
	for _, target := range Formats {
		_, _, report, err := RenderPlanDetailed([]Item{{Ref: "node:7", Link: "  "}}, target, nil)
		if err == nil || report.IncludedNodes != 0 || len(report.ExcludedNodes) != 1 || report.ExcludedNodes[0].Ref != "node:7" {
			t.Fatalf("%s: %+v %v", target, report, err)
		}
		data, _ := json.Marshal(report)
		if strings.Contains(string(data), ":null") {
			t.Fatalf("%s: %s", target, data)
		}
	}
}

func TestCompatibilityUnavailableProviderNameParity(t *testing.T) {
	p := &Plan{Groups: []contracts.ProxyGroup{{ID: 1, Name: "same", Members: []string{"node:missing"}}}, ExcludedNodes: []NodeExclusion{{Ref: "node:missing", Name: "offline"}}}
	items := []Item{{Link: "trojan://synthetic@example.com:443#same", Ref: "node:1", Direct: true, Auto: true}}
	body, _, report, err := RenderPlanDetailed(items, "clash", p)
	if err != nil || len(report.AffectedGroups) != 1 || !report.AffectedGroups[0].Removed {
		t.Fatalf("%+v %v", report, err)
	}
	provider, err := ClashProvider(items, p, AllProvider)
	if err != nil {
		t.Fatal(err)
	}
	var main, part struct {
		Proxies []map[string]any `yaml:"proxies"`
	}
	if yaml.Unmarshal(body, &main) != nil || yaml.Unmarshal(provider, &part) != nil || len(main.Proxies) != 1 || len(part.Proxies) != 1 || main.Proxies[0]["name"] != part.Proxies[0]["name"] {
		t.Fatal("provider names differ")
	}
	_, _, report, err = RenderPlanDetailed(items, "base64", p)
	if err != nil || report.IncludedNodes != 1 || len(report.ExcludedNodes) != 1 || len(report.AffectedGroups) != 0 {
		t.Fatalf("base64: %+v %v", report, err)
	}
}

func TestSurgeDocumentedTLSAndProtocolMappings(t *testing.T) {
	pin := strings.Repeat("ab", 32)
	cases := []struct {
		link string
		want []string
	}{
		{"hysteria2://synthetic@example.com:443?pinSHA256=" + pin + "&spki=c3ludGhldGlj&alpn=h3,h2", []string{"server-cert-fingerprint-sha256=" + pin, `alpn="h3,h2"`}},
		{"tuic://00000000-0000-0000-0000-000000000001:synthetic@example.com:443?alpn=h3", []string{"tuic-v5", "uuid=00000000-0000-0000-0000-000000000001", "password=synthetic"}},
		{"vless://00000000-0000-0000-0000-000000000001@example.com:443?security=tls&type=grpc&serviceName=proxy", []string{"vless", "tls=true", "grpc=true", "grpc-service-name=proxy"}},
	}
	for _, tc := range cases {
		body, _, err := Render([]string{tc.link}, "surge")
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range tc.want {
			if !strings.Contains(string(body), want) {
				t.Fatalf("missing %q", want)
			}
		}
	}
	// A public-key hash cannot be substituted for a complete-certificate hash.
	if _, _, err := Render([]string{"hysteria2://synthetic@example.com:443?spki=c3ludGhldGlj"}, "surge"); err == nil {
		t.Fatal("SPKI silently discarded")
	}
}
