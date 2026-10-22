package control

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestDirectLoopbackRejectsProxiedRequests(t *testing.T) {
	cases := []struct {
		remote string
		header string
		want   bool
	}{
		{"127.0.0.1:5000", "", true},
		{"[::1]:5000", "", true},
		{"203.0.113.9:5000", "", false},
		{"127.0.0.1:5000", "X-Forwarded-For", false},
		{"127.0.0.1:5000", "X-Real-IP", false},
		{"127.0.0.1:5000", "Forwarded", false},
	}
	for _, tc := range cases {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/api/v1/devices/pair", nil)
		c.Request.RemoteAddr = tc.remote
		if tc.header != "" {
			c.Request.Header.Set(tc.header, "198.51.100.1")
		}
		if got := directLoopback(c); got != tc.want {
			t.Fatalf("%s %s: got %v", tc.remote, tc.header, got)
		}
	}
}
