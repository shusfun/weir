package center

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func TestRequestLogKeepsCauseAndDropsSecrets(t *testing.T) {
	buf := captureLog(t)
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(requestLog)
	r.GET("/sub/:token", func(c *gin.Context) {
		failInternal(c, 500, "save_failed", errors.New(`fetch https://u:p@up.example/api/v1/client/subscribe?token=SRC-SECRET: unexpected EOF`))
	})
	r.GET("/conflict", func(c *gin.Context) { failDetail(c, 409, "name_taken", "DETAIL-SECRET", nil) })
	r.GET("/me", func(c *gin.Context) { failure(c, 401, "unauthorized") })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/sub/LINK-SECRET?flag=clash", nil))
	id := w.Header().Get("X-Request-Id")
	if w.Code != 500 || len(id) != 12 || strings.Contains(w.Body.String(), "EOF") {
		t.Fatalf("status %d, id %q, body %s", w.Code, id, w.Body)
	}
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/conflict", nil))
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/me", nil))

	out := buf.String()
	for _, want := range []string{"level=ERROR", "id=" + id, "route=/sub/:token", "code=save_failed", "https://up.example/… unexpected EOF", "level=WARN", "code=name_taken"} {
		if !strings.Contains(out, want) {
			t.Errorf("log lacks %q:\n%s", want, out)
		}
	}
	for _, secret := range []string{"LINK-SECRET", "SRC-SECRET", "u:p@", "clash", "DETAIL-SECRET", "unauthorized"} {
		if strings.Contains(out, secret) {
			t.Errorf("log leaks %q:\n%s", secret, out)
		}
	}
}

func TestTaskLogReportsChangesOnly(t *testing.T) {
	buf := captureLog(t)
	report := taskLog{}
	report.note("renew-certs", errors.New("acme: rate limited"))
	report.note("renew-certs", errors.New("acme: rate limited"))
	report.note("renew-certs", errors.New("acme: timeout"))
	report.note("renew-certs", nil)
	report.note("renew-certs", nil)
	out := buf.String()
	if strings.Count(out, "task failed") != 2 || strings.Count(out, "task recovered") != 1 {
		t.Fatalf("want 2 failures and 1 recovery:\n%s", out)
	}
}

func TestRouterSetsRequestID(t *testing.T) {
	_, admin := newInstalledServer(t)
	if w := admin.do("GET", "/api/v1/me", nil, nil); w.Header().Get("X-Request-Id") == "" {
		t.Fatalf("no X-Request-Id: %v", w.Header())
	}
}
