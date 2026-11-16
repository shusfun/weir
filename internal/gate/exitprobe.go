package gate

import (
	"context"
	"crypto/rand"
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
	"strings"
	"time"

	"github.com/shusfun/weir/internal/kernelconf"
	"github.com/shusfun/weir/packages/contracts"
)

// clientProbe 按出口协议真连一次：用 mihomo 客户端（中心按订阅里的写法生成的出站）在本机回环起一个代理，
// 经它访问 target（中心的回显接口），返回延迟与实际出口 IP。public_only 时出口地址解析后必须都是公网地址。
// payload：address、port、public_only、client（mihomo 出站）、kernels / drivers（mihomo）、target（https）。
func (a *Agent) clientProbe(ctx context.Context, p map[string]any) (result map[string]any, resultErr error) {
	stage := "prepare"
	defer func() {
		if resultErr != nil {
			// Kernel diagnostics can include credentials. Only a stable stage code
			// crosses the task boundary, including errors from external processes.
			result = nil
			resultErr = &contracts.TaskError{Code: "exit_probe_" + stage + "_failed", Origin: "gate.clientProbe", Retryable: stage == "connect" || stage == "dns"}
		}
	}()
	host, port := str(p, "address"), num(p, "port")
	if host == "" || port <= 0 || port > 65535 {
		return nil, errors.New("invalid exit address")
	}
	target, err := url.Parse(str(p, "target"))
	if err != nil || target.Scheme != "https" || target.Host == "" {
		return nil, errors.New("invalid probe target")
	}
	if p["public_only"] == true {
		stage = "dns"
		ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
		if err != nil || len(ips) == 0 {
			return nil, errors.New("出口地址无法解析")
		}
		for _, ip := range ips {
			if !publicIP(ip) {
				stage = "public_address"
				return nil, errors.New("出口地址不是公网地址")
			}
		}
	}
	stage = "kernels"
	var cfg contracts.GateConfig
	raw, _ := json.Marshal(map[string]any{"kernels": p["kernels"], "drivers": p["drivers"]})
	if err = json.Unmarshal(raw, &cfg); err != nil {
		return nil, errors.New("invalid probe kernels")
	}
	engine := "mihomo"
	var native *contracts.Outbound
	if p["native"] != nil {
		native = &contracts.Outbound{}
		if err := convert(p["native"], native); err != nil {
			return nil, err
		}
		engine = "sing-box"
	}
	k, ok := cfg.Kernels[engine]
	if !ok {
		return nil, errors.New(engine + " client kernel missing")
	}
	cfg.Kernels = map[string]contracts.Kernel{engine: k}
	if err = a.ensureKernels(ctx, cfg); err != nil {
		return nil, err
	}
	d, err := kernelconf.DriverFor(cfg, engine)
	if err != nil || d == nil {
		return nil, errors.New(engine + " 驱动不可用")
	}
	args := kernelconf.KernelArgs{Run: d.Run, Check: d.Check}
	stage = "config"
	dir, err := os.MkdirTemp("", "weir-exit-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	socks, err := freeLoopbackPort()
	if err != nil {
		return nil, err
	}
	var cc []byte
	user, password := "", ""
	client, _ := p["client"].(map[string]any)
	if native != nil {
		if native.Address != host || native.Port != port || native.Protocol != "shadowsocks" || native.Settings["udp_over_tcp"] != true {
			return nil, errors.New("invalid native probe endpoint")
		}
		native.ID, native.Tag = 1, "probe-exit"
		if !kernelconf.NativeOutbound(*d, *native) {
			return nil, errors.New("原生出站参数不可用")
		}
		stats, err := freeLoopbackPort(socks)
		if err != nil {
			return nil, err
		}
		user = "probe-user"
		password, err = randomProbeSecret()
		if err != nil {
			return nil, err
		}
		cfg.Ports = contracts.PortPlan{SingboxStats: stats}
		cfg.Outbounds = []contracts.Outbound{*native}
		cfg.Bridges = []contracts.OutboundBridge{{FromKernel: "xray", Kernel: "sing-box", OutboundTag: native.Tag, Port: socks, Username: user, Password: password}}
		built, err := kernelconf.Build(cfg, kernelconf.Options{})
		if err != nil {
			return nil, err
		}
		cc = built.Singbox
	} else {
		if client == nil || client["server"] != host || num(client, "port") != port {
			return nil, errors.New("出口客户端端点与校验地址不一致")
		}
		client["name"] = "exit"
		doc := contracts.NodeTestClient{Proxies: []map[string]any{client}, Target: "exit"}
		cc, _ = json.Marshal(doc.Config(socks))
	}
	path := filepath.Join(dir, "client.json")
	if err = os.WriteFile(path, cc, 0600); err != nil {
		return nil, err
	}
	run, check, err := kernelArgs(engine, args, path)
	if err != nil {
		return nil, err
	}
	checkCtx, stopCheck := context.WithTimeout(ctx, 15*time.Second)
	stage = "check"
	if engine == "mihomo" {
		check = append(check, "-d", dir)
		run = append(run, "-d", dir)
	}
	checkCmd := exec.CommandContext(checkCtx, filepath.Join(a.P.KernelsDir, engine+"-"+k.Version), check...)
	checkCmd.Dir = dir
	checkLog, checkErr := checkCmd.CombinedOutput()
	stopCheck()
	if checkErr != nil {
		return nil, fmt.Errorf("%s 配置检查失败：%s", engine, maskIPs(lastLines(string(checkLog), 20)))
	}
	runCtx, cancel := context.WithTimeout(ctx, 35*time.Second)
	stage = "start"
	defer cancel()
	logs := &tailBuffer{}
	cp := exec.CommandContext(runCtx, filepath.Join(a.P.KernelsDir, engine+"-"+k.Version), run...)
	cp.Env = os.Environ()
	cp.Stdout, cp.Stderr, cp.Dir = logs, logs, dir
	if err = cp.Start(); err != nil {
		return nil, err
	}
	stage = "connect"
	defer func() { _ = cp.Process.Kill(); _ = cp.Wait() }()
	proxy, _ := url.Parse(fmt.Sprintf("socks5://127.0.0.1:%d", socks))
	if user != "" {
		proxy.User = url.UserPassword(user, password)
	}
	hc := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{Proxy: http.ProxyURL(proxy), DisableKeepAlives: true},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	var last error
	for start := time.Now(); time.Since(start) < 25*time.Second; time.Sleep(500 * time.Millisecond) {
		begin := time.Now()
		req, err := http.NewRequestWithContext(runCtx, "GET", target.String(), nil)
		if err != nil {
			return nil, err
		}
		resp, err := hc.Do(req)
		if err != nil {
			last = err
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
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
		// 测上游 SOCKS 自己的 UDP 关联。本地 mihomo 端口总是接受关联，不能当成出口能力。
		if typ, _ := client["type"].(string); typ == "socks5" || typ == "socks" {
			user, _ := client["username"].(string)
			pass, _ := client["password"].(string)
			if udp, ok := socksUDP(runCtx, net.JoinHostPort(host, fmt.Sprint(port)), user, pass); ok {
				out["udp"] = udp
			}
		}
		return out, nil
	}
	diagnostic := maskIPs(lastLines(logs.String(), 20))
	return map[string]any{"mode": "proxy", "client_log": diagnostic}, fmt.Errorf("经出口连接失败：%v；%s：%s", trimProxyError(last), engine, diagnostic)
}

// socksUDP 对上游 SOCKS5 做一次 UDP 关联。关联被拒绝就是不支持；握手失败则结果未知。
func socksUDP(ctx context.Context, address, user, pass string) (bool, bool) {
	dialer := net.Dialer{Timeout: 2 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return false, false
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	method := byte(0x00)
	if user != "" || pass != "" {
		method = 0x02
	}
	if _, err = conn.Write([]byte{0x05, 0x01, method}); err != nil {
		return false, false
	}
	buf := make([]byte, 16)
	if _, err = io.ReadFull(conn, buf[:2]); err != nil || buf[0] != 0x05 || buf[1] != method {
		return false, false
	}
	if method == 0x02 {
		ub, pb := []byte(user), []byte(pass)
		if len(ub) > 255 || len(pb) > 255 {
			return false, false
		}
		msg := append(append([]byte{0x01, byte(len(ub))}, ub...), byte(len(pb)))
		msg = append(msg, pb...)
		if _, err = conn.Write(msg); err != nil {
			return false, false
		}
		if _, err = io.ReadFull(conn, buf[:2]); err != nil || buf[1] != 0x00 {
			return false, false
		}
	}
	if _, err = conn.Write([]byte{0x05, 0x03, 0x00, 0x01, 0, 0, 0, 0, 0, 0}); err != nil {
		return false, false
	}
	if _, err = io.ReadFull(conn, buf[:4]); err != nil {
		return false, false
	}
	return buf[1] == 0x00, true
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

func randomProbeSecret() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
