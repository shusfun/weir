package center

import (
	"context"
	"errors"
	"net"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/net/idna"
	"golang.org/x/text/width"
)

// lookupPublicHost 解析 VPS 公网地址里的域名（测试中替换，不查真实 DNS）。
var lookupPublicHost = func(ctx context.Context, host string) ([]net.IP, error) {
	return net.DefaultResolver.LookupIP(ctx, "ip", host)
}

// normalizePublicHost 把管理员随手粘贴的地址整理成只含主机的形式：全角转半角，
// 去掉协议头、账号、路径、查询参数、端口与 IPv6 的方括号，中文域名转 punycode，统一小写。
// 整理后仍不是 IP 或域名时返回空。
func normalizePublicHost(raw string) string {
	h := strings.ReplaceAll(width.Fold.String(strings.TrimSpace(raw)), "。", ".")
	if i := strings.Index(h, "://"); i >= 0 {
		h = h[i+3:]
	}
	if i := strings.IndexAny(h, "/?#"); i >= 0 {
		h = h[:i]
	}
	if i := strings.LastIndexByte(h, '@'); i >= 0 {
		h = h[i+1:]
	}
	switch {
	case strings.HasPrefix(h, "["):
		end := strings.IndexByte(h, ']')
		if end < 0 {
			return ""
		}
		h = h[1:end]
	case strings.Count(h, ":") == 1:
		h = h[:strings.IndexByte(h, ':')]
	}
	h = strings.TrimSuffix(h, ".")
	if ip := net.ParseIP(h); ip != nil {
		return ip.String()
	}
	if strings.ContainsAny(h, ":%") {
		return ""
	}
	ascii, err := idna.Lookup.ToASCII(h)
	if err != nil || !strings.Contains(ascii, ".") || len(ascii) > 253 {
		return ""
	}
	return ascii
}

// checkPublicHost 整理并检查 VPS 公网地址：IP 必须是公网地址；域名必须能解析，且至少解析到一个公网地址。
// 失败时已写好响应，返回 false。确认不了（DNS 临时故障）也不保存，不把没确认的值当作可用。
func checkPublicHost(c *gin.Context, raw string) (string, bool) {
	h := normalizePublicHost(raw)
	if h == "" {
		failure(c, 400, "invalid_public_host")
		return "", false
	}
	if ip := net.ParseIP(h); ip != nil {
		if !publicAddr(ip) {
			failDetail(c, 422, "public_host_not_public", h, nil)
			return "", false
		}
		return h, true
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	ips, err := lookupPublicHost(ctx, h)
	var dnsErr *net.DNSError
	switch {
	case errors.As(err, &dnsErr) && dnsErr.IsNotFound, err == nil && len(ips) == 0:
		failDetail(c, 422, "public_host_unresolved", h, nil)
		return "", false
	case err != nil:
		_ = c.Error(err)
		failDetail(c, 503, "public_host_lookup_failed", h, nil)
		return "", false
	}
	found := make([]string, 0, len(ips))
	for _, ip := range ips {
		if publicAddr(ip) {
			return h, true
		}
		found = append(found, ip.String())
	}
	failDetail(c, 422, "public_host_not_public", h+" 解析到 "+strings.Join(found, "、"), nil)
	return "", false
}
