package nodeprobe

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
	"syscall"
	"time"
)

// MeasureOptions 让本机 mihomo 经一个代理先访问回显，再下载固定长度的数据。
type MeasureOptions struct {
	Binary, Echo, Download string
	Run, Check             []string
	Proxy                  map[string]any
	PublicOnly             bool
}

// MeasureResult 分开记录握手延迟和下载。下载失败不表示连通失败。
type MeasureResult struct {
	LatencyMS     int64
	ExitIP        string
	Bytes         int64
	DownloadMS    int64
	DownloadError string
}

// ReadDownload 经已有客户端下载固定长度。长度或状态不对时返回已读字节和原因。
func ReadDownload(ctx context.Context, client *http.Client, raw string) (int64, int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return 0, 0, err
	}
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()
	n, err := io.Copy(io.Discard, io.LimitReader(resp.Body, PayloadSize+1))
	ms := time.Since(start).Milliseconds()
	if resp.StatusCode != http.StatusOK {
		return n, ms, fmt.Errorf("下载返回 HTTP %d", resp.StatusCode)
	}
	if err != nil {
		return n, ms, err
	}
	if n != PayloadSize {
		return n, ms, errors.New("下载没有完成")
	}
	return n, ms, nil
}

// Measure 经临时代理访问回显。回显失败即连通失败；下载失败只写入 DownloadError。
func Measure(ctx context.Context, o MeasureOptions) (MeasureResult, error) {
	if o.Echo == "" || o.Proxy == nil {
		return MeasureResult{}, errors.New("出口缺少可实测的客户端配置")
	}
	name, _ := o.Proxy["name"].(string)
	if name == "" {
		name = "exit-1"
		o.Proxy["name"] = name
	}
	dir, err := os.MkdirTemp("", "weir-exit-probe-")
	if err != nil {
		return MeasureResult{}, err
	}
	defer os.RemoveAll(dir)
	mixed, err := freePort("tcp")
	if err != nil {
		return MeasureResult{}, err
	}
	cfg := map[string]any{
		"allow-lan": false, "bind-address": "127.0.0.1", "mixed-port": mixed,
		"log-level": "warning", "mode": "rule", "proxies": []any{o.Proxy},
		"rules": []string{"MATCH," + name},
	}
	if o.PublicOnly {
		hosts, err := PublicHosts(ctx, []map[string]any{o.Proxy}, o.Echo)
		if err != nil {
			return MeasureResult{}, err
		}
		if o.Download != "" {
			more, err := PublicHosts(ctx, []map[string]any{o.Proxy}, o.Download)
			if err != nil {
				return MeasureResult{}, err
			}
			for host, ips := range more {
				hosts[host] = ips
			}
		}
		cfg["hosts"] = hosts
	}
	body, err := json.Marshal(cfg)
	if err != nil {
		return MeasureResult{}, err
	}
	path := filepath.Join(dir, "client.json")
	if err = os.WriteFile(path, body, 0600); err != nil {
		return MeasureResult{}, err
	}
	checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	logs := &tail{}
	check := exec.CommandContext(checkCtx, o.Binary, args(o.Check, path)...)
	check.Stdout, check.Stderr, check.Dir = logs, logs, dir
	if err = check.Run(); err != nil {
		return MeasureResult{}, fmt.Errorf("mihomo 配置检查失败：%s", logs.String())
	}
	cmd := exec.CommandContext(ctx, o.Binary, args(o.Run, path)...)
	cmd.Stdout, cmd.Stderr, cmd.Dir = logs, logs, dir
	if err = cmd.Start(); err != nil {
		return MeasureResult{}, err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	defer func() { _ = cmd.Process.Kill(); <-done }()
	address := net.JoinHostPort("127.0.0.1", fmt.Sprint(mixed))
	if err = waitTCP(ctx, address, logs); err != nil {
		return MeasureResult{}, err
	}
	proxy, _ := url.Parse("socks5://" + address)
	transport := &http.Transport{Proxy: http.ProxyURL(proxy), DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	echoClient := &http.Client{Timeout: 12 * time.Second, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	ip, latency, err := readEcho(ctx, echoClient, o.Echo)
	if err != nil {
		return MeasureResult{}, fmt.Errorf("chain_probe_failed: %s", Diagnostic(err.Error()))
	}
	result := MeasureResult{LatencyMS: latency, ExitIP: ip}
	if o.Download == "" {
		return result, nil
	}
	downloadClient := &http.Client{Timeout: 20 * time.Second, Transport: transport, CheckRedirect: echoClient.CheckRedirect}
	n, ms, err := ReadDownload(ctx, downloadClient, o.Download)
	result.Bytes, result.DownloadMS = n, ms
	if err != nil {
		result.DownloadError = Diagnostic(err.Error())
	}
	return result, nil
}

func readEcho(ctx context.Context, client *http.Client, raw string) (string, int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return "", 0, err
	}
	start := time.Now()
	resp, err := requestAfterStartup(client, req, nil)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	latency := max(1, time.Since(start).Milliseconds())
	if err != nil {
		return "", latency, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", latency, fmt.Errorf("回显返回 HTTP %d", resp.StatusCode)
	}
	var echo struct {
		IP string `json:"ip"`
	}
	if json.Unmarshal(body, &echo) == nil && net.ParseIP(echo.IP) != nil {
		return echo.IP, latency, nil
	}
	return "", latency, nil
}

func waitTCP(ctx context.Context, address string, logs *tail) error {
	ready := time.NewTimer(5 * time.Second)
	defer ready.Stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		conn, err := (&net.Dialer{Timeout: 100 * time.Millisecond}).DialContext(ctx, "tcp", address)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ready.C:
			return fmt.Errorf("mihomo 未就绪：%s", logs.String())
		case <-tick.C:
		}
	}
}

// mihomo 的 SOCKS 监听先于转发就绪；只在启动窗口内重试提前断开的只读请求。
func requestAfterStartup(client *http.Client, req *http.Request, retry func()) (*http.Response, error) {
	deadline := time.Now().Add(5 * time.Second)
	for {
		resp, err := client.Do(req.Clone(req.Context()))
		if err == nil || time.Now().After(deadline) || !(errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, syscall.ECONNRESET)) {
			return resp, err
		}
		if retry != nil {
			retry()
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-req.Context().Done():
			timer.Stop()
			return nil, req.Context().Err()
		case <-timer.C:
		}
	}
}
