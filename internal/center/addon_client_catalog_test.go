package center

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestClientCatalogSafety(t *testing.T) {
	defaults := defaultClientCatalog()
	if validateClientCatalog(defaults) != nil {
		t.Fatal("defaults invalid")
	}
	for _, address := range []string{"javascript:alert(1)", "http://example.com/download", "https://u:secret@example.com/", "https://example.com/?token={subscription}", "https://example.com/#x", "https://example.com/\nheader"} {
		v := defaults[0]
		v.URL = address
		if validateClientCatalog([]catalogClient{v}) == nil {
			t.Fatal("unsafe catalog URL accepted")
		}
	}
	for _, change := range []func(*catalogClient){func(v *catalogClient) { v.ID = "../../owned" }, func(v *catalogClient) { v.Icon = "data:image/svg+xml;base64,raw" }, func(v *catalogClient) { v.Import = "https://evil.test/?token=" }, func(v *catalogClient) { v.Format = "unknown" }, func(v *catalogClient) { v.Platforms = []string{"windows", "windows"} }, func(v *catalogClient) { v.Name = strings.Repeat("x", 121) }} {
		v := defaults[0]
		change(&v)
		if validateClientCatalog([]catalogClient{v}) == nil {
			t.Fatal("invalid client accepted")
		}
	}
	if validateClientCatalog([]catalogClient{defaults[0], defaults[0]}) == nil {
		t.Fatal("duplicate ID accepted")
	}
	var v catalogClient
	if json.Unmarshal([]byte(`{"id":"x","shell":"bad"}`), &v) == nil {
		t.Fatal("unknown catalog field ignored")
	}
	if validateClientCatalog(nil) != nil {
		t.Fatal("empty catalog rejected")
	}
}
