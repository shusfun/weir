// Package misub 是 MiSub 订阅管理的编排层：拉取订阅源（防 SSRF、保护性缓存）、处理手动节点、
// 合并去重、运行算子链，再用 internal/sub 的内置生成器或 ini 模板输出。存储由调用方（中心）负责。
package misub

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// maxBody 是单个订阅源响应的上限。
const maxBody = 16 << 20

// publicIP 拒绝回环、内网、链路本地、组播、云元数据等地址。
func publicIP(ip net.IP) bool {
	if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() || ip.IsMulticast() || ip.IsInterfaceLocalMulticast() {
		return false
	}
	if v4 := ip.To4(); v4 != nil {
		// 100.64.0.0/10（CGNAT）、0.0.0.0/8、198.18.0.0/15（基准测试）
		if v4[0] == 0 || v4[0] == 100 && v4[1]&0xc0 == 64 || v4[0] == 198 && v4[1]&0xfe == 18 {
			return false
		}
		return true
	}
	return ip[0]&0xfe != 0xfc // fc00::/7
}

// CheckURL 只接受 http(s)、不含账号的公网地址（解析后的 IP 在连接时再次检查）。
func CheckURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, errors.New("invalid_source_url")
	}
	if u.User != nil {
		return nil, errors.New("source_url_credentials")
	}
	host := u.Hostname()
	if strings.EqualFold(host, "localhost") || strings.HasSuffix(strings.ToLower(host), ".localhost") {
		return nil, errors.New("source_url_private")
	}
	if ip := net.ParseIP(host); ip != nil && !publicIP(ip) {
		return nil, errors.New("source_url_private")
	}
	return u, nil
}

// Fetcher 拉取订阅源。AllowPrivate 只用于测试。
type Fetcher struct {
	AllowPrivate bool
	Timeout      time.Duration
}

func (f Fetcher) client() *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	tr := &http.Transport{
		Proxy: nil, // 不使用环境代理
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
			if err != nil || len(ips) == 0 {
				return nil, errors.New("source_unresolvable")
			}
			for _, ip := range ips {
				if f.AllowPrivate || publicIP(ip) {
					return dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
				}
			}
			return nil, errors.New("source_url_private")
		},
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 20 * time.Second,
	}
	timeout := f.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	return &http.Client{Transport: tr, Timeout: timeout, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too_many_redirects")
		}
		if req.URL.Scheme != "http" && req.URL.Scheme != "https" || req.URL.User != nil {
			return errors.New("invalid_redirect")
		}
		return nil
	}}
}

// Response 是一次拉取的结果。
type Response struct {
	Body     []byte
	UserInfo string // Subscription-Userinfo 原文
	Title    string // Profile-Title（已解码）
}

// Get 拉取订阅源；User-Agent 只允许可见 ASCII，防止头注入。
func (f Fetcher) Get(ctx context.Context, raw, userAgent string) (Response, error) {
	u, err := CheckURL(raw)
	if err != nil && !f.AllowPrivate {
		return Response{}, err
	}
	if u == nil {
		if u, err = url.Parse(raw); err != nil {
			return Response{}, err
		}
	}
	for _, r := range userAgent {
		if r < 0x20 || r > 0x7e {
			return Response{}, errors.New("invalid_user_agent")
		}
	}
	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return Response{}, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := f.client().Do(req)
	if err != nil {
		return Response{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return Response{}, fmt.Errorf("source_http_%d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return Response{}, err
	}
	if len(body) > maxBody {
		return Response{}, errors.New("source_too_large")
	}
	return Response{Body: body, UserInfo: resp.Header.Get("Subscription-Userinfo"), Title: profileTitle(resp.Header)}, nil
}

func profileTitle(h http.Header) string {
	t := strings.TrimSpace(h.Get("Profile-Title"))
	if strings.HasPrefix(t, "base64:") {
		if b, err := decodeB64(strings.TrimPrefix(t, "base64:")); err == nil {
			return strings.TrimSpace(string(b))
		}
		return ""
	}
	if t != "" {
		return t
	}
	cd := h.Get("Content-Disposition")
	if i := strings.Index(cd, "filename*=UTF-8''"); i >= 0 {
		if v, err := url.PathUnescape(strings.TrimSpace(cd[i+len("filename*=UTF-8''"):])); err == nil {
			return v
		}
	}
	return ""
}
