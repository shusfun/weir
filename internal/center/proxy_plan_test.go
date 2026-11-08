package center

import (
	"reflect"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/store/ent"
	entexit "github.com/shusfun/weir/internal/store/ent/exit"
	"github.com/shusfun/weir/packages/contracts"
)

func TestProxyLegacyTextPreservesOrder(t *testing.T) {
	raw := map[string]any{"custom": map[string]any{"mode": "only", "lines": []string{"IP-CIDR,203.0.113.0/24", "DOMAIN-SUFFIX,example.test", "DOMAIN,first.example.test", "DOMAIN-KEYWORD,keyword"}}}
	p := loadPolicy(&ent.User{ProxyPolicy: raw})
	var kinds []string
	for _, e := range p.Custom.Entries {
		kinds = append(kinds, e.Kind)
	}
	if !reflect.DeepEqual(kinds, []string{"ip_cidr", "domain_suffix", "domain", "domain_keyword"}) {
		t.Fatal(kinds)
	}
	if !reflect.DeepEqual(p, loadPolicy(&ent.User{ProxyPolicy: policyMap(p)})) {
		t.Fatal("重复读取改变迁移结果")
	}
}

// 直接走实际计划入口，保证展示排序不能掩盖订阅的授权和候选过滤。
func TestProxyPlanEntriesAutoAndAuthorization(t *testing.T) {
	s, admin, fg := setupGate(t)
	ctx := t.Context()
	var nodes []*ent.Node
	var entries []Entry
	for _, name := range []string{"首个", "第二个", "链式"} {
		v := mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": fg.id, "preset": "ss2022", "name": name, "skip_probe": true}, 200)
		n := s.Store.Ent.Node.GetX(ctx, int(v["id"].(float64)))
		nodes = append(nodes, n)
		entries = append(entries, Entry{NodeID: n.ID, Ref: "node:" + itoa(n.ID), Name: name, Type: "ss2022", URI: "socks5://test:secret@203.0.113.1:1080#" + name})
	}
	ex := s.Store.Ent.Exit.Create().SetGateID(int(fg.id)).SetTag("测试出口").SetKind(entexit.KindSocks5).SetAddress("203.0.113.7").SetPort(1080).SaveX(ctx)
	nodes[2].Update().SetExitID(ex.ID).ExecX(ctx)
	u := s.Store.Ent.User.Create().SetEmail("plan-auto@example.test").SetPasswordHash("test").SetAllNodes(true).SaveX(ctx)
	assertAuto := func(want []string) {
		t.Helper()
		p, err := s.planEntries(ctx, u, entries, false)
		if err != nil {
			t.Fatal(err)
		}
		plan, items := s.subPlan(ctx, p)
		names := make([]string, len(p.Entries))
		for i, e := range p.Entries {
			names[i] = e.Name
		}
		groups, _ := plan.Layout(names, items)
		var refs []string
		for _, g := range groups {
			if g.Ref == "auto" {
				refs = g.MemberRefs
			}
		}
		if !reflect.DeepEqual(refs, want) {
			t.Fatalf("自动选择=%v，预期=%v", refs, want)
		}
	}
	s.Store.Ent.Gate.UpdateOneID(int(fg.id)).SetLastSeenAt(time.Now().Add(-time.Hour)).ExecX(ctx)
	assertAuto([]string{entries[0].Ref, entries[1].Ref})
	// 没有直连时不创建自动选择，链式线路仍可手动选择。
	u.Update().SetAllNodes(false).SetNodeIds([]int{nodes[2].ID}).ExecX(ctx)
	assertAuto(nil)
	// 旧 User 与旧 Entries 都不能绕过实时授权。
	p, err := s.planEntries(ctx, u, entries, false)
	if err != nil || len(p.Entries) != 1 || p.Entries[0].NodeID != nodes[2].ID {
		t.Fatal(p, err)
	}
	nodes[2].Update().SetEnabled(false).ExecX(ctx)
	p, err = s.planEntries(ctx, u, entries, false)
	if err != nil || len(p.Entries) != 0 {
		t.Fatal("停用后仍输出", p, err)
	}
	u.Update().SetProxyPolicy(policyMap(contracts.ProxyPolicy{Version: 3, Auto: contracts.AutoPolicy{RefreshMinutes: 60}})).ExecX(ctx)
}
