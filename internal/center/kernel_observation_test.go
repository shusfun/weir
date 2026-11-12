package center

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/shusfun/weir/internal/store/ent/resource"
	"github.com/shusfun/weir/packages/contracts"
)

func TestKernelDiagnosticsEnqueuesTextAndAvoidsAssistantFeedback(t *testing.T) {
	s, admin, g := setupGate(t)
	g.serveActions(t, s, map[string]func(contracts.Action) contracts.ActionResult{
		contracts.ActionDiagnostics: func(contracts.Action) contracts.ActionResult {
			return contracts.ActionResult{OK: true, Result: map[string]any{"running": true, "counts": map[string]int{"error": 1}, "tail": []string{"TLS handshake failed password=HIDDENSECRET"}}}
		},
	})
	trace := "66666666666666666666666666666666"
	w := admin.do("POST", "/api/v1/admin/gates/"+itoa(int(g.id))+"/kernels/xray/diagnostics", nil, map[string]string{"traceparent": "00-" + trace + "-7777777777777777-01"})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	row := s.Store.Ent.Resource.Query().Where(resource.KindEQ(observationPendingKind)).OnlyX(t.Context())
	var r observationRecord
	if err := decodeStrict(row.Data, &r); err != nil {
		t.Fatal(err)
	}
	if r.TraceID != trace || r.Point != "gate.kernel" || r.Origin != "center.gateKernelAction" || strings.Contains(jsonText(r), "HIDDENSECRET") {
		t.Fatal("diagnostic observation lost boundary", r)
	}
	if _, err := s.readVia(t.Context(), s.gateKernelAction(contracts.ActionDiagnostics), "POST", pathOf("id", itoa(int(g.id)), "kernel", "xray"), nil, nil); err != nil {
		t.Fatal(err)
	}
	if n := s.Store.Ent.Resource.Query().Where(resource.KindEQ(observationPendingKind)).CountX(t.Context()); n != 1 {
		t.Fatal("assistant diagnostic triggered another analysis", n)
	}
	mustJSON(t, admin, "POST", "/api/v1/admin/gates/"+itoa(int(g.id))+"/kernels/xray/restart", nil, 200)
	if n := s.Store.Ent.Resource.Query().Where(resource.KindEQ(observationPendingKind)).CountX(t.Context()); n != 1 {
		t.Fatal("structured restart result sent to model", n)
	}
}

func TestKernelDiagnosticRejectsInvalidLogs(t *testing.T) {
	for _, tail := range []any{[]any{true}, []string{strings.Repeat("x", 8193)}, make([]string, 51), json.RawMessage(`{"command":"sh"}`)} {
		if _, err := kernelDiagnosticText(map[string]any{"tail": tail}); err == nil {
			t.Fatal("invalid logs accepted")
		}
	}
	for _, result := range []map[string]any{{}, {"tail": []string{}}, {"counts": map[string]int{"error": 100}}} {
		if text, err := kernelDiagnosticText(result); err != nil || text != "" {
			t.Fatal("nontext diagnostic fed to classifier", text, err)
		}
	}
}
