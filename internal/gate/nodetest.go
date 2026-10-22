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

	"github.com/imzyb/MiGate/internal/kernelconf"
	"github.com/imzyb/MiGate/packages/contracts"
)

// freeLoopbackPort 返回一个 TCP 与 UDP 都空闲的回环端口（QUIC 协议监听 UDP）。
func freeLoopbackPort() (int, error) {
	for i := 0; i < 20; i++ {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return 0, err
		}
		port := l.Addr().(*net.TCPAddr).Port
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

// nodeTest 是 AI 草稿与节点修改的实测：在本机回环端口上用候选配置起服务端内核，再用 sing-box 客户端
// 按订阅里的出站连接它，经代理下载 256 KiB 并核对长度。不改动正在运行的内核与配置。
// payload.config 是只含一个入站与一份测试凭据的 GateConfig（端口为 0，由这里分配）；
// payload.client 是 sing-box 客户端出站（server_port 为 0，由这里填入）。
func (a *Agent) nodeTest(ctx context.Context, p map[string]any) (map[string]any, error) {
	var cfg contracts.GateConfig
	raw, _ := json.Marshal(p["config"])
	if err := json.Unmarshal(raw, &cfg); err != nil || len(cfg.Inbounds) != 1 {
		return nil, errors.New("invalid test config")
	}
	client, _ := p["client"].(map[string]any)
	if client == nil {
		return nil, errors.New("invalid test client")
	}
	if _, ok := cfg.Kernels["sing-box"]; !ok {
		return nil, errors.New("sing-box client kernel missing")
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
	if cfg.Ports.XrayStats, err = freeLoopbackPort(); err != nil {
		return nil, err
	}
	if cfg.Ports.SingboxStats, err = freeLoopbackPort(); err != nil {
		return nil, err
	}
	out, err := kernelconf.Build(cfg, kernelconf.Options{CertDir: a.P.CertsDir, Now: time.Now()})
	if err != nil {
		return map[string]any{"stage": "build"}, fmt.Errorf("生成配置失败：%w", err)
	}
	dir, err := os.MkdirTemp("", "migate-test-")
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
	_, test := kernelArgs(kernel, serverPath)
	if b, err := exec.CommandContext(ctx, bin(kernel), test...).CombinedOutput(); err != nil {
		return map[string]any{"stage": "check", "log": lastLines(string(b), 20)}, errors.New("内核自检未通过")
	}
	runCtx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	serverLog, clientLog := &tailBuffer{}, &tailBuffer{}
	run, _ := kernelArgs(kernel, serverPath)
	sp := exec.CommandContext(runCtx, bin(kernel), run...)
	sp.Stdout, sp.Stderr = serverLog, serverLog
	if err = sp.Start(); err != nil {
		return nil, err
	}
	defer func() { _ = sp.Process.Kill(); _ = sp.Wait() }()

	socks, err := freeLoopbackPort()
	if err != nil {
		return nil, err
	}
	client["server"], client["server_port"], client["tag"] = "127.0.0.1", port, "test"
	cc, _ := json.Marshal(map[string]any{
		"log":       map[string]any{"level": "warn"},
		"inbounds":  []any{map[string]any{"type": "mixed", "listen": "127.0.0.1", "listen_port": socks}},
		"outbounds": []any{client},
		"route":     map[string]any{"final": "test"},
	})
	clientPath := filepath.Join(dir, "client.json")
	if err = os.WriteFile(clientPath, cc, 0600); err != nil {
		return nil, err
	}
	run, _ = kernelArgs("sing-box", clientPath)
	cp := exec.CommandContext(runCtx, bin("sing-box"), run...)
	cp.Stdout, cp.Stderr = clientLog, clientLog
	if err = cp.Start(); err != nil {
		return nil, err
	}
	defer func() { _ = cp.Process.Kill(); _ = cp.Wait() }()

	blob := bytes.Repeat([]byte("m"), 256<<10)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(blob) }))
	defer target.Close()
	proxy, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", socks))
	hc := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: http.ProxyURL(proxy), DisableKeepAlives: true}}
	start := time.Now()
	var last error
	for time.Since(start) < 25*time.Second {
		begin := time.Now()
		resp, err := hc.Get(target.URL)
		if err == nil {
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
	return map[string]any{"stage": "connect", "server_log": lastLines(serverLog.String(), 20), "client_log": lastLines(clientLog.String(), 20)}, fmt.Errorf("经候选配置连接失败：%v", last)
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
