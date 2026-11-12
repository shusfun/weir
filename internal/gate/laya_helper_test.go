package gate

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shusfun/weir/packages/laya"
)

func TestLayaHelperRequiresApprovedFixedDeployment(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	doc := layaDeployment{Schema: 1, Version: "v0.18.0", Model: "laya:multilingual", Spec: laya.DockerSpec{Name: "example-laya", Repository: "ghcr.io/ollaya-dev/ollaya", Digest: digest, ModelVolume: "example-models", UID: 1000, GID: 1000, HostPort: 11435, MemoryBytes: 2 << 30, CPUMillis: 1000}}
	doc.Policy = laya.DockerPolicy{Repositories: []string{doc.Spec.Repository}, Digests: []string{digest}, MaxMemoryBytes: 4 << 30, MaxCPUMillis: 2000}
	h := NewHelper(Paths{})
	h.laya.load = func() (layaDeployment, error) { return doc, nil }
	calls := 0
	h.laya.execute = func(_ context.Context, args []string) ([]byte, error) {
		calls++
		if args[0] != "inspect" {
			t.Fatalf("unexpected mutation: %v", args)
		}
		return nil, laya.ErrContainerMissing
	}
	for _, tc := range []struct {
		body   string
		status int
	}{
		{`{"operation":"exec","expected_digest":"` + digest + `","expected_version":"v0.18.0"}`, 400},
		{`{"operation":"ensure","expected_digest":"sha256:` + strings.Repeat("b", 64) + `","expected_version":"v0.18.0"}`, 409},
		{`{"operation":"ensure","expected_digest":"` + digest + `","expected_version":"v0.19.0"}`, 409},
		{`{"operation":"ensure","expected_digest":"` + digest + `","expected_version":"v0.18.0","args":["--privileged"]}`, 400},
		{`{"operation":"status","expected_digest":"` + digest + `","expected_version":"v0.18.0"}`, 422},
	} {
		w := httptest.NewRecorder()
		h.Handler().ServeHTTP(w, httptest.NewRequest("POST", "/laya", strings.NewReader(tc.body)))
		if w.Code != tc.status {
			t.Fatalf("status=%d want=%d %s", w.Code, tc.status, w.Body.String())
		}
	}
	if calls != 1 {
		t.Fatalf("invalid request reached Docker: %d", calls)
	}
	doc.Spec.Repository = "evil.example/image"
	doc.Policy.Repositories = []string{doc.Spec.Repository}
	body, _ := json.Marshal(layaHelperRequest{Operation: "ensure", ExpectedDigest: digest, ExpectedVersion: doc.Version})
	w := httptest.NewRecorder()
	h.Handler().ServeHTTP(w, httptest.NewRequest("POST", "/laya", strings.NewReader(string(body))))
	if w.Code != 409 || calls != 1 {
		t.Fatal("nonofficial image reached Docker")
	}
}
