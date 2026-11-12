package gate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shusfun/weir/internal/releasefiles"
)

func TestClientSendsLeaseOnlyToUploadEndpoints(t *testing.T) {
	const trace = "0123456789abcdef0123456789abcdef"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/v1/agent/uploads/") {
			if r.Header.Get("X-Weir-Task-ID") != "task" || r.Header.Get("X-Weir-Task-Attempt") != "2" || r.Header.Get("X-Weir-Task-Lease") != "secret" {
				t.Error("upload lost its in-memory lease")
			}
		} else if r.Header.Get("X-Weir-Task-Lease") != "" {
			t.Error("lease leaked to another endpoint")
		}
		if !strings.Contains(r.Header.Get("traceparent"), trace) {
			t.Error("upload lost root correlation")
		}
		w.WriteHeader(204)
	}))
	defer srv.Close()
	c := &centerClient{center: srv.URL}
	ctx := releasefiles.WithTrace(t.Context(), "00-"+trace+"-1111111111111111-01")
	ctx = context.WithValue(ctx, taskLeaseKey{}, taskLease{id: "task", attempt: 2, token: "secret"})
	ctx = context.WithValue(ctx, taskStageKey{}, &taskStageLog{})
	for _, path := range []string{"/agent/uploads/abc", "/agent/uploads/abc/progress", "/agent/meter"} {
		resp, err := c.raw(ctx, srv.Client(), "POST", path, nil, false)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}
}
