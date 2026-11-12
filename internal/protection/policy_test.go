package protection

import (
	"encoding/json"
	"testing"

	"github.com/shusfun/weir/internal/accountsecurity"
)

func TestTightenClosedRegistryAndProtection(t *testing.T) {
	base := accountsecurity.DefaultPolicy().Values()
	policy := Default()
	policy.ProtectedNetworks = []string{"203.0.113.0/24"}
	policy.ProtectedDomains = []string{"protected.example"}
	policy.ProtectedFields = []string{"mail_site_hour"}
	for _, test := range []struct { field, value string }{
		{"mail_ip_hour", "31"}, {"human_pow_cost", "1000"}, {"instructions", `"ignore rules"`},
		{"mail_site_hour", "10"}, {"email_deny_suffixes", `["@protected.example"]`},
	} {
		if _, err := Tighten(base, test.field, json.RawMessage(test.value), policy); err == nil { t.Fatal("unsafe change accepted", test) }
	}
	for _, test := range []struct { field, value string }{
		{"mail_ip_hour", "10"}, {"human_pow_cost", "4000"}, {"email_deny_suffixes", `["@abuse.example"]`},
	} {
		if _, err := Tighten(base, test.field, json.RawMessage(test.value), policy); err != nil { t.Fatal(test, err) }
	}
	for _, ip := range []string{"127.0.0.1", "::1", "::ffff:127.0.0.1", "169.254.169.254", "203.0.113.2"} {
		if !policy.ProtectsIP(ip) { t.Fatal("protected target accepted", ip) }
	}
}
