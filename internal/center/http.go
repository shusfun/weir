package center

import (
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
)

func failure(c *gin.Context, status int, code string) {
	c.AbortWithStatusJSON(status, gin.H{"error": gin.H{"code": code, "message": code}})
}

func bind(c *gin.Context, v any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
	if err := c.ShouldBindJSON(v); err != nil {
		failure(c, 400, "invalid_request")
		return false
	}
	return true
}

// 只有来自本机回环的连接（同机反代）才信任 X-Forwarded-Proto / X-Forwarded-Host。
func fromLocalProxy(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func validHost(h string) bool {
	if h == "" || len(h) > 255 {
		return false
	}
	for _, ch := range h {
		switch {
		case ch >= 'a' && ch <= 'z', ch >= 'A' && ch <= 'Z', ch >= '0' && ch <= '9':
		case strings.ContainsRune(".-:[]", ch):
		default:
			return false
		}
	}
	return true
}

func firstForwarded(v string) string {
	if i := strings.IndexByte(v, ','); i >= 0 {
		v = v[:i]
	}
	return strings.TrimSpace(v)
}

func requestScheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	if fromLocalProxy(r) && strings.EqualFold(firstForwarded(r.Header.Get("X-Forwarded-Proto")), "https") {
		return "https"
	}
	return "http"
}

func requestHost(r *http.Request) string {
	if fromLocalProxy(r) {
		if h := firstForwarded(r.Header.Get("X-Forwarded-Host")); validHost(h) {
			return strings.ToLower(h)
		}
	}
	if validHost(r.Host) {
		return strings.ToLower(r.Host)
	}
	return "localhost"
}

func requestSecure(r *http.Request) bool  { return requestScheme(r) == "https" }
func requestOrigin(r *http.Request) string { return requestScheme(r) + "://" + requestHost(r) }

func publicLink(r *http.Request, path string) string {
	return (&url.URL{Scheme: requestScheme(r), Host: requestHost(r), Path: path}).String()
}

// sameOriginRequest 防 CSRF：现代浏览器看 Sec-Fetch-Site，旧浏览器比较 Origin 与本站主机；
// 两者都没有的请求不是浏览器跨站提交（Gate、命令行），放行。
func sameOriginRequest(r *http.Request) bool {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return true
	}
	switch r.Header.Get("Sec-Fetch-Site") {
	case "same-origin", "none":
		return true
	case "":
	default:
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	return err == nil && u.Host != "" && strings.EqualFold(u.Host, requestHost(r))
}

func validEmail(s string) bool {
	if len(s) > 254 || strings.ContainsAny(s, "\r\n ") {
		return false
	}
	a := strings.Split(s, "@")
	return len(a) == 2 && len(a[0]) > 0 && strings.Contains(a[1], ".")
}

// validAccountPassword 只用于设置新密码：6–72 位，同时包含大写、小写字母和数字。
func validAccountPassword(password string) bool {
	if len(password) < 6 || len(password) > 72 {
		return false
	}
	var upper, lower, digit bool
	for _, r := range password {
		switch {
		case r >= 'A' && r <= 'Z':
			upper = true
		case r >= 'a' && r <= 'z':
			lower = true
		case r >= '0' && r <= '9':
			digit = true
		}
	}
	return upper && lower && digit
}
