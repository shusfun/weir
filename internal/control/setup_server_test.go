package control

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUninstalledServerOnlyExposesSetup(t *testing.T) {
	server, err := New(Config{
		Listen:        "127.0.0.1:19810",
		EncryptionKey: strings.Repeat("ab", 32),
	}, t.TempDir()+"/config.json", nil)
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	server.Router().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if recorder.Code != 200 || !strings.Contains(recorder.Body.String(), "setup_required") {
		t.Fatalf("healthz = %d %s", recorder.Code, recorder.Body.String())
	}
	recorder = httptest.NewRecorder()
	server.Router().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings", nil))
	if recorder.Code == 200 {
		t.Fatal("admin API must stay unavailable before setup")
	}
}
