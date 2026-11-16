package center

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/store/ent/credential"
	"github.com/shusfun/weir/internal/store/ent/resource"
	"github.com/shusfun/weir/internal/subgroup"
)

func TestCatalogLinkSaveWithOnlineGate(t *testing.T) {
	s, admin, g := setupGate(t)
	ctx := t.Context()
	n := mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": g.id, "preset": "ss2022", "skip_probe": true}, 200)
	u := mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "online-links@example.test", "all_nodes": true, "unlimited": true}, 200)
	uid := int(u["id"].(float64))
	device := s.Store.Ent.Gate.GetX(ctx, int(g.id))
	s.Store.Ent.Gate.UpdateOneID(device.ID).SetLastSeenAt(time.Now()).SetAppliedRevision(device.ConfigRevision).SetKernelsRunning(map[string]string{"sing-box": "test"}).SetListeningPorts([]map[string]any{{"port": n["port"]}}).ExecX(ctx)
	manual := s.Store.Ent.SubNode.Create().SetName("manual").SetURL("trojan://secret@public.example:443#node").SaveX(ctx)
	ref := "manual:" + itoa(manual.ID)
	s.Store.Ent.Resource.Create().SetKind(catalogGrantKind).SetKey(itoa(uid)).SetOwnerID(uid).SetData(map[string]any{"refs": []string{ref}}).SaveX(ctx)
	for _, refs := range [][]string{{}, {ref, ref}} {
		deadline, cancel := context.WithTimeout(ctx, 3*time.Second)
		l, err := s.saveLink(deadline, uid, 0, linkRequest{CatalogRefs: &refs})
		cancel()
		if err != nil {
			t.Fatalf("online node observations blocked link transaction: %v", err)
		}
		got, err := s.linkCatalogRefs(ctx, uid, l.ID)
		if err != nil || len(got) != min(len(refs), 1) {
			t.Fatalf("selections not committed atomically: %v %v", got, err)
		}
	}
}

func TestCatalogRevisionIgnoresObservations(t *testing.T) {
	items := []catalogNode{{Ref: "node:1", Selected: true, Authorized: true, ChainEntry: true}}
	before := nodeCatalogRevision(items)
	now := time.Now()
	items[0].State = objectState{Status: "stale", Reason: "观测已过期", ObservedAt: &now}
	if nodeCatalogRevision(items) != before {
		t.Fatal("observation invalidated authorization")
	}
	items[0].Authorized = false
	if nodeCatalogRevision(items) == before {
		t.Fatal("authorization change missed")
	}
}

func TestCatalogRefreshKeepsIdentityAndUnavailableReferences(t *testing.T) {
	s, _ := newInstalledServer(t)
	ctx := t.Context()
	src := s.Store.Ent.SubSource.Create().SetName("airport").SetURL("https://airport.example/sub").SaveX(ctx)
	s.Store.Ent.SubProfile.Create().SetName("existing").SetSourceIds([]int{src.ID}).SaveX(ctx)
	first := "trojan://password@public.example:443?sni=public.example#old"
	if err := s.syncSourceCatalog(ctx, src.ID, []string{first}); err != nil {
		t.Fatal(err)
	}
	row := s.Store.Ent.Resource.Query().Where(resource.KindEQ(catalogSourceKind)).OnlyX(ctx)
	if r, _ := catalogRecord(row); !r.Selected {
		t.Fatal("existing profile lost selection")
	}
	if err := s.syncSourceCatalog(ctx, src.ID, []string{strings.Replace(first, "#old", "#renamed", 1)}); err != nil {
		t.Fatal(err)
	}
	renamed := s.Store.Ent.Resource.Query().Where(resource.KindEQ(catalogSourceKind)).OnlyX(ctx)
	if renamed.ID != row.ID {
		t.Fatal("display name changed stable ID")
	}
	if err := s.syncSourceCatalog(ctx, src.ID, []string{strings.Replace(first, "password", "changed-secret", 1)}); err != nil {
		t.Fatal(err)
	}
	rows := s.Store.Ent.Resource.Query().Where(resource.KindEQ(catalogSourceKind)).AllX(ctx)
	if len(rows) != 2 {
		t.Fatal("credential change silently merged")
	}
	old, _ := catalogRecord(s.Store.Ent.Resource.GetX(ctx, row.ID))
	if old.Available || !old.Selected {
		t.Fatal("missing node reference was deleted or remained available")
	}
}

func TestCatalogGrantsAndLinkSelectionsIntersect(t *testing.T) {
	s, admin := newInstalledServer(t)
	ctx := t.Context()
	n := s.Store.Ent.SubNode.Create().SetName("airport").SetURL("trojan://secret@public.example:443#node").SaveX(ctx)
	u := mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "catalog@example.test", "password": "Passw0rd-catalog", "all_nodes": false}, 200)
	uid := int(u["id"].(float64))
	base := "/api/v1/admin/users/" + itoa(uid)
	viewer := login(t, s, "catalog@example.test", "Passw0rd-catalog")
	view := mustJSON(t, admin, "GET", base+"/node-catalog", nil, 200)
	if strings.Contains(mustString(view), "secret") {
		t.Fatal("catalog exposed credential")
	}
	ref := "manual:" + itoa(n.ID)
	mustJSON(t, admin, "PUT", base+"/node-catalog/grants", map[string]any{"refs": []string{ref}, "revision": view["revision"]}, 204)
	selected := mustJSON(t, viewer, "POST", "/api/v1/me/links", map[string]any{"all_nodes": false, "catalog_refs": []string{ref}}, 200)
	token := selected["token"].(string)
	if status, entries := subLinks(t, viewer, token); status != 200 || len(entries) != 1 {
		t.Fatalf("selected catalog node missing: %d %v", status, entries)
	}
	mustJSON(t, viewer, "PUT", base+"/node-catalog/grants", map[string]any{"refs": []string{ref}, "revision": view["revision"]}, 403)
	view = mustJSON(t, admin, "GET", base+"/node-catalog", nil, 200)
	mustJSON(t, admin, "PUT", base+"/node-catalog/grants", map[string]any{"refs": []string{}, "revision": view["revision"]}, 204)
	if _, entries := subLinks(t, viewer, token); len(entries) != 0 {
		t.Fatal("revoked catalog authorization survived in subscription", entries)
	}
	mustJSON(t, viewer, "POST", "/api/v1/me/links", map[string]any{"all_nodes": false, "catalog_refs": []string{ref}}, 400)
}

func catalogItem(t *testing.T, view map[string]any, ref string) map[string]any {
	t.Helper()
	for _, item := range view["items"].([]any) {
		row := item.(map[string]any)
		if row["ref"] == ref {
			return row
		}
	}
	t.Fatalf("catalog missing %s: %v", ref, view["items"])
	return nil
}

func TestCatalogRevokeBlocksChainEntryAndForeignGrant(t *testing.T) {
	s, admin, g := setupGate(t)
	ctx := t.Context()
	n := mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": g.id, "preset": "ss2022", "skip_probe": true}, 200)
	nid := int(n["id"].(float64))
	u := mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "chain-revoke@example.test", "password": "Passw0rd-catalog", "all_nodes": false, "allow_chain": true}, 200)
	uid := int(u["id"].(float64))
	mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "chain-other@example.test", "password": "Passw0rd-other", "all_nodes": false, "allow_chain": true}, 200)
	viewer := login(t, s, "chain-revoke@example.test", "Passw0rd-catalog")
	stranger := login(t, s, "chain-other@example.test", "Passw0rd-other")
	base := "/api/v1/admin/users/" + itoa(uid) + "/node-catalog"
	view := mustJSON(t, admin, "GET", base, nil, 200)
	ref := "node:" + itoa(nid)
	if catalogItem(t, view, ref)["chain_entry"] != false {
		t.Fatal("unassigned node was a chain entry")
	}
	parent := "00-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-bbbbbbbbbbbbbbbb-01"
	mustJSON(t, admin, "PUT", base+"/grants", map[string]any{"refs": []string{ref}, "revision": view["revision"]}, 204)
	if w := stranger.do("PUT", base+"/grants", map[string]any{"refs": []string{ref}, "revision": view["revision"]}, map[string]string{"traceparent": parent}); w.Code != 403 || w.Header().Get("X-Trace-Id") != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatal("foreign grant change accepted", w.Code, w.Body, w.Header().Get("X-Trace-Id"))
	}
	granted := mustJSON(t, admin, "GET", base, nil, 200)
	if catalogItem(t, granted, ref)["authorized"] != true || catalogItem(t, granted, ref)["chain_entry"] != true {
		t.Fatal("grant did not open the chain entry", granted)
	}
	g.serveActions(t, s, nil)
	ch := mustJSON(t, admin, "POST", "/api/v1/admin/users/"+itoa(uid)+"/chains", map[string]any{"name": "家宽", "entry_node_id": nid, "exit": map[string]any{"kind": "socks5", "address": "203.0.113.5", "port": 1080}, "skip_probe": true}, 200)
	cid := int(ch["id"].(float64))
	if s.Store.Ent.Credential.Query().Where(credential.ChainIDEQ(cid)).CountX(ctx) == 0 {
		t.Fatal("chain credential was not issued")
	}
	revokedView := mustJSON(t, admin, "GET", base, nil, 200)
	mustJSON(t, admin, "PUT", base+"/grants", map[string]any{"refs": []string{}, "revision": revokedView["revision"]}, 204)
	after := mustJSON(t, admin, "GET", base, nil, 200)
	if catalogItem(t, after, ref)["authorized"] == true || catalogItem(t, after, ref)["chain_entry"] == true {
		t.Fatal("revoke left the node usable", after)
	}
	if s.Store.Ent.Credential.Query().Where(credential.ChainIDEQ(cid)).CountX(ctx) != 0 {
		t.Fatal("revoked chain credential survived")
	}
	if w := viewer.do("POST", "/api/v1/me/chains", map[string]any{"name": "撤权后", "entry_node_id": nid, "exit": map[string]any{"kind": "socks5", "address": "203.0.113.5", "port": 1080}}, nil); w.Code != 422 || !strings.Contains(w.Body.String(), "entry_not_allowed") {
		t.Fatal("revoked entry accepted", w.Code, w.Body)
	}
	src := s.Store.Ent.SubSource.Create().SetName("airport").SetURL("https://airport.example/sub").SaveX(ctx)
	uri := "trojan://password@public.example:443?sni=public.example#kept"
	if err := s.syncSourceCatalog(ctx, src.ID, []string{uri}); err != nil {
		t.Fatal(err)
	}
	row := s.Store.Ent.Resource.Query().Where(resource.KindEQ(catalogSourceKind), resource.OwnerIDEQ(src.ID)).OnlyX(ctx)
	sourceRef := "source:" + itoa(row.ID)
	mustJSON(t, admin, "PUT", "/api/v1/admin/node-catalog/source/"+itoa(row.ID)+"/selection", map[string]any{"selected": true}, 204)
	current := mustJSON(t, admin, "GET", base, nil, 200)
	mustJSON(t, admin, "PUT", base+"/grants", map[string]any{"refs": []string{sourceRef}, "revision": current["revision"]}, 204)
	current = mustJSON(t, admin, "GET", base, nil, 200)
	mustJSON(t, admin, "PUT", base+"/grants", map[string]any{"refs": []string{}, "revision": current["revision"]}, 204)
	if err := s.syncSourceCatalog(ctx, src.ID, []string{uri}); err != nil {
		t.Fatal(err)
	}
	refreshed := mustJSON(t, admin, "GET", base, nil, 200)
	if catalogItem(t, refreshed, sourceRef)["authorized"] == true || catalogItem(t, refreshed, ref)["chain_entry"] == true {
		t.Fatal("refresh restored a revoked grant", refreshed)
	}
	stored := s.Store.Ent.User.GetX(ctx, uid)
	if stored.AllNodes || len(stored.NodeIds) != 0 {
		t.Fatal("refresh restored native node assignment", stored.NodeIds)
	}
}

func TestStaleCatalogRefreshDoesNotRestoreNodes(t *testing.T) {
	s, _ := newInstalledServer(t)
	ctx := t.Context()
	src := s.Store.Ent.SubSource.Create().SetName("airport").SetURL("https://airport.example/sub").SaveX(ctx)
	uri := "trojan://password@public.example:443?sni=public.example#node"
	version := toSource(s.Store.Ent.SubSource.GetX(ctx, src.ID)).ConfigVersion
	base := time.Date(2026, 11, 16, 1, 0, 0, 0, time.UTC)
	s.recordResults(ctx, []subgroup.SourceResult{{SourceID: src.ID, ConfigVersion: version, Started: base, RawNodes: []string{uri}, Nodes: 1}})
	s.recordResults(ctx, []subgroup.SourceResult{{SourceID: src.ID, ConfigVersion: version, Started: base.Add(2 * time.Minute), RawNodes: []string{}, Nodes: 0}})
	s.recordResults(ctx, []subgroup.SourceResult{{SourceID: src.ID, ConfigVersion: version, Started: base.Add(time.Minute), RawNodes: []string{uri}, Nodes: 1}})
	row := s.Store.Ent.Resource.Query().Where(resource.KindEQ(catalogSourceKind), resource.OwnerIDEQ(src.ID)).OnlyX(ctx)
	record, err := catalogRecord(row)
	if err != nil || record.Available {
		t.Fatal("stale refresh restored a removed node", record, err)
	}
}

func TestRenewalOnlyMovesMatchingFiniteLinks(t *testing.T) {
	s, admin := newInstalledServer(t)
	ctx := t.Context()
	old := time.Now().UTC().Truncate(time.Second).Add(24 * time.Hour)
	u := mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "renewal@example.test", "password": "Passw0rd-renewal", "expires_at": old}, 200)
	uid := int(u["id"].(float64))
	linked := s.Store.Ent.Link.Create().SetUserID(uid).SetToken(linkToken()).SetEndsAt(old).SaveX(ctx)
	custom := s.Store.Ent.Link.Create().SetUserID(uid).SetToken(linkToken()).SetEndsAt(old.Add(-time.Hour)).SaveX(ctx)
	unlimited := s.Store.Ent.Link.Create().SetUserID(uid).SetToken(linkToken()).SaveX(ctx)
	next := old.Add(30 * 24 * time.Hour)
	result := mustJSON(t, admin, "PATCH", "/api/v1/admin/users/"+itoa(uid), map[string]any{"expires_at": next}, 200)
	renewal := result["renewal"].(map[string]any)
	if len(renewal["extended"].([]any)) != 1 {
		t.Fatal("unexpected renewal list", renewal)
	}
	current := s.Store.Ent.Link.GetX(ctx, linked.ID)
	if current.Token != linked.Token || !current.EndsAt.Equal(next) {
		t.Fatal("renewal changed token or did not extend")
	}
	if !s.Store.Ent.Link.GetX(ctx, custom.ID).EndsAt.Equal(*custom.EndsAt) || s.Store.Ent.Link.GetX(ctx, unlimited.ID).EndsAt != nil {
		t.Fatal("independent windows changed")
	}
}
