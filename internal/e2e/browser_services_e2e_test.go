//go:build e2e

package e2e

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
)

// These upstreams exist only in the isolated center's network namespace. They
// exercise the normal source and WebDAV clients without external credentials.
func browserUpstreams(t *testing.T) (sourceURL, davURL string) {
	t.Helper()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintln(w, "trojan://fixture-only@203.0.113.10:443#演示源-仅授权流程-未实测")
	}))
	t.Cleanup(source.Close)
	var mu sync.Mutex
	files := map[string][]byte{}
	dav := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		if !ok || user != "fixture" || password != "FixtureDAV2026" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/dav/")
		if strings.Contains(name, "/") || strings.Contains(name, "..") {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		switch r.Method {
		case "PUT":
			body, err := io.ReadAll(io.LimitReader(r.Body, 16<<20+1))
			if err != nil || len(body) > 16<<20 {
				w.WriteHeader(http.StatusRequestEntityTooLarge)
				return
			}
			files[name] = body
			w.WriteHeader(http.StatusCreated)
		case "GET":
			body, ok := files[name]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Write(body)
		case "DELETE":
			delete(files, name)
			w.WriteHeader(http.StatusNoContent)
		case "PROPFIND":
			names := make([]string, 0, len(files))
			for name := range files {
				names = append(names, name)
			}
			slices.Sort(names)
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusMultiStatus)
			fmt.Fprint(w, `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:"><d:response><d:href>/dav/</d:href></d:response>`)
			for _, name := range names {
				fmt.Fprintf(w, `<d:response><d:href>/dav/%s</d:href><d:propstat><d:prop><d:getcontentlength>%d</d:getcontentlength></d:prop></d:propstat></d:response>`, name, len(files[name]))
			}
			fmt.Fprint(w, `</d:multistatus>`)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(dav.Close)
	return source.URL, dav.URL + "/dav/"
}
