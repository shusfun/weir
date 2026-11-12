package layahost

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shusfun/weir/packages/laya"
)

func TestControllerOnlyUsesApprovedFixedActions(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	doc := Deployment{Schema: 1, Version: "v0.18.0", Model: laya.DefaultModel, Spec: laya.DockerSpec{Name: "test-laya", Repository: "ghcr.io/ollaya-dev/ollaya", Digest: digest, ModelVolume: "test-models", UID: 1000, GID: 1000, HostPort: 11435, MemoryBytes: 2 << 30, CPUMillis: 1000}, Policy: laya.DockerPolicy{Repositories: []string{"ghcr.io/ollaya-dev/ollaya"}, Digests: []string{digest}, MaxMemoryBytes: 2 << 30, MaxCPUMillis: 1000}}
	calls := 0
	c := Controller{Load: func() (Deployment, error) { return doc, nil }, Execute: func(context.Context, []string) ([]byte, error) {
		calls++
		return nil, errors.New("password=DOCKER_PRIVATE_ERROR")
	}}
	for _, body := range []string{
		`{"operation":"exec","expected_digest":"` + digest + `","expected_version":"v0.18.0"}`,
		`{"operation":"ensure","expected_digest":"sha256:` + strings.Repeat("b", 64) + `","expected_version":"v0.18.0"}`,
		`{"operation":"ensure","expected_digest":"` + digest + `","expected_version":"v0.18.0","args":["--privileged"]}`,
	} {
		w := httptest.NewRecorder()
		c.Handler().ServeHTTP(w, httptest.NewRequest("POST", "/laya", strings.NewReader(body)))
		if w.Code < 400 || calls != 0 {
			t.Fatalf("unapproved operation executed: %d %d", w.Code, calls)
		}
	}
	request := httptest.NewRequest("POST", "/laya", strings.NewReader(`{"operation":"status","expected_digest":"`+digest+`","expected_version":"v0.18.0"}`))
	const trace = "0123456789abcdef0123456789abcdef"
	request.Header.Set("traceparent", "00-"+trace+"-1111111111111111-01")
	w := httptest.NewRecorder()
	c.Handler().ServeHTTP(w, request)
	if w.Code != 422 || strings.Contains(w.Body.String(), "DOCKER_PRIVATE_ERROR") || !strings.Contains(w.Header().Get("traceparent"), trace) {
		t.Fatalf("error boundary: %d %s", w.Code, w.Body.String())
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := c.Operation(ctx, Request{Operation: "ensure", ExpectedDigest: digest, ExpectedVersion: doc.Version}); !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("cancel reached Docker: %d %v", calls, err)
	}
}
