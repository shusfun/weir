package center

import (
	"testing"

	"github.com/shusfun/weir/internal/store/ent/audit"
	"github.com/shusfun/weir/internal/store/ent/user"
)

func TestUserQuotaExplicitAllowanceAndLegacyAPI(t *testing.T) {
	s, admin, gate := setupGate(t)
	ctx := t.Context()
	n := mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": gate.id, "preset": "ss2022", "skip_probe": true}, 200)
	uid := 0
	for _, tc := range []struct {
		name      string
		body      map[string]any
		unlimited bool
		quota     int64
	}{
		{"new", map[string]any{}, false, 0},
		{"explicit-zero", map[string]any{"quota": 0, "unlimited": false}, false, 0},
		{"legacy-zero", map[string]any{"quota": 0}, true, 0},
		{"bounded", map[string]any{"quota": 1024}, false, 1024},
		{"unlimited", map[string]any{"quota": 1024, "unlimited": true}, true, 1024},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.body["email"], tc.body["password"], tc.body["node_ids"] = tc.name+"@quota.test", "Passw0rd", []int{int(n["id"].(float64))}
			v := mustJSON(t, admin, "POST", "/api/v1/admin/users", tc.body, 200)
			uid = int(v["id"].(float64))
			if v["unlimited"] != tc.unlimited || int64(v["quota"].(float64)) != tc.quota {
				t.Fatalf("user allowance: %v", v)
			}
			cfg := gate.config(0)
			b, ok := cfg.Budgets[userKey(uid)]
			if !ok || b.Unlimited != tc.unlimited || (!tc.unlimited && b.Allocated-b.Used != tc.quota) {
				t.Fatalf("Gate allowance: %+v", b)
			}
		})
	}
	trace := "7eac1b2c3d4e5f60718293a4b5c6d7e8"
	before := gate.config(0).Revision
	w := admin.do("PATCH", "/api/v1/admin/users/"+itoa(uid), map[string]any{"quota": 0, "unlimited": false}, map[string]string{"traceparent": "00-" + trace + "-0102030405060708-01"})
	if w.Code != 200 || w.Header().Get("X-Trace-Id") != trace {
		t.Fatalf("explicit zero update: %d %s", w.Code, w.Body.String())
	}
	cfg := gate.config(0)
	if cfg.Revision != before {
		t.Fatal("allowance-only change advanced kernel configuration")
	}
	if b := cfg.Budgets[userKey(uid)]; b.Unlimited || b.Allocated-b.Used != 0 {
		t.Fatalf("unlimited access survived zero allowance: %+v", b)
	}
	a := s.Store.Ent.Audit.Query().Where(audit.ActionEQ("user.update"), audit.TargetEQ("user:"+itoa(uid))).Order().AllX(ctx)
	if len(a) != 1 || a[0].Detail["trace_id"] != trace {
		t.Fatal("allowance audit lost trace")
	}
	registered := s.Store.Ent.User.Create().SetEmail("default@quota.test").SetPasswordHash("unused").SaveX(ctx)
	if registered.Unlimited || registered.Quota != 0 {
		t.Fatal("default account gained allowance")
	}
	me := s.Store.Ent.User.Query().Where(user.RoleEQ(user.RoleAdmin)).OnlyX(ctx)
	if !me.Unlimited {
		t.Fatal("initial administrator must retain unlimited access")
	}
}
