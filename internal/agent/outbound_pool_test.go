package agent

import (
	"encoding/json"
	"github.com/imzyb/MiGate/internal/db"
	"github.com/imzyb/MiGate/packages/contracts"
	"path/filepath"
	"testing"
)

func TestPoolAtomicIdentityOwnershipAndRestart(t *testing.T) {
	a, err := New(Config{DeviceID: 8, DataDir: filepath.Join(t.TempDir(), "state")})
	if err != nil {
		t.Fatal(err)
	}
	defer a.DB.Close()
	node := db.Outbound{ID: 101, Tag: "pool-one", Protocol: "http", Address: "example.com", Port: 443, Username: "u", Password: "secret", Enabled: true, Source: db.OutboundSourceSubscription, SubscriptionID: 3, SubscriptionIdentity: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	a.State.Outbounds = []db.Outbound{node, {ID: 7, Tag: "manual", Protocol: "freedom", Enabled: true}}
	change := db.PoolChange{PoolID: 3, Previous: []db.PoolMember{{ID: 101, Digest: db.PoolMemberDigest(node)}}, Next: []db.Outbound{node}}
	task := func(c db.PoolChange, revision int64) contracts.Task {
		b, _ := json.Marshal(c)
		var p map[string]any
		json.Unmarshal(b, &p)
		return contracts.Task{Operation: "outbound.pool.apply", Revision: revision, Payload: p}
	}
	a.State.Routing = []db.RoutingRule{{ID: 99, OutboundID: 101, Enabled: false}}
	removed := change
	removed.Next = nil
	if a.mutate(task(removed, 2)) == nil {
		t.Fatal("removed node referenced by disabled route")
	}
	if len(a.State.Outbounds) != 2 {
		t.Fatal("partial mutation on rejection")
	}
	modified := change
	modified.Previous = []db.PoolMember{{ID: 101, Digest: "bad"}}
	if a.mutate(task(modified, 2)) == nil {
		t.Fatal("accepted stale member snapshot")
	}
	collision := change
	bad := node
	bad.ID = 7
	collision.Next = []db.Outbound{bad}
	if a.mutate(task(collision, 2)) == nil {
		t.Fatal("overwrote manual outbound")
	}
	a.State.Routing = nil
	if err = a.mutate(task(change, 2)); err != nil {
		t.Fatal(err)
	}
	if err = a.mutate(task(change, 2)); err != nil {
		t.Fatal("replay failed", err)
	}
	if err = a.mutate(contracts.Task{Operation: "outbound.delete", Revision: 3, Payload: map[string]any{"id": 101}}); err == nil {
		t.Fatal("manual API deleted pool member")
	}
	if err = a.persist(); err != nil {
		t.Fatal(err)
	}
	restored, err := New(a.Config)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.DB.Close()
	if len(restored.State.Outbounds) != 2 || restored.State.Versions["pool:3"] != 2 {
		t.Fatal("pool lost on restart")
	}
	if restored.mutate(task(change, 1)) == nil {
		t.Fatal("stale task accepted after restart")
	}
	if err = restored.mutate(task(removed, 3)); err != nil {
		t.Fatal(err)
	}
	if len(restored.State.Outbounds) != 1 || restored.State.Outbounds[0].ID != 7 {
		t.Fatal("removal affected unrelated outbound")
	}
}
