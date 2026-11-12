package center

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/packages/contracts"
)

func TestSubscriptionNestedMembersValidationAndPermissions(t *testing.T) {
	s, admin := newInstalledServer(t)
	ctx := t.Context()
	node := s.Store.Ent.SubNode.Create().SetName("node").SetURL("trojan://p@203.0.113.1:443#leaf").SaveX(ctx)
	member := func(kind string, id int) []contracts.SubscriptionMember {
		return []contracts.SubscriptionMember{{Kind: kind, ID: id}}
	}
	create := func(name string, members []contracts.SubscriptionMember) int {
		v := mustJSON(t, admin, "POST", "/api/v1/admin/subscriptions/profiles", map[string]any{"name": name, "members": members}, 200)
		return int(v["id"].(float64))
	}
	leaf := create("leaf", member("node", node.ID))
	child := create("child", member("profile", leaf))
	parent := create("parent", member("profile", child))
	root := create("root", member("profile", parent))
	for _, test := range []struct {
		id      int
		members []contracts.SubscriptionMember
	}{
		{leaf, member("profile", root)}, // cycle
		{leaf, member("profile", child)},
		{root, append(member("profile", parent), member("profile", parent)...)},
		{root, member("profile", 999999)},
		{root, member("invalid", node.ID)},
		{root, member("node", 0)},
	} {
		mustJSON(t, admin, "PATCH", "/api/v1/admin/subscriptions/profiles/"+itoa(test.id), map[string]any{"members": test.members}, 400)
	}
	other := create("other", member("node", node.ID))
	// Editing the leaf, not the root, would put root's deepest member at depth 4.
	mustJSON(t, admin, "PATCH", "/api/v1/admin/subscriptions/profiles/"+itoa(leaf), map[string]any{"members": member("profile", other)}, 400)
	mustJSON(t, admin, "POST", "/api/v1/admin/subscriptions/profiles", map[string]any{"name": "mixed", "all_sources": true, "members": member("node", node.ID)}, 400)
	mustJSON(t, admin, "POST", "/api/v1/admin/subscriptions/profiles", map[string]any{"name": "mixed", "source_ids": []int{}, "members": member("node", node.ID)}, 400)
	mustJSON(t, admin, "DELETE", "/api/v1/admin/subscriptions/profiles/"+itoa(leaf), nil, 409)
	// A grant on root deliberately exposes its composition, not management of children.
	u := mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "nested@dev.test", "password": "Passw0rd", "all_nodes": false}, 200)
	uid := int(u["id"].(float64))
	mustJSON(t, admin, "PUT", "/api/v1/admin/subscriptions/profiles/"+itoa(root)+"/grants", map[string]any{"user_ids": []int{uid}}, 200)
	viewer := &testClient{t: t, router: admin.router}
	mustJSON(t, viewer, "POST", "/api/v1/auth/login", map[string]any{"email": "nested@dev.test", "password": "Passw0rd"}, 200)
	w := viewer.do("PATCH", "/api/v1/admin/subscriptions/profiles/"+itoa(leaf), map[string]any{"members": member("profile", other)}, nil)
	if w.Code != 403 {
		t.Fatal("member management escaped admin boundary", w.Code)
	}
	// The original output survives all rejected mutations.
	p := s.Store.Ent.SubProfile.GetX(ctx, root)
	body, _, _, _, err := s.groupNodes(ctx, p, "clash", false)
	if err != nil || !strings.Contains(body, "node") {
		t.Fatal(body, err)
	}
	for _, childState := range []string{"disabled", "expired"} {
		update := s.Store.Ent.SubProfile.UpdateOneID(leaf).SetEnabled(childState != "disabled")
		if childState == "expired" {
			update.SetExpiresAt(time.Now().Add(-time.Hour))
		}
		update.ExecX(ctx)
		if _, _, _, _, err := s.groupNodes(ctx, p, "clash", false); err == nil {
			t.Fatal("cached parent hid inactive child")
		}
	}
}

func TestSubscriptionNestedDraftUndoAndDeletion(t *testing.T) {
	s, admin := newInstalledServer(t)
	ctx := t.Context()
	n := s.Store.Ent.SubNode.Create().SetName("node").SetURL("trojan://p@203.0.113.1:443#leaf").SaveX(ctx)
	leaf := s.Store.Ent.SubProfile.Create().SetName("leaf").SetNodeIds([]int{n.ID}).SaveX(ctx)
	root := s.Store.Ent.SubProfile.Create().SetName("root").SetMembers([]contracts.SubscriptionMember{{Kind: "profile", ID: leaf.ID}}).SaveX(ctx)
	actor, err := s.systemActor(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cap, _ := findOpCap("profile.update")
	for _, args := range []map[string]any{
		{"source_ids": []any{}, "node_ids": []any{float64(n.ID)}},
		{"all_sources": true},
		{"members": []any{}},
	} {
		args["profile_id"] = float64(root.ID)
		undo, err := execOp(cap)(ctx, s, &execCtx{actor: actor}, &DraftStep{Args: args})
		if err != nil || undo == nil {
			t.Fatal(err)
		}
		if err := undo(ctx); err != nil {
			t.Fatal("nested composition not restored", err)
		}
		got := s.Store.Ent.SubProfile.GetX(ctx, root.ID)
		if got.AllSources || len(got.Members) != 1 || got.Members[0].ID != leaf.ID || got.Members[0].Kind != "profile" {
			t.Fatal("undo lost child reference", got.Members)
		}
	}
	// New member lists and legacy lists are cleaned in the same transaction.
	s.Store.Ent.SubProfile.UpdateOneID(leaf.ID).SetMembers([]contracts.SubscriptionMember{{Kind: "node", ID: n.ID}}).ExecX(ctx)
	w := admin.do("DELETE", "/api/v1/admin/subscriptions/nodes/"+itoa(n.ID), nil, nil)
	if w.Code != 204 {
		t.Fatal(w.Code, w.Body)
	}
	got := s.Store.Ent.SubProfile.GetX(ctx, leaf.ID)
	if len(got.Members) != 0 || got.Members == nil || len(got.NodeIds) != 0 {
		t.Fatal("deletion left dangling members", got.Members)
	}
}

func TestSubscriptionNestedConcurrentCycle(t *testing.T) {
	s, admin := newInstalledServer(t)
	ctx := t.Context()
	a := s.Store.Ent.SubProfile.Create().SetName("a").SaveX(ctx)
	b := s.Store.Ent.SubProfile.Create().SetName("b").SaveX(ctx)
	start := make(chan struct{})
	codes := make(chan int, 2)
	var wg sync.WaitGroup
	for _, pair := range [][2]int{{a.ID, b.ID}, {b.ID, a.ID}} {
		wg.Add(1)
		go func(id, target int) {
			defer wg.Done()
			<-start
			client := &testClient{t: t, router: admin.router, cookie: admin.cookie}
			w := client.do("PATCH", "/api/v1/admin/subscriptions/profiles/"+itoa(id), map[string]any{"members": []contracts.SubscriptionMember{{Kind: "profile", ID: target}}}, nil)
			codes <- w.Code
		}(pair[0], pair[1])
	}
	close(start)
	wg.Wait()
	close(codes)
	counts := map[int]int{}
	for code := range codes {
		counts[code]++
	}
	if counts[200] != 1 || counts[400] != 1 {
		t.Fatal(counts)
	}
	if err := validateProfileGraph(s.Store.Ent.SubProfile.Query().AllX(ctx)); err != nil {
		t.Fatal(err)
	}
}

func TestSubscriptionNestedExpansionBudget(t *testing.T) {
	leaf := &ent.SubProfile{ID: 1, Members: []contracts.SubscriptionMember{}}
	for i := 1; i <= maxSubscriptionMembers/2; i++ {
		leaf.Members = append(leaf.Members, contracts.SubscriptionMember{Kind: "node", ID: i})
	}
	left := &ent.SubProfile{ID: 2, Members: []contracts.SubscriptionMember{{Kind: "profile", ID: 1}}}
	right := &ent.SubProfile{ID: 3, Members: []contracts.SubscriptionMember{{Kind: "profile", ID: 1}}}
	root := &ent.SubProfile{ID: 4, Members: []contracts.SubscriptionMember{{Kind: "profile", ID: 2}, {Kind: "profile", ID: 3}}}
	if err := validateProfileGraph([]*ent.SubProfile{leaf, left, right}); err != nil {
		t.Fatal("individually bounded roots rejected", err)
	}
	if err := validateProfileGraph([]*ent.SubProfile{leaf, left, right, root}); err == nil || err.Error() != "profile_expansion_exceeded" {
		t.Fatal("shared descendants bypassed expansion budget", err)
	}
	s, _ := newInstalledServer(t)
	child := s.Store.Ent.SubProfile.Create().SetName("empty child").SaveX(t.Context())
	p := s.Store.Ent.SubProfile.Create().SetName("root").SetMembers([]contracts.SubscriptionMember{{Kind: "profile", ID: child.ID}}).SaveX(t.Context())
	remaining := 0
	if _, _, _, _, err := s.groupNodesAt(t.Context(), p, "", false, map[int]bool{}, 0, &remaining); err == nil || err.Error() != "profile_expansion_exceeded" {
		t.Fatal("runtime expansion budget not enforced", err)
	}
}

func TestSubscriptionNestedOrderingCacheAndBackup(t *testing.T) {
	s, _ := newInstalledServer(t)
	ctx := t.Context()
	a := s.Store.Ent.SubNode.Create().SetName("first").SetURL("trojan://p@203.0.113.1:443#first").SaveX(ctx)
	b := s.Store.Ent.SubNode.Create().SetName("last").SetURL("trojan://p@203.0.113.2:443#last").SaveX(ctx)
	leaf := s.Store.Ent.SubProfile.Create().SetName("leaf").SetMembers([]contracts.SubscriptionMember{{Kind: "node", ID: a.ID}}).SaveX(ctx)
	root := s.Store.Ent.SubProfile.Create().SetName("root").SetMembers([]contracts.SubscriptionMember{{Kind: "node", ID: b.ID}, {Kind: "profile", ID: leaf.ID}}).SetSettings(map[string]any{"prepend_manual": false, "flag_emoji": false}).SaveX(ctx)
	read := func(p *ent.SubProfile) string {
		t.Helper()
		body, _, _, _, err := s.groupNodes(ctx, p, "clash", false)
		if err != nil {
			t.Fatal(err)
		}
		return body
	}
	body := read(root)
	if strings.Index(body, "last") > strings.Index(body, "first") || strings.Count(body, "trojan://") != 2 {
		t.Fatal(body)
	}
	a.Update().SetName("changed").ExecX(ctx)
	if body := read(root); !strings.Contains(body, "changed") || strings.Contains(body, "first") {
		t.Fatal("parent cache survived nested dependency change", body)
	}
	raw, err := s.ExportSubscriptions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ImportSubscriptions(ctx, raw); err != nil {
		t.Fatal(err)
	}
	rows := s.Store.Ent.SubProfile.Query().AllX(ctx)
	var copied *ent.SubProfile
	for _, p := range rows {
		if p.Name == "root" && p.ID != root.ID {
			copied = p
		}
	}
	if copied == nil || copied.Members[1].ID == leaf.ID || copied.Settings["prepend_manual"] != false || read(copied) != read(root) {
		t.Fatal("backup lost nesting/order/settings", copied)
	}
	// A malicious cycle must roll back the entire import, including its nodes.
	var backup backupFile
	if err := json.Unmarshal(raw, &backup); err != nil {
		t.Fatal(err)
	}
	for _, p := range backup.Data.Profiles {
		if p["name"] == "leaf" {
			p["members"] = []map[string]any{{"kind": "profile", "id": "p" + itoa(root.ID)}}
		}
	}
	bad, _ := json.Marshal(backup)
	before := s.Store.Ent.SubNode.Query().CountX(ctx)
	if _, err := s.ImportSubscriptions(ctx, bad); err == nil {
		t.Fatal("cyclic backup accepted")
	}
	if s.Store.Ent.SubNode.Query().CountX(ctx) != before {
		t.Fatal("invalid backup left partial rows")
	}
}
