package center

import (
	"encoding/json"
	"testing"

	"github.com/shusfun/weir/packages/contracts"
)

func TestNodeReadiness(t *testing.T) {
	s, admin, g := setupGate(t)
	ctx := t.Context()
	// 新安装的管理员有默认链接，新节点马上有凭据；这里按删掉了链接的管理员测「没有任何凭据」。
	for _, l := range mustJSON(t, admin, "GET", "/api/v1/me/links", nil, 200)["items"].([]any) {
		mustJSON(t, admin, "DELETE", "/api/v1/me/links/"+itoa(int(l.(map[string]any)["id"].(float64))), nil, 204)
	}
	created := mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": g.id, "preset": "ss2022", "skip_probe": true}, 200)
	nid := int(created["id"].(float64))
	n := s.Store.Ent.Node.GetX(ctx, nid)
	check := func(want string) {
		t.Helper()
		gate := s.Store.Ent.Gate.GetX(ctx, int(g.id))
		got, _ := s.nodeReadiness(ctx, n, gate)
		if got != want {
			t.Fatalf("readiness=%s want %s", got, want)
		}
	}
	check("credentials")
	mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "reach@example.test", "all_nodes": true}, 200)
	check("pending")
	gate := s.Store.Ent.Gate.GetX(ctx, int(g.id))
	s.Store.Ent.Gate.UpdateOneID(gate.ID).SetAppliedRevision(gate.ConfigRevision).SetKernelsRunning(map[string]string{}).ExecX(ctx)
	check("kernel")
	s.Store.Ent.Gate.UpdateOneID(gate.ID).SetKernelsRunning(map[string]string{"sing-box": "1.14.4"}).ExecX(ctx)
	check("listener")
	s.Store.Ent.Gate.UpdateOneID(gate.ID).SetListeningPorts([]map[string]any{{"port": n.Port, "proto": "tcp", "process": "sing-box"}}).ExecX(ctx)
	check("ready")

}

func TestCompleteNodeTestPayloadForDependentProtocols(t *testing.T) {
	s, admin, g := setupGate(t)
	ctx := t.Context()
	g.serveActions(t, s, map[string]func(contracts.Action) contracts.ActionResult{contracts.ActionNodeTest: func(a contracts.Action) contracts.ActionResult {
		var c contracts.NodeTestClient
		b, _ := json.Marshal(a.Payload["client"])
		if err := json.Unmarshal(b, &c); err != nil {
			return contracts.ActionResult{Error: err.Error()}
		}
		if err := c.Loopback(32123); err != nil {
			return contracts.ActionResult{Error: err.Error()}
		}
		return contracts.ActionResult{OK: true, Result: map[string]any{"stage": "connect"}}
	}})
	for _, preset := range []string{"shadowtls", "wireguard"} {
		n := mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": g.id, "preset": preset, "skip_probe": true}, 200)
		res, err := s.testNode(ctx, s.Store.Ent.Node.GetX(ctx, int(n["id"].(float64))), n["settings"].(map[string]any))
		if err != nil || res["stage"] != "connect" {
			t.Fatalf("%s result=%v err=%v", preset, res, err)
		}
	}
	gate := s.Store.Ent.Gate.GetX(ctx, int(g.id))
	host := gate.Host
	host["node_test_version"] = 0
	s.Store.Ent.Gate.UpdateOneID(gate.ID).SetHost(host).ExecX(ctx)
	n := s.Store.Ent.Node.Query().FirstX(ctx)
	if _, err := s.testNode(ctx, n, n.Settings); err == nil {
		t.Fatal("old Gate accepted new test payload")
	}
}
