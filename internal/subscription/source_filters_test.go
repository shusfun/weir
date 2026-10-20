package subscription

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

var sourceFilterLinks = []string{
	"ss://YWVzLTEyOC1nY206c3ludGhldGlj@example.com:8388#香港-A",
	"trojan://synthetic@example.com:443#日本-B",
	"trojan://synthetic@example.com:443#HK-Expire",
	"http://user:synthetic@example.com:8080#US-A",
	"ss://YWVzLTEyOC1nY206c3ludGhldGlj@example.com:8388#剩余流量",
}

func TestSourceFiltersOrderAndFidelity(t *testing.T) {
	for _, tc := range []struct {
		include, exclude string
		indices          []int
	}{
		{"", "", []int{0, 1, 2, 3, 4}},
		{"keep:proto:ss,trojan", "Expire\n剩余", []int{0, 1}},
		{"keep:香港\nkeep:US", "", []int{0, 3}},
		{"", "Expire\n---\nproto:trojan\n香港", []int{0, 1}},
		{"keep:proto:trojan", "keep:日本", []int{1}},
		{"", "keep:missing", []int{}},
	} {
		got, err := FilterSourceLinks(sourceFilterLinks, tc.include, tc.exclude)
		if err != nil {
			t.Fatal(err)
		}
		want := []string{}
		for _, i := range tc.indices {
			want = append(want, sourceFilterLinks[i])
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatal("source selection/order/credential fidelity mismatch")
		}
	}
	for _, rule := range []string{"[", `(?<=香港)A`, "proto:snell", strings.Repeat("a", 16385), strings.Repeat("a\n", 101)} {
		if ValidateSourceFilters("", rule) == nil {
			t.Fatal("invalid/unsupported filter accepted")
		}
		if _, err := FilterSourceLinks(nil, "", rule); err == nil {
			t.Fatal("empty inventory bypassed validation")
		}
	}
}

func TestSourceFiltersUpstreamComparison(t *testing.T) {
	if os.Getenv("MIGATE_UPSTREAM_TESTS") != "1" {
		t.Skip("requires Node for fixed-source oracle")
	}
	for _, rule := range []string{"", "香港", "keep:香港", "proto:ss,trojan", "keep:proto:trojan", "Expire\n---\n香港\nproto:trojan", "^hk|US", "keep:missing", "KeEp:Proto:SS\n剩余", "---\nproto:http\n---\nUS", "keep:香港\nkeep:US", "Expire\n剩余"} {
		for _, inInclude := range []bool{false, true} {
			include, exclude := "", rule
			if inInclude {
				include, exclude = rule, ""
			}
			body, _ := json.Marshal(map[string]any{"links": sourceFilterLinks, "include": include, "exclude": exclude})
			cmd := exec.Command("node", filepath.Join("..", "..", "third_party", "misub", "reference", "compare-source-filters.mjs"))
			cmd.Stdin = bytes.NewReader(body)
			out, err := cmd.Output()
			if err != nil {
				t.Fatal("fixed-source oracle failed", err)
			}
			var want []string
			if err = json.Unmarshal(out, &want); err != nil {
				t.Fatal(err)
			}
			got, err := FilterSourceLinks(sourceFilterLinks, include, exclude)
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("fixed-source selection differs (%q, include=%v): %v", rule, inInclude, err)
			}
		}
	}
}
