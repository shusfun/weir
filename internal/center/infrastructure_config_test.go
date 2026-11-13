package center

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestInfrastructureOverridesDoNotRewriteFileDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "center.json")
	base := Config{Listen: "127.0.0.1:22341", Subscriptions: SubscriptionInfrastructure{FetchConcurrency: 3}}
	if err := saveConfig(path, base); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WEIR_SUB_FETCH_CONCURRENCY", "7")
	cfg, err := LoadConfig(path)
	if err != nil || cfg.Subscriptions.FetchConcurrency != 7 {
		t.Fatalf("override: %+v %v", cfg.Subscriptions, err)
	}
	cfg.DataDir = t.TempDir()
	if err = saveConfig(path, cfg); err != nil {
		t.Fatal(err)
	}
	var disk Config
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &disk); err != nil {
		t.Fatal(err)
	}
	if disk.Subscriptions.FetchConcurrency != 3 || disk.DataDir != cfg.DataDir {
		t.Fatal("environment persisted or unrelated setting lost")
	}
	t.Setenv("WEIR_SUB_FETCH_CONCURRENCY", "")
	if _, err = LoadConfig(path); err == nil {
		t.Fatal("empty explicit override accepted")
	}
}

func TestInfrastructureStatusTracksRestartAndAccess(t *testing.T) {
	s, admin := newInstalledServer(t)
	status := mustJSON(t, admin, "GET", "/api/v1/admin/infrastructure", nil, 200)
	if status["pending_restart"] != false {
		t.Fatalf("unexpected restart state: %v", status)
	}
	budget := float64(0)
	if s.Store.Driver == "postgres" {
		budget = 16
	}
	if status["effective"].(map[string]any)["postgres_connection_budget"] != budget {
		t.Fatal("incorrect database connection budget")
	}
	cfg := s.Config
	cfg.Subscriptions.FetchConcurrency = 6
	if err := saveConfig(s.Path, cfg); err != nil {
		t.Fatal(err)
	}
	status = mustJSON(t, admin, "GET", "/api/v1/admin/infrastructure", nil, 200)
	if status["pending_restart"] != true {
		t.Fatal("changed file not reported as pending")
	}
	anon := &testClient{t: t, router: admin.router}
	if w := anon.do("GET", "/api/v1/admin/infrastructure", nil, nil); w.Code != 401 {
		t.Fatalf("anonymous status %d", w.Code)
	}
}
