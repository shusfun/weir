package gate

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shusfun/weir/packages/contracts"
)

func TestLayaHelperClientPreservesOnlyKnownFailureCodes(t *testing.T) {
	for _, code := range []string{"laya_container_missing", "laya_container_policy_mismatch", "laya_image_pull_failed", "password=secret"} {
		t.Run(code, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(422)
				_ = json.NewEncoder(w).Encode(ApplyResult{Error: code})
			}))
			defer srv.Close()
			// helperClient always calls its fixed http://helper host.
			client := &http.Client{Transport: helperTestTransport{url: srv.URL, base: http.DefaultTransport}}
			_, err := (&helperClient{http: client}).Laya(t.Context(), "status", "digest", "version")
			var detail *contracts.TaskError
			if !errors.As(err, &detail) || detail.Origin != "gate.layaOperation" {
				t.Fatal(err)
			}
			want := code
			if code == "password=secret" {
				want = "laya_helper_unavailable"
			}
			if detail.Code != want || strings.Contains(err.Error(), "secret") {
				t.Fatalf("%+v", detail)
			}
		})
	}
}

type helperTestTransport struct {
	url  string
	base http.RoundTripper
}

func (h helperTestTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.URL.Scheme = "http"
	r.URL.Host = strings.TrimPrefix(h.url, "http://")
	return h.base.RoundTrip(r)
}

func TestLayaTaskFailureReachesResultAndLocalTimeline(t *testing.T) {
	var got contracts.TaskUpdate
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/result") {
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Error(err)
			}
		}
		w.WriteHeader(204)
	}))
	defer srv.Close()
	task := contracts.Task{ID: strings.Repeat("a", 48), Kind: contracts.TaskLayaRemove, Version: 1, TraceID: strings.Repeat("b", 32), IdempotencyKey: "remove", Payload: json.RawMessage(`{}`), Attempt: 1, LeaseToken: strings.Repeat("c", 48), LeaseUntil: time.Now().Add(time.Minute), Deadline: time.Now().Add(time.Minute)}
	a := &Agent{P: Paths{Root: t.TempDir(), Data: t.TempDir()}, center: &centerClient{center: srv.URL, http: srv.Client(), token: "test", until: time.Now().Add(time.Hour)}}
	a.executeTask(context.Background(), task)
	if got.Error == nil || !strings.HasPrefix(got.Error.Code, "laya_") || got.Error.Code == "laya_task_failed" || got.Error.Origin == "gate.runAction" {
		t.Fatalf("failure lost: %+v", got)
	}
	var journal taskJournal
	if err := readJournal(filepath.Join(a.P.Data, "tasks", task.ID+".json"), &journal); err != nil {
		t.Fatal(err)
	}
	if len(journal.Stages) != 1 || journal.Stages[0].ErrorCode != got.Error.Code || journal.Stages[0].Origin != got.Error.Origin || journal.Stages[0].TraceID != task.TraceID {
		t.Fatalf("timeline lost: %+v", journal)
	}
}
