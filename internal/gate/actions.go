package gate

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/imzyb/MiGate/packages/contracts"
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
	if time.Now().After(act.ExpiresAt) {
		return contracts.ActionResult{Error: "action expired"}
	}
	var out map[string]any
	var err error
	p := act.Payload
	switch act.Kind {
	case contracts.ActionRealityProbe:
		out, err = realityProbe(ctx, p)
	case contracts.ActionChainProbe, contracts.ActionOutboundProbe:
		out, err = chainProbe(ctx, p)
	case contracts.ActionDiagnostics:
		out, err = a.helper.Diagnostics(ctx, str(p, "kernel"))
	case contracts.ActionCoreRestart:
		err = a.helper.Restart(ctx, str(p, "kernel"))
	case contracts.ActionReleaseReplace:
		if !a.selfUpdate(ctx, str(p, "version")) {
			err = errors.New("self update failed")
		}
	case contracts.ActionUninstall:
		err = a.helper.Uninstall(ctx)
	case contracts.ActionNodeTest:
		out, err = a.nodeTest(ctx, p)
	default:
		err = errUnknownAction
	}
	if err != nil {
		return contracts.ActionResult{Error: err.Error(), Result: out}
	}
	return contracts.ActionResult{OK: true, Result: out}
}

// realityProbe 从候选目标中选出适合作 REALITY 伪装的站点：TLS 1.3、协商出 h2、不跳转到其他域名，取延迟最低者。
func realityProbe(ctx context.Context, p map[string]any) (map[string]any, error) {
	list, _ := p["targets"].([]any)
	if len(list) == 0 || len(list) > 20 {
		return nil, errors.New("targets must contain 1-20 hosts")
	}
	// 并行探测，结果按候选顺序返回；总耗时约等于最慢的一个站点（单站最多约 13 秒）。
	results := make([]map[string]any, len(list))
	latencies := make([]time.Duration, len(list))
	var wg sync.WaitGroup
	for i, item := range list {
		host, _ := item.(string)
		if !probeHost.MatchString(host) {
			continue
		}
		wg.Add(1)
		go func(i int, host string) {
			defer wg.Done()
			results[i], latencies[i] = probeOne(host)
		}(i, host)
	}
	wg.Wait()
	best, bestLatency := "", time.Duration(0)
	kept := results[:0]
	for i, r := range results {
		if r == nil {
			continue
		}
		if r["suitable"] == true && (best == "" || latencies[i] < bestLatency) {
			best, bestLatency = r["host"].(string), latencies[i]
		}
		kept = append(kept, r)
	}
	results = kept
	if best == "" {
		return map[string]any{"results": results}, errors.New("没有找到合适的伪装目标")
	}
	return map[string]any{"results": results, "best": best}, nil
}

// probeOne 检查单个站点：TLS 1.3、协商出 h2、首页不跳转到其他域名。
func probeOne(host string) (map[string]any, time.Duration) {
	r := map[string]any{"host": host}
	start := time.Now()
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	conn, err := tls.DialWithDialer(dialer, "tcp", net.JoinHostPort(host, "443"), &tls.Config{ServerName: host, MinVersion: tls.VersionTLS13, NextProtos: []string{"h2", "http/1.1"}})
	if err != nil {
		r["error"] = err.Error()
		return r, 0
	}
	latency := time.Since(start)
	state := conn.ConnectionState()
	conn.Close()
	r["latency_ms"], r["tls13"], r["h2"] = latency.Milliseconds(), state.Version == tls.VersionTLS13, state.NegotiatedProtocol == "h2"
	client := &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	cross := false
	if resp, err := client.Get("https://" + host + "/"); err == nil {
		if loc, e := url.Parse(resp.Header.Get("Location")); e == nil && loc.Host != "" && loc.Hostname() != host {
			cross = true
		}
		resp.Body.Close()
	}
	r["cross_redirect"] = cross
	r["suitable"] = r["tls13"] == true && r["h2"] == true && !cross
	return r, latency
}

func publicIP(ip net.IP) bool {
	return !(ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() || ip.Equal(net.IPv4(100, 100, 100, 200)) || len(ip) == net.IPv6len && ip[0]&0xfe == 0xfc)
}

// chainProbe 实测链式代理出口：socks5 完成握手（含认证）并经它 CONNECT 一个目标；node 只测 TCP 可达。
// public_only 为真（用户自建）时，解析后的地址必须是公网地址，并直接连接该 IP，避免 DNS 被换。
func chainProbe(ctx context.Context, p map[string]any) (map[string]any, error) {
	host, port := str(p, "address"), num(p, "port")
	if host == "" || port <= 0 || port > 65535 {
		return nil, errors.New("invalid exit address")
	}
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil || len(ips) == 0 {
		return nil, errors.New("出口地址无法解析")
	}
	if p["public_only"] == true {
		for _, ip := range ips {
			if !publicIP(ip) {
				return nil, errors.New("出口地址不是公网地址")
			}
		}
	}
	start := time.Now()
	conn, err := (&net.Dialer{Timeout: 8 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(ips[0].String(), strconv.Itoa(port)))
	if err != nil {
		return nil, errors.New("出口不可达：" + err.Error())
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	if str(p, "kind") == "socks5" {
		target := str(p, "target")
		if target == "" {
			target = "www.gstatic.com:443"
		}
		if err = socks5Connect(conn, str(p, "username"), str(p, "password"), target); err != nil {
			return nil, err
		}
	}
	return map[string]any{"latency_ms": time.Since(start).Milliseconds(), "ip": ips[0].String()}, nil
}

// socks5Connect 按 RFC 1928 / 1929 完成握手与认证，并请求连接 target。
func socks5Connect(conn net.Conn, username, password, target string) error {
	methods := []byte{0x00}
	if username != "" {
		methods = []byte{0x02}
	}
	if _, err := conn.Write(append([]byte{5, byte(len(methods))}, methods...)); err != nil {
		return err
	}
	reply := make([]byte, 2)
	if _, err := io.ReadFull(conn, reply); err != nil || reply[0] != 5 {
		return errors.New("出口不是 SOCKS5 服务")
	}
	switch reply[1] {
	case 0x00:
	case 0x02:
		if len(username) > 255 || len(password) > 255 {
			return errors.New("SOCKS5 用户名或密码过长")
		}
		msg := append([]byte{1, byte(len(username))}, username...)
		msg = append(append(msg, byte(len(password))), password...)
		if _, err := conn.Write(msg); err != nil {
			return err
		}
		if _, err := io.ReadFull(conn, reply); err != nil || reply[1] != 0 {
			return errors.New("SOCKS5 认证失败")
		}
	default:
		return errors.New("SOCKS5 不接受所给的认证方式")
	}
	host, portText, err := net.SplitHostPort(target)
	port, perr := strconv.Atoi(portText)
	if err != nil || perr != nil || len(host) > 255 {
		return errors.New("invalid probe target")
	}
	req := append([]byte{5, 1, 0, 3, byte(len(host))}, host...)
	req = binary.BigEndian.AppendUint16(req, uint16(port))
	if _, err = conn.Write(req); err != nil {
		return err
	}
	head := make([]byte, 4)
	if _, err = io.ReadFull(conn, head); err != nil {
		return err
	}
	if head[1] != 0 {
		return fmt.Errorf("SOCKS5 出口无法连接目标（代码 %d）", head[1])
	}
	return nil
}
