package control

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInitializeKeepsOneTokenSource(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(filepath.Join(dir, "initialization.txt"), []byte("stale-token"), 0600); err != nil {
		t.Fatal(err)
	}
	token, err := Initialize(path, Config{Listen: "127.0.0.1:19810", EncryptionKey: strings.Repeat("ab", 32)})
	if err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "initialization.txt")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatal("initialization.txt remains a second token source")
	}
	stored, err := os.ReadFile(filepath.Join(dir, "setup-token"))
	if err != nil || string(stored) != token {
		t.Fatalf("setup-token = %q err=%v", stored, err)
	}
	configText, err := os.ReadFile(path)
	if err != nil || strings.Contains(string(configText), token) {
		t.Fatal("config stores the plaintext token")
	}
	cred, err := readSetupCredential(path)
	if err != nil || cred.Hash != digest(token) || cred.Installed || !cred.Expires.After(time.Now()) {
		t.Fatalf("credential = %+v err=%v", cred, err)
	}
}

func TestRunningServerAcceptsRotatedTokenOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	cfg := Config{Listen: "127.0.0.1:19810", EncryptionKey: strings.Repeat("ab", 32)}
	oldToken, err := Initialize(path, cfg)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err = LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	server, err := New(cfg, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	listener := httptest.NewServer(server.Router())
	defer listener.Close()
	listen := strings.TrimPrefix(listener.URL, "http://")
	newToken, err := Initialize(path, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err = ConfirmTokenActive(listen, newToken, oldToken); err != nil {
		t.Fatal(err)
	}
	expired := cfg
	expired.SetupHash = digest("expired-token")
	expired.SetupExpires = time.Now().Add(-time.Minute)
	if err = saveConfig(path, expired); err != nil {
		t.Fatal(err)
	}
	if status, _ := postSetupVerify(listen, "expired-token"); status != http.StatusUnauthorized {
		t.Fatalf("expired status = %d", status)
	}
	if err = os.WriteFile(path, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	server.Router().ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/setup/verify", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("corrupt config status = %d", recorder.Code)
	}
}

func TestWorkersStartOnce(t *testing.T) {
	starts := 0
	previous := startWorkerSet
	startWorkerSet = func(*Server, context.Context) { starts++ }
	t.Cleanup(func() { startWorkerSet = previous })
	server := &Server{}
	server.BindRuntime(context.Background())
	server.ensureWorkers()
	server.ensureWorkers()
	if starts != 1 || !server.workersReady.Load() {
		t.Fatalf("starts=%d ready=%v", starts, server.workersReady.Load())
	}
}

func TestSetupCLIInputClosedCancelAndRedaction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	err := RunSetupCLI(path, strings.NewReader(""), func(string) (string, error) {
		t.Fatal("password prompt should not run")
		return "", nil
	})
	if !errors.Is(err, errInputClosed) {
		t.Fatalf("eof err = %v", err)
	}
	if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatal("eof wrote configuration")
	}

	restore := stubSetupFlow(t)
	defer restore()
	var applied bool
	applySetupFn = func(string, SetupAnswers) error {
		applied = true
		return nil
	}
	secret := "unique-secret-value"
	passwords := []string{secret, secret, secret, secret}
	lines := strings.Repeat("\n", 5) + strings.Repeat("\n", 3) + "n\nadmin@example.com\n" + strings.Repeat("\n", 2) + "n\n"
	output := captureStderr(t, func() {
		err = RunSetupCLI(path, strings.NewReader(lines), func(string) (string, error) {
			if len(passwords) == 0 {
				t.Fatal("unexpected password prompt")
			}
			value := passwords[0]
			passwords = passwords[1:]
			return value, nil
		})
	})
	if !errors.Is(err, errInstallationCancelled) || applied {
		t.Fatalf("cancel err=%v applied=%v", err, applied)
	}
	if strings.Contains(output, secret) {
		t.Fatal("summary leaked the password")
	}
	if !strings.Contains(output, "admin@example.com") || !strings.Contains(output, "schema=migate") {
		t.Fatalf("summary missing public fields: %s", output)
	}
}

func TestSetupCLIRetriesConnectionAndRejectsMismatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	restore := stubSetupFlow(t)
	defer restore()
	calls := 0
	testDatabaseFn = func(DatabaseConfig) error {
		calls++
		if calls == 1 {
			return errors.New("database connection failed")
		}
		return nil
	}
	var applied SetupAnswers
	applySetupFn = func(_ string, answers SetupAnswers) error {
		applied = answers
		return nil
	}
	secrets := []string{"db-password-12", "db-password-12", "redis-password", "short", "short", "long-enough-password", "other-password-1", "long-enough-password", "long-enough-password"}
	database := strings.Repeat("\n", 5)
	redis := strings.Repeat("\n", 3) + "n\n"
	listen := strings.Repeat("\n", 2)
	lines := database + "\n" + database + redis + "admin@example.com\nadmin@example.com\nadmin@example.com\n" + listen + "y\n"
	err := RunSetupCLI(path, strings.NewReader(lines), func(string) (string, error) {
		if len(secrets) == 0 {
			return "", io.EOF
		}
		value := secrets[0]
		secrets = secrets[1:]
		return value, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || !applied.Confirm || applied.Database.User != "gate" || applied.Redis.Username != "migate" || applied.Listen != "127.0.0.1:19810" {
		t.Fatalf("calls=%d answers=%+v redis=%+v", calls, applied, applied.Redis)
	}
	if applied.Password != "long-enough-password" || len(secrets) != 0 {
		t.Fatalf("password confirmation leftover=%q password=%q", secrets, applied.Password)
	}
}

func stubSetupFlow(t *testing.T) func() {
	t.Helper()
	previousDB, previousRedis := testDatabaseFn, testRedisFn
	previousApply, previousProbe, previousWait := applySetupFn, listenProbe, waitReady
	testDatabaseFn = func(DatabaseConfig) error { return nil }
	testRedisFn = func(RedisConfig) error { return nil }
	listenProbe = func(string) bool { return false }
	waitReady = func(string) error { return nil }
	return func() {
		testDatabaseFn, testRedisFn = previousDB, previousRedis
		applySetupFn, listenProbe, waitReady = previousApply, previousProbe, previousWait
	}
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stderr
	os.Stderr = writer
	fn()
	writer.Close()
	os.Stderr = previous
	out, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}
