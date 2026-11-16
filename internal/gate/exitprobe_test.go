package gate

import (
	"errors"
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
