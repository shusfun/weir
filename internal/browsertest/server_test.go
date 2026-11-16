package browsertest

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/shusfun/weir/packages/contracts"
)

func TestQUICCloseReasonDoesNotLogPeerText(t *testing.T) {
	for _, err := range []error{
		&quic.TransportError{ErrorCode: 0x12e, Remote: true, ErrorMessage: "peer-token-secret"},
		&quic.ApplicationError{ErrorCode: 7, Remote: true, ErrorMessage: "peer-token-secret"},
	} {
		reason := quicCloseReason(err)
		if strings.Contains(reason, "peer-token-secret") || !strings.Contains(reason, "remote=true") {
			t.Fatal(reason)
		}
	}
}

func TestStartLoopbackRejectsNonIPHosts(t *testing.T) {
	for _, host := range []string{"localhost", "192.0.2.10", "example.test"} {
		t.Run(host, func(t *testing.T) {
			_, err := StartLoopback(contracts.BrowserTestPlan{Host: host}, tls.Certificate{})
			if err == nil || !strings.Contains(err.Error(), "loopback IP") {
				t.Fatalf("StartLoopback error = %v", err)
			}
		})
	}
}

func TestRequestRejectionLogsReasonWithoutCredentials(t *testing.T) {
	public, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{plan: contracts.BrowserTestPlan{GateID: 1, PublicKey: base64.RawURLEncoding.EncodeToString(public), Origin: "https://center.test"}, boot: strings.Repeat("b", 32)}
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	for _, tc := range []struct{ purpose, reason string }{{"https", "token_expired"}, {"webtransport", "token_expired"}, {"webrtc", "origin"}} {
		t.Run(tc.purpose, func(t *testing.T) {
			output.Reset()
			claim := contracts.BrowserTestClaim{ID: strings.Repeat("a", 32), GateID: 1, UserID: 1, Boot: s.boot, Purpose: tc.purpose, Bytes: 1024, Expires: time.Now().Add(-time.Minute), TraceID: strings.Repeat("c", 32)}
			raw, err := json.Marshal(claim)
			if err != nil {
				t.Fatal(err)
			}
			token := base64.RawURLEncoding.EncodeToString(raw) + "." + base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, raw))
			method, path := "GET", "/latency"
			if tc.purpose == "webtransport" {
				method, path = "CONNECT", "/quic?token="+token
			} else if tc.purpose == "webrtc" {
				method, path = "POST", "/rtc"
			}
			// CONNECT uses an origin-form target for the WebTransport handler.
			request := httptest.NewRequest(method, path, nil)
			request.Header.Set("Origin", s.plan.Origin)
			request.Header.Set("Authorization", "Bearer "+token)
			if tc.reason == "origin" {
				request.Header.Set("Origin", "https://untrusted.test")
			}
			response := httptest.NewRecorder()
			s.ServeHTTP(response, request)
			if response.Code != 403 {
				t.Fatalf("status = %d", response.Code)
			}
			logs := output.String()
			if !strings.Contains(logs, `"reason":"`+tc.reason+`"`) || !strings.Contains(logs, claim.TraceID) {
				t.Fatalf("missing rejection evidence: %s", logs)
			}
			if strings.Contains(logs, token) || strings.Contains(logs, "/quic?token=") || strings.Contains(logs, "https://untrusted.test") {
				t.Fatal("request secrets or raw origin were logged")
			}
		})
	}
}
