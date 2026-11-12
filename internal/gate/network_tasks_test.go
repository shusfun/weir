package gate

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/shusfun/weir/packages/contracts"
)

type taskTransport func(*http.Request) (*http.Response, error)

func (f taskTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestNetworkTaskUsesMemoryCredentialsAndBoundsCompressedReply(t *testing.T) {
	data := bytes.Repeat([]byte("subscription"), 100)
	var encoded bytes.Buffer
	gz := gzip.NewWriter(&encoded)
	gz.Write(data)
	gz.Close()
	client := &http.Client{Transport: taskTransport(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "Bearer private" || r.URL.Path != "/private-token" || r.Header.Get("Accept-Encoding") != "gzip" {
			t.Fatal("lost request fields")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Encoding": []string{"gzip"}}, Body: io.NopCloser(bytes.NewReader(encoded.Bytes()))}, nil
	})}
	payload := contracts.SubscriptionFetchTask{Credentials: contracts.FetchSecrets{URL: "https://example.com/private-token", Authorization: "Bearer private"}, MaxBytes: 4096}
	raw, _ := json.Marshal(payload)
	result, err := executeNetworkTask(context.Background(), contracts.Task{Kind: contracts.TaskSubscriptionFetch, Payload: raw}, client)
	if err != nil {
		t.Fatal(err)
	}
	var out contracts.SubscriptionFetchResult
	if err = json.Unmarshal(result, &out); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	if !bytes.Equal(out.Body, data) || out.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatal("incorrect digest or body")
	}
	payload.MaxBytes = 100
	raw, _ = json.Marshal(payload)
	if _, err = executeNetworkTask(context.Background(), contracts.Task{Kind: contracts.TaskSubscriptionFetch, Payload: raw}, client); err == nil {
		t.Fatal("compression bomb accepted")
	}
	for _, url := range []string{"http://127.0.0.1/private-token", "http://[::ffff:127.0.0.1]/private-token"} {
		payload.Credentials.URL = url
		raw, _ = json.Marshal(payload)
		if _, err = executeNetworkTask(context.Background(), contracts.Task{Kind: contracts.TaskSubscriptionFetch, Payload: raw}, client); err == nil {
			t.Fatal("private target accepted")
		}
	}
}

func TestNetworkErrorNeverContainsSecretURL(t *testing.T) {
	client := &http.Client{Transport: taskTransport(func(r *http.Request) (*http.Response, error) { return nil, context.DeadlineExceeded })}
	raw, _ := json.Marshal(contracts.SubscriptionFetchTask{Credentials: contracts.FetchSecrets{URL: "https://example.com/private-token"}, MaxBytes: 1024})
	_, err := executeNetworkTask(context.Background(), contracts.Task{Kind: contracts.TaskSubscriptionFetch, Payload: raw}, client)
	if err == nil || strings.Contains(err.Error(), "private-token") {
		t.Fatalf("err=%v", err)
	}
}

func TestReleaseQueryReturnsOnlyValidatedStructuredFields(t *testing.T) {
	q := contracts.ReleaseQueryTask{Repository: "owner/repo", PerPage: 2, MaxBytes: 1 << 20}
	client := &http.Client{Transport: taskTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.github.com" || r.URL.Path != "/repos/owner/repo/releases" || r.URL.Query().Get("per_page") != "2" {
			t.Fatalf("request=%s", r.URL)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`[{"tag_name":"v1","html_url":"https://github.com/owner/repo/releases/tag/v1","assets":[],"ignored_upstream_secret":"do-not-forward"}]`))}, nil
	})}
	raw, _ := json.Marshal(q)
	result, err := executeNetworkTask(t.Context(), contracts.Task{Kind: contracts.TaskReleaseQuery, Payload: raw}, client)
	if err != nil {
		t.Fatal(err)
	}
	var envelope contracts.SubscriptionFetchResult
	if err = json.Unmarshal(result, &envelope); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(envelope.Body), "do-not-forward") {
		t.Fatal("unstructured upstream field forwarded")
	}
	var releases contracts.ReleaseQueryResult
	if err = contracts.DecodeStrict(envelope.Body, &releases, 1<<20); err != nil {
		t.Fatal(err)
	}
	if err = releases.Validate(q); err != nil {
		t.Fatal(err)
	}
}
