package gate

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/shusfun/weir/packages/contracts"
)

func TestHeartbeatSnapshotSamplingAcknowledgementAndRecovery(t *testing.T) {
	var state heartbeatState
	now := time.Now()
	snapshot := contracts.Heartbeat{Version: "v1", Hostname: "host", Arch: "amd64", Host: &contracts.HostSample{ObservedAt: now}}
	prepare := func(at time.Time) contracts.Heartbeat {
		t.Helper()
		wire, err := state.prepare(snapshot, at, 5*time.Minute, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		return wire
	}
	ack := func(wire contracts.Heartbeat, at time.Time) {
		state.acknowledge(wire, contracts.HeartbeatReply{Digest: wire.Digest}, at)
	}
	first := prepare(now)
	if !first.Full || first.Host == nil {
		t.Fatal("first snapshot missing")
	}
	// A lost response cannot advance either the digest or the sampling baseline.
	if retry := prepare(now.Add(30 * time.Second)); !retry.Full || retry.Host == nil {
		t.Fatal("lost acknowledgement was accepted")
	}
	ack(first, now)
	light := prepare(now.Add(30 * time.Second))
	if light.Full || light.Host != nil || light.Version != "" {
		t.Fatalf("unchanged heartbeat was not light: %+v", light)
	}
	fullBytes, _ := json.Marshal(first)
	lightBytes, _ := json.Marshal(light)
	if len(lightBytes) >= len(fullBytes) {
		t.Fatal("snapshot did not shrink")
	}
	sample := prepare(now.Add(time.Minute))
	if sample.Full || sample.Host == nil {
		t.Fatal("history sampling is tied to full snapshot")
	}
	ack(sample, now.Add(time.Minute))
	periodic := prepare(now.Add(5 * time.Minute))
	if !periodic.Full {
		t.Fatal("periodic reconciliation missing")
	}
	ack(periodic, now.Add(5*time.Minute))
	snapshot.Version = "v2"
	changed := prepare(now.Add(5*time.Minute + 30*time.Second))
	if !changed.Full || changed.Host != nil {
		t.Fatal("component change did not produce an independent snapshot")
	}
	ack(changed, now.Add(5*time.Minute+30*time.Second))
	state.acknowledge(changed, contracts.HeartbeatReply{NeedFull: true}, now)
	if recovery := prepare(now.Add(6 * time.Minute)); !recovery.Full {
		t.Fatal("center restart did not request reconciliation")
	}
}

func TestHeartbeatV2FallbackNeverSendsDeltaToLegacyHandler(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if strings.HasSuffix(r.URL.Path, "/v2") {
			w.WriteHeader(404)
			return
		}
		var h contracts.Heartbeat
		if err := json.NewDecoder(r.Body).Decode(&h); err != nil {
			t.Error(err)
		}
		if h.Protocol != 0 || h.Version == "" || h.Arch == "" {
			t.Error("legacy handler received a delta")
		}
		_ = json.NewEncoder(w).Encode(contracts.HeartbeatReply{})
	}))
	defer srv.Close()
	helper := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(HelperStatus{})
	}))
	defer helper.Close()
	a := &Agent{center: &centerClient{center: srv.URL, http: srv.Client(), token: "test", until: time.Now().Add(time.Hour)}, helper: &helperClient{http: &http.Client{Transport: helperTestTransport{url: helper.URL, base: http.DefaultTransport}}}}
	a.heartbeatProtocol.Store(2)
	if err := a.heartbeat(t.Context()); err != nil {
		t.Fatal(err)
	}
	if strings.Join(paths, ",") != "/api/v1/agent/heartbeat/v2,/api/v1/agent/heartbeat" || a.heartbeatProtocol.Load() != 0 {
		t.Fatalf("unsafe downgrade: %v", paths)
	}
}
