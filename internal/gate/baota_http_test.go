package gate

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBaotaStandardWebrootOnly(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "default.conf")
	base := "server {\nlisten 80;\nserver_name _;\nroot " + root + ";\n}\n"
	check := func(conf string, want bool) {
		t.Helper()
		if err := os.WriteFile(path, []byte(conf), 0600); err != nil {
			t.Fatal(err)
		}
		if got := baotaHTTPRoot(path, root) == nil; got != want {
			t.Fatalf("accept=%v want %v", got, want)
		}
	}
	check(base, true)
	for _, extra := range []string{"location / { proxy_pass http://app; }", "return 301 https://$host$request_uri;", "include other.conf;"} {
		check(base+extra, false)
	}
	check("server {\nlisten 443 ssl;\nserver_name _;\nroot "+root+";\n}", false)
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(path, []byte("server {\nlisten 80;\nserver_name _;\nroot "+link+";\n}"), 0600)
	if baotaHTTPRoot(path, link) == nil {
		t.Fatal("symlink root accepted")
	}
}

func TestBaotaChallengeServedAndCleaned(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.FileServer(http.Dir(root)))
	defer srv.Close()
	old := acmePort
	defer func() { acmePort = old }()
	acmePort = srv.Listener.Addr().(*net.TCPAddr).Port
	token := "test-challenge-1234567890"
	auth := token + ".authorization"
	if err := serveBaotaChallenge(root, token, auth, "203.0.113.1", 1); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, ".well-known/acme-challenge", token)
	b, err := os.ReadFile(file)
	if err != nil || string(b) != auth {
		t.Fatal("challenge unavailable")
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(file); os.IsNotExist(err) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("challenge not cleaned")
}

func TestBaotaChallengeRejectsRedirectAndPreservesExistingFile(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "https://example.com/", 302) }))
	defer srv.Close()
	old := acmePort
	defer func() { acmePort = old }()
	acmePort = srv.Listener.Addr().(*net.TCPAddr).Port
	token := "test-challenge-1234567890"
	file := filepath.Join(root, ".well-known/acme-challenge", token)
	if serveBaotaChallenge(root, token, token+".value", "203.0.113.1", 1) == nil {
		t.Fatal("redirect accepted")
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatal("failed challenge not cleaned")
	}
	if err := os.WriteFile(file, []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	if serveBaotaChallenge(root, token, token+".value", "203.0.113.1", 1) == nil {
		t.Fatal("existing file overwritten")
	}
	b, _ := os.ReadFile(file)
	if string(b) != "existing" {
		t.Fatal("unrelated file changed")
	}
}
