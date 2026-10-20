package subscriptions

import (
	"encoding/json"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestRegionScopedTemplate(t *testing.T) {
	var ops []Operator
	err := json.Unmarshal([]byte(`[{"type":"filter","params":{"regions":{"enabled":true,"values":["HK","日本"]}}},{"type":"rename","params":{"template":{"enabled":true,"template":"{emoji} {region:zh}-{index:02} {protocol:UPPER}"}}}]`), &ops)
	if err != nil {
		t.Fatal(err)
	}
	links := []string{
		"ss://YWVzLTEyOC1nY206cGFzcw@example.com:8388#Hong%20Kong",
		"ss://YWVzLTEyOC1nY206cGFzcw@example.com:8389#Japan",
		"ss://YWVzLTEyOC1nY206cGFzcw@example.com:8390#HK-02",
		"ss://YWVzLTEyOC1nY206cGFzcw@example.com:8391#USA",
	}
	out, err := Run(links, ops, "base64")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"🇭🇰 香港-01 SS", "🇯🇵 日本-01 SS", "🇭🇰 香港-02 SS"}
	if len(out) != len(want) {
		t.Fatal(out)
	}
	for i, s := range out {
		u, err := url.Parse(s)
		if err != nil || u.Fragment != want[i] {
			t.Fatalf("%s != %s (%v)", s, want[i], err)
		}
	}
	if _, err := RenderOperatorTemplate("{index:999999}", map[string]any{"index": 1}); err == nil {
		t.Fatal("unbounded padding accepted")
	}
}

func TestUpstreamRegionComparison(t *testing.T) {
	if os.Getenv("MIGATE_UPSTREAM_TESTS") != "1" {
		t.Skip("Node only for local upstream comparison")
	}
	names := []string{"", "unknown", "user", "kristi", "abcHK HK", "🇯🇵 Japan", "hk.example.com", "us-west-1", "HK-JP", "香港新加坡"}
	for _, r := range regions {
		for _, keyword := range r.Keywords {
			names = append(names, keyword, "prefix-"+keyword+"-01", "abc"+keyword+"xyz")
		}
	}
	samples := make([]map[string]string, 0, len(names))
	for _, name := range names {
		samples = append(samples, map[string]string{"name": name, "expected": extractRegion(name)})
	}
	raw, _ := json.Marshal(samples)
	file := filepath.Join(t.TempDir(), "regions.json")
	if err := os.WriteFile(file, raw, 0600); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join("..", "..", "third_party", "misub", "reference", "compare-regions.mjs")
	if out, err := exec.Command("node", script, file).CombinedOutput(); err != nil {
		t.Fatalf("%s: %v", out, err)
	}
	t.Logf("%d region names compared against pinned upstream", len(names))
}
