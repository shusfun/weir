package store

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/shusfun/weir/internal/store/ent/exit"
	"github.com/shusfun/weir/internal/store/ent/exitbinding"
)

func TestExitBindingMigrationKeepsIDAndCredentials(t *testing.T) {
	for name, open := range backends(t) {
		t.Run(name, func(t *testing.T) {
			s := open(t)
			ctx := t.Context()
			g := s.Ent.Gate.Create().SetName("入口").SetPublicKey("migration-key").SetPublicHost("203.0.113.10").SaveX(ctx)
			e := s.Ent.Exit.Create().SetGateID(g.ID).SetKind(exit.KindSocks5).SetTag("exit-legacy").SetAddress("203.0.113.8").SetPort(1080).SetPassword("sealed-password").SaveX(ctx)
			if err := s.KV.Set(ctx, "exit-probe:exit:"+itoa(e.ID), `{"ok":true}`, 0); err != nil {
				t.Fatal(err)
			}
			if err := s.migrateExitBindings(ctx); err != nil {
				t.Fatal(err)
			}
			got := s.Ent.Exit.GetX(ctx, e.ID)
			if got.GateID != nil || got.Password != "sealed-password" || got.Address != "203.0.113.8" {
				t.Fatalf("migrated exit = %+v", got)
			}
			b := s.Ent.ExitBinding.Query().Where(exitbinding.ExitIDEQ(e.ID), exitbinding.GateIDEQ(g.ID)).OnlyX(ctx)
			if b.Status != exitbinding.StatusApplied || b.Password != "sealed-password" || b.Port != 1080 {
				t.Fatalf("binding = %+v", b)
			}
			raw, ok, err := s.KV.Get(ctx, "exit-probe:exit:"+itoa(e.ID)+":gate:"+itoa(g.ID))
			if err != nil || !ok || raw != `{"ok":true}` {
				t.Fatalf("probe moved = %q ok=%v err=%v", raw, ok, err)
			}
			source := s.Ent.Exit.Create().SetGateID(g.ID).SetKind(exit.KindSource).SetTag("legacy-source").SaveX(ctx)
			snapshot := `{"outbound":{"protocol":"socks","address":"203.0.113.9","port":1080,"password":"retained"}}`
			if err := s.KV.Set(ctx, "source-exit:exit:"+itoa(source.ID), snapshot, 0); err != nil {
				t.Fatal(err)
			}
			if err := s.migrateExitBindings(ctx); err != nil {
				t.Fatal(err)
			}
			bound := s.Ent.ExitBinding.Query().Where(exitbinding.ExitIDEQ(source.ID)).OnlyX(ctx)
			var gotSnapshot, wantSnapshot any
			if json.Unmarshal(bound.Resolved, &gotSnapshot) != nil || json.Unmarshal([]byte(snapshot), &wantSnapshot) != nil || !reflect.DeepEqual(gotSnapshot, wantSnapshot) {
				t.Fatal("migration lost source snapshot")
			}
			direct := s.Ent.Exit.Create().SetGateID(g.ID).SetKind(exit.KindFreedom).SetTag("direct").SaveX(ctx)
			if err := s.migrateExitBindings(ctx); err != nil {
				t.Fatal(err)
			}
			if s.Ent.Exit.GetX(ctx, direct.ID).GateID == nil {
				t.Fatal("direct exit lost its VPS")
			}
		})
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
