package subout

import (
	"strings"
	"testing"
)

func TestCustomEntriesPreserveOrderAndTargets(t *testing.T) {
	p, items := testPlan()
	p.Custom = []Rule{
		{ID: "exact", Kind: "domain", Values: []string{"child.example.com"}, Target: "proxy"},
		{ID: "suffix", Kind: "domain_suffix", Values: []string{"example.com"}, Target: "direct"},
		{ID: "ip", Kind: "ip_cidr", Values: []string{"8.8.8.0/24"}, Target: "proxy"},
	}
	for _, target := range []string{"clash", "singbox", "surge", "loon", "quanx", "egern"} {
		use := items
		if target == "surge" || target == "quanx" || target == "egern" {
			use = items[1:]
		}
		b, _, err := RenderPlan(use, target, p)
		if err != nil {
			t.Fatalf("%s: %v", target, err)
		}
		i, j := strings.Index(string(b), "child.example.com"), strings.LastIndex(string(b), "example.com")
		if i < 0 || j < 0 || i > j {
			t.Fatalf("%s changed rule order: %s", target, b)
		}
	}
	m := p.Match("child.example.com", []string{"HK", "JP", "US"}, items)
	if m.EntryID != "exact" || m.Policy != "PROXY" {
		t.Fatalf("exact: %+v", m)
	}
	m = p.Match("other.example.com", []string{"HK", "JP", "US"}, items)
	if m.EntryID != "suffix" || m.Policy != "DIRECT" {
		t.Fatalf("suffix: %+v", m)
	}
	m = p.Match("8.8.8.8", []string{"HK", "JP", "US"}, items)
	if m.EntryID != "ip" || m.Policy != GroupProxy {
		t.Fatalf("ip: %+v", m)
	}
}
