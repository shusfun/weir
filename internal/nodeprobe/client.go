// Package nodeprobe 用短时 mihomo 客户端实测已部署节点。
package nodeprobe

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/net/dns/dnsmessage"

	"github.com/shusfun/weir/packages/contracts"
)

const PayloadSize = 256 << 10

type Check = contracts.NodeProbeCheck
type Result = contracts.NodeProbeResult

type Options struct {
	Binary, Target, Token, Traceparent string
	Run, Check                         []string
	Client                             contracts.NodeTestClient
	UDP                                bool
	PublicOnly                         bool
	Step                               func(string)
	// UDPServers is only overridden by isolated tests; production always uses DNS.
	UDPServers []string
}

// Payload binds the response to this test without retaining download data.
func Payload(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return bytes.Repeat(sum[:], PayloadSize/len(sum))
}

type tail struct {
	sync.Mutex
	b []byte
}

func (t *tail) Write(p []byte) (int, error) {
	t.Lock()
	defer t.Unlock()
	t.b = append(t.b, p...)
	if len(t.b) > 4096 {
		t.b = t.b[len(t.b)-4096:]
	}
	return len(p), nil
}

var terminalEscape = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)

func (t *tail) String() string {
	t.Lock()
	defer t.Unlock()
	return terminalEscape.ReplaceAllString(string(t.b), "")
}

func args(template []string, path string) []string {
	out := append([]string(nil), template...)
	for i := range out {
		out[i] = strings.ReplaceAll(out[i], "{config}", path)
	}
	return append(out, "-d", filepath.Dir(path))
}

func freePort(network string) (int, error) {
	if network == "udp" {
		c, err := net.ListenPacket("udp4", "127.0.0.1:0")
		if err != nil {
			return 0, err
		}
		defer c.Close()
		return c.LocalAddr().(*net.UDPAddr).Port, nil
	}
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// Run never redirects the node endpoint or installs a direct route for payloads.
func Run(ctx context.Context, o Options) (Result, error) {
	r := Result{TCP: Check{Status: "unknown"}, UDP: Check{Status: "not_applicable", Why: "此方案不支持 UDP 转发"}}
	if o.UDP {
		r.UDP = Check{Status: "unknown"}
	}
	step := func(s string) {
		if o.Step != nil {
			o.Step(s)
		}
	}
	dir, err := os.MkdirTemp("", "weir-node-probe-")
	if err != nil {
		return r, err
	}
	defer os.RemoveAll(dir)
	socks, err := freePort("tcp")
	if err != nil {
		return r, err
	}
	var tunnels []any
	servers := o.UDPServers
	if len(servers) == 0 {
		servers = []string{"1.1.1.1:53", "8.8.8.8:53"}
	}
	var udpPorts []int
	if o.UDP {
		for _, server := range servers {
			host, port, err := net.SplitHostPort(server)
			if err != nil {
				return r, err
			}
			var targetPort int
			if _, err = fmt.Sscan(port, &targetPort); err != nil {
				return r, err
			}
			p, err := freePort("udp")
			if err != nil {
				return r, err
			}
			udpPorts = append(udpPorts, p)
			tunnels = append(tunnels, map[string]any{"network": []string{"udp"}, "address": net.JoinHostPort("127.0.0.1", fmt.Sprint(p)), "target": net.JoinHostPort(host, fmt.Sprint(targetPort)), "proxy": o.Client.Target})
		}
	}
	cfg := o.Client.Config(socks)
	if o.PublicOnly {
		hosts, err := PublicHosts(ctx, o.Client.Proxies, o.Target)
		if err != nil {
			return r, err
		}
		cfg["hosts"] = hosts
	}
	// 测试端点使用中心的解析结果，避免临时内核忽略系统 hosts；连接和 TLS 仍完整经过待测节点。
	endpoint, err := url.Parse(o.Target)
	if err != nil || endpoint.Hostname() == "" {
		return r, errors.New("测试端点地址无效")
	}
	if !o.PublicOnly && net.ParseIP(endpoint.Hostname()) == nil {
		addresses, err := net.DefaultResolver.LookupHost(ctx, endpoint.Hostname())
		if err != nil {
			return r, fmt.Errorf("无法解析测试端点：%w", err)
		}
		cfg["hosts"] = map[string]any{endpoint.Hostname(): addresses}
	}
	if len(tunnels) > 0 {
		cfg["tunnels"] = tunnels
	}
	body, err := json.Marshal(cfg)
	if err != nil {
		return r, err
	}
	path := filepath.Join(dir, "client.json")
	if err = os.WriteFile(path, body, 0600); err != nil {
		return r, err
	}
	step("正在检查测试客户端配置")
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	checkCtx, stopCheck := context.WithTimeout(ctx, 10*time.Second)
	log := &tail{}
	check := exec.CommandContext(checkCtx, o.Binary, args(o.Check, path)...)
	check.Stdout, check.Stderr, check.Dir = log, log, dir
	err = check.Run()
	stopCheck()
	if err != nil {
		return r, fmt.Errorf("测试客户端自检失败：%s", log.String())
	}
	log = &tail{}
	cmd := exec.CommandContext(ctx, o.Binary, args(o.Run, path)...)
	cmd.Stdout, cmd.Stderr, cmd.Dir = log, log, dir
	if err = cmd.Start(); err != nil {
		return r, fmt.Errorf("测试客户端启动失败：%w", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	defer func() { _ = cmd.Process.Kill(); <-done }()
	addr := fmt.Sprintf("127.0.0.1:%d", socks)
	ready := time.NewTimer(5 * time.Second)
	defer ready.Stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
wait:
	for {
		c, err := (&net.Dialer{Timeout: 100 * time.Millisecond}).DialContext(ctx, "tcp", addr)
		if err == nil {
			c.Close()
			break
		}
		select {
		case <-ctx.Done():
			return r, ctx.Err()
		case <-ready.C:
			return r, fmt.Errorf("测试客户端没有就绪：%s", log.String())
		case err := <-done:
			done <- err
			return r, fmt.Errorf("测试客户端退出：%v；%s", err, log.String())
		case <-tick.C:
			continue wait
		}
	}
	step("正在经节点完成 TCP 认证并校验下载")
	r.TCP = download(ctx, addr, o)
	if r.TCP.Status == "failed" {
		detail := strings.ToLower(log.String())
		switch {
		case strings.Contains(detail, "authentication"), strings.Contains(detail, "unauthorized"), strings.Contains(detail, "invalid user"):
			r.TCP.Why = "节点认证失败：" + r.TCP.Why
		case strings.Contains(detail, "x509:"), strings.Contains(detail, "certificate"):
			r.TCP.Why = "证书验证失败：" + r.TCP.Why
		}
	}
	if o.UDP {
		step("正在经节点验证 UDP 往返")
		r.UDP = Check{Status: "failed"}
		var reasons []string
		for i, p := range udpPorts {
			check := dnsCheck(ctx, fmt.Sprintf("127.0.0.1:%d", p))
			if check.Status == "passed" {
				r.UDP = check
				break
			}
			reasons = append(reasons, servers[i]+"："+check.Why)
		}
		if r.UDP.Status != "passed" {
			r.UDP.Why = strings.Join(reasons, "；")
		}
	}
	if ctx.Err() != nil {
		return r, ctx.Err()
	}
	if r.TCP.Status == "failed" || r.UDP.Status == "failed" {
		return r, fmt.Errorf("%s", strings.TrimSpace(log.String()))
	}
	return r, nil
}

func download(ctx context.Context, addr string, o Options) Check {
	proxy, _ := url.Parse("socks5://" + addr)
	transport := &http.Transport{Proxy: http.ProxyURL(proxy), DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	hc := &http.Client{Timeout: 20 * time.Second, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequestWithContext(ctx, "GET", o.Target, nil)
	if err != nil {
		return Check{Status: "unknown", Why: err.Error()}
	}
	req.Header.Set("Authorization", "Bearer "+o.Token)
	req.Header.Set("traceparent", o.Traceparent)
	start := time.Now()
	// mihomo 先监听 SOCKS，再启用转发；启动窗口内完成 SOCKS 握手仍可能被断开。
	// 只重试传输层提前断开，HTTP 拒绝、证书错误和内容校验失败保持原结果。
	readyUntil := start.Add(5 * time.Second)
	var resp *http.Response
	for {
		resp, err = hc.Do(req.Clone(ctx))
		if err == nil || time.Now().After(readyUntil) || !(errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, syscall.ECONNRESET)) {
			break
		}
		if o.Step != nil {
			o.Step("测试客户端连接提前断开，正在重试")
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return Check{Status: "failed", Why: "经节点访问测试端点失败：" + ctx.Err().Error()}
		case <-timer.C:
		}
	}
	if err != nil {
		return Check{Status: "failed", Why: "经节点访问测试端点失败：" + err.Error()}
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, PayloadSize+1))
	if resp.StatusCode != 200 {
		return Check{Status: "failed", Why: fmt.Sprintf("测试端点返回 HTTP %d", resp.StatusCode)}
	}
	if err != nil || !bytes.Equal(data, Payload(o.Token)) {
		return Check{Status: "failed", Why: "下载内容校验失败"}
	}
	return Check{Status: "passed", Bytes: int64(len(data)), LatencyMS: max(1, time.Since(start).Milliseconds())}
}

func dnsQuery() ([]byte, dnsmessage.Message, error) {
	var seed [10]byte
	if _, err := rand.Read(seed[:]); err != nil {
		return nil, dnsmessage.Message{}, err
	}
	name, err := dnsmessage.NewName(hex.EncodeToString(seed[2:]) + ".example.com.")
	if err != nil {
		return nil, dnsmessage.Message{}, err
	}
	m := dnsmessage.Message{Header: dnsmessage.Header{ID: binary.BigEndian.Uint16(seed[:2]), RecursionDesired: true}, Questions: []dnsmessage.Question{{Name: name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}}}
	b, err := m.Pack()
	return b, m, err
}

func validDNS(b []byte, q dnsmessage.Message) bool {
	var m dnsmessage.Message
	if m.Unpack(b) != nil || !m.Response || m.ID != q.ID || len(m.Questions) != 1 || m.Questions[0] != q.Questions[0] {
		return false
	}
	return m.RCode == dnsmessage.RCodeSuccess || m.RCode == dnsmessage.RCodeNameError
}

func dnsCheck(ctx context.Context, addr string) Check {
	query, m, err := dnsQuery()
	if err != nil {
		return Check{Status: "unknown", Why: err.Error()}
	}
	c, err := (&net.Dialer{}).DialContext(ctx, "udp", addr)
	if err != nil {
		return Check{Status: "failed", Why: err.Error()}
	}
	defer c.Close()
	deadline := time.Now().Add(8 * time.Second)
	if end, ok := ctx.Deadline(); ok && end.Before(deadline) {
		deadline = end
	}
	_ = c.SetDeadline(deadline)
	stop := context.AfterFunc(ctx, func() { _ = c.Close() })
	defer stop()
	start := time.Now()
	if _, err = c.Write(query); err != nil {
		return Check{Status: "failed", Why: err.Error()}
	}
	buf := make([]byte, 4096)
	for {
		n, err := c.Read(buf)
		if err != nil {
			if errors.Is(err, os.ErrDeadlineExceeded) {
				return Check{Status: "failed", Why: "UDP 请求超时"}
			}
			return Check{Status: "failed", Why: err.Error()}
		}
		if validDNS(buf[:n], m) {
			return Check{Status: "passed", Bytes: int64(n), LatencyMS: max(1, time.Since(start).Milliseconds())}
		}
	}
}
