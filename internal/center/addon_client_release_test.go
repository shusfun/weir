package center

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type releaseTransport func(*http.Request) (*http.Response, error)

func (f releaseTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestClientReleaseProjection(t *testing.T) {
	now := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	valid := `{"tag_name":"v1.2.3","html_url":"https://github.com/owner/project/releases/tag/v1.2.3","published_at":"2026-10-18T12:00:00Z","body":"private discarded field"}`
	for _, test := range []struct {
		name, body, status string
		code               int
	}{
		{"valid", valid, "current", 200},
		{"redirect", "", "unknown", 302},
		{"missing", "", "not_found", 404},
		{"rate", "", "rate_limited", 429},
		{"unsafe-link", strings.ReplaceAll(valid, "https://github.com/owner/project/releases/tag/v1.2.3", "https://evil.test/"), "unknown", 200},
		{"different-repository", strings.ReplaceAll(valid, "github.com/owner/project/", "github.com/other/project/"), "unknown", 200},
		{"credentials", strings.ReplaceAll(valid, "https://github.com", "https://private@github.com"), "unknown", 200},
		{"oversized", strings.Repeat("x", 512*1024+1), "unknown", 200},
		{"malformed", "{}", "unknown", 200},
		{"future", strings.ReplaceAll(valid, "2026-10-18", "2027-10-18"), "unknown", 200},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &http.Client{Transport: releaseTransport(func(r *http.Request) (*http.Response, error) {
				if r.URL.String() != "https://api.github.com/repos/owner/project/releases/latest" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
					t.Fatal("unexpected fetch target/credentials")
				}
				return &http.Response{StatusCode: test.code, Body: io.NopCloser(strings.NewReader(test.body)), Header: make(http.Header)}, nil
			})}
			got := fetchClientRelease(context.Background(), client, "owner/project", now)
			if got.Status != test.status {
				t.Fatalf("status %s want %s", got.Status, test.status)
			}
			if got.Status != "current" && (got.Version != "" || got.URL != "") {
				t.Fatal("unverified metadata retained")
			}
		})
	}
	for _, repo := range []string{"https://evil.test/a", "owner/project?secret=1", "owner/project/extra"} {
		got := fetchClientRelease(context.Background(), &http.Client{Transport: releaseTransport(func(*http.Request) (*http.Response, error) { t.Fatal("invalid repo fetched"); return nil, nil })}, repo, now)
		if got.Status != "unknown" {
			t.Fatal("invalid repo")
		}
	}
	r := clientRelease{Status: "current", Source: "github-releases", ObservedAt: now.Add(-25 * time.Hour).Format(time.RFC3339)}
	if validCachedRelease(r, now) {
		t.Fatal("old cache fresh")
	}
	r.ObservedAt = now.Add(time.Hour).Format(time.RFC3339)
	if validCachedRelease(r, now) {
		t.Fatal("future cache fresh")
	}
}

func TestClientReleaseRedirectBoundary(t *testing.T) {
	for _, test := range []struct {
		url     string
		allowed bool
	}{
		{"https://api.github.com/repositories/123/releases/latest", true},
		{"https://api.github.com/repos/new-owner/new-name/releases/latest", true},
		{"https://evil.test/repos/owner/project/releases/latest", false},
		{"http://api.github.com/repos/owner/project/releases/latest", false},
		{"https://api.github.com/user", false},
		{"https://api.github.com/repos/a/b/releases/latest?token=private", false},
		{"https://api.github.com:443/repos/a/b/releases/latest", false},
		{"https://private@api.github.com/repos/a/b/releases/latest", false},
	} {
		req, _ := http.NewRequest("GET", test.url, nil)
		if (releaseRedirect(req, []*http.Request{{}}) == nil) != test.allowed {
			t.Fatal("redirect policy mismatch")
		}
		if releaseRedirect(req, make([]*http.Request, 3)) == nil {
			t.Fatal("redirect limit")
		}
	}
	now := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	calls := 0
	client := &http.Client{CheckRedirect: releaseRedirect, Transport: releaseTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return &http.Response{StatusCode: 301, Header: http.Header{"Location": []string{"https://api.github.com/repositories/123/releases/latest"}}, Request: r, Body: io.NopCloser(strings.NewReader(""))}, nil
		}
		if r.URL.String() != "https://api.github.com/repositories/123/releases/latest" {
			t.Fatal("unexpected redirected target")
		}
		return &http.Response{StatusCode: 200, Request: r, Body: io.NopCloser(strings.NewReader(`{"tag_name":"v2","html_url":"https://github.com/new-owner/new-name/releases/tag/v2","published_at":"2026-10-18T00:00:00Z"}`))}, nil
	})}
	if fetchClientRelease(context.Background(), client, "old-owner/old-name", now).Status != "current" || calls != 2 {
		t.Fatal("official renamed repository unsupported")
	}
	client = &http.Client{CheckRedirect: releaseRedirect, Transport: releaseTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.github.com" {
			t.Fatal("cross-host redirect followed")
		}
		return &http.Response{StatusCode: 301, Header: http.Header{"Location": []string{"https://evil.test/private"}}, Request: r, Body: io.NopCloser(strings.NewReader(""))}, nil
	})}
	if fetchClientRelease(context.Background(), client, "owner/repo", now).Status != "unknown" {
		t.Fatal("unsafe redirect published")
	}
}
