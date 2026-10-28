package center

import (
	"strings"
	"testing"

	"github.com/shusfun/weir/internal/store/ent/audit"
	"github.com/shusfun/weir/internal/store/ent/link"
	"github.com/shusfun/weir/internal/store/ent/user"
)

func TestMultiStepDraftRefsEditsAndRollback(t *testing.T) {
	s, admin := newInstalledServer(t)
	m, _ := s.ensureMCP()
	h := s.mcpHandler(m)
	ctx := t.Context()
	latest := func() map[string]any {
		list := mustJSON(t, admin, "GET", "/api/v1/admin/ai/drafts", nil, 200)["drafts"].([]any)
		return list[len(list)-1].(map[string]any)
	}
	submit := func(steps []any) (string, bool) {
		return toolText(t, h, m.webToken, "submit_draft", map[string]any{"explain": "开用户并建两个链接", "steps": steps})
	}
	// 后面的步骤引用前面新建的对象；引用不存在的名字、类型不对时交回原因，不弹窗。
	if text, isErr := submit([]any{map[string]any{"action": "link.create", "args": map[string]any{"user_id": "$nobody"}}}); !isErr || !strings.Contains(text, "$nobody") {
		t.Fatalf("bad ref = %q", text)
	}
	if text, isErr := submit([]any{map[string]any{"action": "admin.role", "args": map[string]any{}}}); !isErr || !strings.Contains(text, "没有能力") {
		t.Fatalf("action outside the catalog = %q", text)
	}
	steps := []any{
		map[string]any{"action": "user.create", "as": "xm", "args": map[string]any{"email": "xm@example.test", "name": "小明", "quota_gb": 20, "expires_days": 30}},
		map[string]any{"action": "link.create", "args": map[string]any{"user_id": "$xm", "name": "手机"}},
		map[string]any{"action": "link.create", "args": map[string]any{"user_id": "$xm", "name": "电脑"}},
	}
	if text, isErr := submit(steps); isErr {
		t.Fatal(text)
	}
	d := latest()
	first := d["steps"].([]any)[0].(map[string]any)
	// 确认窗字段带中文名与单位。
	for _, f := range first["fields"].([]any) {
		if fm := f.(map[string]any); fm["key"] == "quota_gb" && (fm["label"] != "流量额度" || fm["display"] != "20 GB") {
			t.Fatalf("field = %v", fm)
		}
	}
	// 管理员在确认窗里把额度改成 5 GB：改过的字段做标记并重新试运行，整体生效。
	res := mustJSON(t, admin, "POST", "/api/v1/admin/ai/drafts/"+d["id"].(string)+"/apply", map[string]any{"edits": map[string]any{"0": map[string]any{"quota_gb": 5}}}, 200)
	u, err := s.Store.Ent.User.Query().Where(user.EmailEQ("xm@example.test")).Only(ctx)
	if err != nil || u.Quota != 5<<30 {
		t.Fatalf("user = %+v %v", u, err)
	}
	// 新用户默认一条链接 + 草稿建的两条；初始密码只交给确认者。
	if n, _ := s.Store.Ent.Link.Query().Where(link.UserIDEQ(u.ID)).Count(ctx); n != 3 {
		t.Fatalf("links = %d", n)
	}
	if out := res["outputs"].([]any); len(out) != 1 || out[0].(map[string]any)["password"] == "" {
		t.Fatalf("outputs = %v", res["outputs"])
	}
	a, err := s.Store.Ent.Audit.Query().Where(audit.ActionEQ("ai.apply")).Only(ctx)
	if err != nil || !strings.Contains(a.Target, "dsh 草稿 · 管理员确认") || a.Detail["steps"] == nil {
		t.Fatalf("audit = %+v %v", a, err)
	}
	if edited := a.Detail["steps"].([]any)[0].(map[string]any)["edited"].([]any); len(edited) != 1 || edited[0] != "quota_gb" {
		t.Fatalf("audit edited = %v", a.Detail["steps"])
	}

	// 任一步失败全部不生效：第二步新建同一个邮箱会失败，第一步建的用户被撤回。
	dup := []any{
		map[string]any{"action": "user.create", "as": "a", "args": map[string]any{"email": "twin@example.test"}},
		map[string]any{"action": "user.create", "args": map[string]any{"email": "twin@example.test"}},
	}
	if text, isErr := submit(dup); isErr {
		t.Fatal(text)
	}
	d = latest()
	w := admin.do("POST", "/api/v1/admin/ai/drafts/"+d["id"].(string)+"/apply", nil, nil)
	if w.Code != 422 || !strings.Contains(w.Body.String(), "前面的步骤已撤回") {
		t.Fatalf("failing draft = %d %s", w.Code, w.Body.String())
	}
	if n, _ := s.Store.Ent.User.Query().Where(user.EmailEQ("twin@example.test")).Count(ctx); n != 0 {
		t.Fatal("the first step must be rolled back")
	}

	// 依据的数据已被改动：拒绝确认，让 dsh 重新生成。
	if text, isErr := submit([]any{map[string]any{"action": "user.update", "args": map[string]any{"user_id": u.ID, "quota_gb": 50}}}); isErr {
		t.Fatal(text)
	}
	d = latest()
	mustJSON(t, admin, "PATCH", "/api/v1/admin/users/"+itoa(u.ID), map[string]any{"quota": 7 << 30}, 200)
	if w := admin.do("POST", "/api/v1/admin/ai/drafts/"+d["id"].(string)+"/apply", nil, nil); w.Code != 409 || !strings.Contains(w.Body.String(), "draft_stale") {
		t.Fatalf("stale draft = %d %s", w.Code, w.Body.String())
	}
	// 危险操作（删除用户）要求当前密码。
	if text, isErr := submit([]any{map[string]any{"action": "user.delete", "args": map[string]any{"user_id": u.ID}}}); isErr {
		t.Fatal(text)
	}
	var del map[string]any
	for _, x := range mustJSON(t, admin, "GET", "/api/v1/admin/ai/drafts", nil, 200)["drafts"].([]any) {
		if x.(map[string]any)["danger"] == true {
			del = x.(map[string]any)
		}
	}
	mustJSON(t, admin, "POST", "/api/v1/admin/ai/drafts/"+del["id"].(string)+"/apply", map[string]any{}, 401)
	mustJSON(t, admin, "POST", "/api/v1/admin/ai/drafts/"+del["id"].(string)+"/apply", map[string]any{"password": "Passw0rd"}, 200)
	if n, _ := s.Store.Ent.User.Query().Where(user.EmailEQ("xm@example.test")).Count(ctx); n != 0 {
		t.Fatal("user must be deleted")
	}
}
