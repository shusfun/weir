//go:build !windows

package agent

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"path/filepath"
	"sync"
	"testing"

	"github.com/imzyb/MiGate/internal/xray"
)

type fixedStats struct{ xray.StubStatsClient }

func (fixedStats) QueryAllStats(context.Context) (map[string]*xray.ClientStats, error) {
	return map[string]*xray.ClientStats{"grant-1": {Uplink: 100}}, nil
}

func TestEnforcementRetrySurvivesAgentRestart(t *testing.T) {
	ctx := context.Background()
	config := Config{DataDir: t.TempDir(), Socket: filepath.Join(t.TempDir(), "helper.sock")}
	listener, err := net.Listen("unix", config.Socket)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	attempts := 0
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var state State
		if json.NewDecoder(r.Body).Decode(&state) != nil || len(state.Grants) != 1 || state.Grants[0].Enabled {
			t.Error("quota-exhausted client must be disabled in applied state")
		}
		mu.Lock()
		defer mu.Unlock()
		attempts++
		if attempts == 1 {
			w.WriteHeader(503)
			return
		}
		w.WriteHeader(200)
	})}
	go server.Serve(listener)
	defer server.Close()
	a, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	a.stats.Close()
	a.stats = &fixedStats{}
	a.State.Grants = []LocalGrant{{GrantID: 1, Enabled: true, DesiredEnabled: true, Allocated: 10}}
	a.sample(ctx)
	if !a.State.ApplyPending || a.TrafficStatus != "enforcement_error" {
		t.Fatal("failed helper apply lost pending enforcement")
	}
	a.DB.Close()
	b, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer b.DB.Close()
	b.stats.Close()
	b.stats = &fixedStats{}
	defer b.stats.Close()
	b.sample(ctx)
	mu.Lock()
	defer mu.Unlock()
	if attempts != 2 || b.State.ApplyPending || b.State.Grants[0].Enabled {
		t.Fatal("pending enforcement was not retried after restart")
	}
}
