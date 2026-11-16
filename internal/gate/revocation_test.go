package gate

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestRevocationRequiresExplicitAuthenticatedResult(t *testing.T) {
	for _, tc := range []struct {
		status  int
		body    string
		revoked bool
	}{
		{403, `{"error":{"code":"gate_revoked"}}`, true},
		{401, `{"error":{"code":"gate_revoked"}}`, false},
		{403, `{"error":{"code":"forbidden"}}`, false},
		{502, `gateway unavailable`, false},
	} {
		err := centerError(&http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body))})
		if errors.Is(err, errGateRevoked) != tc.revoked {
			t.Fatalf("status %d body %s: %v", tc.status, tc.body, err)
		}
	}
}

func TestRevocationPersistsAcrossHelperAndAgentRestart(t *testing.T) {
	dir := t.TempDir()
	p := Paths{KernelsDir: filepath.Join(dir, "kernels"), CoresDir: filepath.Join(dir, "configs")}
	h := NewHelper(p)
	w := httptest.NewRecorder()
	h.disable(w, httptest.NewRequest(http.MethodPost, "/disable", nil))
	if w.Code != 200 || !NewHelper(p).disabled() {
		t.Fatal("revocation marker not durable", w.Body.String())
	}
	NewHelper(p).Boot()
	statePath := filepath.Join(dir, "state.json")
	st := loadState(statePath)
	st.Revoked = true
	if err := st.save(statePath); err != nil {
		t.Fatal(err)
	}
	if !loadState(statePath).Revoked {
		t.Fatal("agent reenabled after restart")
	}
}

func TestMeterKernelInstancesDoNotResetOtherEngines(t *testing.T) {
	st := loadState(filepath.Join(t.TempDir(), "state.json"))
	st.absorbKernel("xray", 10, "x1", map[string]Counter{"x": {Up: 100}})
	st.absorbKernel("sing-box", 11, "s1", map[string]Counter{"s": {Up: 200}})
	st.absorbKernel("xray", 10, "x2", map[string]Counter{"x": {Up: 150}})
	st.absorbKernel("sing-box", 11, "s1", map[string]Counter{"s": {Up: 250}})
	if st.Totals["x"].Up != 250 || st.Totals["s"].Up != 250 {
		t.Fatalf("isolated restart totals: %v", st.Totals)
	}
	path := filepath.Join(t.TempDir(), "state.json")
	if err := st.save(path); err != nil {
		t.Fatal(err)
	}
	restored := loadState(path)
	restored.absorbKernel("sing-box", 11, "s2", map[string]Counter{"s": {Up: 300}})
	if restored.Totals["s"].Up != 550 {
		t.Fatal("PID reuse lost usage", restored.Totals)
	}
}

func TestMeterSharedCredentialHasIndependentKernelBaseline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	st := loadState(path)
	st.absorbKernel("xray", 1, "x1", map[string]Counter{"same": {Up: 100}})
	st.absorbKernel("sing-box", 2, "s1", map[string]Counter{"same": {Up: 30}})
	st.absorbKernel("xray", 1, "x1", map[string]Counter{"same": {Up: 120}})
	if st.Totals["same"].Up != 150 {
		t.Fatal(st.Totals)
	}
	if err := st.save(path); err != nil {
		t.Fatal(err)
	}
	st = loadState(path)
	st.absorbKernel("sing-box", 2, "s2", map[string]Counter{"same": {Up: 10}})
	st.absorbKernel("xray", 1, "x1", map[string]Counter{"same": {Up: 140}})
	if st.Totals["same"].Up != 180 {
		t.Fatal("one restart reset the other baseline", st.Totals)
	}
}
