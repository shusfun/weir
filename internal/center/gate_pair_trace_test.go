package center

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/store/ent/audit"
	"github.com/shusfun/weir/packages/contracts"
)

func TestGatePairTrace(t *testing.T) {
	s, admin := newInstalledServer(t)
	invite := decode(t, admin.do("POST", "/api/v1/admin/gates/invite", map[string]string{"name": "re-pair"}, nil))
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	body := contracts.PairRequest{Name: "Gate", Token: invite["token"].(string), PublicKey: base64.StdEncoding.EncodeToString(pub)}
	logs := captureLog(t)
	for _, tc := range []struct {
		trace, action string
		status        int
	}{
		{"0123456789abcdef0123456789abcdef", "gate.pair", http.StatusCreated},
		{"1123456789abcdef0123456789abcdef", "security.gate-pair-rejected", http.StatusForbidden},
	} {
		w := admin.do("POST", "/api/v1/agent/pair", body, map[string]string{"traceparent": "00-" + tc.trace + "-1111111111111111-01"})
		requestID := w.Header().Get("X-Request-Id")
		if w.Code != tc.status || requestID == "" || w.Header().Get("X-Trace-Id") != tc.trace {
			t.Fatalf("pair response: status=%d headers=%v", w.Code, w.Header())
		}
		record := s.Store.Ent.Audit.Query().Where(audit.ActionEQ(tc.action)).OnlyX(t.Context())
		if record.Detail["trace_id"] != tc.trace || record.Detail["request_id"] != requestID || !strings.Contains(logs.String(), "id="+requestID+" trace="+tc.trace) {
			t.Fatalf("pair trace mismatch: audit=%v logs=%s", record.Detail, logs.String())
		}
	}
	if strings.Contains(logs.String(), body.Token) || strings.Contains(logs.String(), body.PublicKey) {
		t.Fatal("pair log contains device credentials")
	}
}

func TestGatePairInvalidAndExpiredInvites(t *testing.T) {
	for _, kind := range []string{"invalid", "expired"} {
		t.Run(kind, func(t *testing.T) {
			s, admin := newInstalledServer(t)
			token := strings.Repeat("a", 64)
			if kind == "expired" {
				key := "pair:" + digest(token)
				if err := s.Store.KV.Set(t.Context(), key, `{"name":"expired"}`, time.Millisecond); err != nil {
					t.Fatal(err)
				}
				// Redis expiry has millisecond precision; observe expiry before pairing.
				deadline := time.Now().Add(time.Second)
				for {
					_, exists, err := s.Store.KV.Get(t.Context(), key)
					if err != nil {
						t.Fatal(err)
					}
					if !exists {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("invitation did not expire")
					}
					time.Sleep(time.Millisecond)
				}
			}
			pub, _, err := ed25519.GenerateKey(rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			w := admin.do("POST", "/api/v1/agent/pair", contracts.PairRequest{Name: "VPS", Token: token, PublicKey: base64.StdEncoding.EncodeToString(pub)}, nil)
			if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "pair_token_invalid") || s.Store.Ent.Gate.Query().CountX(t.Context()) != 0 {
				t.Fatalf("invalid invitation created a device: status=%d", w.Code)
			}
		})
	}
}
