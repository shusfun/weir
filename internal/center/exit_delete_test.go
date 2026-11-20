package center

import (
	"testing"

	"github.com/shusfun/weir/internal/store/ent/chain"
	"github.com/shusfun/weir/internal/store/ent/credential"
	"github.com/shusfun/weir/internal/store/ent/node"
	"github.com/shusfun/weir/packages/contracts"
)

// 删除被节点、链式代理、规则和客户端分组引用的代理：一次删除全部级联，分组里不留失效成员。
func TestDeleteExitCascades(t *testing.T) {
	s, admin, g := setupGate(t)
	g.serveActions(t, s, nil)
	ctx := t.Context()
	n := mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": int(g.id), "preset": "ss2022", "name": "经代理", "skip_probe": true,
		"exit": map[string]any{"kind": "socks5", "remark": "落地", "address": "203.0.113.30", "port": 1080}}, 200)
	nid, eid := int(n["id"].(float64)), int(n["exit_id"].(float64))
	direct := mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": int(g.id), "preset": "ss2022", "name": "直连", "skip_probe": true}, 200)
	directID := int(direct["id"].(float64))
	u := mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "cascade@example.test", "allow_chain": true, "all_nodes": true}, 200)
	uid := int(u["id"].(float64))
	// 用户在直连节点上自建一条落地到同一代理的链式代理
	ch := s.Store.Ent.Chain.Create().SetUserID(uid).SetEntryNodeID(directID).SetName("用户链").SetExitID(eid).SaveX(ctx)
	s.Store.Ent.Route.Create().SetGateID(int(g.id)).SetExitID(eid).SetDomain("domain:example.com").SaveX(ctx)
	user := s.Store.Ent.User.GetX(ctx, uid)
	p := loadPolicy(user)
	p.Groups = []contracts.ProxyGroup{
		{ID: 1, Name: "混合", Kind: "select", Members: []string{"node:" + itoa(nid), "chain:" + itoa(ch.ID), "node:" + itoa(directID)}, IntervalSeconds: 60},
		{ID: 2, Name: "只有链", Kind: "select", Members: []string{"chain:" + itoa(ch.ID)}, IntervalSeconds: 60},
	}
	user.Update().SetProxyPolicy(policyMap(p)).ExecX(ctx)

	preview := mustJSON(t, admin, "GET", "/api/v1/admin/exits/"+itoa(eid)+"/delete-preview", nil, 200)
	if got := len(preview["removes"].([]any)); got != 3 {
		t.Fatalf("preview removes=%d want node, chain and route: %v", got, preview)
	}
	late := s.Store.Ent.Route.Create().SetGateID(int(g.id)).SetExitID(eid).SetDomain("domain:late.example.com").SaveX(ctx)
	path := "/api/v1/admin/exits/" + itoa(eid)
	mustJSON(t, admin, "DELETE", path, map[string]any{"preview_token": preview["preview_token"]}, 409)
	if !s.Store.Ent.Node.Query().Where(node.IDEQ(nid)).ExistX(ctx) {
		t.Fatal("stale preview deleted a node")
	}
	s.Store.Ent.Route.DeleteOne(late).ExecX(ctx)
	previous := s.Store.Ent.Gate.GetX(ctx, int(g.id)).ConfigRevision
	if w := admin.do("DELETE", path, map[string]any{"preview_token": preview["preview_token"]}, nil); w.Code != 204 {
		t.Fatalf("delete=%d %s", w.Code, w.Body)
	}
	if current := s.Store.Ent.Gate.GetX(ctx, int(g.id)); current.ConfigRevision <= previous {
		t.Fatalf("revocation revision did not advance: %d to %d", previous, current.ConfigRevision)
	}
	if s.Store.Ent.Node.Query().Where(node.IDEQ(nid)).ExistX(ctx) {
		t.Fatal("node using the proxy must be deleted")
	}
	if !s.Store.Ent.Node.Query().Where(node.IDEQ(directID)).ExistX(ctx) {
		t.Fatal("unrelated node must stay")
	}
	if s.Store.Ent.Chain.Query().Where(chain.IDEQ(ch.ID)).ExistX(ctx) || s.Store.Ent.Credential.Query().Where(credential.ChainIDEQ(ch.ID)).ExistX(ctx) {
		t.Fatal("chain landing on the proxy and its credentials must be deleted")
	}
	if s.Store.Ent.Route.Query().CountX(ctx) != 0 {
		t.Fatal("routes through the proxy must be deleted")
	}
	groups := loadPolicy(s.Store.Ent.User.GetX(ctx, uid)).Groups
	if len(groups) != 1 || len(groups[0].Members) != 1 || groups[0].Members[0] != "node:"+itoa(directID) {
		t.Fatalf("groups must keep only live members and drop emptied groups: %+v", groups)
	}
}

// 历史残留的失效成员在启动清理时移除；再跑一次不再改动。
func TestGroupMemberPruneIdempotent(t *testing.T) {
	s, admin, g := setupGate(t)
	ctx := t.Context()
	n := mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": int(g.id), "preset": "ss2022", "skip_probe": true}, 200)
	nid := int(n["id"].(float64))
	u := mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "prune@example.test", "all_nodes": true}, 200)
	user := s.Store.Ent.User.GetX(ctx, int(u["id"].(float64)))
	p := loadPolicy(user)
	p.Groups = []contracts.ProxyGroup{{ID: 1, Name: "组", Kind: "select", Members: []string{"node:" + itoa(nid), "node:999999", "chain:888888"}, IntervalSeconds: 60}}
	user.Update().SetProxyPolicy(policyMap(p)).ExecX(ctx)
	before := loadPolicy(s.Store.Ent.User.GetX(ctx, user.ID)).Revision
	removed, err := s.pruneGroupMembers(ctx)
	if err != nil || removed != 2 {
		t.Fatalf("first prune removed=%d err=%v", removed, err)
	}
	after := loadPolicy(s.Store.Ent.User.GetX(ctx, user.ID))
	if len(after.Groups[0].Members) != 1 || after.Revision != before+1 {
		t.Fatalf("prune result %+v", after)
	}
	if removed, err = s.pruneGroupMembers(ctx); err != nil || removed != 0 {
		t.Fatalf("second prune removed=%d err=%v", removed, err)
	}
}

// 删除节点时同步摘掉分组里的这个节点。
func TestDeleteNodePrunesGroupMembers(t *testing.T) {
	s, admin, g := setupGate(t)
	g.serveActions(t, s, nil)
	ctx := t.Context()
	a := mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": int(g.id), "preset": "ss2022", "name": "A", "skip_probe": true}, 200)
	b := mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": int(g.id), "preset": "ss2022", "name": "B", "skip_probe": true}, 200)
	aid, bid := int(a["id"].(float64)), int(b["id"].(float64))
	u := mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "node-prune@example.test", "all_nodes": true}, 200)
	user := s.Store.Ent.User.GetX(ctx, int(u["id"].(float64)))
	p := loadPolicy(user)
	p.Groups = []contracts.ProxyGroup{{ID: 1, Name: "组", Kind: "select", Members: []string{"node:" + itoa(aid), "node:" + itoa(bid)}, IntervalSeconds: 60}}
	user.Update().SetProxyPolicy(policyMap(p)).ExecX(ctx)
	if w := admin.do("DELETE", "/api/v1/admin/nodes/"+itoa(aid), nil, nil); w.Code != 204 {
		t.Fatalf("delete node=%d %s", w.Code, w.Body)
	}
	members := loadPolicy(s.Store.Ent.User.GetX(ctx, user.ID)).Groups[0].Members
	if len(members) != 1 || members[0] != "node:"+itoa(bid) {
		t.Fatalf("members=%v", members)
	}
}
