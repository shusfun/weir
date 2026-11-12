package gate

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/shusfun/weir/internal/publicnet"
	"github.com/shusfun/weir/packages/contracts"
)

var probeHost = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,62})(\.[A-Za-z0-9]([A-Za-z0-9-]{0,62}))+$`)

func str(m map[string]any, k string) string { v, _ := m[k].(string); return v }

func num(m map[string]any, k string) int {
	switch v := m[k].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return 0
}

// runAction 只执行白名单动作；过期动作不执行。
func (a *Agent) runAction(ctx context.Context, act contracts.Action) contracts.ActionResult {
	if err := a.checkExecutionLease(ctx); err != nil {
		return contracts.ActionResult{Error: err.Error()}
	}
	if time.Now().After(act.ExpiresAt) {
		return contracts.ActionResult{Error: "action expired"}
	}
	var out map[string]any
	var err error
	p := act.Payload
	switch act.Kind {
	case contracts.TaskCoreInstall:
		var req contracts.KernelInstallTask
		if err = convert(p, &req); err == nil {
			out, err = a.installKernelTask(ctx, req)
		}
	case contracts.ActionRealityProbe:
		out, err = realityProbe(ctx, p)
	case contracts.ActionChainProbe, contracts.ActionOutboundProbe:
		out, err = a.clientProbe(ctx, p)
	case contracts.ActionLiveStart:
		if a.runCtx == nil {
			err = errors.New("agent not running")
		} else {
			a.startLive(num(p, "seconds"))
		}
	case contracts.ActionDiagnostics:
		out, err = a.helper.Diagnostics(ctx, str(p, "kernel"))
	case contracts.ActionHostRepair:
		var prev string
		prev, err = a.helper.RepairHost(ctx, str(p, "key"), str(p, "value"))
		out = map[string]any{"previous": prev}
	case contracts.ActionCoreRestart:
		err = a.helper.Restart(ctx, str(p, "kernel"))
	case contracts.ActionReleaseReplace:
		// 旧中心只给 version：没有地址时直接从中心下载。
		var rel contracts.ReleaseTarget
		if err = convert(p, &rel); err == nil {
			err = a.manualUpdate(ctx, rel)
		}
	case contracts.ActionAssetFetch:
		out, err = a.assetFetch(ctx, p)
	case contracts.ActionSourceProbe:
		out, err = a.sourceProbe(ctx, p)
	case contracts.ActionUninstall:
		err = a.helper.Uninstall(ctx)
	case contracts.ActionNodeTest:
		out, err = a.nodeTest(ctx, p)
	case contracts.ActionACMEHTTP01:
		err = a.helper.ACMEHTTP01(ctx, str(p, "token"), str(p, "key_auth"), num(p, "seconds"), str(p, "host"))
	case contracts.ActionPanelConfigure:
		out, err = a.configurePanel(ctx, p)
	case contracts.ActionPanelDiscover:
		var st contracts.PanelStatus
		st, err = a.helper.DiscoverPanel(ctx)
		out = map[string]any{"http01_webroot": st.HTTP01Webroot, "kind": st.Kind, "detected": st.Detected, "configured": st.Configured, "version": st.Version, "error": st.Error}
	case contracts.ActionPanelRemove:
		err = a.helper.RemovePanel(ctx)
	case contracts.ActionPanelCert:
		var pc contracts.PanelCert
		if !validPanelIP(str(p, "ip")) {
			err = errors.New("invalid ip")
		} else if pc, err = a.helper.PanelCert(ctx, int64(num(p, "cert_id")), str(p, "ip")); err == nil {
			out = map[string]any{"cert_pem": pc.CertPEM, "sha256": pc.SHA256, "not_after": pc.NotAfter}
		}
	default:
		err = errUnknownAction
	}
	if err != nil {
		return contracts.ActionResult{Error: err.Error(), Result: out}
	}
	return contracts.ActionResult{OK: true, Result: out}
}

// configurePanel 凭动作里的一次性编号从中心取回 1Panel API 密钥（只取一次，中心不落库），交给辅助程序测试并保存。
func (a *Agent) configurePanel(ctx context.Context, p map[string]any) (map[string]any, error) {
	var secret struct {
		APIKey string `json:"api_key"`
	}
	if _, err := a.center.call(ctx, "POST", "/agent/panel-secret", map[string]string{"nonce": str(p, "nonce")}, &secret); err != nil || secret.APIKey == "" {
		return nil, errors.New("无法从中心取回 1Panel 密钥（可能已过期，请重新提交）")
	}
	v, err := a.helper.SetPanel(ctx, PanelConfig{Port: num(p, "port"), HTTPS: p["https"] == true, APIKey: secret.APIKey})
	if err != nil {
		return nil, err
	}
	return map[string]any{"version": v}, nil
}

// realityProbe 从候选目标中选出适合作 REALITY 伪装的站点：TLS 1.3、协商出 h2、不跳转到其他域名，取延迟最低者。
func realityProbe(ctx context.Context, p map[string]any) (map[string]any, error) {
	list, _ := p["targets"].([]any)
	if len(list) == 0 || len(list) > 20 {
		return nil, errors.New("reality_targets_invalid")
	}
	// 并行探测，结果按候选顺序返回；总耗时约等于最慢的一个站点（单站最多约 13 秒）。
	hosts := make([]string, len(list))
	for i, item := range list {
		host, _ := item.(string)
		if !probeHost.MatchString(host) {
			return nil, errors.New("reality_targets_invalid")
		}
		hosts[i] = host
	}
	results := make([]map[string]any, len(hosts))
	latencies := make([]time.Duration, len(hosts))
	var wg sync.WaitGroup
	for i, host := range hosts {
		wg.Add(1)
		go func(i int, host string) {
			defer wg.Done()
			results[i], latencies[i] = probeOneContext(ctx, host)
		}(i, host)
	}
	wg.Wait()
	best, bestLatency := "", time.Duration(0)
	for i, r := range results {
		if r == nil {
			continue
		}
		if r["suitable"] == true && (best == "" || latencies[i] < bestLatency) {
			best, bestLatency = r["host"].(string), latencies[i]
		}
	}
	out := map[string]any{"results": results}
	if best != "" {
		out["best"] = best
	}
	return out, nil
}

// probeOne 检查单个站点：TLS 1.3、协商出 h2、首页不跳转到其他域名。
func probeOne(host string) (map[string]any, time.Duration) {
	return probeOneContext(context.Background(), host)
}

func probeOneContext(ctx context.Context, host string) (map[string]any, time.Duration) {
	r := map[string]any{"host": host}
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, 13*time.Second)
	defer cancel()
	ips, dohErr := dohResolve(ctx, host)
	if dohErr != nil || len(ips) == 0 {
		r["doh_error"] = dohErrorCode(dohErr)
		r["suitable"] = false
		return r, 0
	}
	r["doh_ips"] = ips
	// DoH 常返回多个地址，第一个不通不代表站点不可用；逐个试，最多 3 个，避免拖过单站时限。
	var raw net.Conn
	var err error
	for i, ip := range ips {
		if i >= 3 || ctx.Err() != nil {
			break
		}
		if raw, err = publicnet.DialIP(ctx, "tcp", net.JoinHostPort(ip, "443"), &net.Dialer{Timeout: 3 * time.Second}); err == nil {
			break
		}
	}
	if raw == nil {
		if err == nil {
			err = errors.New("public_connection_failed")
		}
		r["error"] = "public_connection_failed"
		r["suitable"] = false
		return r, 0
	}
	conn := tls.Client(raw, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS13, NextProtos: []string{"h2", "http/1.1"}})
	defer conn.Close()
	if err = conn.HandshakeContext(ctx); err != nil {
		r["error"] = "tls_handshake_failed"
		return r, 0
	}
	latency := time.Since(start)
	state := conn.ConnectionState()
	conn.Close()
	r["latency_ms"], r["tls13"], r["h2"] = latency.Milliseconds(), state.Version == tls.VersionTLS13, state.NegotiatedProtocol == "h2"
	client := pinnedProbeClient(ips[0])
	defer client.CloseIdleConnections()
	cross := false
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+host+"/", nil)
	if err != nil {
		r["error"] = "probe_request_invalid"
		return r, 0
	}
	if resp, err := client.Do(req); err == nil {
		if loc, e := url.Parse(resp.Header.Get("Location")); e == nil && loc.Host != "" && loc.Hostname() != host {
			cross = true
		}
		resp.Body.Close()
	} else {
		r["error"] = "http_probe_failed"
		return r, latency
	}
	r["cross_redirect"] = cross
	// 候选必须能在这台 VPS 上用 DoH 解析，再满足 TLS 1.3、h2 且不跨域跳转。
	r["suitable"] = r["tls13"] == true && r["h2"] == true && !cross
	return r, latency
}

func pinnedProbeClient(ip string) *http.Client {
	return pinnedNameClient(ip, "", 8*time.Second)
}

func pinnedNameClient(ip, serverName string, timeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	tlsConf := &tls.Config{MinVersion: tls.VersionTLS12}
	if serverName != "" {
		tlsConf.ServerName = serverName
	}
	return &http.Client{
		Timeout:       timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Transport: &http.Transport{
			Proxy: nil, ForceAttemptHTTP2: true, TLSHandshakeTimeout: timeout, TLSClientConfig: tlsConf,
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				host := ip
				if host == "" {
					host, _, _ = net.SplitHostPort(addr)
				}
				return publicnet.DialIP(ctx, network, net.JoinHostPort(host, "443"), dialer)
			},
		},
	}
}

func dohResolve(ctx context.Context, host string) ([]string, error) {
	// 固定解析器地址：系统 DNS 被污染时仍能做 DoH，也不会把假 IP 当成解析器。
	var last error
	for _, ep := range []struct{ ip, name, raw string }{
		{"1.1.1.1", "cloudflare-dns.com", "https://cloudflare-dns.com/dns-query"},
		{"8.8.8.8", "dns.google", "https://dns.google/resolve"},
	} {
		client := pinnedNameClient(ep.ip, ep.name, 5*time.Second)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, ep.raw+"?name="+url.QueryEscape(host)+"&type=A", nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/dns-json")
		resp, err := client.Do(req)
		if err != nil {
			client.CloseIdleConnections()
			last = err
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		resp.Body.Close()
		client.CloseIdleConnections()
		var doc struct {
			Answer []struct {
				Data string `json:"data"`
				Type int    `json:"type"`
			} `json:"Answer"`
		}
		if json.Unmarshal(body, &doc) != nil {
			last = errors.New("doh_unavailable")
			continue
		}
		var ips []string
		for _, a := range doc.Answer {
			if a.Type == 1 && net.ParseIP(a.Data) != nil {
				ips = append(ips, a.Data)
			}
		}
		if len(ips) > 0 {
			return ips, nil
		}
		last = errors.New("doh_empty")
	}
	if last == nil {
		last = errors.New("doh_unavailable")
	}
	return nil, last
}

func dohErrorCode(err error) string {
	if err == nil {
		return "doh_unavailable"
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Err != nil {
		err = urlErr.Err
	}
	text := err.Error()
	switch {
	case strings.Contains(text, "certificate") || strings.Contains(text, "x509"):
		return "doh_certificate_invalid"
	case strings.Contains(text, "public_address"):
		return "public_address_private"
	case strings.Contains(text, "public_connection_failed") || strings.Contains(text, "connection refused") || strings.Contains(text, "i/o timeout") || strings.Contains(text, "no such host"):
		return "doh_unreachable"
	default:
		return "doh_unavailable"
	}
}

func publicIP(ip net.IP) bool {
	addr, ok := netip.AddrFromSlice(ip)
	return ok && publicnet.Public(addr)
}
