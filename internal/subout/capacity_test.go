package subout

import (
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestCapacityOnlyReordersBuiltInAutomaticCandidates(t *testing.T) {
	p, items := testPlan()
	items[0].Deprioritized = true
	lay := p.layout([]string{"HK", "JP", "US"}, items)
	if !reflect.DeepEqual(lay.Groups[1].MemberRefs, []string{"node:3", "node:1"}) {
		t.Fatal(lay.Groups[1])
	}
	if !reflect.DeepEqual(lay.Groups[0].MemberRefs, []string{"auto", "group:1", "node:1"}) || !reflect.DeepEqual(lay.Groups[3].MemberRefs, []string{"chain:2", "node:3"}) {
		t.Fatal("manual or custom fallback order changed", lay.Groups)
	}
	body, err := ClashProvider(items, p, AutoProvider)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Proxies []struct {
			Name string `yaml:"name"`
		} `yaml:"proxies"`
	}
	if err := yaml.Unmarshal(body, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Proxies) != 2 || doc.Proxies[0].Name != "US" || doc.Proxies[1].Name != "HK" {
		t.Fatal(string(body))
	}
	// If all candidates are constrained, retain their relative order and
	// availability; quota hints must never become access restrictions.
	items[2].Deprioritized = true
	if got := p.layout([]string{"HK", "JP", "US"}, items).Groups[1].MemberRefs; !reflect.DeepEqual(got, []string{"node:1", "node:3"}) {
		t.Fatal(got)
	}
}
