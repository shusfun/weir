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
	ex := mustJSON(t, admin, "POST", "/api/v1/admin/exits", map[string]any{"kind": "socks5", "address": "203.0.113.5", "port": 1080, "password": "private-password"}, 200)
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
	link := mustJSON(t, admin, "POST", "/api/v1/admin/exits", map[string]any{"kind": "link", "link": ss2022Link}, 200)
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
	w := admin.do("POST", "/api/v1/admin/exits", map[string]any{"kind": "blackhole", "remark": "拦截"}, map[string]string{"traceparent": "00-" + trace + "-1111111111111111-01"})
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
	// 两个节点引用同一个出口：停用返回占用，删除预览列出两个会被一并删除的节点，备注修改保持身份。
	for _, node := range []map[string]any{n, mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": int(g.id), "preset": "ss2022", "skip_probe": true}, 200)} {
		mustJSON(t, admin, "PATCH", "/api/v1/admin/nodes/"+itoa(int(node["id"].(float64))), map[string]any{"exit_id": id, "skip_probe": true}, 200)
	}
	if refs, err := s.exitReferences(ctx, id); err != nil || len(refs) != 2 {
		t.Fatalf("shared refs=%v err=%v", refs, err)
	}
	mustJSON(t, admin, "PATCH", "/api/v1/admin/exits/"+itoa(id), map[string]any{"enabled": false}, 409)
	if p := mustJSON(t, admin, "GET", "/api/v1/admin/exits/"+itoa(id)+"/delete-preview", nil, 200); len(p["removes"].([]any)) != 2 {
		t.Fatalf("delete preview must list both nodes: %v", p)
	}
	mustJSON(t, admin, "PATCH", "/api/v1/admin/exits/"+itoa(id), map[string]any{"remark": "更新备注"}, 200)
	// VPS 身份固定，不能引用另一台 VPS 的出口。
	eg := pairFakeGate(t, admin)
	eg.sendHeartbeat(contracts.Heartbeat{NodeTestVersion: 3, Version: "test", Arch: "amd64"})
	other := s.Store.Ent.Exit.Query().Where(entexit.GateIDEQ(int(eg.id)), entexit.TagEQ("direct")).OnlyX(ctx)
	mustJSON(t, admin, "PATCH", "/api/v1/admin/nodes/"+itoa(int(n["id"].(float64))), map[string]any{"exit_id": other.ID, "skip_probe": true}, 422)
}

func TestProxyLibraryDeleteAndReferences(t *testing.T) {
	s, admin, firstGate := setupGate(t)
	secondGate := pairFakeGate(t, admin)
	ctx := t.Context()
	base := "/api/v1/admin/exits"
	newProxy := func(remark string) int {
		exit := mustJSON(t, admin, "POST", base, map[string]any{"kind": "socks5", "remark": remark, "address": "203.0.113.20", "port": 1080}, 200)
		return int(exit["id"].(float64))
	}
	unused, used := newProxy("待删除"), newProxy("同址保留")
	listItem := func(path string, id int) map[string]any {
		for _, value := range mustJSON(t, admin, "GET", path, nil, 200)["items"].([]any) {
			item := value.(map[string]any)
			if item["id"] == float64(id) {
				return item
			}
		}
		return nil
	}
	if item := listItem(base, unused); item == nil || len(item["references"].([]any)) != 0 {
		t.Fatalf("unused exit missing or in use: %v", item)
	}
	for _, gid := range []int{int(firstGate.id), int(secondGate.id)} {
		s.Store.Ent.Route.Create().SetGateID(gid).SetExitID(used).SetDomain("domain:example.com").SaveX(ctx)
	}
	item := listItem(base, used)
	if item == nil || len(item["references"].([]any)) != 2 || len(item["uses"].([]any)) != 2 {
		t.Fatalf("references across both VPSs: %v", item)
	}
	for _, use := range item["uses"].([]any) {
		if use.(map[string]any)["status"] != "unknown" {
			t.Fatalf("unverified reference reported as applied: %v", use)
		}
	}
	if item := listItem(base+"?gate_id="+itoa(int(firstGate.id)), unused); item != nil {
		t.Fatalf("gate-scoped catalog includes unused proxy: %v", item)
	}
	logs := captureLog(t)
	traceDelete := func(id int, trace string, status int, outcome string) {
		t.Helper()
		path := base + "/" + itoa(id)
		preview := mustJSON(t, admin, "GET", path+"/delete-preview", nil, 200)
		res := admin.do("DELETE", path, map[string]any{"preview_token": preview["preview_token"]}, map[string]string{"traceparent": "00-" + trace + "-1111111111111111-01"})
		if res.Code != status || res.Header().Get("X-Trace-Id") != trace {
			t.Fatalf("delete status=%d trace=%s body=%s", res.Code, res.Header().Get("X-Trace-Id"), res.Body.String())
		}
		record := s.Store.Ent.Audit.Query().Where(audit.ActionEQ("exit.delete"), audit.TargetEQ(exitOwner(id)), audit.ResultEQ(outcome)).OnlyX(ctx)
		if record.Detail["trace_id"] != trace || !strings.Contains(logs.String(), "operation=exit.delete trace="+trace) {
			t.Fatalf("delete trace missing from operation or audit: %v", record.Detail)
		}
	}
	traceDelete(unused, "0123456789abcdef0123456789abcdef", 204, "ok nodes=0 chains=0 routes=0")
	if listItem(base, unused) != nil || listItem(base, used) == nil {
		t.Fatal("delete must remove only the selected proxy and keep same-address records")
	}
	// 被引用的代理可以直接删除：预览列出要一起删除的两条规则，删除后规则随之消失。
	preview := mustJSON(t, admin, "GET", base+"/"+itoa(used)+"/delete-preview", nil, 200)
	if len(preview["removes"].([]any)) != 2 {
		t.Fatalf("preview must list both routes: %v", preview)
	}
	traceDelete(used, "abcdef0123456789abcdef0123456789", 204, "ok nodes=0 chains=0 routes=2")
	if n := s.Store.Ent.Route.Query().CountX(ctx); n != 0 || listItem(base, used) != nil {
		t.Fatalf("cascade left routes=%d", n)
	}
}

func TestUserExitIsolationAndReuse(t *testing.T) {
	s, admin, g := setupGate(t)
	n := mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": int(g.id), "preset": "ss2022", "skip_probe": true}, 200)
	g.serveActions(t, s, nil)
	u := mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "owner@example.test", "allow_chain": true, "all_nodes": true}, 200)
	u2 := mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "other@example.test", "allow_chain": true, "all_nodes": true}, 200)
	owner := login(t, s, "owner@example.test", u["password"].(string))
	other := login(t, s, "other@example.test", u2["password"].(string))
	ex := mustJSON(t, owner, "POST", "/api/v1/me/exits", map[string]any{"kind": "https", "address": "203.0.113.5", "port": 443, "username": "u", "password": "secret"}, 200)
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
	mustJSON(t, owner, "POST", "/api/v1/me/exits", map[string]any{"kind": "blackhole"}, 422)
	mustJSON(t, owner, "POST", "/api/v1/me/exits", map[string]any{"kind": "socks5", "address": "127.0.0.1", "port": 1080}, 422)
	for i := 0; i < 2; i++ {
		mustJSON(t, owner, "POST", "/api/v1/me/chains", map[string]any{"name": "共用", "entry_node_id": n["id"], "exit_id": id}, 200)
	}
	if refs, err := s.exitReferences(t.Context(), id); err != nil || len(refs) != 2 {
		t.Fatalf("references=%v err=%v", refs, err)
	}
	mustJSON(t, other, "POST", "/api/v1/me/chains", map[string]any{"name": "越权", "entry_node_id": n["id"], "exit_id": id}, 422)
	mustJSON(t, owner, "DELETE", "/api/v1/me/exits/"+itoa(id), nil, 409)
}
