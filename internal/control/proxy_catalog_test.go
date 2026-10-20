package control

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestProxyCatalogBoundsAndFixedSources(t *testing.T) {
	input := `[{"proxy":"http://user:secret@8.8.8.8:8080","ip":"8.8.8.8","port":8080,"country":"US","country_cn":"美国","city":"City","asOrganization":"Org","unrelated":"not projected"},{"proxy":"http://user:secret@8.8.8.8:8080"},{"ip":"127.0.0.1","port":443},{"ip":"100.64.1.1","port":443},{"proxy":"https://8.8.8.8:8080"},{"proxy":"http://8.8.8.8:8080/path"},{"proxy":"http://8.8.8.8:8080","ip":"1.1.1.1"}]`
	d, e := parseProxyCatalog([]byte(input), "http")
	if e != nil || len(d.Items) != 1 || d.Rejected != 5 {
		t.Fatal("catalog filtering", e, len(d.Items), d.Rejected)
	}
	if d.Items[0].CountryCode != "US" || d.Items[0].Password != "secret" || len(d.Items[0].Key) != 64 {
		t.Fatal("lost metadata/identity")
	}
	if d, e := parseProxyCatalog([]byte(`[{"proxy":"http://8.8.8.8:8080"}]`), "http"); e != nil || len(d.Items) != 1 {
		t.Fatal("optional metadata/credentials rejected", e)
	}
	b, _ := json.Marshal(d)
	if strings.Contains(string(b), "not projected") {
		t.Fatal("untrusted metadata projected")
	}
	for _, raw := range []string{`{}`, `null`, `[{"ip":"::1","port":1}]`, `[{"proxy":"http://8.8.8.8:8080?q=secret"}]`} {
		if _, e = parseProxyCatalog([]byte(raw), "http"); e == nil {
			t.Fatal("invalid catalog accepted")
		}
	}
	for _, protocol := range []string{"ftp", "http://internal", "../../foo"} {
		if _, e = parseProxyCatalog([]byte(input), protocol); e == nil {
			t.Fatal("arbitrary source accepted")
		}
	}
	for _, path := range proxyCatalogPaths {
		if !strings.HasPrefix(path, "https://raw.githubusercontent.com/EDT-Pages/Proxy-List/refs/heads/main/data/") {
			t.Fatal("nonofficial source")
		}
	}
}
