package laya

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestWarmupUsesSeparateBoundedDeadline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(60 * time.Millisecond):
		}
		w.Write([]byte(`{"answers":{"ok":{"type":"noul","noul":0.9}}}`))
	}))
	defer srv.Close()
	client := Client{BaseURL: srv.URL, HTTP: &http.Client{Timeout: 10 * time.Millisecond}}
	request := Request{Model: "test:v1", State: "fixed", Questions: map[string]Question{"ok": {Type: AnswerNoul, Instructions: "answer"}}}
	if _, err := client.Classify(context.Background(), request); err == nil {
		t.Fatal("ordinary timeout missing")
	}
	if _, err := client.Warmup(context.Background(), request, time.Second); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Warmup(context.Background(), request, 5*time.Millisecond); err == nil {
		t.Fatal("warmup unbounded")
	}
	if client.HTTP.Timeout != 10*time.Millisecond {
		t.Fatal("normal client timeout changed")
	}
}
