package gate

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/shusfun/weir/internal/fetch"
	"github.com/shusfun/weir/internal/releasefiles"
)

type centerDownloadTransport struct {
	client *centerClient
	next   http.RoundTripper
	url    string
}

func (t centerDownloadTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.String() != t.url || req.Method != http.MethodGet {
		return nil, errors.New("center_download_target_changed")
	}
	token, err := t.client.bearer(req.Context())
	if err != nil {
		return nil, err
	}
	copy := req.Clone(req.Context())
	copy.Header.Set("Authorization", "Bearer "+token)
	copy.Header.Set("traceparent", releasefiles.Traceparent(req.Context()))
	response, err := t.next.RoundTrip(copy)
	if err == nil && response.StatusCode == http.StatusUnauthorized {
		t.client.mu.Lock()
		t.client.until = time.Time{}
		t.client.mu.Unlock()
	}
	return response, err
}

// downloadFile authenticates each Range request without forwarding the device
// bearer to redirects. The archive digest is supplied by the pinned manifest.
func (c *centerClient) downloadFile(ctx context.Context, path, sha string, size int64, destination string) error {
	if !strings.HasPrefix(path, "/agent/kernel-assets/") {
		return errors.New("center_download_path_invalid")
	}
	raw := c.origin() + "/api/v1" + path
	next := c.long.Transport
	if next == nil {
		next = http.DefaultTransport
	}
	client := &http.Client{Transport: centerDownloadTransport{client: c, next: next, url: raw}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return fetch.Download(ctx, fetch.Request{URLs: []string{raw}, Dest: destination, SHA256: sha, MaxSize: size, HTTP: client, MinSpeed: -1})
}
