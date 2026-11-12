package laya

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDecisionClientTypedAnswersAndObservation(t *testing.T) {
	var path, auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, auth = r.URL.Path, r.Header.Get("Authorization")
		var in Request
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Fatal(err)
		}
		if in.Questions["ok"].Type != AnswerNoul {
			t.Fatalf("request = %+v", in)
		}
		_, _ = w.Write([]byte(`{"answers":{"ok":{"type":"noul","noul":0.9}}}`))
	}))
	defer srv.Close()
	observed := 0
	c := Client{BaseURL: srv.URL, APIKey: "secret", HTTP: srv.Client(), Observer: func(_ context.Context, e Event) {
		observed++
		if e.Status != "succeeded" {
			t.Errorf("event=%+v", e)
		}
	}}
	r, err := c.Classify(context.Background(), Request{Model: "example:v1", State: map[string]any{"signal": "fixed"}, Questions: map[string]Question{"ok": {Type: AnswerNoul, Instructions: "answer"}}})
	if err != nil || r.Answers["ok"].Noul == nil || *r.Answers["ok"].Noul != 0.9 || path != "/v1/systemone" || auth != "Bearer secret" || observed != 1 {
		t.Fatalf("result=%+v err=%v path=%q auth=%q observed=%d", r, err, path, auth, observed)
	}
}

func TestFixedManifestPinsArtifacts(t *testing.T) {
	m := FixedManifest()
	if m.Name != DefaultModel || m.SourceCommit == "" || len(m.Files) < 2 {
		t.Fatalf("manifest=%+v", m)
	}
	for _, f := range m.Files {
		if f.Size <= 0 || len(f.SHA256) != 64 || f.URL == "" {
			t.Fatalf("file=%+v", f)
		}
	}
}
