package center

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/audit"
	"github.com/shusfun/weir/packages/contracts"
)

func TestCustomEntryMigration(t *testing.T) {
	for _, mode := range []string{"", "bypass", "only"} {
		raw := map[string]any{"custom": map[string]any{"mode": mode, "source": "a.example\nb.example", "lines": []string{"DOMAIN-SUFFIX,a.example", "DOMAIN-SUFFIX,b.example"}}, "rules": map[string]string{"ai": "auto"}}
		p := loadPolicy(&ent.User{ProxyPolicy: raw})
		if p.Version != 3 || len(p.Custom.Entries) != 2 {
			t.Fatal(p)
		}
		for _, e := range p.Custom.Entries {
			if e.ID == "" || e.Enabled != (mode != "") || (mode == "only") != (e.Target == "proxy") {
				t.Fatal(mode, e)
			}
		}
		if policyFingerprint(loadPolicy(&ent.User{ProxyPolicy: policyMap(p)})) != policyFingerprint(p) {
			t.Fatal("迁移不幂等")
		}
	}
	r := parseCustomEntries("example.com\nDOMAIN,child.example.com\n1.2.3.0/25\n1.2.3.128/25\nexample.com\nGEOSITE,cn")
	if len(r.Rules) != 4 || r.Reduced != 1 || len(r.Unused) != 1 || r.Rules[1].Kind != "domain" {
		t.Fatalf("ordered parse: %+v", r)
	}
	failed := loadPolicy(&ent.User{ProxyPolicy: map[string]any{"custom": map[string]any{"source": "failed.example", "lines": []string{"DOMAIN-SUFFIX,valid.example"}, "check": map[string]any{"status": "failed"}}}})
	if failed.Custom.Pending == nil || failed.Custom.Entries[0].Value != "valid.example" || failed.Custom.Entries[0].Enabled {
		t.Fatal(failed)
	}
}

func TestCustomEntriesFlow(t *testing.T) {
	s, admin, g := setupGate(t)
	mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": int(g.id), "preset": "vless-reality", "skip_probe": true}, 200)
	u := mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "entries@example.test", "password": "Entries2026", "all_nodes": true}, 200)
	uid := int(u["id"].(float64))
	owner := login(t, s, "entries@example.test", "Entries2026")
	t.Cleanup(func() { subCheckBinary, subCheckRun = defaultSubCheckBinary, runSubCheck })
	subCheckBinary = func(*Server, context.Context, string) (string, string, error) { return "checker", "entries-test", nil }
	fail := false
	subCheckRun = func(context.Context, string, string, string, []byte) ([]byte, error) {
		if fail {
			return []byte("invalid candidate test"), errors.New("exit status 1")
		}
		return nil, nil
	}
	root := "/api/v1/proxy/custom"
	view := mustJSON(t, owner, "POST", root+"/append", map[string]any{"revision": 0, "source": "example.com\nDOMAIN,child.example.com\nGEOSITE,cn", "target": "direct"}, 200)
	c := view["custom"].(map[string]any)
	rows := c["entries"].([]any)
	if len(rows) != 2 || c["revision"] != float64(1) || view["parsed"].(map[string]any)["total"] != float64(3) {
		t.Fatalf("append: %v", view)
	}
	first, second := rows[0].(map[string]any)["id"].(string), rows[1].(map[string]any)["id"].(string)
	revision := 1
	rowURL := func(id string) string { return root + "/entries/" + id }
	patch := func(id string, data map[string]any) map[string]any {
		data["revision"] = revision
		v := mustJSON(t, owner, "PATCH", rowURL(id), data, 200)
		revision = int(v["custom"].(map[string]any)["revision"].(float64))
		return v["custom"].(map[string]any)
	}
	test := func(id, target string) map[string]any {
		return mustJSON(t, owner, "POST", rowURL(id)+"/test", map[string]any{"revision": revision, "target": target}, 200)
	}
	if r := test(second, "child.example.com"); r["entry_matches"] != true || r["match"].(map[string]any)["entry_id"] != first {
		t.Fatalf("shadowed: %v", r)
	}
	patch(second, map[string]any{"target": "proxy", "position": 0})
	if r := test(second, "child.example.com"); r["match"].(map[string]any)["entry_id"] != second || r["match"].(map[string]any)["policy"] != "PROXY" {
		t.Fatalf("ordered: %v", r)
	}
	patch(second, map[string]any{"target": "direct"})
	if r := patch(second, map[string]any{"target": "proxy"}); r["entries"].([]any)[0].(map[string]any)["target"] != "proxy" {
		t.Fatalf("restore group: %v", r)
	}
	patch(second, map[string]any{"enabled": false})
	if r := test(second, "child.example.com"); r["entry_enabled"] != false || r["match"].(map[string]any)["entry_id"] != first {
		t.Fatalf("disabled: %v", r)
	}
	// 重复追加不改变条目走向，且不会跨规则合并。
	view = mustJSON(t, owner, "POST", root+"/append", map[string]any{"revision": revision, "source": "example.com\n8.8.8.8", "target": "proxy"}, 200)
	c = view["custom"].(map[string]any)
	revision = int(c["revision"].(float64))
	if len(c["entries"].([]any)) != 3 || c["entries"].([]any)[1].(map[string]any)["target"] != "direct" {
		t.Fatalf("dedup: %v", c)
	}
	// 保存失败保留有效条目和完整候选；同一种方式的失败也必须可见。
	fail = true
	mustJSON(t, owner, "PATCH", rowURL(first), map[string]any{"revision": revision, "value": "failed.example"}, 422)
	c = mustJSON(t, owner, "GET", "/api/v1/proxy", nil, 200)["custom"].(map[string]any)
	revision = int(c["revision"].(float64))
	if c["pending"] == nil || c["entries"].([]any)[1].(map[string]any)["value"] != "example.com" {
		t.Fatalf("failed candidate: %v", c)
	}
	fail = false
	view = mustJSON(t, owner, "PUT", root, map[string]any{"revision": revision, "entries": c["pending"].(map[string]any)["entries"]}, 200)
	c = view["custom"].(map[string]any)
	revision = int(c["revision"].(float64))
	if c["pending"] != nil || c["entries"].([]any)[1].(map[string]any)["value"] != "failed.example" {
		t.Fatalf("retry: %v", c)
	}
	mustJSON(t, owner, "PATCH", rowURL(first), map[string]any{"revision": revision - 1, "enabled": false}, 409)
	mustJSON(t, owner, "PATCH", rowURL(first), map[string]any{"enabled": false}, 409)
	mustJSON(t, owner, "PATCH", rowURL(first), map[string]any{"revision": revision, "target": "not-authorized"}, 400)
	mustJSON(t, owner, "PATCH", rowURL(first), map[string]any{"revision": revision, "kind": "domain_suffix", "value": "8.8.8.8\nMATCH,DIRECT"}, 400)
	mustJSON(t, owner, "POST", rowURL(first)+"/test", map[string]any{"revision": revision, "target": "??"}, 400)
	mustJSON(t, owner, "POST", rowURL(first)+"/test", map[string]any{"revision": revision, "target": "example.com", "entry": map[string]any{"kind": "domain", "value": "example.com", "target": "not-authorized", "enabled": true}}, 400)
	mustJSON(t, owner, "POST", root+"/test", map[string]any{"revision": revision, "target": "example.com", "entries": make([]contracts.CustomEntry, customMaxRules+1)}, 400)
	r := mustJSON(t, owner, "POST", rowURL(first)+"/test", map[string]any{"revision": revision, "target": "draft.example", "entry": map[string]any{"kind": "domain", "value": "draft.example", "target": "direct", "enabled": true}}, 200)
	if r["entry_matches"] != true || r["match"].(map[string]any)["policy"] != "DIRECT" {
		t.Fatalf("draft test: %v", r)
	}
	mustJSON(t, owner, "POST", root+"/parse", map[string]any{"source": strings.Repeat("x", customMaxSource+1)}, 400)
	// 普通用户不能通过管理员路径读写别人的清单。
	mustJSON(t, owner, "PATCH", "/api/v1/proxy/custom/entries/"+first+"?user=1", map[string]any{"revision": revision, "enabled": false}, 403)
	view = mustJSON(t, admin, "GET", "/api/v1/proxy?user="+itoa(uid), nil, 200)
	if view["custom"].(map[string]any)["revision"] != float64(revision) {
		t.Fatal("admin sees different list")
	}
	view = mustJSON(t, owner, "DELETE", rowURL(first), map[string]any{"revision": revision}, 200)
	if len(view["custom"].(map[string]any)["entries"].([]any)) != 2 {
		t.Fatalf("delete: %v", view)
	}
	token := mustJSON(t, owner, "GET", "/api/v1/me/links", nil, 200)["items"].([]any)[0].(map[string]any)["token"].(string)
	w := owner.do("GET", "/sub/"+token, nil, map[string]string{"User-Agent": "clash.meta"})
	if strings.Contains(w.Body.String(), "RULE-SET,weir-custom") || !strings.Contains(w.Body.String(), "IP-CIDR,8.8.8.8/32,PROXY") || w.Header().Get("Profile-Update-Interval") != "1" {
		t.Fatalf("inline subscription: %s", w.Body.String())
	}
	a := s.Store.Ent.Audit.Query().Where(audit.ActionEQ("proxy.custom_rules")).FirstX(t.Context())
	if a.Detail["trace_id"] == "" || a.Detail["request_id"] == "" {
		t.Fatalf("missing trace: %v", a.Detail)
	}
}

func TestCustomEntriesRejectConcurrentPolicyChange(t *testing.T) {
	s, admin, g := setupGate(t)
	mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": int(g.id), "preset": "vless-reality", "skip_probe": true}, 200)
	t.Cleanup(func() { subCheckBinary, subCheckRun = defaultSubCheckBinary, runSubCheck })
	subCheckBinary = func(*Server, context.Context, string) (string, string, error) { return "checker", "conflict-test", nil }
	changed := false
	subCheckRun = func(ctx context.Context, _ string, _ string, _ string, _ []byte) ([]byte, error) {
		if !changed {
			changed = true
			u := s.Store.Ent.User.GetX(ctx, 1)
			p := loadPolicy(u)
			p.HiddenLines = []string{"node:1"}
			u.Update().SetProxyPolicy(policyMap(p)).ExecX(ctx)
		}
		return nil, nil
	}
	mustJSON(t, admin, "POST", "/api/v1/proxy/custom/append", map[string]any{"revision": 0, "source": "concurrent.example", "target": "direct"}, 409)
	u := s.Store.Ent.User.GetX(t.Context(), 1)
	if p := loadPolicy(u); len(p.Custom.Entries) != 0 || len(p.HiddenLines) != 1 {
		t.Fatalf("lost settings: %+v", p)
	}
}

func TestCustomEntriesUnknownStillSaves(t *testing.T) {
	_, owner, _ := setupGate(t)
	v := mustJSON(t, owner, "POST", "/api/v1/proxy/custom/append", map[string]any{"revision": 0, "source": "unknown.example", "target": "direct"}, 200)
	c := v["custom"].(map[string]any)
	if c["check"].(map[string]any)["status"] != "unknown" || c["pending"] != nil || len(c["entries"].([]any)) != 1 {
		t.Fatalf("unknown must save effective entries: %v", c)
	}
	id := c["entries"].([]any)[0].(map[string]any)["id"].(string)
	v = mustJSON(t, owner, "PATCH", "/api/v1/proxy/custom/entries/"+id, map[string]any{"revision": 1, "enabled": false}, 200)
	c = v["custom"].(map[string]any)
	if c["check"].(map[string]any)["status"] != "unknown" || c["entries"].([]any)[0].(map[string]any)["enabled"] != false {
		t.Fatalf("unknown single save: %v", c)
	}
}
