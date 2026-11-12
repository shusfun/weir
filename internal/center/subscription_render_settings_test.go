package center

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSubscriptionRenderingValidationAndInheritance(t *testing.T) {
	s, admin := newInstalledServer(t)
	ctx := t.Context()
	settings := toMap(defaultSubscriptionSettings())
	settings["enable_tfo"] = true
	settings["dns_mode"] = "polluted"
	settings["custom_dns"] = "nameserver:\n  - https://1.1.1.1/dns-query\n"
	settings["region_overrides"] = []any{map[string]any{"pattern": "leaf", "region": "自定义", "flags": "i"}}
	mustJSON(t, admin, "PUT", "/api/v1/admin/subscriptions/settings", settings, 200)
	node := s.Store.Ent.SubNode.Create().SetURL("trojan://fixture@203.0.113.1:443#leaf").SaveX(ctx)
	p := s.Store.Ent.SubProfile.Create().SetName("inherit").SetNodeIds([]int{node.ID}).SaveX(ctx)
	_, _, _, _, effective, err := s.groupInput(ctx, p, "clash-meta")
	if err != nil || !effective.EnableTFO || effective.DNSMode != "polluted" || len(effective.RegionOverrides) != 1 {
		t.Fatal("global settings not inherited", effective, err)
	}
	preview := mustJSON(t, admin, "POST", "/api/v1/admin/subscriptions/profiles/"+itoa(p.ID)+"/preview", map[string]any{"target": "clash"}, 200)
	output, _ := preview["output"].(string)
	if !strings.Contains(output, "tfo: true") || !strings.Contains(output, "自定义") {
		t.Fatal("rendering did not receive effective settings", preview["output_error"])
	}
	overrides := map[string]any{"enable_tfo": false, "enable_udp": false, "skip_cert_verify": true, "dns_mode": "clean", "custom_dns": "", "region_overrides": []any{}}
	mustJSON(t, admin, "PATCH", "/api/v1/admin/subscriptions/profiles/"+itoa(p.ID), map[string]any{"settings": overrides}, 200)
	p = s.Store.Ent.SubProfile.GetX(ctx, p.ID)
	_, _, _, _, effective, err = s.groupInput(ctx, p, "clash-meta")
	if err != nil || effective.EnableTFO || effective.EnableUDP || !effective.SkipCertVerify || effective.DNSMode != "clean" || effective.CustomDNS != "" || len(effective.RegionOverrides) != 0 {
		t.Fatal("explicit false/empty overrides lost", effective, err)
	}
	for _, invalid := range []struct {
		key   string
		value any
		code  string
	}{
		{"dns_mode", "invalid", "invalid_dns_mode"},
		{"custom_dns", "nameserver: [", "invalid_custom_dns"},
		{"custom_dns", "- not-an-object", "invalid_custom_dns"},
		{"region_overrides", []any{map[string]any{"pattern": "(?=leaf)", "region": "test"}}, "invalid_region_overrides"},
		{"region_overrides", []any{map[string]any{"pattern": "leaf", "region": "test", "extra": true}}, "invalid_region_overrides"},
	} {
		w := admin.do("PATCH", "/api/v1/admin/subscriptions/profiles/"+itoa(p.ID), map[string]any{"settings": map[string]any{invalid.key: invalid.value}}, nil)
		if w.Code != 400 || !strings.Contains(w.Body.String(), invalid.code) {
			t.Fatal(invalid.key, w.Code, w.Body)
		}
		bad := toMap(defaultSubscriptionSettings())
		bad[invalid.key] = invalid.value
		w = admin.do("PUT", "/api/v1/admin/subscriptions/settings", bad, nil)
		if w.Code != 400 || !strings.Contains(w.Body.String(), invalid.code) {
			t.Fatal(invalid.key, w.Code, w.Body)
		}
	}
	// Settings survive their persisted JSON representation, including an empty
	// override list which must not revert to the global rules.
	raw, _ := json.Marshal(p.Settings)
	var roundTrip map[string]any
	_ = json.Unmarshal(raw, &roundTrip)
	decoded, err := decodeProfileSettings(roundTrip)
	if err != nil || decoded.RegionOverrides == nil || len(*decoded.RegionOverrides) != 0 || decoded.EnableTFO == nil || *decoded.EnableTFO {
		t.Fatal("override round trip changed semantics", decoded, err)
	}
}
