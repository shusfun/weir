package gate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/shusfun/weir/internal/kernelconf"
	"github.com/shusfun/weir/packages/contracts"
)

// freeLoopbackPort 返回一个 TCP 与 UDP 都空闲的回环端口（QUIC 协议监听 UDP）。
var listenLoopback = net.Listen

func freeLoopbackPort(excluded ...int) (int, error) {
	for i := 0; i < 20; i++ {
		l, err := listenLoopback("tcp", "127.0.0.1:0")
		if err != nil {
			return 0, err
		}
		port := l.Addr().(*net.TCPAddr).Port
		skip := false
		for _, p := range excluded {
			if p == port {
				skip = true
				break
			}
		}
		if skip {
			_ = l.Close()
			continue
		}
		u, err := net.ListenPacket("udp", fmt.Sprintf("127.0.0.1:%d", port))
		l.Close()
		if err == nil {
			u.Close()
			return port, nil
		}
	}
	return 0, errors.New("no free loopback port")
}

// tailBuffer 保留子进程最后一段输出（诊断用，最多 4 KiB）。
type tailBuffer struct {
	mu  sync.Mutex
	buf []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > 4096 {
		t.buf = t.buf[len(t.buf)-4096:]
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.TrimSpace(string(t.buf))
}

// nodeTest 是 AI 草稿与节点修改的实测：在本机回环端口上用候选配置起服务端内核，再用 mihomo 客户端
// 按订阅里的出站连接它，经代理下载 256 KiB 并核对长度。不改动正在运行的内核与配置。
// payload.config 是只含一个入站与一份测试凭据的 GateConfig（端口为 0，由这里分配）；
// payload.client 保留订阅客户端的完整依赖图与目标标签，由这里改成临时本机端点。
func (a *Agent) nodeTest(ctx context.Context, p map[string]any) (map[string]any, error) {
	var cfg contracts.GateConfig
	raw, _ := json.Marshal(p["config"])
	if err := json.Unmarshal(raw, &cfg); err != nil || len(cfg.Inbounds) != 1 {
		return nil, errors.New("invalid test config")
	}
	var client contracts.NodeTestClient
	raw, _ = json.Marshal(p["client"])
	if err := json.Unmarshal(raw, &client); err != nil || client.Target == "" {
		return nil, errors.New("invalid test client")
	}
	if _, ok := cfg.Kernels["mihomo"]; !ok {
		return nil, errors.New("mihomo 测试内核未就绪")
	}
	if err := a.ensureKernels(ctx, cfg); err != nil {
		return nil, err
	}
	if err := a.ensureCerts(ctx, cfg); err != nil {
		return nil, err
	}
	port, err := freeLoopbackPort()
	if err != nil {
		return nil, err
	}
	cfg.Inbounds[0].Port = port
	if cfg.Ports.XrayStats, err = freeLoopbackPort(port); err != nil {
		return nil, err
	}
	if cfg.Ports.SingboxStats, err = freeLoopbackPort(port, cfg.Ports.XrayStats); err != nil {
		return nil, err
	}
	out, err := kernelconf.Build(cfg, kernelconf.Options{CertDir: a.P.CertsDir, Now: time.Now()})
	if err != nil {
		return map[string]any{"stage": "build"}, fmt.Errorf("生成配置失败：%w", err)
	}
	dir, err := os.MkdirTemp("", "weir-test-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	kernel := cfg.Inbounds[0].Kernel
	server := out.Xray
	if kernel == "sing-box" {
		server = out.Singbox
	}
	if server == nil {
		return map[string]any{"stage": "build"}, errors.New("候选配置没有生成入站")
	}
	bin := func(name string) string { return filepath.Join(a.P.KernelsDir, name+"-"+cfg.Kernels[name].Version) }
	serverPath := filepath.Join(dir, "server.json")
	if err = os.WriteFile(serverPath, server, 0600); err != nil {
		return nil, err
	}
	// 内核自检：Xray run -test / sing-box check。
	run, test, err := kernelArgs(kernel, out.Args[kernel], serverPath)
	if err != nil {
		return nil, err
	}
	if b, err := exec.CommandContext(ctx, bin(kernel), test...).CombinedOutput(); err != nil {
		return map[string]any{"stage": "check", "log": maskIPs(lastLines(string(b), 20))}, errors.New("内核自检未通过")
	}
	runCtx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	serverLog, clientLog := &tailBuffer{}, &tailBuffer{}
	sp := exec.CommandContext(runCtx, bin(kernel), run...)
	sp.Stdout, sp.Stderr = serverLog, serverLog
	if err = sp.Start(); err != nil {
		return nil, err
	}
	defer func() { _ = sp.Process.Kill(); _ = sp.Wait() }()

	socks, err := freeLoopbackPort(port, cfg.Ports.XrayStats, cfg.Ports.SingboxStats)
	if err != nil {
		return nil, err
	}
	if err := client.Loopback(port); err != nil {
		return map[string]any{"stage": "client_build"}, err
	}
	cc, _ := json.Marshal(client.Config(socks))
	clientPath := filepath.Join(dir, "client.json")
	if err = os.WriteFile(clientPath, cc, 0600); err != nil {
		return nil, err
	}
	d, err := kernelconf.DriverFor(cfg, "mihomo")
	if err != nil || d == nil {
		return nil, errors.New("mihomo 测试驱动缺失或无效")
	}
	clientArgs := kernelconf.KernelArgs{Run: d.Run, Check: d.Check}
	// 测试驱动只决定临时客户端的调用参数。
	if run, test, err = kernelArgs("mihomo", clientArgs, clientPath); err != nil {
		return nil, err
	}
	run, test = append(run, "-d", dir), append(test, "-d", dir)
	if b, err := exec.CommandContext(runCtx, bin("mihomo"), test...).CombinedOutput(); err != nil {
		return map[string]any{"stage": "client_check", "log": maskIPs(lastLines(string(b), 20))}, errors.New("客户端内核自检未通过")
	}
	cp := exec.CommandContext(runCtx, bin("mihomo"), run...)
	cp.Stdout, cp.Stderr, cp.Dir = clientLog, clientLog, dir
	if err = cp.Start(); err != nil {
		return nil, err
	}
	defer func() { _ = cp.Process.Kill(); _ = cp.Wait() }()

	blob := bytes.Repeat([]byte("m"), 256<<10)
	target := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(blob) }))
	if cfg.Inbounds[0].Protocol == "wireguard" {
		// WireGuard 的用户态网络栈拒绝目标为 loopback 的报文；在本机网卡上放置临时测试目标。
		addr, err := nodeTestTargetAddress()
		if err != nil {
			return map[string]any{"stage": "target"}, err
		}
		_ = target.Listener.Close()
		target.Listener, err = net.Listen("tcp", net.JoinHostPort(addr, "0"))
		if err != nil {
			return map[string]any{"stage": "target"}, err
		}
	}
	target.Start()
	defer target.Close()
	proxy, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", socks))
	hc := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: http.ProxyURL(proxy), DisableKeepAlives: true}}
	start := time.Now()
	var last error
	stage := "connect"
	for time.Since(start) < 25*time.Second {
		begin := time.Now()
		resp, err := hc.Get(target.URL)
		if err == nil {
			stage = "download"
			n, cerr := io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if cerr == nil && n == int64(len(blob)) {
				return map[string]any{"stage": "connect", "latency_ms": time.Since(begin).Milliseconds(), "bytes": n}, nil
			}
			err = fmt.Errorf("只收到 %d 字节", n)
		}
		last = err
		time.Sleep(500 * time.Millisecond)
	}
	if strings.Contains(clientLog.String(), "FATAL") || strings.Contains(serverLog.String(), "FATAL") {
		stage = "start"
	}
	return map[string]any{"stage": stage, "server_log": maskIPs(lastLines(serverLog.String(), 20)), "client_log": maskIPs(lastLines(clientLog.String(), 20))}, fmt.Errorf("经候选配置连接失败：%v", last)
}

func nodeTestTargetAddress() (string, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "", err
	}
	for _, addr := range addrs {
		ip, _, err := net.ParseCIDR(addr.String())
		if err == nil && ip.To4() != nil && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() {
			return ip.String(), nil
		}
	}
	return "", errors.New("WireGuard 实测需要本机非回环 IPv4 地址")
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
