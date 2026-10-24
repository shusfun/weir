package subscription

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchCustomUserAgentAndRedirect(t *testing.T) {
	for _, custom := range []string{"", "Weir-Acceptance/69 (Inventory)"} {
		seen := []string{}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			seen = append(seen, r.UserAgent())
			if r.URL.Path == "/start" {
				http.Redirect(w, r, "/nodes", http.StatusFound)
				return
			}
			w.Write([]byte("trojan://p@example.com:443#n"))
		}))
		_, err := (HTTPFetcher{UserAgent: custom}).FetchInventory(context.Background(), server.URL+"/start", true)
		server.Close()
		if err != nil {
			t.Fatal(err)
		}
		want := custom
		if want == "" {
			want = UserAgent
		}
		if len(seen) != 2 || seen[0] != want || seen[1] != want {
			t.Fatal("user agent changed across redirect")
		}
	}
	for _, bad := range []string{"valid\r\nAuthorization: secret", "bad\x00agent", string(make([]byte, 513))} {
		if _, err := (HTTPFetcher{UserAgent: bad}).FetchInventory(context.Background(), "http://127.0.0.1:1/", true); err == nil || err.Error() != "invalid subscription user agent" {
			t.Fatal("unsafe header not rejected before dial")
		}
	}
}
