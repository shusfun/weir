package control

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestModelResponseIsDataOnly(t *testing.T) {
	var data map[string]any
	json.Unmarshal([]byte(`{"answers":{"category":{"choice":"suspicious","confidence":0.8,"command":"rm -rf /","key":"do-not-forward"}},"secret":"do-not-forward"}`), &data)
	out, e := modelDecision(data)
	if e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(out)
	if strings.Contains(string(b), "command") || strings.Contains(string(b), "secret") || strings.Contains(string(b), "do-not-forward") {
		t.Fatal("untrusted response fields forwarded")
	}
	data["answers"].(map[string]any)["category"].(map[string]any)["choice"] = "execute_command"
	if _, e = modelDecision(data); e == nil {
		t.Fatal("model action accepted")
	}
}

func TestModelHTTPFailureBoundaries(t *testing.T) {
	for _, tt := range []struct {
		name   string
		status int
		body   string
	}{
		{"unauthorized", 401, `{"secret":"do-not-forward"}`},
		{"invalid-json", 200, `not json`},
		{"oversized", 200, strings.Repeat("x", 128*1024+1)},
		{"action-injection", 200, `{"answers":{"category":{"choice":"execute_command","confidence":1}}}`},
		{"redirect", 302, `{}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", "/other")
				w.WriteHeader(tt.status)
				w.Write([]byte(tt.body))
			}))
			defer srv.Close()
			client := srv.Client()
			client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
			req, _ := http.NewRequest("POST", srv.URL+"/v1/systemone", strings.NewReader(`{}`))
			if _, err := classifyRequest(client, req); err == nil {
				t.Fatal("bad model response accepted")
			}
		})
	}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "POST", srv.URL+"/v1/systemone", nil)
	if _, err := classifyRequest(srv.Client(), req); err == nil {
		t.Fatal("model timeout accepted")
	}
}
