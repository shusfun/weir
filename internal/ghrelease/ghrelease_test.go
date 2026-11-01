package ghrelease

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSourcesOrderAndHostMirror(t *testing.T) {
	c := Client{Sources: []Source{{Kind: SourceMirror, Prefix: "https://m.example/"}, {Kind: SourceGate, GateID: 3}, {Kind: SourceDirect}}}
	urls, err := c.URLs("https://github.com/a/b/releases/download/v1/x")
	if err != nil || strings.Join(urls, " ") != "https://m.example/https://github.com/a/b/releases/download/v1/x https://github.com/a/b/releases/download/v1/x" {
		t.Fatalf("urls = %v %v", urls, err)
	}
	if _, err = c.URLs("https://evil.example/x"); err == nil {
		t.Fatal("non-GitHub url accepted")
	}
	if got := HTTPOnly(c.Sources); len(got) != 2 || got[0].Kind != SourceMirror || got[1].Kind != SourceDirect {
		t.Fatalf("http only = %v", got)
	}
	for prefix, host := range map[string]string{"https://m.example/": "m.example", "https://m.example/gh/": "", "https://m.example:8443/": ""} {
		if got := HostMirror(Source{Kind: SourceMirror, Prefix: prefix}); got != host {
			t.Fatalf("%s → %q", prefix, got)
		}
	}
	t.Setenv("WEIR_GITHUB_MIRRORS", "off")
	if d := DefaultSources(); len(d) != 1 || d[0].Kind != SourceDirect {
		t.Fatalf("defaults = %v", d)
	}
}

func TestLatestAndProbe(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/a/b/releases/latest":
			_, _ = w.Write([]byte(`{"tag_name":"v1"}`))
		case "/repos/a/limited/releases/latest":
			w.WriteHeader(429)
		case "/file":
			if r.Header.Get("Range") != "bytes=0-99" {
				t.Errorf("range = %q", r.Header.Get("Range"))
			}
			w.WriteHeader(206)
			_, _ = w.Write(make([]byte, 100))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := Client{API: srv.URL, HTTP: srv.Client()}
	rel, final, err := c.Latest(context.Background(), "a/b")
	if err != nil || rel.Tag != "v1" || final.Path != "/repos/a/b/releases/latest" {
		t.Fatalf("latest = %+v %v %v", rel, final, err)
	}
	if _, _, err = c.Latest(context.Background(), "a/limited"); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("rate = %v", err)
	}
	if _, _, err = c.Latest(context.Background(), "a/none"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing = %v", err)
	}
	p := Probe(context.Background(), srv.Client(), srv.URL+"/file", 100, 5*time.Second)
	if p.Err != nil || p.Bytes != 100 || p.Speed <= 0 {
		t.Fatalf("probe = %+v", p)
	}
	if p = Probe(context.Background(), srv.Client(), srv.URL+"/missing", 100, 5*time.Second); p.Err == nil || !strings.Contains(p.Err.Error(), "404") {
		t.Fatalf("probe 404 = %+v", p)
	}
}
