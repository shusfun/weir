package nodeprobe

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
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
)

type BatchOptions struct {
	Binary, Target string
	Run, Check     []string
	Proxies        []map[string]any
}

// Batch 一轮共用一个临时 mihomo；每条线路由内核完成协议握手并请求指定地址。
func Batch(ctx context.Context, o BatchOptions) (map[string]Check, error) {
	results := map[string]Check{}
	if len(o.Proxies) == 0 {
		return results, nil
	}
	dir, err := os.MkdirTemp("", "weir-reach-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	port, err := freePort("tcp")
	if err != nil {
		return nil, err
	}
	var secretBytes [24]byte
	if _, err = rand.Read(secretBytes[:]); err != nil {
		return nil, err
	}
	secret := hex.EncodeToString(secretBytes[:])
	address := net.JoinHostPort("127.0.0.1", fmt.Sprint(port))
	cfg := map[string]any{"allow-lan": false, "bind-address": "127.0.0.1", "external-controller": address, "secret": secret,
		"log-level": "warning", "mode": "rule", "proxies": o.Proxies, "rules": []string{"MATCH,REJECT"}}
	body, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "client.json")
	if err = os.WriteFile(path, body, 0600); err != nil {
		return nil, err
	}
	checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	logs := &tail{}
	check := exec.CommandContext(checkCtx, o.Binary, args(o.Check, path)...)
	check.Stdout, check.Stderr, check.Dir = logs, logs, dir
	if err = check.Run(); err != nil {
		return nil, fmt.Errorf("mihomo 配置检查失败：%s", logs.String())
	}
	cmd := exec.CommandContext(ctx, o.Binary, args(o.Run, path)...)
	cmd.Stdout, cmd.Stderr, cmd.Dir = logs, logs, dir
	if err = cmd.Start(); err != nil {
		return nil, err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	defer func() { _ = cmd.Process.Kill(); <-done }()
	ready := time.NewTimer(5 * time.Second)
	defer ready.Stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
wait:
	for {
		conn, err := (&net.Dialer{Timeout: 100 * time.Millisecond}).DialContext(ctx, "tcp", address)
		if err == nil {
			_ = conn.Close()
			break wait
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ready.C:
			return nil, fmt.Errorf("mihomo 未就绪：%s", logs.String())
		case err := <-done:
			done <- err
			return nil, fmt.Errorf("mihomo 已退出：%v；%s", err, logs.String())
		case <-tick.C:
		}
	}
	hc := &http.Client{Timeout: 12 * time.Second, Transport: &http.Transport{Proxy: nil}}
	defer hc.CloseIdleConnections()
	for _, proxy := range o.Proxies {
		name, _ := proxy["name"].(string)
		query := url.Values{"url": {o.Target}, "timeout": {"8000"}}
		req, err := http.NewRequestWithContext(ctx, "GET", "http://"+address+"/proxies/"+url.PathEscape(name)+"/delay?"+query.Encode(), nil)
		if err != nil {
			return results, err
		}
		req.Header.Set("Authorization", "Bearer "+secret)
		resp, err := hc.Do(req)
		if err != nil {
			results[name] = Check{Status: "failed", Why: Diagnostic(err.Error())}
			continue
		}
		b, readErr := io.ReadAll(io.LimitReader(resp.Body, 8192))
		_ = resp.Body.Close()
		var result struct {
			Delay   *int64 `json:"delay"`
			Message string `json:"message"`
		}
		if readErr != nil || json.Unmarshal(b, &result) != nil {
			results[name] = Check{Status: "unknown", Why: "无法读取 mihomo 延迟结果"}
			continue
		}
		if resp.StatusCode == 200 && result.Delay != nil {
			results[name] = Check{Status: "passed", LatencyMS: *result.Delay}
		} else {
			why := result.Message
			if why == "" {
				why = fmt.Sprintf("mihomo 返回 HTTP %d", resp.StatusCode)
			}
			results[name] = Check{Status: "failed", Why: Diagnostic(why)}
		}
	}
	return results, nil
}

// 只分类内核实际报告的阶段；超时不推断防火墙或云安全组。
func Diagnostic(reason string) string {
	s := strings.ToLower(reason)
	stage := "经代理请求失败"
	switch {
	case strings.Contains(s, "handshake"), strings.Contains(s, "x509"), strings.Contains(s, "authentication"), strings.Contains(s, "certificate"):
		stage = "协议握手失败"
	case strings.Contains(s, "connection refused"), strings.Contains(s, "network is unreachable"), strings.Contains(s, "no route to host"), strings.Contains(s, "dial tcp"), strings.Contains(s, "dial udp"):
		stage = "连接节点失败"
	case strings.Contains(s, "timeout"), strings.Contains(s, "deadline"):
		stage = "请求超时"
	}
	return stage + "：" + reason
}
