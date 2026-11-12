package center

import (
	"net/http/httptest"
	"testing"
)

// CENTER-01：中心只接受 HTTPS（本机回环直连除外），只信任本机回环反代的转发头。
func TestCenterHTTPSMatrix(t *testing.T) {
	s, _ := newInstalledServer(t)
	cases := []struct {
		name    string
		url     string
		remote  string
		headers map[string]string
		want    int
	}{
		{"公网明文直连", "http://203.0.113.1/api/v1/public", "198.51.100.7:4000", nil, 403},
		{"本机回环直连明文", "http://127.0.0.1:22341/api/v1/public", "127.0.0.1:5000", nil, 200},
		{"本机反代转发 HTTPS", "http://center.example.com/api/v1/public", "127.0.0.1:5000", map[string]string{"X-Forwarded-Proto": "https", "X-Forwarded-Host": "center.example.com"}, 200},
		{"本机反代转发明文", "http://center.example.com/api/v1/public", "127.0.0.1:5000", map[string]string{"X-Forwarded-Proto": "http", "X-Forwarded-Host": "center.example.com"}, 403},
		{"公网伪造转发头", "http://center.example.com/api/v1/public", "198.51.100.7:4000", map[string]string{"X-Forwarded-Proto": "https"}, 403},
		{"公网 TLS 直连", "https://center.example.com/api/v1/public", "198.51.100.7:4000", nil, 200},
	}
	for _, tc := range cases {
		req := httptest.NewRequest("GET", tc.url, nil)
		req.RemoteAddr = tc.remote
		for k, v := range tc.headers {
			req.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		s.Router().ServeHTTP(w, req)
		if w.Code != tc.want {
			t.Errorf("%s = %d, want %d: %s", tc.name, w.Code, tc.want, w.Body.String())
		}
	}
}
