package laya

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRuntimeVersionUsesOfficialMetadataAndObservesSafeFailure(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
		status           int
	}{
		{"release", `{"version":"0.18.0"}`, "0.18.0", 200},
		{"prerelease", `{"version":"0.19.0-rc.1"}`, "0.19.0-rc.1", 200},
		{"missing", `{}`, "", 200},
		{"invalid", `{"version":"password=secret"}`, "", 200},
		{"trailing", `{"version":"0.18.0"} {}`, "", 200},
		{"too_large", strings.Repeat("x", 4097), "", 200},
		{"unavailable", `secret`, "", 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/api/version" || r.Header.Get("Authorization") != "Bearer fixture" {
					t.Error("incorrect version request")
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			var events []Event
			client := Client{BaseURL: srv.URL + "/v1/systemone", APIKey: "fixture", Observer: func(_ context.Context, e Event) { events = append(events, e) }}
			got, err := client.RuntimeVersion(WithTrace(t.Context(), "version-trace"))
			if got != tc.want || (err != nil) != (tc.want == "") {
				t.Fatalf("version=%q err=%v", got, err)
			}
			if len(events) != 1 || events[0].Operation != "laya.runtime.version" || strings.Contains(events[0].Code, "secret") {
				t.Fatalf("unsafe/missing observation: %+v", events)
			}
			if err != nil && strings.Contains(err.Error(), "secret") {
				t.Fatal("remote text leaked")
			}
		})
	}
}

func TestRuntimeVersionRejectsRedirectAndHonorsCancellation(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("redirect followed") }))
	defer target.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 302) }))
	defer srv.Close()
	client := Client{BaseURL: srv.URL, APIKey: "fixture"}
	if _, err := client.RuntimeVersion(t.Context()); err == nil {
		t.Fatal("redirect accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := client.RuntimeVersion(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
}
