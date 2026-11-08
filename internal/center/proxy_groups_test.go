package center

import (
	"context"
	"encoding/json"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shusfun/weir/internal/store/ent/audit"
	"github.com/shusfun/weir/internal/store/ent/credential"
	"github.com/shusfun/weir/internal/store/ent/ratesample"
	"github.com/shusfun/weir/internal/store/ent/sample"
	"github.com/shusfun/weir/packages/contracts"
)

func TestProxyTargetsMigration(t *testing.T) {
	p := contracts.ProxyPolicy{Custom: contracts.CustomRules{Version: 2, Entries: []contracts.CustomEntry{{ID: "a", Target: "group:2", Enabled: true}, {ID: "b", Target: "reject", Value: "keep.example", Enabled: true}}, Pending: &contracts.CustomCandidate{Entries: []contracts.CustomEntry{{ID: "c", Target: "node:9", Enabled: true}}}}}
	p = migrateProxyTargets(p)
	if p.Version != 3 || p.Custom.Entries[0].Target != "proxy" || p.Custom.Entries[1].Enabled || p.Custom.Entries[1].Notice != "已不支持拒绝" || p.Custom.Pending.Entries[0].Target != "proxy" {
		t.Fatal(p)
	}
	if !reflect.DeepEqual(p, migrateProxyTargets(p)) {
		t.Fatal("迁移不幂等")
	}
	p.TargetMigration = nil
	if len(migrateProxyTargets(p).TargetMigration) > 0 {
		t.Fatal("重复提示")
	}
}

func TestProxyGroupValidation(t *testing.T) {
	g := contracts.ProxyGroup{ID: 1, Name: "主备", Kind: "fallback", Members: []string{"node:1", "chain:2"}, IntervalSeconds: 60}
	for _, tt := range []struct {
		name, field string
		change      func(*contracts.ProxyGroup)
	}{
		{"空名", "name", func(g *contracts.ProxyGroup) { g.Name = "" }},
		{"内置重名", "name", func(g *contracts.ProxyGroup) { g.Name = "DIRECT" }},
		{"重名", "name", func(g *contracts.ProxyGroup) { g.Name = "已有" }},
		{"空成员", "members", func(g *contracts.ProxyGroup) { g.Members = nil }},
		{"组嵌套", "members", func(g *contracts.ProxyGroup) { g.Members = []string{"group:2"} }},
		{"未授权", "members", func(g *contracts.ProxyGroup) { g.Members = []string{"node:9"} }},
		{"重复", "members", func(g *contracts.ProxyGroup) { g.Members = []string{"node:1", "node:1"} }},
		{"过多", "members", func(g *contracts.ProxyGroup) { g.Members = make([]string, 51) }},
		{"方式", "kind", func(g *contracts.ProxyGroup) { g.Kind = "load-balance" }},
		{"容差", "tolerance_ms", func(g *contracts.ProxyGroup) { g.ToleranceMS = -1 }},
		{"间隔", "interval_seconds", func(g *contracts.ProxyGroup) { g.IntervalSeconds = 29 }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			candidate := g
			tt.change(&candidate)
			field, _ := validateProxyGroup(candidate, contracts.ProxyPolicy{Groups: []contracts.ProxyGroup{{ID: 2, Name: "已有"}}}, map[string]bool{"node:1": true, "chain:2": true, "group:2": true})
			if field != tt.field {
				t.Fatalf("字段=%s", field)
			}
		})
	}
	if f, d := validateProxyGroup(g, contracts.ProxyPolicy{}, map[string]bool{"node:1": true, "chain:2": true}); f != "" {
		t.Fatal(f, d)
	}
}

func TestProxyGroupsHTTPFlow(t *testing.T) {
	s, admin, gate := setupGate(t)
	ctx := t.Context()
	n := mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": gate.id, "preset": "ss2022", "skip_probe": true}, 200)
	nid := int(n["id"].(float64))
	user := mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "groups@example.test", "password": "Groups2026", "all_nodes": true}, 200)
	uid := int(user["id"].(float64))
	owner := login(t, s, "groups@example.test", "Groups2026")
	buf := captureLog(t)
	trace := "0123456789abcdef0123456789abcdef"
	headers := map[string]string{"traceparent": "00-" + trace + "-1111111111111111-01"}
	body := map[string]any{"revision": 0, "name": "测试组", "members": []string{"node:" + itoa(nid)}}
	w := owner.do("POST", "/api/v1/proxy/groups", body, headers)
	if w.Code != 200 || w.Header().Get("X-Trace-Id") != trace {
		t.Fatal(w.Code, w.Body)
	}
	created := decode(t, w)
	id := int(created["id"].(float64))
	layout := created["layout"].([]any)
	selection := layout[2].(map[string]any)
	automatic := layout[3].(map[string]any)
	if selection["kind"] != "select" || selection["hidden"] != false || !reflect.DeepEqual(selection["member_refs"], []any{"group:" + itoa(id) + ":auto", "node:" + itoa(nid)}) || automatic["kind"] != "url-test" || automatic["hidden"] != true {
		t.Fatalf("默认延迟组必须同时提供自动和手动选择：%v", layout)
	}
	p := loadPolicy(s.Store.Ent.User.GetX(ctx, uid))
	if p.Revision != 1 || p.Groups[0].Kind != "url-test" || p.Groups[0].ToleranceMS != 50 {
		t.Fatal(p)
	}
	a := s.Store.Ent.Audit.Query().Where(audit.ActionEQ("proxy.group.post")).OnlyX(ctx)
	if a.Detail["trace_id"] != trace || !strings.Contains(buf.String(), "status=200") {
		t.Fatal("trace缺失")
	}
	mustJSON(t, owner, "PUT", "/api/v1/proxy?user=1", body, 403)
	mustJSON(t, owner, "POST", "/api/v1/proxy/groups", body, 409)
	// 同一并发版本只能有一个写入成功。
	body["revision"] = 1
	body["name"] = "管理员改名"
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		codes <- admin.do("PUT", "/api/v1/proxy/groups/"+itoa(id)+"?user="+itoa(uid), body, nil).Code
	}()
	go func() {
		defer wg.Done()
		codes <- owner.do("PUT", "/api/v1/proxy", map[string]any{"revision": 1, "hidden_lines": []string{"node:" + itoa(nid)}, "auto": map[string]any{"refresh_minutes": 60}}, nil).Code
	}()
	wg.Wait()
	close(codes)
	success, conflict := 0, 0
	for code := range codes {
		if code == 200 {
			success++
		}
		if code == 409 {
			conflict++
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal("并发未受保护", success, conflict)
	}
	p = loadPolicy(s.Store.Ent.User.GetX(ctx, uid))
	mustJSON(t, owner, "DELETE", "/api/v1/proxy/groups/"+itoa(id), map[string]any{"revision": p.Revision}, 200)
	p = loadPolicy(s.Store.Ent.User.GetX(ctx, uid))
	body["revision"] = p.Revision
	body["name"] = "第二组"
	v := mustJSON(t, owner, "POST", "/api/v1/proxy/groups", body, 200)
	if int(v["id"].(float64)) <= id {
		t.Fatal("编号被复用")
	}
	s.Store.Ent.Gate.UpdateOneID(int(gate.id)).SetStatus("revoked").ExecX(ctx)
	v = mustJSON(t, owner, "GET", "/api/v1/proxy", nil, 200)
	if v["groups"].([]any)[0].(map[string]any)["valid_members"] != float64(0) {
		t.Fatal("撤销后仍输出成员")
	}
}

func TestProxySameNameMembersInViewAndPlan(t *testing.T) {
	s, admin, gate := setupGate(t)
	ctx := t.Context()
	refs := []string{}
	entries := []Entry{}
	for i := 0; i < 2; i++ {
		v := mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": gate.id, "preset": "ss2022", "name": "美东", "skip_probe": true}, 200)
		id := int(v["id"].(float64))
		ref := "node:" + itoa(id)
		refs = append(refs, ref)
		entries = append(entries, Entry{NodeID: id, Ref: ref, Name: "美东", Type: "ss2022", URI: "socks5://test:secret@203.0.113.1:1080#美东"})
	}
	u := mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "same-name@example.test", "password": "SameName2026", "all_nodes": true}, 200)
	uid := int(u["id"].(float64))
	view := mustJSON(t, admin, "POST", "/api/v1/proxy/groups?user="+itoa(uid), map[string]any{"revision": 0, "name": "双线路", "kind": "select", "members": refs}, 200)
	layout := view["layout"].([]any)
	root := layout[0].(map[string]any)
	rootRefs := root["member_refs"].([]any)
	if len(rootRefs) != 2 || rootRefs[0] != "auto" || rootRefs[1] != "group:1" {
		t.Fatalf("同名成员错误地单独输出：%v", rootRefs)
	}
	group := layout[2].(map[string]any)
	memberRefs := group["member_refs"].([]any)
	members := group["members"].([]any)
	if len(memberRefs) != 2 || memberRefs[0] != refs[0] || memberRefs[1] != refs[1] || members[0] == members[1] {
		t.Fatalf("页面布局丢失或混淆同名线路：%v", group)
	}
	user := s.Store.Ent.User.GetX(ctx, uid)
	planned, err := s.planEntries(ctx, user, entries, false)
	if err != nil {
		t.Fatal(err)
	}
	plan, items := s.subPlan(ctx, planned)
	names := []string{"美东", "美东（" + refs[1] + "）"}
	groups, _ := plan.Layout(names, items)
	if !reflect.DeepEqual(groups[2].MemberRefs, refs) || len(groups[0].MemberRefs) != 2 {
		t.Fatalf("真实计划丢失同名线路：%+v", groups)
	}
	path := "/api/v1/proxy/groups/1?user=" + itoa(uid)
	invalid := mustJSON(t, admin, "PUT", path, map[string]any{"revision": 1, "name": "双线路", "kind": "select", "members": []string{}}, 400)
	if invalid["error"].(map[string]any)["code"] != "invalid_group_members" || loadPolicy(s.Store.Ent.User.GetX(ctx, uid)).Revision != 1 {
		t.Fatal("移除最后成员必须拒绝且不写入", invalid)
	}
	actor, err := s.systemActor(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{"PUT", "DELETE"} {
		code, result := call(actor, func(c *gin.Context) { s.writeProxyGroup(c, false) }, method, gin.Params{{Key: "group", Value: "1"}}, url.Values{"user": {itoa(uid)}}, map[string]any{"revision": 1, "name": "试运行", "kind": "select", "members": refs[:1]}, true)
		if code != 200 || result["dry_run"] == nil || loadPolicy(s.Store.Ent.User.GetX(ctx, uid)).Revision != 1 {
			t.Fatalf("分组 %s 试运行写入：%d %v", method, code, result)
		}
	}
	user.Update().SetAllNodes(false).SetNodeIds([]int{entries[0].NodeID}).ExecX(ctx)
	planned, err = s.planEntries(ctx, user, entries, false)
	if err != nil {
		t.Fatal(err)
	}
	plan, items = s.subPlan(ctx, planned)
	groups, _ = plan.Layout(names[:1], items)
	if len(items) != 1 || !reflect.DeepEqual(groups[1].MemberRefs, refs[:1]) || !reflect.DeepEqual(groups[2].MemberRefs, refs[:1]) || len(groups[0].MemberRefs) != 2 {
		t.Fatalf("重新授权后仍输出失效引用：%+v", groups)
	}
	view = mustJSON(t, admin, "GET", "/api/v1/proxy?user="+itoa(uid), nil, 200)
	membersView := view["groups"].([]any)[0].(map[string]any)["members"].([]any)
	if membersView[1].(map[string]any)["available"] != false || membersView[1].(map[string]any)["reason"] == "" {
		t.Fatal("失效成员缺少原因", membersView)
	}
}

func TestClientIdentificationHistory(t *testing.T) {
	for ua, want := range map[string]string{
		"FlClash/v0.9.2 clash-verge Platform/windows": "FlClash", "FlClash/v0.9.3 clash-verge Platform/android": "FlClash",
		"Clash-Verge-Rev/2.4 clash.meta": "Clash Verge Rev", "Clash Party/1.0 mihomo": "Clash Party", "mihomo.party/1.0 clash-verge": "Mihomo Party", "FlyClash/1.0 clash-verge": "FlyClash", "ClashMetaForAndroid/2 mihomo": "Clash Meta for Android", "nyanpasu clash-verge": "Clash Nyanpasu", "Stash/3 clash": "Stash", "Shadowrocket/2.2": "Shadowrocket", "": "",
	} {
		if got := clientName(ua); got != want {
			t.Errorf("%s = %q，期望 %q", ua, got, want)
		}
	}
	s, admin := newInstalledServer(t)
	ctx := t.Context()
	links := mustJSON(t, admin, "GET", "/api/v1/me/links", nil, 200)["items"].([]any)
	lid := int(links[0].(map[string]any)["id"].(float64))
	row := s.Store.Ent.SubPull.Create().SetLinkID(lid).SetUserID(1).SetAt(time.Now()).SetUserAgent("FlClash/v0.9.3 clash-verge Platform/android").SetClient("Clash Verge").SetStatus("ok").SaveX(ctx)
	v := mustJSON(t, admin, "GET", "/api/v1/me/link-stats", nil, 200)["items"].([]any)[0].(map[string]any)
	if v["clients"].([]any)[0].(map[string]any)["name"] != "FlClash" || v["last_pull"].(map[string]any)["client"] != "FlClash" {
		t.Fatalf("历史识别=%v", v)
	}
	if s.Store.Ent.SubPull.GetX(ctx, row.ID).Client != "Clash Verge" {
		t.Fatal("修改了历史行")
	}
}

func TestProxyGroupCapabilities(t *testing.T) {
	s, admin, gate := setupGate(t)
	ctx := t.Context()
	n := mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": int(gate.id), "preset": "ss2022", "skip_probe": true}, 200)
	u := mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "group-draft@example.test", "password": "GroupDraft2026", "all_nodes": true}, 200)
	uid := int(u["id"].(float64))
	m, _ := s.ensureMCP()
	h := s.mcpHandler(m)
	args := map[string]any{"revision": 0, "user_id": uid, "name": "草稿主备", "members": "node:" + itoa(int(n["id"].(float64)))}
	text, bad := toolText(t, h, m.webToken, "submit_draft", map[string]any{"explain": "建立主备组", "steps": []any{map[string]any{"action": "proxy.group_create", "args": args}}})
	if bad {
		t.Fatal(text)
	}
	if len(loadPolicy(s.Store.Ent.User.GetX(ctx, uid)).Groups) != 0 {
		t.Fatal("预演写入了分组")
	}
	d := mustJSON(t, admin, "GET", "/api/v1/admin/ai/drafts", nil, 200)["drafts"].([]any)[0].(map[string]any)
	mustJSON(t, admin, "POST", "/api/v1/admin/ai/drafts/"+d["id"].(string)+"/apply", nil, 200)
	p := loadPolicy(s.Store.Ent.User.GetX(ctx, uid))
	if len(p.Groups) != 1 || p.Groups[0].Name != "草稿主备" {
		t.Fatalf("确认后=%+v", p.Groups)
	}
	id := p.Groups[0].ID
	ref, ok := s.resolveObject(ctx, "proxy_group", itoa(uid)+":"+itoa(id))
	if !ok || ref.Owner != uid || ref.Name != "草稿主备" {
		t.Fatalf("复合对象引用=%+v %v", ref, ok)
	}
	if _, ok := s.resolveObject(ctx, "proxy_group", "999999:"+itoa(id)); ok {
		t.Fatal("用户局部编号串到了其他用户")
	}
	actor := systemActor(ctx)
	actor.Set("account", s.Store.Ent.User.GetX(ctx, 1))
	x := &execCtx{actor: actor, created: map[string]int{}, tags: map[string]string{}}
	update, _ := findOpCap("proxy.group_update")
	undo, err := update.exec(ctx, s, x, &DraftStep{Args: map[string]any{"revision": p.Revision, "user_id": uid, "group_id": id, "name": "改名", "interval_seconds": 120}})
	if err != nil || undo == nil {
		t.Fatalf("更新=%v", err)
	}
	if err := undo(ctx); err != nil {
		t.Fatal(err)
	}
	g := loadPolicy(s.Store.Ent.User.GetX(ctx, uid)).Groups[0]
	if g.Name != "草稿主备" || g.IntervalSeconds != 60 || !reflect.DeepEqual(g.Members, p.Groups[0].Members) {
		t.Fatalf("更新撤回=%+v", g)
	}
	create, _ := findOpCap("proxy.group_create")
	args["name"] = "可撤回的新组"
	args["revision"] = loadPolicy(s.Store.Ent.User.GetX(ctx, uid)).Revision
	undo, err = create.exec(ctx, s, x, &DraftStep{Args: args})
	if err != nil || undo == nil {
		t.Fatalf("新建=%v", err)
	}
	if err := undo(ctx); err != nil {
		t.Fatal(err)
	}
	if groups := loadPolicy(s.Store.Ent.User.GetX(ctx, uid)).Groups; len(groups) != 1 || groups[0].ID != id {
		t.Fatalf("新建撤回=%+v", groups)
	}
}

func TestEffectiveMinuteBoundary(t *testing.T) {
	s, _ := newInstalledServer(t)
	ctx := t.Context()
	at := minuteOf(time.Now()).Add(-time.Minute)
	for i, bytes := range []int64{(1 << 20) - 1, 1 << 20, 2 << 20} {
		s.Store.Ent.RateSample.Create().SetUserID(1).SetNodeID(i + 1).SetAt(at).SetBytes(bytes).SetSeconds(20).ExecX(ctx)
	}
	s.Store.Ent.RateSample.Create().SetUserID(1).SetNodeID(4).SetAt(minuteOf(time.Now())).SetBytes(10 << 20).SetSeconds(20).ExecX(ctx)
	r, err := s.userRates(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(r) != 2 || r[pathKey(2, 0)].P90 != float64(1<<20)/20 || r[pathKey(4, 0)].known() {
		t.Fatalf("边界=%+v", r)
	}
	// 同一分钟的两次小增量累计达到阈值后才参与，不按单次上报过滤。
	s.Store.Ent.RateSample.Create().SetUserID(1).SetNodeID(5).SetAt(at).SetBytes(512<<10).SetSeconds(10).OnConflictColumns(ratesample.FieldUserID, ratesample.FieldNodeID, ratesample.FieldChainID, ratesample.FieldAt).AddBytes(512 << 10).AddSeconds(10).ExecX(ctx)
	s.Store.Ent.RateSample.Create().SetUserID(1).SetNodeID(5).SetAt(at).SetBytes(512<<10).SetSeconds(10).OnConflictColumns(ratesample.FieldUserID, ratesample.FieldNodeID, ratesample.FieldChainID, ratesample.FieldAt).AddBytes(512 << 10).AddSeconds(10).ExecX(ctx)
	r, err = s.userRates(ctx, 1)
	if err != nil || r[pathKey(5, 0)].Minutes != 1 || r[pathKey(5, 0)].P90 != float64(1<<20)/20 {
		t.Fatalf("累计=%+v %v", r, err)
	}
	if got := p90([]float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}); got != 9 {
		t.Fatal(got)
	}
}

func TestIdleMeterAndLiveUnknown(t *testing.T) {
	s, admin, g := setupGate(t)
	ctx := t.Context()
	mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": int(g.id), "preset": "ss2022", "skip_probe": true}, 200)
	mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "idle@example.test", "password": "Idle2026", "all_nodes": true}, 200)
	report := func(seq int64, status string) {
		t.Helper()
		if code, b := g.call("POST", "/api/v1/agent/meter", contracts.MeterReport{Epoch: "idle", Seq: seq, Status: status}); code != 200 {
			t.Fatalf("%d %s", code, b)
		}
	}
	report(1, "unavailable")
	if s.Store.Ent.Sample.Query().Where(sample.ScopeEQ(sample.ScopeUser)).CountX(ctx) != 0 {
		t.Fatal("不可用上报产生样本")
	}
	report(2, "fresh")
	if rows := s.Store.Ent.Sample.Query().Where(sample.ScopeEQ(sample.ScopeUser)).AllX(ctx); len(rows) != 2 || rows[0].Traffic != 0 || rows[1].Traffic != 0 {
		t.Fatalf("空闲=%+v", rows)
	}
	if points, err := s.history(ctx, sample.ScopeUser, 2, 1); err != nil || points[len(points)-1].Traffic == nil || *points[len(points)-1].Traffic != 0 || points[0].Traffic != nil {
		t.Fatalf("曲线=%+v %v", points, err)
	}
	// 事务失败时撤回已写的样本和累计计数。
	s.Store.Ent.Sample.Delete().ExecX(ctx)
	cred := s.Store.Ent.Credential.Query().Where(credential.UserIDEQ(2)).OnlyX(ctx)
	gate := s.Store.Ent.Gate.GetX(ctx, int(g.id))
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.applyMeter(cancelled, gate, contracts.MeterReport{Epoch: "failure", Seq: 3, Status: "fresh", Usage: []contracts.MeterSample{{StatsKey: cred.StatsKey, Down: 1}}}); err == nil {
		t.Fatal("取消应失败")
	}
	if s.Store.Ent.Sample.Query().Where(sample.ScopeEQ(sample.ScopeUser)).CountX(ctx) != 0 {
		t.Fatal("失败产生假零")
	}
	var hub liveHub
	now := time.Now()
	for i, users := range [][]contracts.MeterSample{{{StatsKey: "idle"}}, {{StatsKey: "idle"}}, nil, {{StatsKey: "idle", Down: 10}}, {{StatsKey: "idle", Down: 1}}} {
		hub.ingest(1, contracts.LiveReport{At: now.Add(time.Duration(i) * 2 * time.Second), Users: users})
	}
	frames := hub.frames(1, time.Time{})
	if len(frames) != 4 {
		t.Fatal(len(frames))
	}
	if r, ok := frames[0].Keys["idle"]; !ok || r.Down != 0 {
		t.Fatal("已知空闲应为零")
	}
	for _, f := range frames[1:] {
		if _, ok := f.Keys["idle"]; ok {
			t.Fatal("缺失、首次或回退应未知")
		}
	}
	_, _ = json.Marshal(frames)
}
