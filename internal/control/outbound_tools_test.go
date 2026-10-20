package control

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/imzyb/MiGate/packages/contracts"
)

func TestOutboundProbeResultProjection(t *testing.T) {
	payload := map[string]any{"items": []contracts.OutboundProbeItem{{ID: 1, Digest: randomToken(32)}}}
	data := map[string]any{"source": "gate_tcp_connect", "observed_at": time.Now().UTC().Format(time.RFC3339Nano), "items": []any{map[string]any{"id": float64(1), "status": "reachable", "latency_ms": float64(7), "password": "must-not-return"}}, "raw_log": "secret"}
	out, e := outboundProbeResult(payload, data)
	b, _ := json.Marshal(out)
	if e != nil || string(b) == "" || out["raw_log"] != nil {
		t.Fatal("projection failed", e)
	}
	if out["items"].([]map[string]any)[0]["password"] != nil {
		t.Fatal("secret projected")
	}
	for _, bad := range []any{float64(-1), float64(3001), float64(1.5), "7", nil} {
		data["items"].([]any)[0].(map[string]any)["latency_ms"] = bad
		if _, e = outboundProbeResult(payload, data); e == nil {
			t.Fatal("invalid counter accepted")
		}
	}
	data["items"] = []any{map[string]any{"id": float64(2), "status": "blocked"}}
	if _, e = outboundProbeResult(payload, data); e == nil {
		t.Fatal("wrong inventory accepted")
	}
	data["items"] = []any{map[string]any{"id": float64(1), "status": "blocked"}}
	data["observed_at"] = time.Now().Add(-10 * time.Minute).Format(time.RFC3339Nano)
	if _, e = outboundProbeResult(payload, data); e == nil {
		t.Fatal("stale observation accepted")
	}
}
