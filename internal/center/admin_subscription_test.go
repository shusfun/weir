package center

import (
	"context"
	"testing"

	"github.com/shusfun/weir/internal/store/ent/user"
)

// 管理员安装后就有默认链接，新节点不用任何分配直接进他的订阅。
func TestAdminDefaultLinkGetsNewNodes(t *testing.T) {
	_, admin, g := setupGate(t)
	links := mustJSON(t, admin, "GET", "/api/v1/me/links", nil, 200)["items"].([]any)
	if len(links) != 1 || links[0].(map[string]any)["all_nodes"] != true {
		t.Fatalf("admin must start with one all-nodes link: %v", links)
	}
	before := links[0].(map[string]any)["nodes"].(float64)
	mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": int(g.id), "preset": "ss2022", "skip_probe": true}, 200)
	links = mustJSON(t, admin, "GET", "/api/v1/me/links", nil, 200)["items"].([]any)
	if got := links[0].(map[string]any)["nodes"].(float64); got != before+1 {
		t.Fatalf("new node must join admin subscription: before %v, after %v", before, got)
	}
}

// 管理员不能被缩小节点范围；普通用户默认全部、也能单选；旧数据由维护任务改回来。
func TestAdminsAlwaysHaveAllNodes(t *testing.T) {
	s, admin, g := setupGate(t)
	ctx := context.Background()
	me := mustJSON(t, admin, "GET", "/api/v1/me", nil, 200)
	selfID := itoa(int(me["id"].(float64)))
	mustJSON(t, admin, "PATCH", "/api/v1/admin/users/"+selfID, map[string]any{"all_nodes": false}, 400)
	mustJSON(t, admin, "PATCH", "/api/v1/admin/users/"+selfID, map[string]any{"all_nodes": true, "name": "管理员"}, 200)
	mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "a2@example.test", "role": "admin", "all_nodes": false}, 400)
	mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "a3@example.test", "role": "admin", "node_ids": []int{}}, 400)

	byDefault := mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "all@example.test"}, 200)
	if byDefault["all_nodes"] != true {
		t.Fatalf("users default to all nodes: %v", byDefault)
	}
	n := mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": int(g.id), "preset": "ss2022", "skip_probe": true}, 200)
	picked := mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "one@example.test", "all_nodes": false, "node_ids": []int{int(n["id"].(float64))}}, 200)
	if picked["all_nodes"] != false || len(picked["node_ids"].([]any)) != 1 {
		t.Fatalf("users can pick single nodes: %v", picked)
	}

	stale, err := s.Store.Ent.User.Create().SetEmail("old@example.test").SetPasswordHash("x").SetRole(user.RoleAdmin).SetAllNodes(false).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.EnsureAdminsAllNodes(ctx); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Store.Ent.User.Get(ctx, stale.ID); err != nil || !got.AllNodes {
		t.Fatalf("stale admin must be repaired: %v %v", got, err)
	}
}
