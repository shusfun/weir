package control

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func originRequest(method, remote, host string, headers map[string]string) *http.Request {
	r := httptest.NewRequest(method, "/api/v1/setup/verify", nil)
	r.RemoteAddr = remote
	r.Host = host
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	return r
}

func TestRequestOriginTrustsOnlyLocalProxy(t *testing.T) {
	proxied := originRequest("GET", "127.0.0.1:50000", "127.0.0.1:19810", map[string]string{"X-Forwarded-Proto": "https", "X-Forwarded-Host": "Sub.Example.com"})
	if got := requestOrigin(proxied); got != "https://sub.example.com" {
		t.Fatalf("proxied origin = %s", got)
	}
	if got := publicLink(proxied, "/sub/abc"); got != "https://sub.example.com/sub/abc" {
		t.Fatalf("link = %s", got)
	}
	// 外部直连伪造转发头无效
	remote := originRequest("GET", "203.0.113.5:40000", "198.51.100.1:19810", map[string]string{"X-Forwarded-Proto": "https", "X-Forwarded-Host": "evil.example"})
	if got := requestOrigin(remote); got != "http://198.51.100.1:19810" {
		t.Fatalf("remote origin = %s", got)
	}
	if requestSecure(remote) {
		t.Fatal("remote forwarded proto must not mark secure")
	}
	// 反代未转发主机时回退到 Host；非法主机回退 localhost
	plain := originRequest("GET", "[::1]:50000", "sub.example.com", map[string]string{"X-Forwarded-Host": "bad host/x"})
	if got := requestHost(plain); got != "sub.example.com" {
		t.Fatalf("fallback host = %s", got)
	}
	plain.Host = "a b"
	if got := requestHost(plain); got != "localhost" {
		t.Fatalf("invalid host = %s", got)
	}
	direct := originRequest("GET", "203.0.113.5:40000", "sub.example.com", nil)
	direct.TLS = &tls.ConnectionState{}
	if !requestSecure(direct) {
		t.Fatal("direct TLS must be secure")
	}
}

func TestSameOriginRequest(t *testing.T) {
	cases := []struct {
		name    string
		method  string
		remote  string
		host    string
		headers map[string]string
		want    bool
	}{
		{"safe method", "GET", "127.0.0.1:1", "127.0.0.1:19810", map[string]string{"Sec-Fetch-Site": "cross-site"}, true},
		{"browser same origin via proxy", "POST", "127.0.0.1:1", "127.0.0.1:19810", map[string]string{"Sec-Fetch-Site": "same-origin", "Origin": "https://sub.example.com"}, true},
		{"cross site", "POST", "127.0.0.1:1", "127.0.0.1:19810", map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "https://evil.example"}, false},
		{"same site subdomain", "POST", "127.0.0.1:1", "127.0.0.1:19810", map[string]string{"Sec-Fetch-Site": "same-site", "Origin": "https://evil.example.com"}, false},
		{"old browser matching host", "POST", "127.0.0.1:1", "127.0.0.1:19810", map[string]string{"Origin": "https://sub.example.com", "X-Forwarded-Host": "sub.example.com"}, true},
		{"old browser foreign origin", "POST", "127.0.0.1:1", "127.0.0.1:19810", map[string]string{"Origin": "https://evil.example", "X-Forwarded-Host": "sub.example.com"}, false},
		{"null origin", "POST", "127.0.0.1:1", "sub.example.com", map[string]string{"Origin": "null"}, false},
		{"remote spoofed forwarded host", "POST", "203.0.113.5:1", "198.51.100.1:19810", map[string]string{"Origin": "https://evil.example", "X-Forwarded-Host": "evil.example"}, false},
		{"non browser client", "POST", "203.0.113.5:1", "198.51.100.1:19810", nil, true},
	}
	for _, tc := range cases {
		if got := sameOriginRequest(originRequest(tc.method, tc.remote, tc.host, tc.headers)); got != tc.want {
			t.Errorf("%s = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// 回归：反代后的同源初始化提交不能被来源校验拦截，跨站提交必须拒绝
func TestSetupBehindProxyAcceptsSameOriginAndRejectsCrossSite(t *testing.T) {
	server, err := New(Config{Listen: "127.0.0.1:19810", EncryptionKey: strings.Repeat("ab", 32)}, t.TempDir()+"/config.json", nil)
	if err != nil {
		t.Fatal(err)
	}
	same := originRequest("POST", "127.0.0.1:50000", "127.0.0.1:19810", map[string]string{"Sec-Fetch-Site": "same-origin", "Origin": "https://sub.example.com", "X-Forwarded-Host": "sub.example.com", "X-Forwarded-Proto": "https"})
	recorder := httptest.NewRecorder()
	server.Router().ServeHTTP(recorder, same)
	if strings.Contains(recorder.Body.String(), "origin_rejected") {
		t.Fatalf("same-origin setup rejected: %d %s", recorder.Code, recorder.Body.String())
	}
	if recorder.Code != 401 {
		t.Fatalf("expected token check, got %d %s", recorder.Code, recorder.Body.String())
	}
	cross := originRequest("POST", "127.0.0.1:50000", "127.0.0.1:19810", map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "https://evil.example"})
	recorder = httptest.NewRecorder()
	server.Router().ServeHTTP(recorder, cross)
	if recorder.Code != 403 || !strings.Contains(recorder.Body.String(), "origin_rejected") {
		t.Fatalf("cross-site setup = %d %s", recorder.Code, recorder.Body.String())
	}
}
