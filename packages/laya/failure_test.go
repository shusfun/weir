package laya

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecisionFailureCodesRequireExplicitEvidence(t *testing.T) {
	for _, tc := range []struct{ body, want string }{
		{`{"code":"OUT_OF_MEMORY","error":"private detail"}`, "resources_insufficient"},
		{`{"code":"MODEL_NOT_FOUND"}`, "model_not_ready"},
		{`{"error":"private detail"}`, "classification_failed"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			_, err := (Client{BaseURL: srv.URL}).Classify(t.Context(), Request{Model: "test", Questions: map[string]Question{"ok": {Type: AnswerChoice}}})
			if FailureCode(err) != tc.want || strings.Contains(err.Error(), "private detail") {
				t.Fatalf("unexpected failure: %v", err)
			}
		})
	}
}
