package center

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/audit"
	entexit "github.com/shusfun/weir/internal/store/ent/exit"
	"github.com/shusfun/weir/packages/contracts"
)

func TestExitPartialUpdateEncryptionAndAtomicBinding(t *testing.T) {
	s, admin, g := setupGate(t)
	ctx := t.Context()
	var probes atomic.Int32
	g.serveActions(t, s, map[string]func(contracts.Action) contracts.ActionResult{
		contracts.ActionChainProbe: func(contracts.Action) contracts.ActionResult {
			probes.Add(1)
			return contracts.ActionResult{OK: true}
		},
	})
	ex := mustJSON(t, admin, "POST", "/api/v1/admin/exits", map[string]any{"gate_id": int(g.id), "kind": "socks5", "address": "203.0.113.5", "port": 1080, "password": "private-password"}, 200)
	id := int(ex["id"].(float64))
	stored := s.Store.Ent.Exit.GetX(ctx, id)
	if stored.Password == "" || stored.Password == "private-password" || strings.Contains(stored.String(), "private-password") {
		t.Fatal("password must be encrypted and sensitive")
	}
	before := probes.Load()
	updated := mustJSON(t, admin, "PATCH", "/api/v1/admin/exits/"+itoa(id), map[string]any{"remark": "只改备注"}, 200)
	if probes.Load() != before || updated["address"] != ex["address"] || updated["password"] != ex["password"] || updated["port"] != ex["port"] {
		t.Fatalf("remark update changed connection or probed: %v", updated)
	}
	for _, body := range []map[string]any{{"address": ""}, {"port": 0}, {"kind": ""}} {
		mustJSON(t, admin, "PATCH", "/api/v1/admin/exits/"+itoa(id), body, 422)
	}
	unchanged := mustJSON(t, admin, "PATCH", "/api/v1/admin/exits/"+itoa(id), map[string]any{"address": nil}, 200)
	if unchanged["address"] != ex["address"] || probes.Load() != before {
		t.Fatalf("null PATCH field changed connection or probed: %v", unchanged)
	}
	link := mustJSON(t, admin, "POST", "/api/v1/admin/exits", map[string]any{"gate_id": int(g.id), "kind": "link", "link": ss2022Link}, 200)
	sealed := s.Store.Ent.Exit.GetX(ctx, int(link["id"].(float64)))
	if sealed.Link == ss2022Link || sealed.Link == "" || sealed.Remark != "SS-HK" {
		t.Fatal("link encryption or parsed default remark missing")
	}
	n := mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": int(g.id), "preset": "ss2022", "skip_probe": true}, 200)
	mustJSON(t, admin, "PATCH", "/api/v1/admin/nodes/"+itoa(int(n["id"].(float64))), map[string]any{"exit_id": id, "exit": map[string]any{"kind": "blackhole"}}, 400)
	count := s.Store.Ent.Exit.Query().CountX(ctx)
	s.Store.Ent.Node.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			if m.Op().Is(ent.OpCreate) {
				return nil, errors.New("test binding failed")
			}
			return next.Mutate(ctx, m)
		})
	})
	mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": int(g.id), "preset": "ss2022", "exit": map[string]any{"kind": "blackhole"}, "skip_probe": true}, 409)
	if got := s.Store.Ent.Exit.Query().CountX(ctx); got != count {
		t.Fatalf("failed node binding left an exit: before=%d after=%d", count, got)
	}
}

func TestExitSharingOwnershipAndTrace(t *testing.T) {
	s, admin, g := setupGate(t)
	ctx := t.Context()
	n := mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": int(g.id), "preset": "ss2022", "skip_probe": true}, 200)
	direct := int(n["exit_id"].(float64))
	logs := captureLog(t)
	trace := "0123456789abcdef0123456789abcdef"
	w := admin.do("POST", "/api/v1/admin/exits", map[string]any{"gate_id": int(g.id), "kind": "blackhole", "remark": "拦截"}, map[string]string{"traceparent": "00-" + trace + "-1111111111111111-01"})
	if w.Code != 200 || w.Header().Get("X-Trace-Id") != trace || w.Header().Get("X-Request-Id") == "" {
		t.Fatalf("create trace: %d %v", w.Code, w.Header())
	}
	ex := decode(t, w)
	id := int(ex["id"].(float64))
	record := s.Store.Ent.Audit.Query().Where(audit.ActionEQ("exit.save"), audit.TargetEQ(exitOwner(id)), audit.ResultEQ("ok")).OnlyX(ctx)
	if record.Detail["trace_id"] != trace || record.Detail["request_id"] != w.Header().Get("X-Request-Id") {
		t.Fatalf("audit trace=%v", record.Detail)
	}
	w = admin.do("DELETE", "/api/v1/admin/exits/"+itoa(direct), nil, map[string]string{"traceparent": "00-" + trace + "-2222222222222222-01"})
	if w.Code != 409 || w.Header().Get("X-Trace-Id") != trace {
		t.Fatalf("reject trace: %d %v", w.Code, w.Header())
	}
	record = s.Store.Ent.Audit.Query().Where(audit.ActionEQ("exit.delete"), audit.TargetEQ(exitOwner(direct)), audit.ResultEQ("failed")).OnlyX(ctx)
	if record.Detail["trace_id"] != trace || !strings.Contains(logs.String(), "trace="+trace) || !strings.Contains(logs.String(), "code=system_exit") {
		t.Fatalf("rejection evidence audit=%v log=%s", record.Detail, logs.String())
	}
	// 两个节点引用同一个出口；停用与删除都返回占用，备注修改保持身份。
	for _, node := range []map[string]any{n, mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": int(g.id), "preset": "ss2022", "skip_probe": true}, 200)} {
		mustJSON(t, admin, "PATCH", "/api/v1/admin/nodes/"+itoa(int(node["id"].(float64))), map[string]any{"exit_id": id, "skip_probe": true}, 200)
	}
	if refs, err := s.exitReferences(ctx, id); err != nil || len(refs) != 2 {
		t.Fatalf("shared refs=%v err=%v", refs, err)
	}
	mustJSON(t, admin, "PATCH", "/api/v1/admin/exits/"+itoa(id), map[string]any{"enabled": false}, 409)
	mustJSON(t, admin, "DELETE", "/api/v1/admin/exits/"+itoa(id), nil, 409)
	mustJSON(t, admin, "PATCH", "/api/v1/admin/exits/"+itoa(id), map[string]any{"remark": "更新备注"}, 200)
	// VPS 身份固定，不能引用另一台 VPS 的出口。
	eg := pairFakeGate(t, admin)
	eg.call("POST", "/api/v1/agent/heartbeat", contracts.Heartbeat{NodeTestVersion: 3, Version: "test", Arch: "amd64"})
	other := s.Store.Ent.Exit.Query().Where(entexit.GateIDEQ(int(eg.id)), entexit.TagEQ("direct")).OnlyX(ctx)
	mustJSON(t, admin, "PATCH", "/api/v1/admin/nodes/"+itoa(int(n["id"].(float64))), map[string]any{"exit_id": other.ID, "skip_probe": true}, 422)
}

func TestUserExitIsolationAndReuse(t *testing.T) {
	s, admin, g := setupGate(t)
	n := mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": int(g.id), "preset": "ss2022", "skip_probe": true}, 200)
	g.serveActions(t, s, nil)
	u := mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "owner@example.test", "allow_chain": true, "all_nodes": true}, 200)
	u2 := mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "other@example.test", "allow_chain": true, "all_nodes": true}, 200)
	owner := login(t, s, "owner@example.test", u["password"].(string))
	other := login(t, s, "other@example.test", u2["password"].(string))
	ex := mustJSON(t, owner, "POST", "/api/v1/me/exits", map[string]any{"gate_id": int(g.id), "kind": "https", "address": "203.0.113.5", "port": 443, "username": "u", "password": "secret"}, 200)
	id := int(ex["id"].(float64))
	if ex["owner_user_id"] != u["id"] {
		t.Fatalf("owner=%v", ex)
	}
	for _, method := range []string{"GET", "PATCH", "DELETE"} {
		mustJSON(t, other, method, "/api/v1/me/exits/"+itoa(id), map[string]any{"remark": "入侵"}, 404)
	}
	if items := mustJSON(t, other, "GET", "/api/v1/me/exits", nil, 200)["items"].([]any); len(items) != 0 {
		t.Fatalf("leaked exits=%v", items)
	}
	mustJSON(t, owner, "POST", "/api/v1/me/exits", map[string]any{"gate_id": int(g.id), "kind": "blackhole"}, 422)
	mustJSON(t, owner, "POST", "/api/v1/me/exits", map[string]any{"gate_id": int(g.id), "kind": "socks5", "address": "127.0.0.1", "port": 1080}, 422)
	for i := 0; i < 2; i++ {
		mustJSON(t, owner, "POST", "/api/v1/me/chains", map[string]any{"name": "共用", "entry_node_id": n["id"], "exit_id": id}, 200)
	}
	if refs, err := s.exitReferences(t.Context(), id); err != nil || len(refs) != 2 {
		t.Fatalf("references=%v err=%v", refs, err)
	}
	mustJSON(t, other, "POST", "/api/v1/me/chains", map[string]any{"name": "越权", "entry_node_id": n["id"], "exit_id": id}, 422)
	mustJSON(t, owner, "DELETE", "/api/v1/me/exits/"+itoa(id), nil, 409)
}
