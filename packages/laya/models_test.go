package laya

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFixedManifestPreservesRegistryDigest(t *testing.T) {
	model := FixedManifest()
	sum := sha256.Sum256(model.Manifest)
	if hex.EncodeToString(sum[:]) != ModelManifestSHA256 {
		t.Fatal("embedded manifest no longer matches the official pinned bytes")
	}
}

func TestPullModelPinsManifestAndVerifiesActivation(t *testing.T) {
	model := FixedManifest()
	var operations []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		operations = append(operations, r.URL.Path)
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Error("missing runtime token")
		}
		var req struct {
			Model  string `json:"model"`
			Stream bool   `json:"stream"`
		}
		if json.NewDecoder(r.Body).Decode(&req) != nil || req.Model != model.Name+"@sha256:"+ModelManifestSHA256 {
			t.Error("manifest was not pinned")
		}
		switch r.URL.Path {
		case "/api/pull":
			if !req.Stream {
				t.Error("missing streaming mode")
			}
			_, _ = w.Write([]byte("{\"status\":\"pulling manifest\"}\n{\"status\":\"success\"}\n"))
		case "/api/show":
			_ = json.NewEncoder(w).Encode(map[string]string{"digest": ModelManifestSHA256})
		default:
			t.Error("unexpected endpoint")
		}
	}))
	defer srv.Close()
	var stages []string
	var event Event
	client := Client{BaseURL: srv.URL + "/v1/systemone", APIKey: "secret", HTTP: srv.Client(), Observer: func(_ context.Context, e Event) { event = e }}
	err := client.PullModel(WithTrace(t.Context(), "operation"), model, func(p ModelProgress) { stages = append(stages, p.Stage) })
	if err != nil || strings.Join(operations, ",") != "/api/pull,/api/show" || strings.Join(stages, ",") != "preparing,complete" || event.TraceID != "operation" || event.Status != "succeeded" {
		t.Fatalf("err=%v operations=%v stages=%v event=%+v", err, operations, stages, event)
	}
}

func TestPullModelRejectsInterruptedUnverifiedOrUnboundedProgress(t *testing.T) {
	for _, tc := range []struct{ name, stream, digest, code string }{
		{"digest", "{\"status\":\"success\"}\n", strings.Repeat("a", 64), "model_digest_mismatch"},
		{"interrupted", "{\"status\":\"pulling manifest\"}\n", ModelManifestSHA256, "model_pull_interrupted"},
		{"upstream", "{\"error\":\"password=secret\",\"code\":\"DIGEST_MISMATCH\"}\n", "", "model_digest_mismatch"},
		{"unknown_error", "{\"error\":\"password=secret\",\"code\":\"private-secret\"}\n", "", "model_api_failed"},
		{"unexpected_blob", "{\"digest\":\"sha256:unknown\",\"completed\":0,\"total\":100}\n", "", "model_progress_invalid"},
		{"duplicate_terminal", "{\"status\":\"success\"}\n{\"status\":\"success\"}\n", "", "model_progress_invalid"},
		{"large_line", strings.Repeat("x", 65<<10) + "\n", "", "model_pull_interrupted"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/show" {
					_ = json.NewEncoder(w).Encode(map[string]string{"digest": tc.digest})
					return
				}
				_, _ = w.Write([]byte(tc.stream))
			}))
			defer srv.Close()
			err := (Client{BaseURL: srv.URL}).PullModel(t.Context(), FixedManifest(), nil)
			var detail *APIError
			if !errors.As(err, &detail) || detail.Code != tc.code || strings.Contains(err.Error(), "secret") {
				t.Fatalf("%v", err)
			}
		})
	}
}

func TestPullModelCancellationAndRedirectIsolation(t *testing.T) {
	t.Run("cancel", func(t *testing.T) {
		started := make(chan struct{})
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/x-ndjson")
			_, _ = w.Write([]byte("{\"status\":\"pulling manifest\"}\n"))
			w.(http.Flusher).Flush()
			close(started)
			<-r.Context().Done()
		}))
		defer srv.Close()
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		var event Event
		go func() { <-started; cancel() }()
		err := (Client{BaseURL: srv.URL, Observer: func(_ context.Context, e Event) { event = e }}).PullModel(ctx, FixedManifest(), nil)
		if !errors.Is(err, context.Canceled) || event.Status != "cancelled" {
			t.Fatalf("%v %+v", err, event)
		}
	})
	t.Run("redirect", func(t *testing.T) {
		calls := 0
		target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(200) }))
		defer target.Close()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 307) }))
		defer srv.Close()
		err := (Client{BaseURL: srv.URL, APIKey: "secret", HTTP: srv.Client()}).PullModel(t.Context(), FixedManifest(), nil)
		if err == nil || calls != 0 {
			t.Fatalf("redirect followed: %v calls=%d", err, calls)
		}
	})
}
