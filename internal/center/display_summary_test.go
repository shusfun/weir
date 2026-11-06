package center

import (
	"testing"
	"time"

	"github.com/shusfun/weir/internal/store/ent"
)

func TestDisplaySummaryUsesRemarksOrObservedExit(t *testing.T) {
	s, _, g := setupGate(t)
	ctx := t.Context()
	s.GeoDB = fakeGeo{"203.0.113.1": {Country: "US", Lon: -120}, "203.0.113.2": {Country: "JP"}}
	gate := &ent.Gate{ID: int(g.id), PublicHost: "203.0.113.1"}
	n := &ent.Node{Name: "入口备注"}
	direct := &ent.Exit{Kind: "freedom", GateID: gate.ID}
	if name := s.nodeDisplayName(ctx, n, gate, direct, nil); name != "入口备注" {
		t.Fatal(name)
	}
	n.Name = ""
	if name := s.nodeDisplayName(ctx, n, gate, direct, nil); name != "美西" {
		t.Fatal(name)
	}
	e := &ent.Exit{ID: 994, Kind: "socks5", Address: "203.0.113.2"}
	if name := s.exitSummary(ctx, e, "").Name; name != "地区未知" {
		t.Fatalf("connection address leaked into region: %s", name)
	}
	s.saveExitProbe(ctx, exitOwner(e.ID), &exitProbe{OK: true, Mode: "tcp", ExitIP: "203.0.113.2", At: time.Now()})
	if name := s.exitSummary(ctx, e, "").Name; name != "地区未知" {
		t.Fatal(name)
	}
	s.saveExitProbe(ctx, exitOwner(e.ID), &exitProbe{OK: true, Mode: "proxy", ExitIP: "203.0.113.2", At: time.Now()})
	if name := s.exitSummary(ctx, e, "").Name; name != "日本" {
		t.Fatal(name)
	}
	e.Remark = "203.0.113.99"
	if name := s.nodeDisplayName(ctx, n, gate, e, &ent.Chain{Name: "手动备注"}); name != e.Remark {
		t.Fatal(name)
	}
	e.Remark = ""
	if name := s.nodeDisplayName(ctx, n, gate, e, &ent.Chain{Name: "手动备注"}); name != "手动备注" {
		t.Fatal(name)
	}
	e.PolicyType = "vless"
	if name := s.nodeDisplayName(ctx, n, gate, e, &ent.Chain{Name: "系统生成名"}); name != "日本" {
		t.Fatal(name)
	}
	s.saveExitProbe(ctx, exitOwner(e.ID), &exitProbe{OK: false, Mode: "proxy", ExitIP: "203.0.113.2", At: time.Now()})
	if name := s.exitSummary(ctx, e, "").Name; name != "地区未知" {
		t.Fatal(name)
	}
}
