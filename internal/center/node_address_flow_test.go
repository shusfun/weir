package center

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/shusfun/weir/internal/store/ent/audit"
	"github.com/shusfun/weir/packages/contracts"
)

func TestNodeAddressSetupTrace(t *testing.T) {
	s, admin, gate := setupGate(t)
	ctx := t.Context()
	path := "/api/v1/admin/gates/" + itoa(int(gate.id))
	mustJSON(t, admin, "PATCH", path, map[string]any{"public_host": ""}, 200)
	const trace = "901234567890abcdef0123456789abcd"
	headers := map[string]string{"traceparent": "00-" + trace + "-0123456789abcdef-01"}
	logs := captureLog(t)
	before := s.Store.Ent.Node.Query().CountX(ctx)
	for _, input := range []string{"127.0.0.1", "10.0.0.1"} {
		w := admin.do("PATCH", path, map[string]any{"public_host": input}, headers)
		if w.Code != 422 || w.Header().Get("X-Trace-Id") != trace || !strings.Contains(logs.String(), "id="+w.Header().Get("X-Request-Id")) {
			t.Fatalf("address rejection lost trace: %d %s", w.Code, w.Body)
		}
	}
	if s.Store.Ent.Gate.GetX(ctx, int(gate.id)).PublicHost != "" || s.Store.Ent.Node.Query().CountX(ctx) != before {
		t.Fatal("rejected address changed resources")
	}
	for _, p := range s.presetsForGate(ctx, s.Store.Ent.Gate.GetX(ctx, int(gate.id))) {
		if p.Available {
			t.Fatal("missing address marked ready", p.ID)
		}
	}
	w := admin.do("PATCH", path, map[string]any{"public_host": "203.0.113.40:443"}, headers)
	if w.Code != 200 || decode(t, w)["public_host"] != "203.0.113.40" {
		t.Fatalf("address save: %d %s", w.Code, w.Body)
	}
	row := s.Store.Ent.Audit.Query().Where(audit.ActionEQ("gate.update")).Order(audit.ByID()).AllX(ctx)
	last := row[len(row)-1]
	if last.Detail["trace_id"] != trace || last.Detail["request_id"] != w.Header().Get("X-Request-Id") {
		t.Fatal("address audit lost trace", last.Detail)
	}
	ready := false
	for _, p := range s.presetsForGate(ctx, s.Store.Ent.Gate.GetX(ctx, int(gate.id))) {
		if p.ID == "ss2022" {
			ready = p.Available
		}
	}
	if !ready {
		t.Fatal("saved address did not unlock protocol")
	}
	heartbeat(t, gate, contracts.Heartbeat{Version: "test", Arch: "amd64", Firewall: &contracts.FirewallStatus{Tool: "ufw", Active: true}})
	w = admin.do("POST", "/api/v1/admin/nodes", map[string]any{"gate_id": gate.id, "preset": "ss2022", "name": "address flow", "port": 23457, "user_ids": []int{1}}, headers)
	if w.Code != 200 {
		t.Fatalf("create: %d %s", w.Code, w.Body)
	}
	n := decode(t, w)
	id := int(n["id"].(float64))
	raw, ok, err := s.Store.KV.Get(ctx, pendingNodeProbePrefix+itoa(id))
	var pending pendingNodeProbe
	if err != nil || !ok || json.Unmarshal([]byte(raw), &pending) != nil || pending.Trace.ID != trace {
		t.Fatal("creation lost durable trace", err, raw)
	}
	stored := s.Store.Ent.Node.GetX(ctx, id)
	if stored.Port != 23457 || stored.Name != "address flow" || s.Store.Ent.Node.Query().CountX(ctx) != before+1 {
		t.Fatal("node draft changed or duplicated")
	}
	for _, value := range n["ports"].([]any) {
		if value.(map[string]any)["firewall"] != "blocked" {
			t.Fatal("firewall state", value)
		}
	}
	preview := mustJSON(t, admin, "GET", "/api/v1/proxy/preview?target=singbox", nil, 200)
	if !strings.Contains(preview["body"].(string), "203.0.113.40") {
		t.Fatal("subscription lost confirmed address")
	}
	mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "address-user@example.test", "password": "AddressTest1"}, 200)
	user := login(t, s, "address-user@example.test", "AddressTest1")
	mustJSON(t, user, "PATCH", path, map[string]any{"public_host": "203.0.113.41"}, 403)
	if s.Store.Ent.Gate.GetX(ctx, int(gate.id)).PublicHost != "203.0.113.40" {
		t.Fatal("unauthorized address change")
	}
	t.Logf("trace=%s gate=%d node=%d stages=address-validation,address-save,audit,presets,node-save,persistent-probe,subscription,permission-denied", trace, gate.id, id)
}
