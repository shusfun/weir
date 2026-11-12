// Package publicnet supplies the common transport for public network tasks.
package publicnet

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

var excluded = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"), netip.MustParsePrefix("64:ff9b:1::/48"), netip.MustParsePrefix("2002::/16"), netip.MustParsePrefix("2001::/32"),
}

func Public(addr netip.Addr) bool {
	if !addr.IsValid() || addr.Zone() != "" {
		return false
	}
	addr = addr.Unmap()
	if !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.IsMulticast() || addr.IsUnspecified() {
		return false
	}
	for _, p := range excluded {
		if p.Contains(addr) {
			return false
		}
	}
	return true
}

func URL(raw string) (*url.URL, error) {
	u, e := url.Parse(raw)
	if e != nil || u.Host == "" || u.User != nil || u.Fragment != "" || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, errors.New("public_url_invalid")
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if host == "" || host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.Contains(host, "%") {
		return nil, errors.New("public_url_private")
	}
	if ip, e := netip.ParseAddr(host); e == nil && !Public(ip) {
		return nil, errors.New("public_url_private")
	}
	return u, nil
}

type Resolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

type Transport struct {
	Resolver Resolver
	Dialer   *net.Dialer
}

// Resolve validates the complete answer before a caller pins it into a
// separate runtime's hosts table. A later lookup must not replace this answer.
func Resolve(ctx context.Context, host string, resolver Resolver) ([]netip.Addr, error) {
	if resolver == nil { resolver = net.DefaultResolver }
	ips, err := resolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(ips) == 0 { return nil, errors.New("public_address_unresolvable") }
	for _, ip := range ips {
		if !Public(ip) { return nil, errors.New("public_address_private") }
	}
	return ips, nil
}

// DialIP 连接已经由 DoH 选定的公网地址，不再交给系统 DNS 重解析。
func DialIP(ctx context.Context, network, address string, dialer *net.Dialer) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, errors.New("public_address_invalid")
	}
	ip, err := netip.ParseAddr(host)
	if err != nil || !Public(ip) {
		return nil, errors.New("public_address_private")
	}
	if dialer == nil {
		dialer = &net.Dialer{Timeout: 10 * time.Second}
	}
	return dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
}

func (t Transport) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, e := net.SplitHostPort(address)
	if e != nil {
		return nil, errors.New("public_address_invalid")
	}
	r := t.Resolver
	if r == nil {
		r = net.DefaultResolver
	}
	ips, e := Resolve(ctx, host, r)
	if e != nil { return nil, e }
	d := t.Dialer
	if d == nil {
		d = &net.Dialer{Timeout: 10 * time.Second}
	}
	for _, ip := range ips {
		conn, err := d.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	return nil, errors.New("public_connection_failed")
}

func Client(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: &http.Transport{Proxy: nil, DisableCompression: true, DialContext: (Transport{}).DialContext, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 20 * time.Second}, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("public_redirect_limit")
		}
		_, err := URL(req.URL.String())
		return err
	}}
}
