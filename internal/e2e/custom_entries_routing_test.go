//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/subout"
	"gopkg.in/yaml.v3"
)

// 只替换监听地址与可观测的测试节点，保留生成器产出的规则和分组；用两个出口计数核对真实内核首次命中。
func TestCustomEntriesRealKernelRouting(t *testing.T) {
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "ok") }))
	defer httpServer.Close()
	upstream := strings.TrimPrefix(httpServer.URL, "http://")
	firstPort, first := socksServer(t, upstream)
	secondPort, second := socksServer(t, upstream)
	items := []subout.Item{
		{Link: fmt.Sprintf("socks5://127.0.0.1:%d#First", firstPort), Type: "first"},
		{Link: fmt.Sprintf("socks5://127.0.0.1:%d#Second", secondPort), Type: "second"},
	}
	plan := &subout.Plan{Types: []subout.TypeGroup{{Key: "first", Label: "FirstGroup"}, {Key: "second", Label: "SecondGroup"}}, Final: "direct", Custom: []subout.Rule{
		{ID: "exact", Kind: "domain", Values: []string{"api.example.test"}, Target: "first"},
		{ID: "suffix", Kind: "domain_suffix", Values: []string{"example.test"}, Target: "second"},
		{ID: "keyword", Kind: "domain_keyword", Values: []string{"keyword"}, Target: "first"},
		{ID: "public-ip", Kind: "ip_cidr", Values: []string{"203.0.113.0/24"}, Target: "second"},
		{ID: "private-ip", Kind: "ip_cidr", Values: []string{"127.0.0.0/8"}, Target: "first"},
	}}
	for _, kernel := range []string{"mihomo", "sing-box"} {
		t.Run(kernel, func(t *testing.T) {
			bin := os.Getenv(map[string]string{"mihomo": "WEIR_E2E_MIHOMO", "sing-box": "WEIR_E2E_SINGBOX"}[kernel])
			if bin == "" {
				t.Fatal("real kernel binary required")
			}
			target := map[string]string{"mihomo": "clash", "sing-box": "singbox"}[kernel]
			body, _, err := subout.RenderPlan(items, target, plan)
			if err != nil {
				t.Fatal(err)
			}
			var cfg map[string]any
			if kernel == "mihomo" {
				err = yaml.Unmarshal(body, &cfg)
			} else {
				err = json.Unmarshal(body, &cfg)
			}
			if err != nil {
				t.Fatal(err)
			}
			port := freePort(t)
			dir := t.TempDir()
			args := []string{}
			path := filepath.Join(dir, "config.json")
			if kernel == "mihomo" {
				cfg["mixed-port"], cfg["log-level"] = port, "warning"
				args = []string{"-d", dir, "-f", path}
			} else {
				cfg["log"] = map[string]any{"level": "warn"}
				cfg["inbounds"] = []any{map[string]any{"type": "mixed", "listen": "127.0.0.1", "listen_port": port}}
				args = []string{"run", "-c", path}
			}
			body, err = json.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(path, body, 0600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(bin, args...)
			cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
			if err = cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { cmd.Process.Kill(); cmd.Wait() }()
			eventually(t, 10*time.Second, "client listener", func() bool {
				c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 100*time.Millisecond)
				if err == nil {
					c.Close()
				}
				return err == nil
			})
			for _, host := range []string{"api.example.test", "www.example.test", "keyword.example.org", "203.0.113.7", "127.0.0.1"} {
				m := plan.Match(host, []string{"First", "Second"}, items)
				a, b := first.Load(), second.Load()
				url := "http://" + host + "/"
				if host == "127.0.0.1" {
					url = httpServer.URL
				}
				if err := fetchVia(port, url, 2); err != nil {
					t.Fatalf("%s (%+v): %v", host, m, err)
				}
				gotA, gotB := first.Load()-a, second.Load()-b
				if gotA != boolCount(m.Policy == "FirstGroup") || gotB != boolCount(m.Policy == "SecondGroup") {
					t.Fatalf("%s matched %+v but packets first=%d second=%d", host, m, gotA, gotB)
				}
				t.Logf("%s rule=%s policy=%s packets first=%d second=%d", host, m.EntryID, m.Policy, gotA, gotB)
			}
		})
	}
}

func boolCount(b bool) int64 {
	if b {
		return 1
	}
	return 0
}
