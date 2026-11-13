package center

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/shusfun/weir/internal/store"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/link"
	"github.com/shusfun/weir/internal/store/ent/subgrant"
	"github.com/shusfun/weir/internal/store/ent/user"
	"github.com/shusfun/weir/packages/contracts"

	"github.com/shusfun/weir/internal/store/ent/credential"
	"github.com/shusfun/weir/internal/store/ent/resource"
)

func TestUserDeletionRollsBackAndKeepsRevocationJournal(t *testing.T) {
	s, admin, g := setupGate(t)
	mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": g.id, "preset": "vless-reality", "skip_probe": true}, 200)
	u := mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "delete@example.test", "password": "Deletion2026!", "all_nodes": true, "unlimited": true}, 200)
	uid := int(u["id"].(float64))
	ctx := t.Context()
	p := s.Store.Ent.SubProfile.Create().SetName("deletion").SaveX(ctx)
	s.Store.Ent.SubGrant.Create().SetProfileID(p.ID).SetUserID(uid).SetToken("local-deletion-fixture").SaveX(ctx)
	broken := true
	s.Store.Ent.User.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			if broken && m.Op().Is(ent.OpDeleteOne) {
				return nil, errors.New("injected user deletion failure")
			}
			return next.Mutate(ctx, m)
		})
	})
	path := "/api/v1/admin/users/" + itoa(uid)
	if w := admin.do("DELETE", path, nil, nil); w.Code != 503 {
		t.Fatalf("failure status %d", w.Code)
	}
	if !s.Store.Ent.User.Query().Where(user.IDEQ(uid)).ExistX(ctx) {
		t.Fatal("lost account on failed deletion")
	}
	if s.Store.Ent.Credential.Query().Where(credential.UserIDEQ(uid)).CountX(ctx) == 0 || s.Store.Ent.Link.Query().Where(link.UserIDEQ(uid)).CountX(ctx) == 0 || s.Store.Ent.SubGrant.Query().Where(subgrant.UserIDEQ(uid)).CountX(ctx) != 1 {
		t.Fatal("partial deletion committed")
	}
	broken = false
	before := s.Store.Ent.Gate.GetX(ctx, int(g.id)).ConfigRevision
	mustJSON(t, admin, "DELETE", path, nil, 204)
	if s.Store.Ent.Link.Query().Where(link.UserIDEQ(uid)).ExistX(ctx) || s.Store.Ent.Credential.Query().Where(credential.UserIDEQ(uid)).ExistX(ctx) || s.Store.Ent.SubGrant.Query().Where(subgrant.UserIDEQ(uid)).ExistX(ctx) {
		t.Fatal("deleted user resources remain")
	}
	if s.Store.Ent.Gate.GetX(ctx, int(g.id)).ConfigRevision <= before {
		t.Fatal("deletion did not revoke gate configuration")
	}
}

func TestAuthorizationProjectionRevokesWithoutRotatingCredentials(t *testing.T) {
	s, admin, g := setupGate(t)
	n := mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": g.id, "preset": "vless-reality", "skip_probe": true}, 200)
	u := mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "projection@example.test", "password": "Projection2026!", "all_nodes": true, "unlimited": true}, 200)
	uid := int(u["id"].(float64))
	ctx := t.Context()
	if err := s.SyncUser(ctx, uid); err != nil {
		t.Fatal(err)
	}
	creds := s.Store.Ent.Credential.Query().Where(credential.UserIDEQ(uid)).AllX(ctx)
	if len(creds) == 0 {
		t.Fatal("no credential fixture")
	}
	before := s.Store.Ent.Gate.GetX(ctx, int(g.id)).ConfigRevision
	mustJSON(t, admin, "PATCH", fmt.Sprintf("/api/v1/admin/users/%d", uid), map[string]any{"enabled": false}, 200)
	after := s.Store.Ent.Gate.GetX(ctx, int(g.id)).ConfigRevision
	if after <= before {
		t.Fatal("disabled account did not advance config revision")
	}
	cfg, err := s.compileGateConfig(ctx, s.Store.Ent.Gate.GetX(ctx, int(g.id)))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range cfg.Credentials {
		if c.UserKey == userKey(uid) {
			found = true
			if c.Revoke != "disabled" {
				t.Fatal("disabled credential still active")
			}
		}
	}
	if !found {
		t.Fatal("revocation missing")
	}
	if got := s.Store.Ent.Credential.GetX(ctx, creds[0].ID); got.UUID != creds[0].UUID || got.Password != creds[0].Password {
		t.Fatal("disable rotated credentials")
	}
	if err = s.SyncUser(ctx, uid); err != nil {
		t.Fatal(err)
	}
	if current := s.Store.Ent.Gate.GetX(ctx, int(g.id)).ConfigRevision; current != after {
		t.Fatalf("no-op sync advanced revision %d -> %d", after, current)
	}
	mustJSON(t, admin, "PATCH", fmt.Sprintf("/api/v1/admin/users/%d", uid), map[string]any{"enabled": true}, 200)
	if current := s.Store.Ent.Gate.GetX(ctx, int(g.id)).ConfigRevision; current <= after {
		t.Fatal("restore did not advance revision")
	}
	_ = n
}

func TestPublicGuestbookContractAndDatabasePagination(t *testing.T) {
	logs := captureLog(t)
	s, admin := newInstalledServer(t)
	page := mustJSON(t, admin, "POST", "/api/v1/admin/public-pages", map[string]any{"slug": "contract", "data": map[string]any{"enabled": true, "title": "Contract", "guestbook": map[string]any{"enabled": true, "allow_anonymous": false, "require_audit": true}}}, 200)
	pid := int(page["id"].(float64))
	configState := mustJSON(t, admin, "GET", fmt.Sprintf("/api/v1/admin/public-pages/%d/guestbook/config", pid), nil, 200)
	mustJSON(t, admin, "PUT", fmt.Sprintf("/api/v1/admin/public-pages/%d/guestbook/config", pid), map[string]any{"config": map[string]any{"enabled": true, "allow_anonymous": false, "require_audit": true}, "revision": configState["revision"]}, 200)

	for i := 0; i < 45; i++ {
		status := "approved"
		if i%3 == 0 {
			status = "pending"
		}
		s.Store.Ent.Resource.Create().SetKind(kindGuestbook).SetKey(fmt.Sprint(i)).SetOwnerID(pid).SetData(toMap(guestbookMessage{Nickname: "Visitor", Content: fmt.Sprintf("message-%02d", i), Type: "general", Status: status})).SaveX(t.Context())
	}
	anon := &testClient{t: t, router: admin.router}
	publicTrace := "11111111111111111111111111111111"
	publicResponse := anon.do("GET", "/api/v1/pages/contract/guestbook?page=2", nil, map[string]string{"traceparent": "00-" + publicTrace + "-0123456789abcdef-01"})
	if publicResponse.Code != 200 || publicResponse.Header().Get("X-Trace-Id") != publicTrace {
		t.Fatalf("public guestbook trace: status=%d headers=%v", publicResponse.Code, publicResponse.Header())
	}
	var list map[string]any
	if err := json.Unmarshal(publicResponse.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if list["total"] != float64(30) || len(list["items"].([]any)) != 10 {
		t.Fatalf("page %v", list)
	}
	config := list["config"].(map[string]any)
	if config["allow_anonymous"] != false || config["enabled"] != true || config["require_audit"] != nil {
		t.Fatalf("unsafe config %v", config)
	}
	for _, it := range list["items"].([]any) {
		item := it.(map[string]any)
		if item["status"] == "pending" {
			t.Fatal("pending message leaked")
		}
		if _, ok := item["revision"]; ok {
			t.Fatal("admin revision leaked to public listing")
		}
		if _, ok := item["visible"]; ok {
			t.Fatal("admin visibility leaked to public listing")
		}
	}
	adminTrace := "22222222222222222222222222222222"
	adminResponse := admin.do("GET", fmt.Sprintf("/api/v1/admin/public-pages/%d/guestbook/messages?status=pending&search=message-0", pid), nil, map[string]string{"traceparent": "00-" + adminTrace + "-fedcba9876543210-01"})
	if adminResponse.Code != 200 || adminResponse.Header().Get("X-Trace-Id") != adminTrace {
		t.Fatalf("admin guestbook trace: status=%d headers=%v", adminResponse.Code, adminResponse.Header())
	}
	var filtered map[string]any
	if err := json.Unmarshal(adminResponse.Body.Bytes(), &filtered); err != nil {
		t.Fatal(err)
	}
	if filtered["total"] != float64(4) {
		t.Fatalf("database filter mismatch %v", filtered)
	}
	adminItems := filtered["items"].([]any)
	if len(adminItems) == 0 || adminItems[0].(map[string]any)["revision"] == "" || adminItems[0].(map[string]any)["visible"] != false {
		t.Fatalf("admin moderation fields missing: %v", adminItems)
	}
	for _, trace := range []string{publicTrace, adminTrace} {
		if !strings.Contains(logs.String(), "trace="+trace) {
			t.Fatalf("trace %s missing from request logs: %s", trace, logs.String())
		}
	}
	base := fmt.Sprintf("/api/v1/admin/public-pages/%d/guestbook/messages", pid)
	latest := mustJSON(t, admin, "GET", base+"?status=pending", nil, 200)["items"].([]any)[0].(map[string]any)
	id := int(latest["id"].(float64))
	moderate := func(action, status, reply string, visible bool, total float64) {
		t.Helper()
		list := mustJSON(t, admin, "GET", base, nil, 200)
		var selected map[string]any
		for _, item := range list["items"].([]any) {
			if int(item.(map[string]any)["id"].(float64)) == id {
				selected = item.(map[string]any)
				break
			}
		}
		// The selected pending message is on the first page under this fixture's ID order.
		if selected == nil {
			t.Fatal("moderation fixture missing")
		}
		body := map[string]any{"id": id, "revision": selected["revision"], "action": action, "status": status, "reply": reply}
		mustJSON(t, admin, "POST", base, body, 200)
		public := mustJSON(t, anon, "GET", "/api/v1/pages/contract/guestbook", nil, 200)
		if public["total"] != total {
			t.Fatalf("%s public total=%v expected=%v", action, public["total"], total)
		}
		current := s.Store.Ent.Resource.GetX(t.Context(), id)
		var message guestbookMessage
		if err := decodeStrict(current.Data, &message); err != nil || message.visible() != visible {
			t.Fatalf("%s visibility %v %v", action, message.visible(), err)
		}
		if selected["revision"] != messageRevision(current) {
			mustJSON(t, admin, "POST", base, body, 409)
		}
	}
	moderate("status", "approved", "", false, 30)
	moderate("publish", "", "", true, 31)
	moderate("hide", "", "", false, 30)
	moderate("reply", "", "受控回复", true, 31)
	moderate("status", "pending", "", false, 30)
	moderate("status", "approved", "", false, 30)
	mustJSON(t, anon, "POST", "/api/v1/pages/contract/guestbook", map[string]any{"nickname": "Visitor", "content": "must require login", "type": "general"}, 401)
	if count := s.Store.Ent.Resource.Query().Where(resource.KindEQ(kindGuestbook)).CountX(t.Context()); count != 45 {
		t.Fatal("read mutated data")
	}
}

func TestProfileListsUseConstantQueryCount(t *testing.T) {
	s, _ := newInstalledServer(t)
	for i := 0; i < 100; i++ {
		p := s.Store.Ent.SubProfile.Create().SetName(fmt.Sprint(i)).SaveX(t.Context())
		s.Store.Ent.SubGrant.Create().SetProfileID(p.ID).SetUserID(1).SetToken(fmt.Sprint(i)).SaveX(t.Context())
	}
	ctx, metrics := store.ObserveQueries(t.Context())
	profiles, err := s.Store.Ent.SubProfile.Query().All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	views, err := s.profileViews(ctx, profiles)
	if err != nil || len(views) != 100 {
		t.Fatalf("views=%d error=%v", len(views), err)
	}
	if n := metrics.Count.Load(); n != 2 {
		t.Fatalf("list uses %d SQL calls, expected 2", n)
	}
}
func TestGateLayaPreparationUsesFixedQueueAndTarget(t *testing.T) {
	s, admin := newInstalledServer(t)
	g := pairFakeGate(t, admin)
	heartbeat(t, g, contracts.Heartbeat{Version: "0.1.11", Arch: "amd64"})
	caps := contracts.CapabilityDocument{Protocol: contracts.AgentProtocol, Version: "0.1.11", Arch: "amd64", Tasks: map[string]int{contracts.TaskLayaPrepare: contracts.TaskVersion}}
	if err := s.Store.Tasks.SetCapabilities(t.Context(), int(g.id), caps); err != nil {
		t.Fatal(err)
	}
	base := fmt.Sprintf("/api/v1/admin/gates/%d/laya", g.id)
	p := mustJSON(t, admin, "POST", base+"/preview", map[string]any{"operation": "prepare"}, 200)
	result := mustJSON(t, admin, "POST", base+"/"+p["id"].(string)+"/confirm", nil, 202)
	taskID := result["operation"].(map[string]any)["task_id"].(string)
	record, err := s.Store.Tasks.Get(t.Context(), taskID)
	if err != nil {
		t.Fatal(err)
	}
	if record.Task.Kind != contracts.TaskLayaPrepare || record.Task.Requirements.TargetGateID != int(g.id) {
		t.Fatal("wrong preparation target")
	}
	duplicate := mustJSON(t, admin, "POST", base+"/"+p["id"].(string)+"/confirm", nil, 200)
	if duplicate["operation"].(map[string]any)["task_id"] != taskID {
		t.Fatal("duplicate host preparation")
	}
}
