package gate

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/shusfun/weir/packages/contracts"
)

type sourceProbeTransport func(*http.Request) (*http.Response, error)

func (f sourceProbeTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestWorkerSourceProbesDoNotRetainResponsesOrCredentials(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: sourceProbeTransport(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.Method != http.MethodHead || req.URL.Scheme != "https" || req.Header.Get("Authorization") != "" || req.URL.User != nil {
			t.Fatal("source probe sent an unsafe request")
		}
		if req.URL.Host == "huggingface.co" {
			return nil, errors.New("external failure containing secret")
		}
		status := 200
		if req.URL.Host == "hf-mirror.com" {
			status = 403
		}
		return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("external secret"))}, nil
	})}
	observed := probeWorkerSources(t.Context(), client)
	if calls != 4 || !observed["github"] || observed["huggingface"] || observed["hf_mirror"] || !observed["modelscope"] {
		t.Fatalf("source capability results: %+v calls=%d", observed, calls)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if got := probeWorkerSources(ctx, client); len(got) != 0 || calls != 4 {
		t.Fatal("cancelled probes performed network access")
	}
}

func TestWorkerObservationSnapshotsAreIndependent(t *testing.T) {
	a := &Agent{}
	observed := contracts.ComponentObservation{State: "available", ObservedAt: time.Now(), Version: "v0.18.0"}
	a.workerObserved.components = map[string]contracts.ComponentObservation{"laya": observed}
	copy := a.workerComponentSnapshot()
	delete(copy, "laya")
	if got := a.workerComponentSnapshot(); got["laya"].State != "available" {
		t.Fatal("caller mutated stored observation")
	}
}
