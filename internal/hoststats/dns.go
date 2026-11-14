package hoststats

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/shusfun/weir/internal/publicnet"
)

// CompareDNS compares resolver answers; CDN geolocation can legitimately differ.
func CompareDNS(ctx context.Context) (string, bool) {
	const name = "www.microsoft.com"
	system, err := net.DefaultResolver.LookupNetIP(ctx, "ip4", name)
	if err != nil || len(system) == 0 {
		return "", false
	}
	doh, err := dohA(ctx, name)
	if err != nil || len(doh) == 0 {
		return "", false
	}
	return compareDNSAnswers(system, doh)
}

func compareDNSAnswers(system, doh []netip.Addr) (string, bool) {
	if len(system) == 0 || len(doh) == 0 {
		return "", false
	}
	known := map[string]bool{}
	for _, ip := range doh {
		known[ip.String()] = true
	}
	for _, ip := range system {
		if !publicnet.Public(ip) {
			return "polluted", true
		}
	}
	for _, ip := range system {
		if !known[ip.String()] {
			return "answers_differ", true
		}
	}
	return "clean", true
}

func dohA(ctx context.Context, name string) ([]netip.Addr, error) {
	dialer := &net.Dialer{Timeout: 1500 * time.Millisecond}
	client := &http.Client{Timeout: 1500 * time.Millisecond, Transport: &http.Transport{Proxy: nil, TLSHandshakeTimeout: 1500 * time.Millisecond, DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
		return publicnet.DialIP(ctx, network, net.JoinHostPort("1.1.1.1", "443"), dialer)
	}}}
	defer client.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://cloudflare-dns.com/dns-query?name="+name+"&type=A", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/dns-json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return nil, err
	}
	var doc struct {
		Answer []struct {
			Data string `json:"data"`
			Type int    `json:"type"`
		} `json:"Answer"`
	}
	if err = json.Unmarshal(body, &doc); err != nil {
		return nil, err
	}
	var ips []netip.Addr
	for _, a := range doc.Answer {
		ip, err := netip.ParseAddr(a.Data)
		if a.Type == 1 && err == nil && publicnet.Public(ip) {
			ips = append(ips, ip)
		}
	}
	if len(ips) == 0 {
		return nil, io.EOF
	}
	return ips, nil
}

func certRemainingDays(dir string, now time.Time) (int, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, false
	}
	found := false
	min := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".crt") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		block, _ := pem.Decode(raw)
		if block == nil {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			continue
		}
		days := int(cert.NotAfter.Sub(now).Hours() / 24)
		if days < 0 {
			days = 0
		}
		if !found || days < min {
			min, found = days, true
		}
	}
	return min, found
}
