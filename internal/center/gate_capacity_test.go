package center

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/store/ent/resource"
	"github.com/shusfun/weir/internal/store/ent/user"
)

func TestGateCapacityNoticesAndAutomaticPlan(t *testing.T) {
	s, admin, fg := setupGate(t)
	ctx := context.WithValue(t.Context(), httpTraceKey{}, httpTrace{ID: "2c3a17b1d47b4bd3aa8f692b26357a11"})
	other := pairFakeGate(t, admin)
	s.Store.Ent.Gate.UpdateOneID(int(other.id)).SetLastSeenAt(time.Now()).ExecX(ctx)
	var entries []Entry
	for i, gid := range []int{int(fg.id), int(other.id)} {
		ex, err := s.directExit(ctx, s.Store.Ent, gid)
		if err != nil {
			t.Fatal(err)
		}
		n := s.Store.Ent.Node.Create().SetGateID(gid).SetExitID(ex.ID).SetName("capacity-" + itoa(i)).SetKernel("xray").SetProtocol("vless").SetPort(31050 + i).SaveX(ctx)
		entries = append(entries, Entry{NodeID: n.ID, Ref: "node:" + itoa(n.ID), Name: n.Name, Type: "vless", URI: "vless://test@203.0.113.1:443?security=tls#" + n.Name})
	}
	u := s.Store.Ent.User.Create().SetEmail("capacity@example.test").SetPasswordHash("x").SetAllNodes(true).SaveX(ctx)
	path := "/api/v1/admin/gates/" + itoa(int(fg.id)) + "/traffic"
	v := mustJSON(t, admin, "GET", path, nil, 200)
	body := map[string]any{"quota": 1000, "mode": "sum", "reset_day": 1, "revision": v["revision"]}
	mustJSON(t, admin, "PUT", path, body, 200)
	now := time.Now().UTC()
	start, next := networkPeriod(now, 1)
	usage := gateNetworkUsage{Start: start, Next: next, ObservedAt: now, TotalTX: 800}
	saveUsage := func() {
		s.Store.Ent.Resource.Create().SetKind(gateTrafficKind).SetKey(itoa(int(fg.id))).SetData(toMap(usage)).OnConflictColumns(resource.FieldKind, resource.FieldKey).UpdateNewValues().ExecX(ctx)
	}
	saveUsage()
	warning := s.gateTrafficNotices(ctx)
	if len(warning) != 1 || warning[0].Extra["alert"] != "warning" || !strings.Contains(warning[0].Action.To, "traffic=") {
		t.Fatal(warning)
	}
	mustJSON(t, admin, "POST", "/api/v1/admin/notices/"+warning[0].ID+"/dismiss", map[string]any{}, 200)
	aid := s.Store.Ent.User.Query().Where(user.RoleEQ(user.RoleAdmin)).FirstX(ctx).ID
	if !s.dismissed(ctx, aid)[warning[0].ID] {
		t.Fatal("dismiss not stored")
	}
	usage.TotalTX = 970
	saveUsage()
	critical := s.gateTrafficNotices(ctx)
	if len(critical) != 1 || critical[0].ID == warning[0].ID || critical[0].Extra["alert"] != "critical" {
		t.Fatal(critical)
	}
	assertOrder := func(want []string) {
		t.Helper()
		p, err := s.planEntries(ctx, u, entries, false)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(p.Entries, entries) {
			t.Fatal("manual order changed", p.Entries)
		}
		plan, items := s.subPlan(ctx, p)
		names := []string{entries[0].Name, entries[1].Name}
		groups, _ := plan.Layout(names, items)
		for _, g := range groups {
			if g.Ref == "auto" {
				if !reflect.DeepEqual(g.MemberRefs, want) {
					t.Fatal(g.MemberRefs, want)
				}
				return
			}
		}
		t.Fatal("automatic group missing")
	}
	assertOrder([]string{entries[0].Ref, entries[1].Ref})
	v = mustJSON(t, admin, "GET", path, nil, 200)
	body["revision"] = v["revision"]
	body["auto_deprioritize"] = true
	mustJSON(t, admin, "PUT", path, body, 200)
	assertOrder([]string{entries[1].Ref, entries[0].Ref})
	// A stale high total is still a known lower bound within the same period.
	usage.ObservedAt = now.Add(-5 * time.Minute)
	saveUsage()
	assertOrder([]string{entries[1].Ref, entries[0].Ref})
	if !strings.Contains(s.gateTrafficNotices(ctx)[0].Detail, "过期") {
		t.Fatal("stale status hidden")
	}
	usage.Start = start.AddDate(0, -1, 0)
	saveUsage()
	assertOrder([]string{entries[0].Ref, entries[1].Ref})
	if len(s.gateTrafficNotices(ctx)) != 0 {
		t.Fatal("prior period still alerts")
	}
	// A current period can resume after missing data without losing settings.
	usage.Start, usage.Next = start, next
	usage.ObservedAt = now
	saveUsage()
	settings, _, err := readGateTraffic(ctx, s.Store.Ent, int(fg.id))
	if err != nil {
		t.Fatal(err)
	}
	if !settings.AutoDeprioritize {
		t.Fatal("strategy not persisted")
	}
}
