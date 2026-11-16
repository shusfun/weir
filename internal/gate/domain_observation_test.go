package gate

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/kernelconf"
	"github.com/shusfun/weir/packages/contracts"
)

func TestDomainObservationPrivacyBaselinesAndUnknown(t *testing.T) {
	now := time.Now().UTC()
	d := domainCollector{}
	if err := d.setNetworks(contracts.CountryNetworks{Fetched: now, Prefixes: []netip.Prefix{netip.MustParsePrefix("1.2.3.0/24")}}, now); err != nil {
		t.Fatal(err)
	}
	cfg := contracts.GateConfig{Inbounds: []contracts.Inbound{{ID: 1, Kernel: "sing-box", Protocol: "socks", Tag: "entry", Port: 20001, Network: "tcp"}}, Credentials: []contracts.Credential{{ID: 1, InboundID: 1, StatsKey: "person-secret", UserKey: "user-secret", UUID: "user", Password: "pass"}}}
	tag := ""
	for name := range kernelconf.NativeInboundNames(cfg, "sing-box") {
		tag = name
	}
	if tag == "" {
		t.Fatal("missing native tag")
	}
	makeConn := func(id, host, ip string, n int64) nativeConnection {
		c := nativeConnection{ID: id, Start: "connection-start", Download: n}
		c.Metadata.Host = host
		c.Metadata.DestinationIP = ip
		c.Metadata.InboundName = tag
		c.Metadata.InboundUser = "person-secret"
		return c
	}
	plan := contracts.ConnectionObserver{Port: 20002, Secret: strings.Repeat("s", 32)}
	st := State{Epoch: "a", Seq: 1}
	snapshot := []nativeConnection{makeConn("raw-connection-id", "Domestic.Example", "1.2.3.4", 100), makeConn("foreign", "foreign.example", "8.8.8.8", 500), makeConn("url", "https://private.example/path", "1.2.3.5", 900)}
	d.absorb(&st, cfg, "sing-box", plan, snapshot, now)
	if len(st.Domains) != 0 {
		t.Fatal("first snapshot replayed existing bytes")
	}
	snapshot[0].Download = 140
	d.absorb(&st, cfg, "sing-box", plan, snapshot, now.Add(time.Second))
	rep := st.prepareMeter(true)
	if len(rep.Domains) != 1 || rep.Domains[0].Bytes != 40 || rep.Domains[0].Users == nil || *rep.Domains[0].Users != 1 {
		t.Fatalf("wrong observation: %+v", rep.Domains)
	}
	raw, _ := json.Marshal(st)
	for _, secret := range []string{"raw-connection-id", "1.2.3.4", "person-secret", "user-secret", "foreign.example", "private.example"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("private raw metadata persisted: %s", secret)
		}
	}
	snapshot[0].Download = 190
	d.absorb(&st, cfg, "sing-box", plan, snapshot, now.Add(2*time.Second))
	if st.prepareMeter(true).Domains[0].Bytes != 40 {
		t.Fatal("pending report changed")
	}
	if err := st.acknowledgeMeter(contracts.MeterAck{Epoch: "a", Seq: 1, Accepted: true}); err != nil {
		t.Fatal(err)
	}
	if st.prepareMeter(true).Domains[0].Bytes != 90 {
		t.Fatal("unacknowledged delta lost")
	}
	restarted := domainCollector{Prefixes: d.Prefixes}
	restarted.absorb(&st, cfg, "sing-box", plan, snapshot, now.Add(3*time.Second))
	for _, v := range st.Domains {
		if v.Bytes != 90 {
			t.Fatal("restart replayed bytes")
		}
	}
	unknown := snapshot[0]
	unknown.ID = "unknown"
	unknown.Metadata.InboundName = "unrecognized"
	unknown.Download = 10000
	d.absorb(&st, cfg, "sing-box", plan, []nativeConnection{unknown}, now.Add(4*time.Second))
	if st.DomainCoverage["sing-box"].Reason != "metadata_unavailable" {
		t.Fatal("missing metadata hidden")
	}
}

func TestDomainObservationNativeAPIAuthenticationAndBounds(t *testing.T) {
	calls := 0
	body := `{"connections":null}`
	status := 200
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/connections" || r.Header.Get("Authorization") != "Bearer "+strings.Repeat("s", 32) {
			t.Error("wrong native request")
		}
		w.Header().Set("Location", "http://127.0.0.1:1/secrets")
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	}))
	defer server.Close()
	port := server.Listener.Addr().(*net.TCPAddr).Port
	plan := contracts.ConnectionObserver{Port: port, Secret: strings.Repeat("s", 32)}
	if rows, err := readNativeConnections(t.Context(), plan); err != nil || len(rows) != 0 {
		t.Fatal("null native empty snapshot", err)
	}
	status = 302
	if _, err := readNativeConnections(t.Context(), plan); err == nil || calls != 2 {
		t.Fatal("redirect followed")
	}
	status = 200
	body = `{"connections":[{"id":"a","upload":1,"download":2,"metadata":{"host":"a.example","destinationIP":"1.2.3.4"}}]}`
	if rows, err := readNativeConnections(t.Context(), plan); err != nil || len(rows) != 1 || rows[0].Metadata.Host != "a.example" {
		t.Fatal("native schema", err)
	}
	body = strings.Repeat("x", (8<<20)+1)
	if _, err := readNativeConnections(t.Context(), plan); err == nil || err.Error() != "snapshot_limit" {
		t.Fatal("unbounded native body", err)
	}
}

func TestDomainObservationSingboxInboundMetadata(t *testing.T) {
	for _, tc := range []struct{ engine, kind, want string }{
		{"sing-box", "socks/weir-bridge-1", "weir-bridge-1"},
		{"sing-box", "vless/node-1", "node-1"},
		{"sing-box", "unknown/node-1", ""},
		{"sing-box", "socks/foo/node-1", ""},
		{"mihomo", "socks/node-1", ""},
	} {
		c := nativeConnection{}
		c.Metadata.Type = tc.kind
		if got := c.inboundName(tc.engine); got != tc.want {
			t.Errorf("%s %s: got %q, want %q", tc.engine, tc.kind, got, tc.want)
		}
	}
	// An explicit native tag takes precedence over the compatibility field.
	c := nativeConnection{}
	c.Metadata.Type, c.Metadata.InboundName = "socks/other", "node-1"
	if c.inboundName("sing-box") != "node-1" {
		t.Fatal("explicit tag overwritten")
	}
}

func TestDomainObservationIdentityMemoryBudgetIsGlobal(t *testing.T) {
	d := domainCollector{Users: map[string]map[string]bool{}}
	for i := 0; i < maxObservedIdentities; i++ {
		if !d.addIdentity(fmt.Sprintf("domain-%d", i), "user") {
			t.Fatal("identity rejected below memory budget")
		}
	}
	if !d.addIdentity("domain-0", "user") {
		t.Fatal("existing identity should not consume additional budget")
	}
	if d.addIdentity("overflow", "user") || d.UserCount != maxObservedIdentities {
		t.Fatal("identity memory budget exceeded")
	}
}
