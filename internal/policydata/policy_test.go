package policydata

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestResolveRejectsHiddenInvalidValuesAndRestoresInheritance(t *testing.T) {
	defaults := Values{"budget": json.RawMessage(`10`)}
	validate := func(v Values) error {
		var n int
		if json.Unmarshal(v["budget"], &n) != nil || n < 1 || n > 100 {
			return errors.New("budget out of range")
		}
		return nil
	}
	doc := Empty()
	doc.External["budget"] = json.RawMessage(`20`)
	doc.Site["budget"] = json.RawMessage(`30`)
	a, err := Resolve(defaults, doc, validate)
	if err != nil || string(a.Effective["budget"]) != "30" || a.Sources["budget"] != "site" {
		t.Fatal(a, err)
	}
	delete(doc.Site, "budget")
	b, err := Resolve(defaults, doc, validate)
	if err != nil || string(b.Effective["budget"]) != "20" || b.Sources["budget"] != "external" || a.Revision == b.Revision {
		t.Fatal(b, err)
	}
	delete(doc.External, "budget")
	c, err := Resolve(defaults, doc, validate)
	if err != nil || string(c.Effective["budget"]) != "10" || c.Sources["budget"] != "default" {
		t.Fatal(c, err)
	}
	for _, bad := range []string{`0`, `101`, `null`, `"10"`} {
		doc.External["budget"] = json.RawMessage(bad)
		doc.Site["budget"] = json.RawMessage(`30`)
		if _, err := Resolve(defaults, doc, validate); err == nil {
			t.Fatal("invalid external layer hidden by site", bad)
		}
	}
	doc = Empty()
	doc.Site["shell"] = json.RawMessage(`"sh"`)
	if _, err := Resolve(defaults, doc, validate); err == nil {
		t.Fatal("unregistered field accepted")
	}
}
