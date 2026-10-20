package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/imzyb/MiGate/internal/db"
	"github.com/imzyb/MiGate/packages/contracts"
)

func TestOutboundOrderKeepsIdentityPoolAndPersists(t *testing.T) {
	a, e := New(Config{DeviceID: 8, DataDir: filepath.Join(t.TempDir(), "state")})
	if e != nil {
		t.Fatal(e)
	}
	defer a.DB.Close()
	a.State.Outbounds = []db.Outbound{{ID: 1, Tag: "a", Protocol: "http", Address: "one.example", Port: 8080, Username: "user", Password: "secret", Enabled: true}, {ID: 2, Tag: "b", Protocol: "http", Address: "two.example", Port: 8080}, {ID: 3, Source: db.OutboundSourceSubscription, SubscriptionID: 7, Sort: 33}}
	for _, payload := range []map[string]any{{"items": []contracts.RoutePosition{{ID: 1}}}, {"items": []contracts.RoutePosition{{ID: 1}, {ID: 1}}}, {"items": []contracts.RoutePosition{{ID: 1, Sort: 3}, {ID: 2}}}, {"items": []contracts.RoutePosition{{ID: 1}, {ID: 2}}, "exec": "x"}} {
		if a.mutate(contracts.Task{Operation: "outbound.reorder", Revision: 5, Payload: payload}) == nil {
			t.Fatal("invalid order accepted")
		}
		if a.State.Outbounds[0].Sort != 0 {
			t.Fatal("partial change")
		}
	}
	task := contracts.Task{ID: 10, DeviceID: 8, Operation: "outbound.reorder", Revision: 5, ExpiresAt: time.Now().Add(time.Minute), Payload: map[string]any{"items": []contracts.RoutePosition{{ID: 2}, {ID: 1}}}}
	// No helper/core configured: the state-only order task must still succeed.
	r := a.execute(context.Background(), task)
	if r.Status != "applied" {
		t.Fatal(r.Data)
	}
	if a.State.Outbounds[0].Sort != 20 || a.State.Outbounds[1].Sort != 10 || a.State.Outbounds[2].Sort != 33 || a.State.Outbounds[0].Password != "secret" {
		t.Fatal("sort altered identity or pool")
	}
	if a.execute(context.Background(), task).Status != "applied" {
		t.Fatal("duplicate failed")
	}
	b, e := New(a.Config)
	if e != nil {
		t.Fatal(e)
	}
	defer b.DB.Close()
	if b.State.Outbounds[0].Sort != 20 {
		t.Fatal("order lost")
	}
	old := contracts.Task{Operation: "outbound.upsert", Revision: 6, Payload: map[string]any{"id": 1, "tag": "a", "protocol": "http", "address": "one.example", "port": 8080, "sort": 0}}
	if e = a.mutate(old); e != nil || a.State.Outbounds[0].Sort != 20 {
		t.Fatal("edit reset order", e)
	}
	if a.mutate(contracts.Task{Operation: "outbound.reorder", Revision: 4, Payload: task.Payload}) == nil {
		t.Fatal("old revision accepted")
	}
}

func TestProbePinsPublicAddressAndBoundsTargets(t *testing.T) {
	lookup := func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}, nil
	}
	dialed := ""
	dial := func(_ context.Context, network, address string) (net.Conn, error) {
		dialed = address
		a, b := net.Pipe()
		b.Close()
		return a, nil
	}
	for _, host := range []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "100.64.0.1", "192.0.2.1", "::ffff:127.0.0.1", "::1", "fe80::1"} {
		if status, _ := probeTCP(context.Background(), host, 443, lookup, dial); status != "blocked" || dialed != "" {
			t.Fatal("unsafe dial", host, status)
		}
	}
	mixed := func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}, {IP: net.ParseIP("127.0.0.1")}}, nil
	}
	if status, _ := probeTCP(context.Background(), "mixed.example", 443, mixed, dial); status != "blocked" || dialed != "" {
		t.Fatal("mixed DNS accepted")
	}
	if status, _ := probeTCP(context.Background(), "public.example", 443, lookup, dial); status != "reachable" || dialed != "8.8.8.8:443" {
		t.Fatal("validated target not pinned")
	}
	badDial := func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("private error with secret")
	}
	if status, _ := probeTCP(context.Background(), "public.example", 443, lookup, badDial); status != "unreachable" {
		t.Fatal(status)
	}
}

func TestProbeIsReadOnlyReplayAndReleasesStateLock(t *testing.T) {
	a, e := New(Config{DeviceID: 8, DataDir: filepath.Join(t.TempDir(), "state")})
	if e != nil {
		t.Fatal(e)
	}
	defer a.DB.Close()
	out := db.Outbound{ID: 1, Protocol: "http", Address: "127.0.0.1", Port: 9090, Password: "secret"}
	a.State.Outbounds = []db.Outbound{out}
	task := contracts.Task{ID: 20, DeviceID: 8, Operation: "outbound.probe", Revision: 20, ExpiresAt: time.Now().Add(time.Minute), Payload: map[string]any{"items": []contracts.OutboundProbeItem{{ID: 1, Digest: db.PoolMemberDigest(out)}}}}
	before, _ := json.Marshal(a.State)
	r := a.execute(context.Background(), task)
	after, _ := json.Marshal(a.State)
	if r.Status != "applied" || string(before) != string(after) {
		t.Fatal("probe changed state or required core", r)
	}
	items := r.Data["items"].([]map[string]any)
	if items[0]["status"] != "blocked" {
		t.Fatal(items)
	}
	if a.execute(context.Background(), task).Status != "applied" {
		t.Fatal("read-only receipt lost")
	}
	// A cancelled network call must not restore the snapshot over newer state.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a.mu.Lock()
	_, e = a.probeOutbounds(ctx, task)
	a.mu.Unlock()
	if e != nil {
		t.Fatal(e)
	}
}
