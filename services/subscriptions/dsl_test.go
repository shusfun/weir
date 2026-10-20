package subscriptions

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

type dslSample struct {
	Expression string         `json:"expression"`
	Template   string         `json:"template"`
	Context    map[string]any `json:"context"`
	Expected   any            `json:"expected"`
}

func TestDSLExamples(t *testing.T) {
	for _, s := range samples() {
		var got any
		var e error
		if s.Template != "" {
			got, e = Render(s.Template, s.Context)
		} else {
			got, e = Evaluate(s.Expression, s.Context)
		}
		if e != nil || got != s.Expected {
			t.Fatalf("%+v => %v (%v)", s, got, e)
		}
	}
}
func samples() []dslSample {
	return []dslSample{{Expression: `protocol === 'vless' && contains(name, 'hk')`, Context: map[string]any{"protocol": "vless", "name": "HK-01"}, Expected: true}, {Expression: `port >= 443`, Context: map[string]any{"port": 443}, Expected: true}, {Expression: `match(name, '^jp') || protocol === 'trojan'`, Context: map[string]any{"name": "JP-01", "protocol": "vless"}, Expected: true}, {Template: `{upper(protocol)} · {trim(name)} · {fallback(region, 'Other')}`, Context: map[string]any{"protocol": "vless", "name": " HK-01 "}, Expected: "VLESS · HK-01 · Other"}, {Template: `{replace(name, 'HK', 'Hong Kong')} #{index}`, Context: map[string]any{"name": "HK-01", "index": 2}, Expected: "Hong Kong-01 #2"}, {Template: `{pick(true, 'allow', 'deny')}`, Context: map[string]any{}, Expected: "allow"}}
}
func TestUpstreamDSLComparison(t *testing.T) {
	if os.Getenv("MIGATE_UPSTREAM_TESTS") != "1" {
		t.Skip("Node only for local upstream comparison")
	}
	node, e := exec.LookPath("node")
	if e != nil {
		t.Fatal(e)
	}
	payload, _ := json.Marshal(samples())
	file := filepath.Join(t.TempDir(), "samples.json")
	os.WriteFile(file, payload, 0600)
	script := filepath.Join("..", "..", "third_party", "misub", "reference", "compare.mjs")
	out, e := exec.Command(node, script, file).CombinedOutput()
	if e != nil {
		t.Fatalf("upstream comparison failed: %s %v", out, e)
	}
}
