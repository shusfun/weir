package subscriptions

import (
	"bytes"
	"encoding/json"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMetadataUpstreamComparison(t *testing.T) {
	if os.Getenv("MIGATE_UPSTREAM_TESTS") != "1" {
		t.Skip("local Node upstream comparison")
	}
	samples := []string{"", "🇭🇰 HK [2x]", "JP (0.5倍)", "name 3X end", "[1.25x] US", "US#test [tag]", "🇺🇸", "prefix2x", "x3x", "a [2x] b [4x]", "香港  02", "(1倍) 🇯🇵 JP"}
	raw, _ := json.Marshal(samples)
	cmd := exec.Command("node", filepath.Join("..", "..", "third_party", "misub", "reference", "compare-metadata.mjs"))
	cmd.Stdin = bytes.NewReader(raw)
	out, e := cmd.Output()
	if e != nil {
		t.Fatal("upstream metadata oracle failed", e)
	}
	var expected []NodeNameMetadata
	if e = json.Unmarshal(out, &expected); e != nil {
		t.Fatal(e)
	}
	for i, name := range samples {
		if actual := ExtractNodeMetadata(name); actual != expected[i] {
			t.Fatalf("metadata differs sample %d: %#v != %#v", i, actual, expected[i])
		}
	}
}

func TestProvenanceAcrossSortRenameAndDuplicateURLs(t *testing.T) {
	link := "trojan://synthetic@example.com:443#" + url.PathEscape("🇭🇰 Original [2x]")
	ops := []Operator{
		{Type: "sort", Params: json.RawMessage(`{"keys":[{"key":"group"}]}`)},
		{Type: "rename", Params: json.RawMessage(`{"template":{"enabled":true,"template":"changed"}}`)},
		{Type: "script", Params: json.RawMessage(`{"dsl":[{"action":"rename","template":"{group}-{provider}-{originalName}-{multiplier}"}]}`)},
	}
	out, e := RunWithMetadata([]string{link, link}, ops, "base64", []NodeMetadata{{Group: "Z", Provider: "second"}, {Group: "A", Provider: "first"}})
	if e != nil {
		t.Fatal(e)
	}
	for i, prefix := range []string{"A-first-", "Z-second-"} {
		u, e := url.Parse(out[i])
		if e != nil || u.Fragment != prefix+"🇭🇰 Original [2x]-2" {
			t.Fatal("lost original or occurrence provenance", i)
		}
	}
	filtered, e := RunWithMetadata([]string{link, link}, []Operator{{Type: "script", Params: json.RawMessage(`{"dsl":[{"action":"filter","field":"group","op":"eq","value":"A"}]}`)}}, "base64", []NodeMetadata{{Group: "Z"}, {Group: "A"}})
	if e != nil || len(filtered) != 1 {
		t.Fatal("source DSL filtering failed", e)
	}
	if _, e = RunWithMetadata([]string{link}, nil, "base64", []NodeMetadata{}); e == nil {
		t.Fatal("misaligned provenance accepted")
	}
	if _, e = RunWithMetadata([]string{link}, nil, "base64", []NodeMetadata{{Group: strings.Repeat("x", 121)}}); e == nil {
		t.Fatal("unbounded provenance accepted")
	}
}
