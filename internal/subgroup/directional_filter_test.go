package subgroup

import (
	"github.com/shusfun/weir/internal/sub"
	"testing"
)

func TestDirectionalBusinessFilters(t *testing.T) {
	nodes := []string{"trojan://a@example.test:443#SOURCE-HK", "trojan://b@example.test:443#MANUAL-HK", "trojan://c@example.test:443#SOURCE-US"}
	for _, include := range []string{"SOURCE", "keep:SOURCE"} {
		got := filterRules(nodes, include, "US")
		if len(got) != 1 || got[0] != nodes[0] {
			t.Fatalf("include %q: %v", include, got)
		}
	}
	if got := filterRules(nodes, "proto:trojan", "MANUAL"); len(got) != 2 {
		t.Fatal(got)
	}
	if got := sub.ParseFilterRuleText("SOURCE"); !got.ExcludeRules.HasRules || got.IncludeRules.HasRules {
		t.Fatal("legacy parser changed")
	}
}
