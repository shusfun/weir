package gate

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shusfun/weir/packages/contracts"
)

func TestExitProbeBoundaryErrors(t *testing.T) {
	for _, tc := range []struct {
		name    string
		payload map[string]any
		code    string
	}{
		{"invalid address", map[string]any{"address": "", "password": "never-publish-this"}, "exit_probe_prepare_failed"},
		{"private exit", map[string]any{"address": "127.0.0.1", "port": 1080, "public_only": true, "target": "https://example.com"}, "exit_probe_public_address_failed"},
		{"missing kernel", map[string]any{"address": "203.0.113.1", "port": 1080, "target": "https://example.com", "kernels": map[string]any{}}, "exit_probe_kernels_failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := (&Agent{}).clientProbe(t.Context(), tc.payload)
			var detail *contracts.TaskError
			if result != nil || !errors.As(err, &detail) || detail.Code != tc.code || detail.Validate() != nil {
				t.Fatalf("unsafe or incorrect failure: result=%v err=%v", result, err)
			}
			if detail.Origin != "gate.clientProbe" || detail.Retryable {
				t.Fatalf("invalid failure metadata: %+v", detail)
			}
		})
	}
}

func TestRecordProbeDownloadKeepsEchoResult(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("short"))
	}))
	defer srv.Close()
	out := map[string]any{"mode": "proxy"}
	recordProbeDownload(t.Context(), srv.Client(), srv.URL, out)
	if out["mode"] != "proxy" || out["download_bytes"] != int64(5) || !strings.Contains(out["download_error"].(string), "下载没有完成") {
		t.Fatalf("download = %v", out)
	}
	out = map[string]any{}
	recordProbeDownload(t.Context(), srv.Client(), "http://example.com/probe", out)
	if out["download_error"] != "下载地址无效" || out["download_bytes"] != nil {
		t.Fatalf("invalid download = %v", out)
	}
}
