package laya

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPrepareModelReusesPinnedModelOrInstallsBeforeChecking(t *testing.T) {
	for _, installed := range []bool{true, false} {
		t.Run(map[bool]string{true: "existing", false: "missing"}[installed], func(t *testing.T) {
			present, pulls := installed, 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					Model  string `json:"model"`
					Stream bool   `json:"stream"`
				}
				if json.NewDecoder(r.Body).Decode(&req) != nil || req.Model != DefaultModel+"@sha256:"+ModelManifestSHA256 {
					t.Error("unlocked model request")
				}
				switch r.URL.Path {
				case "/api/show":
					if !present {
						w.WriteHeader(404)
						_, _ = w.Write([]byte(`{"code":"MODEL_NOT_FOUND"}`))
						return
					}
					_ = json.NewEncoder(w).Encode(map[string]string{"digest": ModelManifestSHA256})
				case "/api/pull":
					present, pulls = true, pulls+1
					_, _ = w.Write([]byte("{\"status\":\"success\"}\n"))
				default:
					t.Error("preparing a model must not run inference")
				}
			}))
			defer srv.Close()
			if err := (Client{BaseURL: srv.URL}).PrepareModel(t.Context(), FixedManifest(), nil); err != nil {
				t.Fatal(err)
			}
			if pulls != map[bool]int{true: 0, false: 1}[installed] {
				t.Fatalf("pulls=%d", pulls)
			}
		})
	}
}

func TestPrepareModelRejectsDigestMismatchAndHonorsCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/show" {
			t.Error("digest mismatch must not be overwritten by installation")
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"digest": strings.Repeat("a", 64)})
	}))
	defer srv.Close()
	var detail *APIError
	err := (Client{BaseURL: srv.URL}).PrepareModel(t.Context(), FixedManifest(), nil)
	if !errors.As(err, &detail) || detail.Code != "model_digest_mismatch" {
		t.Fatalf("mismatch: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err = (Client{BaseURL: srv.URL}).PrepareModel(ctx, FixedManifest(), nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	if err = (Client{BaseURL: srv.URL}).PrepareModel(t.Context(), Model{Name: DefaultModel}, nil); !errors.As(err, &detail) || detail.Code != "model_manifest_invalid" {
		t.Fatal("missing fixed manifest accepted")
	}
}

func TestValidateResponseRejectsNonFiniteProbabilities(t *testing.T) {
	request := Request{Questions: map[string]Question{"q": {Type: AnswerChoice, Criteria: []string{"ready"}}}}
	for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), -0.1, 1.1} {
		for _, answer := range []Answer{{Choice: "ready", Confidence: &value}, {Choice: "ready", Probabilities: map[string]float64{"ready": value}}} {
			if err := ValidateResponse(request, Response{Answers: map[string]Answer{"q": answer}}); err == nil {
				t.Fatalf("invalid probability accepted: %+v", answer)
			}
		}
		if err := ValidateResponse(Request{Questions: map[string]Question{"q": {Type: AnswerNoul}}}, Response{Answers: map[string]Answer{"q": {Noul: &value}}}); err == nil {
			t.Fatal("invalid noul accepted")
		}
	}
}
