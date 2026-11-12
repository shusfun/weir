package subgroup

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSourceMalformedContentPreservesCacheAndKnownEmpty(t *testing.T) {
	good := trojan(1, "good")
	var body string
	f := Fetcher{Fetch: func(context.Context, string, string) (Response, error) {
		return Response{Body: []byte(body)}, nil
	}}
	for _, bad := range []string{
		`{"outbounds": [`,
		`{"outbounds": "wrong"}`,
		`{"outbounds":null}`,
		"proxies: [broken\n" + good,
		"proxies: {}",
		"proxies: []\n---\nproxies: []",
		"<html>upstream temporarily unavailable</html>",
		"proxies:\n  - {name: unknown, type: unsupported, server: example.com, port: 443}",
	} {
		for _, protected := range []bool{true, false} {
			body = bad
			cache := &memCache{m: map[int][]string{1: {good}}}
			nodes, result := fetchSource(t.Context(), f, cache, Source{ID: 1, URL: "https://source.example", NodeCache: protected}, Options{})
			if result.Error == "" || result.FromCache != protected || (protected && (len(nodes) != 1 || nodes[0] != good)) || (!protected && len(nodes) != 0) {
				t.Fatalf("invalid content accepted: protected=%v body=%q result=%+v", protected, bad, result)
			}
			if saved, _ := cache.Load(t.Context(), 1); len(saved) != 1 || saved[0] != good {
				t.Fatal("malformed content overwrote raw cache")
			}
		}
	}
	for _, empty := range []string{"", " \n", "proxies: []", "Proxy: []", `{"outbounds":[]}`, base64.StdEncoding.EncodeToString([]byte("proxies: []\n"))} {
		body = empty
		cache := &memCache{m: map[int][]string{1: {good}}}
		nodes, result := fetchSource(t.Context(), f, cache, Source{ID: 1, URL: "https://source.example", NodeCache: true, KnownNodes: 12}, Options{})
		if result.Error != "" || result.FromCache || len(nodes) != 0 {
			t.Fatalf("valid empty rejected: %q %+v", empty, result)
		}
		if saved, ok := cache.Load(t.Context(), 1); !ok || len(saved) != 0 {
			t.Fatal("old raw nodes survived a valid empty response")
		}
	}
}

func TestRawCacheReappliesRulesAndAcceptsEmpty(t *testing.T) {
	body := ""
	for i := 1; i <= 12; i++ {
		body += trojan(i, fmt.Sprintf("node-%02d", i)) + "\n"
	}
	fail := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail {
			w.WriteHeader(502)
			return
		}
		fmt.Fprint(w, body)
	}))
	defer srv.Close()
	cache := &memCache{m: map[int][]string{}}
	src := Source{ID: 1, URL: srv.URL, NodeCache: true}
	f := Fetcher{AllowPrivate: true}
	ctx := t.Context()
	_, r := fetchSource(ctx, f, cache, src, Options{})
	if r.Nodes != 12 {
		t.Fatal(r)
	}
	src.FilterInclude = "keep:node-01"
	src.KnownNodes = 12
	nodes, r := fetchSource(ctx, f, cache, src, Options{})
	if len(nodes) != 1 || r.Nodes != 12 || r.FromCache || r.Error != "" {
		t.Fatalf("filter: %v %+v", nodes, r)
	}
	fail = true
	nodes, r = fetchSource(ctx, f, cache, src, Options{})
	if len(nodes) != 1 || !r.FromCache || !strings.Contains(nodes[0], "node-01") {
		t.Fatalf("fallback: %v %+v", nodes, r)
	}
	fail = false
	body = ""
	nodes, r = fetchSource(ctx, f, cache, src, Options{})
	if len(nodes) != 0 || r.FromCache || r.Error != "" {
		t.Fatalf("empty: %v %+v", nodes, r)
	}
	src.NodeCache = false
	body = trojan(1, "small")
	nodes, r = fetchSource(ctx, f, cache, src, Options{})
	if r.Nodes != 1 || r.Error != "" {
		t.Fatalf("uncached: %v %+v", nodes, r)
	}
}

func TestRenderRejectsUnsupportedAndInvalidTemplate(t *testing.T) {
	if _, _, err := Render(trojan(1, "test"), "invalid", RenderOptions{}, Template{}); err == nil {
		t.Fatal("unsupported format accepted")
	}
	if _, _, err := Render(trojan(1, "test"), "clash", RenderOptions{}, Template{Kind: "custom", Text: "[invalid"}); err == nil {
		t.Fatal("invalid YAML silently replaced")
	}
}
