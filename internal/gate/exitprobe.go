package gate

import (
	"context"
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
	"strings"
	"time"

	"github.com/shusfun/weir/internal/kernelconf"
	"github.com/shusfun/weir/packages/contracts"
)

// clientProbe 按出口协议真连一次：用 sing-box 客户端（中心按订阅里的写法生成的出站）在本机回环起一个代理，
// 经它访问 target（中心的回显接口），返回延迟与实际出口 IP。public_only 时出口地址解析后必须都是公网地址。
// payload：address、port、public_only、client（sing-box 出站）、kernels / drivers（sing-box）、target（https）。
func (a *Agent) clientProbe(ctx context.Context, p map[string]any) (map[string]any, error) {
	host, port := str(p, "address"), num(p, "port")
	if host == "" || port <= 0 || port > 65535 {
		return nil, errors.New("invalid exit address")
	}
	target, err := url.Parse(str(p, "target"))
	if err != nil || target.Scheme != "https" || target.Host == "" {
		return nil, errors.New("invalid probe target")
	}
	if p["public_only"] == true {
		ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
		if err != nil || len(ips) == 0 {
			return nil, errors.New("出口地址无法解析")
		}
		for _, ip := range ips {
			if !publicIP(ip) {
				return nil, errors.New("出口地址不是公网地址")
			}
		}
	}
	var cfg contracts.GateConfig
	raw, _ := json.Marshal(map[string]any{"kernels": p["kernels"], "drivers": p["drivers"]})
	if err = json.Unmarshal(raw, &cfg); err != nil {
		return nil, errors.New("invalid probe kernels")
	}
	k, ok := cfg.Kernels["sing-box"]
	if !ok {
		return nil, errors.New("sing-box client kernel missing")
	}
	cfg.Kernels = map[string]contracts.Kernel{"sing-box": k}
	if err = a.ensureKernels(ctx, cfg); err != nil {
		return nil, err
	}
	var args kernelconf.KernelArgs
	if d, err := kernelconf.DriverFor(cfg, "sing-box"); err == nil && d != nil {
		args = kernelconf.KernelArgs{Run: d.Run, Check: d.Check}
	}
	dir, err := os.MkdirTemp("", "weir-exit-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	socks, err := freeLoopbackPort()
	if err != nil {
		return nil, err
	}
	client, _ := p["client"].(map[string]any)
	client["tag"] = "exit"
	cc, _ := json.Marshal(map[string]any{
		"log":       map[string]any{"level": "warn"},
		"inbounds":  []any{map[string]any{"type": "mixed", "listen": "127.0.0.1", "listen_port": socks}},
		"outbounds": []any{client},
		"route":     map[string]any{"final": "exit"},
	})
	path := filepath.Join(dir, "client.json")
	if err = os.WriteFile(path, cc, 0600); err != nil {
		return nil, err
	}
	run, _, err := kernelArgs("sing-box", args, path)
	if err != nil {
		return nil, err
	}
	runCtx, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	logs := &tailBuffer{}
	cp := exec.CommandContext(runCtx, filepath.Join(a.P.KernelsDir, "sing-box-"+k.Version), run...)
	cp.Stdout, cp.Stderr = logs, logs
	if err = cp.Start(); err != nil {
		return nil, err
	}
	defer func() { _ = cp.Process.Kill(); _ = cp.Wait() }()
	proxy, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", socks))
	hc := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{Proxy: http.ProxyURL(proxy), DisableKeepAlives: true},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	var last error
	for start := time.Now(); time.Since(start) < 25*time.Second; time.Sleep(500 * time.Millisecond) {
		begin := time.Now()
		resp, err := hc.Get(target.String())
		if err != nil {
			last = err
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		if resp.StatusCode >= 400 {
			last = fmt.Errorf("经出口访问返回 %d", resp.StatusCode)
			continue
		}
		out := map[string]any{"mode": "proxy", "latency_ms": time.Since(begin).Milliseconds()}
		var echo struct {
			IP string `json:"ip"`
		}
		if json.Unmarshal(body, &echo) == nil && net.ParseIP(echo.IP) != nil {
			out["exit_ip"] = echo.IP
		}
		return out, nil
	}
	return map[string]any{"mode": "proxy", "client_log": lastLines(logs.String(), 20)}, fmt.Errorf("经出口连接失败：%v", trimProxyError(last))
}

// trimProxyError 去掉本机代理地址，只留出口那一侧的原因。
func trimProxyError(err error) string {
	if err == nil {
		return "超时"
	}
	msg := err.Error()
	if i := strings.LastIndex(msg, ": "); i > 0 && strings.Contains(msg[:i], "127.0.0.1") {
		return msg[i+2:]
	}
	return msg
}
