package control

import (
	"context"
	"encoding/json"
	"github.com/imzyb/MiGate/ent/resource"
	sub "github.com/imzyb/MiGate/internal/subscription"
	pipeline "github.com/imzyb/MiGate/services/subscriptions"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNamingValidationAndPrefixes(t *testing.T) {
	for _, raw := range []string{`{"region_overrides":[{"pattern":"(?=bad)","region":"美国","flags":"i"}]}`, `{"region_overrides":[{"pattern":"x","region":"美国","flags":"g"}]}`, `{"region_overrides":[{"pattern":"x","region":"美国","flags":"i","unknown":true}]}`, `{"enable_manual_nodes":"true"}`, `{"unknown":true}`} {
		if _, e := decodeSubscriptionNaming([]byte(raw)); e == nil {
			t.Fatal("accepted", raw)
		}
	}
	links := []string{"trojan://user@example.com:443?security=tls&sni=safe.example#HK", "http://u:p@example.com:80#Japan"}
	metadata := []pipeline.NodeMetadata{{Manual: true, Group: "Office"}, {Provider: "Airport"}}
	n := subscriptionNaming{EnableManualNodes: true, EnableSubscriptions: true, ManualNodePrefix: "Manual", PrependGroupName: true}
	out, e := applyInventoryPrefixes(links, metadata, n)
	if e != nil {
		t.Fatal(e)
	}
	for i, want := range []string{"Manual - Office - HK", "Airport - Japan"} {
		a, _ := sub.ParseLink(links[i])
		b, _ := sub.ParseLink(out[i])
		if b.Remark != want || a.SettingsJSON != b.SettingsJSON || a.Username != b.Username || a.Password != b.Password || a.Address != b.Address || a.Port != b.Port {
			t.Fatal("connection changed", i)
		}
	}
	duplicate, e := pipeline.PrependName(out[1], "Airport")
	if e != nil || duplicate != out[1] {
		t.Fatal("duplicate prefix")
	}
	if _, e = applyInventoryPrefixes(links, nil, n); e == nil {
		t.Fatal("metadata mismatch")
	}
	model := outputTemplate{AutoGroups: "region", regionOverrides: []regionOverride{{"Japan", "香港", "i"}, {".*", "美国", ""}}}
	b, e := templateAutoBuckets(links, model)
	if e != nil || len(b) != 2 || b[0].name != "美国 节点" || b[1].name != "香港 节点" {
		t.Fatal("first region match", b, e)
	}
}
func testSubscriptionNaming(t *testing.T, s *Server, request func(string, string, any, string, string) *httptest.ResponseRecorder, admin, ordinary string) {
	t.Helper()
	ctx := context.Background()
	defer s.Store.Resource.Delete().Where(resource.KindEQ("settings"), resource.KeyEQ("subscription-naming")).Exec(ctx)
	ep := "/api/v1/admin/subscription-naming"
	read := func() map[string]any {
		w := request("GET", ep, nil, "", admin)
		if w.Code != 200 {
			t.Fatal("read", w.Code)
		}
		var d map[string]any
		json.Unmarshal(w.Body.Bytes(), &d)
		return d
	}
	if read()["defaults"].(map[string]any)["enable_manual_nodes"] != false {
		t.Fatal("legacy naming changed")
	}
	d := map[string]any{"enable_manual_nodes": true, "enable_subscriptions": true, "manual_node_prefix": "Manual", "prepend_group_name": true, "region_overrides": []any{map[string]any{"pattern": "HK", "region": "美国", "flags": "i"}}}
	body := map[string]any{"defaults": d, "revision": read()["revision"], "confirm": true, "password": "wrong"}
	if request("PUT", ep, body, "", admin).Code != 401 {
		t.Fatal("reauth")
	}
	body["password"] = "local-testing-password"
	if request("GET", ep, nil, "", ordinary).Code != 403 || request("PUT", ep, body, "", ordinary).Code != 403 || request("PUT", ep, body, "", admin).Code != 200 {
		t.Fatal("permissions/save")
	}
	if request("PUT", ep, body, "", admin).Code != 409 {
		t.Fatal("CAS")
	}
	node, e := s.Store.Resource.Create().SetKind("nodes").SetKey(randomToken(12)).SetData(map[string]any{"url": "trojan://naming-fixture@example.com:443#HK", "group": "Office"}).Save(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Store.Resource.DeleteOneID(node.ID).Exec(ctx)
	data := map[string]any{"enabled": true, "node_ids": []any{float64(node.ID)}}
	p, e := s.Store.Resource.Create().SetKind("profiles").SetKey(randomToken(12)).SetData(data).Save(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Store.Resource.DeleteOneID(p.ID).Exec(ctx)
	preview := func(want string) {
		out, status, code := s.renderProfile(ctx, p.ID, "links")
		if status != 200 || code != "" {
			t.Fatal("render", status, code)
		}
		n, e := sub.ParseLink(out["links"].([]string)[0])
		if e != nil || n.Remark != want {
			t.Fatal("prefix result", n.Remark, e)
		}
	}
	preview("Manual - Office - HK")
	data["prefix_settings"] = map[string]any{"enable_manual_nodes": false, "prepend_group_name": false}
	s.Store.Resource.UpdateOneID(p.ID).SetData(data).Save(ctx)
	preview("HK")
	data["prefix_settings"] = map[string]any{"unknown": true}
	if request("POST", "/api/v1/admin/resources/profiles", map[string]any{"id": p.ID, "data": data}, "", admin).Code != 400 {
		t.Fatal("unknown group prefix")
	}
	tpl, e := s.Store.Resource.Create().SetKind("templates").SetKey(randomToken(12)).SetData(map[string]any{"name": "regions", "target": "clash", "auto_groups": "region", "groups": []any{map[string]any{"name": "PROXY", "type": "select", "members": []any{"$auto", "DIRECT"}}}, "rules": []any{map[string]any{"type": "FINAL", "policy": "PROXY"}}}).Save(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Store.Resource.DeleteOneID(tpl.ID).Exec(ctx)
	delete(data, "prefix_settings")
	data["template_ids"] = []any{float64(tpl.ID)}
	s.Store.Resource.UpdateOneID(p.ID).SetData(data).Save(ctx)
	out, status, _ := s.renderProfile(ctx, p.ID, "clash")
	if status != 200 || !strings.Contains(out["output"].(string), "美国 节点") {
		t.Fatal("template ignored override", status)
	}
	r, _ := s.Store.Resource.Query().Where(resource.KindEQ("settings"), resource.KeyEQ("subscription-naming")).Only(ctx)
	s.Store.Resource.UpdateOneID(r.ID).SetData(map[string]any{"region_overrides": "corrupt"}).Save(ctx)
	if request("GET", ep, nil, "", admin).Code != 503 {
		t.Fatal("corrupt read")
	}
	if _, status, _ = s.renderProfile(ctx, p.ID, "links"); status != 422 {
		t.Fatal("corrupt output not rejected")
	}
	t.Log("naming reauth/CAS, legacy defaults, prefix inheritance/explicit disable, unchanged credentials, region template first match, invalid group and corrupt state refusal passed")
}
