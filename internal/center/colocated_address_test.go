package center

import (
	"bytes"
	"encoding/json"
	"errors"
	"net"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/store"
	"github.com/shusfun/weir/internal/store/ent/audit"
	"github.com/shusfun/weir/packages/contracts"
)

func TestColocatedAddressInitializationTrace(t *testing.T) {
	s, admin := newInstalledServer(t)
	g := pairFakeGate(t, admin)
	original := localInterfaceAddrs
	t.Cleanup(func() { localInterfaceAddrs = original })
	localInterfaceAddrs = func() ([]net.Addr, error) {
		return []net.Addr{&net.IPNet{IP: net.ParseIP("127.0.0.1")}, &net.IPNet{IP: net.ParseIP("10.0.0.2")}, &net.IPNet{IP: net.ParseIP("8.8.4.4")}}, nil
	}
	const trace = "1234567890abcdef1234567890abcdef"
	full := contracts.Heartbeat{Protocol: 2, Full: true, Version: "test", Arch: "amd64", Center: "http://127.0.0.1:19810"}
	full.Digest, _ = contracts.HeartbeatDigest(full)
	call := func(remote string) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(full)
		r := httptest.NewRequest("POST", "https://example.com/api/v1/agent/heartbeat/v2", bytes.NewReader(raw))
		r.RemoteAddr = remote
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+g.bearer)
		r.Header.Set("traceparent", "00-"+trace+"-0123456789abcdef-01")
		w := httptest.NewRecorder()
		admin.router.ServeHTTP(w, r)
		return w
	}
	// A remote worker cannot borrow the center address by claiming a loopback URL.
	if w := call("10.0.0.8:12345"); w.Code != 200 {
		t.Fatalf("remote heartbeat: %d %s", w.Code, w.Body)
	}
	if s.Store.Ent.Gate.GetX(t.Context(), int(g.id)).PublicHost != "" {
		t.Fatal("remote worker borrowed center address")
	}
	w := call("127.0.0.1:12345")
	if w.Code != 200 || w.Header().Get("X-Trace-Id") != trace {
		t.Fatalf("local heartbeat: %d %s", w.Code, w.Body)
	}
	view := mustJSON(t, admin, "GET", "/api/v1/admin/gates/"+itoa(int(g.id)), nil, 200)
	if view["public_host"] != "8.8.4.4" || view["online"] != true || view["colocated"] != true {
		t.Fatalf("local gate unavailable: %v", view)
	}
	row := s.Store.Ent.Audit.Query().Where(audit.ActionEQ("gate.address.initialize")).OnlyX(t.Context())
	if row.Detail["trace_id"] != trace || row.Detail["request_id"] != w.Header().Get("X-Request-Id") {
		t.Fatal("initialization lost request correlation")
	}
	for _, p := range s.presetsForGate(t.Context(), s.Store.Ent.Gate.GetX(t.Context(), int(g.id))) {
		if p.ID == "ss2022" && !p.Available {
			t.Fatal("initialized gate cannot create a node", p.Why)
		}
	}
	mustJSON(t, admin, "PATCH", "/api/v1/admin/gates/"+itoa(int(g.id)), map[string]any{"public_host": "8.8.8.8"}, 200)
	if w := call("127.0.0.1:12345"); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	if s.Store.Ent.Gate.GetX(t.Context(), int(g.id)).PublicHost != "8.8.8.8" {
		t.Fatal("heartbeat replaced administrator address")
	}
	t.Logf("trace=%s gate=%d stages=heartbeat,address-initialize,audit,presets,manual-address-preserved", trace, g.id)
}

func TestLocalPublicHostAmbiguity(t *testing.T) {
	original := localInterfaceAddrs
	t.Cleanup(func() { localInterfaceAddrs = original })
	for _, tc := range []struct {
		name string
		ips  []string
		want string
	}{
		{"private", []string{"127.0.0.1", "10.0.0.1", "172.18.0.1", "192.168.1.1"}, ""},
		{"single", []string{"10.0.0.1", "8.8.4.4"}, "8.8.4.4"},
		{"duplicate", []string{"8.8.4.4", "8.8.4.4"}, "8.8.4.4"},
		{"ambiguous", []string{"8.8.4.4", "8.8.8.8"}, ""},
		{"ipv6", []string{"2001:4860:4860::8888"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			localInterfaceAddrs = func() ([]net.Addr, error) {
				var out []net.Addr
				for _, ip := range tc.ips {
					out = append(out, &net.IPNet{IP: net.ParseIP(ip)})
				}
				return out, nil
			}
			if got, err := localPublicHost(); err != nil || got != tc.want {
				t.Fatalf("got %q, %v; want %q", got, err, tc.want)
			}
		})
	}
	localInterfaceAddrs = func() ([]net.Addr, error) { return nil, errors.New("interfaces unavailable") }
	if _, err := localPublicHost(); err == nil {
		t.Fatal("interface failure hidden")
	}
}

func TestGateUpdateRetainsLiveObservation(t *testing.T) {
	s, admin := newInstalledServer(t)
	g := pairFakeGate(t, admin)
	ctx := t.Context()
	s.Store.Ent.Gate.UpdateOneID(int(g.id)).SetLastSeenAt(time.Now().Add(-time.Hour)).SaveX(ctx)
	if err := s.Store.SetGateObservation(ctx, int(g.id), store.GateObservation{SeenAt: time.Now(), Host: map[string]any{"cpu_percent": 12.0}}); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/admin/gates/" + itoa(int(g.id))
	updated := mustJSON(t, admin, "PATCH", path, map[string]any{"public_host": "8.8.4.4"}, 200)
	current := mustJSON(t, admin, "GET", path, nil, 200)
	if updated["online"] != true || updated["last_seen_at"] != current["last_seen_at"] || updated["host"].(map[string]any)["cpu_percent"] != 12.0 {
		t.Fatalf("update returned stale runtime state: %v", updated)
	}
}
