package control

import (
	"net"
	"net/http"
	"net/url"
	"strings"
)

// 公共地址不再配置，按每次请求推导。
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

func requestSecure(r *http.Request) bool { return requestScheme(r) == "https" }

// 浏览器看到的本站来源，例如 https://sub.example.com
func requestOrigin(r *http.Request) string { return requestScheme(r) + "://" + requestHost(r) }

func publicLink(r *http.Request, path string) string {
	return (&url.URL{Scheme: requestScheme(r), Host: requestHost(r), Path: path}).String()
}

// 防 CSRF：现代浏览器以 Sec-Fetch-Site 判断，旧浏览器回退比较 Origin 与本站主机。
// 都没有的请求不是浏览器跨站提交（如 Gate、命令行），照旧放行。
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
