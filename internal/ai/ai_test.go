package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProvidersRequestShapeAndExtraction(t *testing.T) {
	var got map[string]any
	var path, auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &got)
		path, auth = r.URL.Path, r.Header.Get("Authorization")+r.Header.Get("x-api-key")
		switch {
		case strings.HasSuffix(path, "/messages"):
			w.Write([]byte(`{"content":[{"type":"text","text":"A"}]}`))
		case strings.HasSuffix(path, "/chat/completions"):
			w.Write([]byte(`{"choices":[{"message":{"content":"B"}}]}`))
		default:
			w.Write([]byte(`{"answers":{"draft":{"text":"C"}}}`))
		}
	}))
	defer srv.Close()
	cases := []struct{ protocol, base, path, want string }{
		{"anthropic", srv.URL, "/v1/messages", "A"},
		{"openai", srv.URL + "/v1", "/v1/chat/completions", "B"},
		{"laya", srv.URL + "/v1/systemone", "/v1/systemone", "C"},
	}
	for _, c := range cases {
		p := Provider{Protocol: c.protocol, BaseURL: c.base, Model: "m", Key: "k-secret"}
		if err := p.Validate(); err != nil {
			t.Fatal(err)
		}
		out, err := Client{}.Complete(context.Background(), p, "sys", []Message{{Role: "user", Content: "hi"}})
		if err != nil || out != c.want || path != c.path || !strings.Contains(auth, "k-secret") {
			t.Fatalf("%s: out=%q err=%v path=%s", c.protocol, out, err, path)
		}
	}
	if (Provider{Protocol: "openai", BaseURL: "http://example.com", Model: "m", Key: "k"}).Validate() == nil {
		t.Fatal("plain http to a remote host must be rejected")
	}
}

func TestParsePatchRejectsSecretsAndPlaceholders(t *testing.T) {
	p, err := ParsePatch("好的：\n```json\n{\"set\":{\"ws_path\":\"/x\",\"tls_alpn\":null},\"explain\":\"换路径\"}\n```")
	if err != nil || p.Set["ws_path"] != "/x" || p.Explain != "换路径" {
		t.Fatalf("patch = %+v %v", p, err)
	}
	for _, bad := range []string{`{"set":{"reality_private_key":"x"}}`, `{"set":{"ss_password":"y"}}`, `{"set":{"note":"<redacted>"}}`, `{"set":{}}`, `not json`} {
		if _, err := ParsePatch(bad); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
	before := map[string]any{"ws_path": "/a", "reality_private_key": "PRIV", "tls_alpn": "h2"}
	after := Apply(before, p)
	if after["reality_private_key"] != "PRIV" || after["tls_alpn"] != nil {
		t.Fatalf("apply = %v", after)
	}
	d := Diff(before, after)
	b, _ := json.Marshal(d)
	if len(d) != 2 || strings.Contains(string(b), "PRIV") {
		t.Fatalf("diff = %s", b)
	}
	if r := Redact(before); r["reality_private_key"] != placeholder || r["ws_path"] != "/a" {
		t.Fatalf("redact = %v", r)
	}
}
