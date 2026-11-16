package center

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/audit"
)

func TestExitCredentialsRequireExplicitAuditedAdminReveal(t *testing.T) {
	s, admin, g := setupGate(t)
	u := mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "secret-owner@example.test", "allow_chain": true}, 200)
	uid := int(u["id"].(float64))
	user := login(t, s, "secret-owner@example.test", u["password"].(string))
	e, err := s.persistExit(t.Context(), s.Store.Ent, int(g.id), &uid, "", ExitSpec{Kind: "socks5", Address: "203.0.113.7", Port: 1080, Username: "account", Password: "fixture-private-password"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/admin/exits/" + itoa(e.ID)
	for _, check := range []struct {
		client *testClient
		path   string
	}{{admin, path}, {admin, "/api/v1/admin/exits"}, {user, "/api/v1/me/exits"}} {
		w := check.client.do("GET", check.path, nil, nil)
		if w.Code != 200 || strings.Contains(w.Body.String(), "fixture-private-password") || strings.Contains(w.Body.String(), `"password":`) || !strings.Contains(w.Body.String(), `"has_password":true`) {
			t.Fatalf("unsafe view %d %s", w.Code, w.Body)
		}
	}
	mustJSON(t, user, "POST", path+"/credentials", map[string]any{"confirm": true}, 403)
	mustJSON(t, admin, "POST", path+"/credentials", map[string]any{}, 400)
	w := admin.do("POST", path+"/credentials", map[string]any{"confirm": true}, nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "fixture-private-password") || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, w.Body)
	}
	a := s.Store.Ent.Audit.Query().Where(audit.ActionEQ("exit.credentials.reveal"), audit.TargetEQ(exitOwner(e.ID))).OnlyX(t.Context())
	if a.Detail["trace_id"] != w.Header().Get("X-Trace-Id") || a.Actor == uid {
		t.Fatal("missing correlated admin audit")
	}
	// A metadata-only patch must preserve encrypted credentials without revealing them.
	mustJSON(t, admin, "PATCH", path, map[string]any{"remark": "renamed"}, 200)
	saved, err := s.exitSpec(s.Store.Ent.Exit.GetX(t.Context(), e.ID))
	if err != nil || saved.Password != "fixture-private-password" || saved.Remark != "renamed" {
		t.Fatal("metadata patch lost credentials", err)
	}
	s.Store.Ent.Audit.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			return nil, errors.New("audit unavailable")
		})
	})
	w = admin.do("POST", path+"/credentials", map[string]any{"confirm": true}, nil)
	if w.Code != 503 || strings.Contains(w.Body.String(), "fixture-private-password") {
		t.Fatal("revealed without durable audit", w.Code, w.Body)
	}
}

func TestExitDirectoryGroupsDeploymentsWithoutMergingConflictingCredentials(t *testing.T) {
	s, _, g := setupGate(t)
	uid := 8
	items := []*ent.Exit{}
	views := []gin.H{}
	for i, spec := range []ExitSpec{
		{Kind: "socks5", Address: "proxy.example.test", Port: 1080, Username: "a", Password: "one"},
		{Kind: "socks5", Address: "PROXY.example.test", Port: 1080, Username: "a", Password: "one", Remark: "another deployment"},
		{Kind: "socks5", Address: "proxy.example.test", Port: 1080, Username: "a", Password: "two"},
		{Kind: "socks5", Address: "proxy.example.test", Port: 1080, Username: "a", Password: "one"},
		{Kind: "blackhole", Remark: "keep blocking controls"},
	} {
		var owner *int
		if i == 3 {
			owner = &uid
		}
		gid := int(g.id)
		if i > 0 {
			gid = s.Store.Ent.Gate.Create().SetName("another VPS").SetPublicKey("directory-gate-" + itoa(i)).SaveX(t.Context()).ID
		}
		e, err := s.persistExit(t.Context(), s.Store.Ent, gid, owner, "", spec, nil)
		if err != nil {
			t.Fatal(err)
		}
		v, err := s.exitResourceView(t.Context(), e)
		if err != nil {
			t.Fatal(err)
		}
		items, views = append(items, e), append(views, v)
	}
	groups, err := s.exitDirectory(t.Context(), items, views)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 4 || len(groups[0]["deployments"].([]gin.H)) != 2 || groups[0]["conflict"] != true || groups[1]["conflict"] != true || groups[2]["conflict"] != false || groups[3]["kind"] != "blackhole" {
		t.Fatalf("directory = %#v", groups)
	}
}
