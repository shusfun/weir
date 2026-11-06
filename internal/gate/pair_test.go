package gate

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/shusfun/weir/packages/contracts"
)

func TestPairReplaceIdentity(t *testing.T) {
	for _, status := range []int{http.StatusCreated, http.StatusForbidden} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			root := t.TempDir()
			p := Paths{Config: filepath.Join(root, "agent.json"), Data: root}
			old := Config{Center: "https://old.example.test", GateID: 42, PrivateKey: "old-device-key"}
			if err := saveConfig(p.Config, old, ""); err != nil {
				t.Fatal(err)
			}
			statePath := filepath.Join(root, "state.json")
			if err := os.WriteFile(statePath, []byte(`{"applied":900,"epoch":"old-center"}`), 0600); err != nil {
				t.Fatal(err)
			}
			cachePath := filepath.Join(root, "keep-kernel-cache")
			if err := os.WriteFile(cachePath, []byte("cached"), 0600); err != nil {
				t.Fatal(err)
			}
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				var req contracts.PairRequest
				if r.Method != "POST" || r.URL.Path != "/api/v1/agent/pair" || json.NewDecoder(r.Body).Decode(&req) != nil || req.Token != "new-invite" || req.PublicKey == "" {
					t.Error("invalid pairing request")
				}
				if !regexp.MustCompile(`^00-[0-9a-f]{32}-[0-9a-f]{16}-01$`).MatchString(r.Header.Get("traceparent")) {
					t.Error("missing pairing trace")
				}
				w.Header().Set("X-Request-Id", "123456abcdef")
				w.Header().Set("X-Trace-Id", "0123456789abcdef0123456789abcdef")
				w.WriteHeader(status)
				if status == http.StatusCreated {
					_ = json.NewEncoder(w).Encode(contracts.PairResult{GateID: 1})
				} else {
					_, _ = w.Write([]byte(`{"error":{"code":"pair_token_invalid","detail":"must-not-log-response-detail"}}`))
				}
			}))
			defer srv.Close()
			// Ordinary invocation retains the identity without consuming a new invite.
			if err := Pair(context.Background(), p, srv.URL, "new-invite", "VPS", "", false); err != nil || calls != 0 {
				t.Fatalf("existing pairing: calls=%d err=%v", calls, err)
			}
			err := Pair(context.Background(), p, srv.URL, "new-invite", "VPS", "", true)
			got, loadErr := LoadConfig(p.Config)
			if loadErr != nil || calls != 1 {
				t.Fatalf("pair calls=%d load=%v", calls, loadErr)
			}
			if status == http.StatusCreated {
				if err != nil || got.Center != srv.URL || got.GateID != 1 || got.PrivateKey == old.PrivateKey {
					t.Fatalf("replacement failed: err=%v", err)
				}
				if _, err := os.Stat(statePath); !os.IsNotExist(err) {
					t.Fatal("old center state remains after replacement")
				}
			} else {
				if err == nil || got != old {
					t.Fatal("rejected replacement changed device identity")
				}
				if b, err := os.ReadFile(statePath); err != nil || string(b) != `{"applied":900,"epoch":"old-center"}` {
					t.Fatal("rejected replacement changed agent state")
				}
				if !strings.Contains(err.Error(), "pair_token_invalid") || !strings.Contains(err.Error(), "123456abcdef") || strings.Contains(err.Error(), "must-not-log") {
					t.Fatalf("pair failure is not useful or safe: %v", err)
				}
				if _, err := NewAgent(p); err != nil {
					t.Fatalf("rejected pairing blocked the previous identity: %v", err)
				}
			}
			if b, err := os.ReadFile(cachePath); err != nil || string(b) != "cached" {
				t.Fatal("replacement changed unrelated files")
			}
		})
	}
}

func TestPairFirstInstall(t *testing.T) {
	for _, replace := range []bool{false, true} {
		root := t.TempDir()
		p := Paths{Config: filepath.Join(root, "config", "agent.json"), Data: filepath.Join(root, "state")}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(contracts.PairResult{GateID: 7})
		}))
		if err := Pair(t.Context(), p, srv.URL, "fresh-invite", "fresh", "", replace); err != nil {
			t.Fatal(err)
		}
		srv.Close()
		if cfg, err := LoadConfig(p.Config); err != nil || cfg.GateID != 7 {
			t.Fatalf("first pairing: id=%d err=%v", cfg.GateID, err)
		}
	}
}

func TestPairLocalFailureStopsAgentUntilRepaired(t *testing.T) {
	for _, failure := range []string{"marker", "identity", "state"} {
		t.Run(failure, func(t *testing.T) {
			root := t.TempDir()
			p := Paths{Config: filepath.Join(root, "agent.json"), Data: root}
			if err := saveConfig(p.Config, Config{Center: "https://old.example.test", GateID: 42, PrivateKey: "old-key"}, ""); err != nil {
				t.Fatal(err)
			}
			blocked := map[string]string{"marker": p.Config + ".pairing", "identity": p.Config + ".new", "state": filepath.Join(root, "state.json")}[failure]
			if err := os.Mkdir(blocked, 0700); err != nil {
				t.Fatal(err)
			}
			if failure != "marker" {
				if err := os.WriteFile(filepath.Join(blocked, "keep"), []byte("cannot replace directory as a file"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusCreated)
				_ = json.NewEncoder(w).Encode(contracts.PairResult{GateID: 1})
			}))
			defer srv.Close()
			if err := Pair(t.Context(), p, srv.URL, "valid", "VPS", "", true); err == nil {
				t.Fatal("local pairing failure was reported as success")
			}
			if _, err := NewAgent(p); err == nil {
				t.Fatal("agent can start with an incomplete identity replacement")
			}
			if err := Pair(t.Context(), p, srv.URL, "valid", "VPS", "", false); err == nil {
				t.Fatal("ordinary pairing bypassed incomplete replacement")
			}
			if failure == "marker" {
				if err := os.Remove(blocked); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.RemoveAll(blocked); err != nil {
					t.Fatal(err)
				}
			}
			if err := Pair(t.Context(), p, srv.URL, "new-valid", "VPS", "", true); err != nil {
				t.Fatal(err)
			}
			if _, err := NewAgent(p); err != nil {
				t.Fatalf("agent cannot start after repair: %v", err)
			}
			if _, err := os.Stat(p.Config + ".pairing"); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("completed pairing retained its startup barrier")
			}
		})
	}
}
