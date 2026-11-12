package accountsecurity

import (
	"encoding/json"
	"testing"

	"github.com/shusfun/weir/internal/policydata"
)

func TestAccountPolicyTypeAndBoundaries(t *testing.T) {
	for _, entry := range []struct{ name, value string }{
		{"code_lifetime_seconds", "0"}, {"code_lifetime_seconds", "9223372036854775807"},
		{"code_attempts", "11"}, {"code_cooldown_seconds", "[60,300]"},
		{"code_cooldown_seconds", "[60,300,900,1000]"}, {"code_cooldown_seconds", "[60,30,900]"},
		{"mail_ip_hour", "1001"}, {"mail_network_hour", "0"}, {"mail_site_hour", "1000001"},
		{"mail_email_hour", "1.5"}, {"mail_domain_hour", "\"20\""}, {"mail_domain_hour", "null"},
	} {
		doc := policydata.Empty()
		doc.Site[entry.name] = json.RawMessage(entry.value)
		if _, err := ResolvePolicy(doc); err == nil {
			t.Fatal("unbounded or invalid policy accepted", entry)
		}
	}
	doc := policydata.Empty()
	doc.External["code_attempts"] = json.RawMessage(`3`)
	doc.Site["mail_site_hour"] = json.RawMessage(`200`)
	v, err := ResolvePolicy(doc)
	if err != nil {
		t.Fatal(err)
	}
	p, err := ParsePolicy(v.Effective)
	if err != nil || p.Attempts != 3 || p.Site != 200 || p.IP != 30 || v.Sources["code_attempts"] != "external" || v.Sources["mail_site_hour"] != "site" {
		t.Fatal(p, v, err)
	}
}
