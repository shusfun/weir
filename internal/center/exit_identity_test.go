package center

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/subscription"
)

func TestExitLinkConfirmationBoundToConnection(t *testing.T) {
	const raw = "trojan://fixture-password@203.0.113.17:443?sni=fixture.example&allowInsecure=1#first"
	id := subscription.InventoryIdentity(raw)
	if _, _, err := parseExitLink(raw); err == nil {
		t.Fatal("insecure accepted without confirmation")
	}
	o, _, err := parseConfirmedExitLink(raw, []string{id})
	if err != nil || !o.ClientOnly || o.Client["skip-cert-verify"] != true {
		t.Fatal("lost confirmed TLS parameters", err)
	}
	renamed := strings.Replace(raw, "#first", "#renamed", 1)
	if _, _, err = parseConfirmedExitLink(renamed, []string{id}); err != nil {
		t.Fatal("rename invalidated confirmation", err)
	}
	for _, changed := range []string{strings.Replace(raw, "fixture-password", "changed", 1), strings.Replace(raw, "sni=fixture.example", "sni=other.example", 1)} {
		if _, _, err := parseConfirmedExitLink(changed, []string{id}); err == nil {
			t.Fatal("confirmation reused for changed connection")
		}
	}
	s, admin, g := setupGate(t)
	w := admin.do("POST", "/api/v1/me/exit-link-preview", map[string]any{"link": raw}, nil)
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Body.String(), "fixture-password") {
		t.Fatal(w.Code, w.Body)
	}
	v := decode(t, w)
	if v["identity"] != id || v["requires_confirmation"] != true {
		t.Fatal(v)
	}
	e, err := s.persistExit(t.Context(), s.Store.Ent, int(g.id), nil, "", ExitSpec{Kind: "link", Link: raw, InsecureIdentities: []string{id}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := s.compileExit(t.Context(), e)
	if err != nil || compiled.Client["skip-cert-verify"] != true {
		t.Fatal("persisted confirmation lost", err)
	}
}

func TestSourceStableSelectionAndLegacyBinding(t *testing.T) {
	s, admin, g := setupGate(t)
	ctx := t.Context()
	mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": int(g.id), "preset": "vless-reality", "skip_probe": true}, 200)
	g.serveActions(t, s, nil)
	src := s.Store.Ent.SubSource.Create().SetName("stable source").SetURL("https://fixture.example/sub").SaveX(ctx)
	const raw = "trojan://original@203.0.113.17:443?sni=fixture.example#same"
	cache := kvCache{s}
	cache.Store(ctx, src.ID, []string{raw})
	v := mustJSON(t, admin, "POST", "/api/v1/admin/exits", map[string]any{"gate_id": int(g.id), "kind": "source", "source_id": src.ID, "mode": "node", "match": "same"}, 200)
	eid := int(v["id"].(float64))
	e := s.Store.Ent.Exit.GetX(ctx, eid)
	id := subscription.InventoryIdentity(raw)
	if specOf(e.Source).Identity != id {
		t.Fatal("identity not persisted")
	}
	cache.Store(ctx, src.ID, []string{strings.Replace(raw, "#same", "#new-name", 1)})
	if err := s.refreshSourceExit(ctx, exitOwner(eid), int(g.id), specOf(e.Source), false, false); err != nil {
		t.Fatal("rename broke selection", err)
	}
	prev, _ := s.loadExit(ctx, exitOwner(eid))
	cache.Store(ctx, src.ID, []string{strings.Replace(raw, "original", "substitute", 1)})
	if err := s.refreshSourceExit(ctx, exitOwner(eid), int(g.id), specOf(e.Source), false, false); err == nil {
		t.Fatal("replaced by same-name connection")
	}
	after, _ := s.loadExit(ctx, exitOwner(eid))
	a, _ := json.Marshal(prev)
	b, _ := json.Marshal(after)
	if string(a) != string(b) {
		t.Fatal("lost last good on missing identity")
	}
	cache.Store(ctx, src.ID, []string{raw, strings.Replace(raw, "original", "other", 1)})
	if _, err := s.bindSourceIdentity(ctx, sourceSpec{SourceID: src.ID, Mode: "node", Match: "same"}); err == nil {
		t.Fatal("ambiguous legacy name accepted")
	}
	// A pre-migration reference is bound durably before another refresh.
	cache.Store(ctx, src.ID, []string{raw})
	legacy := sourceSpec{SourceID: src.ID, Mode: "node", Match: "same"}
	s.Store.Ent.Exit.UpdateOneID(eid).SetSource(legacy.toMap()).ExecX(ctx)
	o, _, _ := parseExitLink(raw)
	buf, _ := json.Marshal(storedExit{Outbound: o, Names: []string{"same"}, At: time.Now().UTC()})
	if err := s.Store.KV.Set(ctx, sourceExitKey(exitOwner(eid)), string(buf), 0); err != nil {
		t.Fatal(err)
	}
	if err := s.refreshSourceExit(ctx, exitOwner(eid), int(g.id), legacy, false, false); err != nil {
		t.Fatal("legacy binding failed", err)
	}
	if got := specOf(s.Store.Ent.Exit.GetX(ctx, eid).Source).Identity; got != id {
		t.Fatal("legacy binding was not durable", got)
	}
}
