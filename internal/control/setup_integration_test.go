package control

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSetupWebPromotion(t *testing.T) {
	resetInstallDatabase(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	token, err := Initialize(path, Config{Listen: "127.0.0.1:19810", EncryptionKey: strings.Repeat("ab", 32)})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	previous := startWorkerSet
	var starts atomic.Int32
	startWorkerSet = func(s *Server, ctx context.Context) {
		starts.Add(1)
		previous(s, ctx)
	}
	t.Cleanup(func() { startWorkerSet = previous })
	server, err := New(cfg, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server.StartBackground(ctx)
	defer server.Close()
	router := server.Router()
	db := DatabaseConfig{Host: "127.0.0.1", Port: 25432, User: "gate", Password: "local-test-only", Database: "gate", SSLMode: "disable"}
	redis := RedisConfig{Address: "127.0.0.1:26379", Username: "migate", Password: "local-test-only"}
	wrong := redis
	wrong.Username = "wrong-acl-user"
	call := func(method, target string, body any, auth string, remote string, headers map[string]string) *httptest.ResponseRecorder {
		t.Helper()
		payload, _ := json.Marshal(body)
		request := httptest.NewRequest(method, target, bytes.NewReader(payload))
		request.Header.Set("Content-Type", "application/json")
		if auth != "" {
			request.Header.Set("Authorization", "Bearer "+auth)
		}
		if remote != "" {
			request.RemoteAddr = remote
		}
		for key, value := range headers {
			request.Header.Set(key, value)
		}
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		return recorder
	}
	if call(http.MethodPost, "/api/v1/setup/verify", nil, "", "", nil).Code != http.StatusUnauthorized {
		t.Fatal("missing token was accepted")
	}
	if call(http.MethodPost, "/api/v1/setup/verify", nil, "wrong-token", "", nil).Code != http.StatusUnauthorized {
		t.Fatal("wrong token was accepted")
	}
	if call(http.MethodPost, "/api/v1/setup/test-db", DatabaseConfig{Host: "127.0.0.1", Port: 1, User: "gate", Password: "local-test-only", Database: "gate", SSLMode: "disable"}, token, "", nil).Code != http.StatusBadRequest {
		t.Fatal("bad database was accepted")
	}
	if call(http.MethodPost, "/api/v1/setup/test-db", db, token, "", nil).Code != http.StatusOK {
		t.Fatal("database retry did not recover")
	}
	if call(http.MethodPost, "/api/v1/setup/test-redis", wrong, token, "", nil).Code != http.StatusBadRequest {
		t.Fatal("wrong redis acl user was accepted")
	}
	if call(http.MethodPost, "/api/v1/setup/test-redis", redis, token, "", nil).Code != http.StatusOK {
		t.Fatal("redis acl user was rejected")
	}
	installed := call(http.MethodPost, "/api/v1/setup/install", map[string]any{"database": db, "redis": redis, "email": "admin-web@isolated.test", "password": "isolated-web-password"}, token, "", nil)
	if installed.Code != http.StatusCreated {
		t.Fatalf("install = %d %s", installed.Code, installed.Body.String())
	}
	status := call(http.MethodGet, "/api/v1/setup/status", nil, "", "", nil)
	if status.Code != 200 || !strings.Contains(status.Body.String(), `"ready":true`) || starts.Load() != 1 {
		t.Fatalf("status=%s starts=%d", status.Body.String(), starts.Load())
	}
	server.ensureWorkers()
	if starts.Load() != 1 {
		t.Fatal("workers started twice")
	}
	health := call(http.MethodGet, "/healthz", nil, "", "", nil)
	if !strings.Contains(health.Body.String(), `"status":"ok"`) {
		t.Fatalf("health = %s", health.Body.String())
	}
	if call(http.MethodPost, "/api/v1/setup/install", map[string]any{}, token, "", nil).Code != http.StatusConflict {
		t.Fatal("completed install was accepted again")
	}
	secure := call(http.MethodPost, "/api/v1/auth/login", map[string]any{"email": "admin-web@isolated.test", "password": "isolated-web-password"}, "", "127.0.0.1:1234", map[string]string{"X-Forwarded-Proto": "https", "X-Forwarded-Host": "sub.example.test"})
	if secure.Code != 200 || !strings.Contains(secure.Header().Get("Set-Cookie"), "Secure") {
		t.Fatalf("proxy login = %d cookie=%q", secure.Code, secure.Header().Get("Set-Cookie"))
	}
	plain := call(http.MethodPost, "/api/v1/auth/login", map[string]any{"email": "admin-web@isolated.test", "password": "isolated-web-password"}, "", "192.0.2.1:1234", map[string]string{"X-Forwarded-Proto": "https"})
	if plain.Code != 200 || strings.Contains(plain.Header().Get("Set-Cookie"), "Secure") {
		t.Fatalf("untrusted proxy upgraded the request: %d %q", plain.Code, plain.Header().Get("Set-Cookie"))
	}
	cancel()
	server.Close()
	reloaded, err := LoadConfig(path)
	if err != nil || !reloaded.Installed {
		t.Fatal(err)
	}
	restarted, err := New(reloaded, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	restartCtx, restartCancel := context.WithCancel(context.Background())
	defer restartCancel()
	restarted.StartBackground(restartCtx)
	recorder := httptest.NewRecorder()
	restarted.Router().ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/setup/install", strings.NewReader("{}")))
	if recorder.Code != http.StatusConflict {
		t.Fatalf("reloaded install = %d", recorder.Code)
	}
}

func TestSetupCLIAndWebInstallOnce(t *testing.T) {
	resetInstallDatabase(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	token, err := Initialize(path, Config{Listen: "127.0.0.1:9", EncryptionKey: strings.Repeat("ab", 32)})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	server, err := New(cfg, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server.StartBackground(ctx)
	defer server.Close()
	db := DatabaseConfig{Host: "127.0.0.1", Port: 25432, User: "gate", Password: "local-test-only", Database: "gate", SSLMode: "disable"}
	redis := RedisConfig{Address: "127.0.0.1:26379", Username: "migate", Password: "local-test-only"}
	var wg sync.WaitGroup
	var cliErr error
	var httpCode int
	wg.Add(2)
	go func() {
		defer wg.Done()
		cliErr = ApplySetup(path, SetupAnswers{Database: db, Redis: redis, Email: "admin-cli@isolated.test", Password: "isolated-cli-password", Listen: "127.0.0.1:9", Confirm: true})
	}()
	go func() {
		defer wg.Done()
		body, _ := json.Marshal(map[string]any{"database": db, "redis": redis, "email": "admin-web@isolated.test", "password": "isolated-web-password"})
		request := httptest.NewRequest(http.MethodPost, "/api/v1/setup/install", bytes.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer "+token)
		recorder := httptest.NewRecorder()
		server.Router().ServeHTTP(recorder, request)
		httpCode = recorder.Code
	}()
	wg.Wait()
	if cliErr != nil {
		t.Fatal(cliErr)
	}
	if httpCode != http.StatusCreated && httpCode != http.StatusConflict {
		t.Fatalf("web install = %d", httpCode)
	}
	deadline := time.Now().Add(5 * time.Second)
	for server.Store == nil && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if server.Store == nil {
		t.Fatal("running service did not keep a database connection")
	}
	count, err := server.Store.User.Query().Count(context.Background())
	if err != nil || count != 1 {
		t.Fatalf("admin count=%d err=%v http=%d", count, err, httpCode)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "initialization.txt")); !os.IsNotExist(statErr) {
		t.Fatal("legacy token file was created")
	}
}

func TestSetupCLIInstallsIsolatedServices(t *testing.T) {
	resetInstallDatabase(t)
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"listen":"127.0.0.1:9"}`), 0600); err != nil {
		t.Fatal(err)
	}
	secrets := []string{"local-test-only", "local-test-only", "isolated-cli-password", "isolated-cli-password"}
	lines := "\n25432\n\n\n\n" + "\n26379\n\nn\n" + "admin-cli@isolated.test\n" + "\n\n" + "y\n"
	if err := RunSetupCLI(path, strings.NewReader(lines), func(string) (string, error) {
		value := secrets[0]
		secrets = secrets[1:]
		return value, nil
	}); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil || !cfg.Installed || cfg.Database.User != "gate" || cfg.Redis.Username != "migate" || cfg.Listen != "127.0.0.1:9" {
		t.Fatalf("config=%+v err=%v", cfg, err)
	}
	server, err := New(cfg, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	body, _ := json.Marshal(map[string]any{"email": "admin-cli@isolated.test", "password": "isolated-cli-password"})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	server.Router().ServeHTTP(recorder, request)
	if recorder.Code != 200 {
		t.Fatalf("login = %d %s", recorder.Code, recorder.Body.String())
	}
	recorder = httptest.NewRecorder()
	server.Router().ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/setup/install", strings.NewReader("{}")))
	if recorder.Code != http.StatusConflict {
		t.Fatalf("second install = %d", recorder.Code)
	}
}

func resetInstallDatabase(t *testing.T) {
	t.Helper()
	if os.Getenv("MIGATE_INTEGRATION") != "1" {
		t.Skip("requires isolated local Docker services")
	}
	db, err := sql.Open("pgx", "postgres://gate:local-test-only@127.0.0.1:25432/gate?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err = db.ExecContext(ctx, "DROP SCHEMA IF EXISTS migate CASCADE"); err != nil {
		t.Fatal(err)
	}
}
