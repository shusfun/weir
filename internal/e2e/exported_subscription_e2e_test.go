//go:build e2e

package e2e

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
	"gopkg.in/yaml.v3"
)

// TestExportedSubscriptionRealClients consumes explicitly supplied private exports.
// Raw validation precedes isolated per-node runtime checks; these do not prove selector routing.
func TestExportedSubscriptionRealClients(t *testing.T) {
	dir := os.Getenv("WEIR_E2E_SUBSCRIPTION_DIR")
	if dir == "" {
		t.Skip("no exported subscriptions supplied")
	}
	target := os.Getenv("WEIR_E2E_PROBE_URL")
	parsed, err := url.Parse(target)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil {
		t.Fatal("an explicit HTTPS probe URL is required")
	}
	runID := time.Now().UTC().Format("20060102T150405Z")
	t.Logf("run=%s stage=begin", runID)
	defer func() { t.Logf("run=%s stage=complete failed=%t", runID, t.Failed()) }()
	var sb struct {
		Outbounds []map[string]any `json:"outbounds"`
	}
	raw := mustRead(t, filepath.Join(dir, "singbox-explicit.txt"))
	if json.Unmarshal(raw, &sb) != nil {
		t.Fatal("invalid sing-box export")
	}
	var provider struct {
		Proxies []map[string]any `yaml:"proxies"`
	}
	if yaml.Unmarshal(mustRead(t, filepath.Join(dir, "clash-nodes.yaml")), &provider) != nil {
		t.Fatal("invalid Clash provider export")
	}
	for _, core := range []string{"mihomo", "sing-box"} {
		t.Run(core, func(t *testing.T) {
			bin := os.Getenv("WEIR_E2E_MIHOMO")
			name := "clash-explicit.txt"
			args := []string{"-t", "-d", dir, "-f", filepath.Join(dir, name)}
			nodes := provider.Proxies
			if core == "sing-box" {
				bin = os.Getenv("WEIR_E2E_SINGBOX")
				name = "singbox-explicit.txt"
				args = []string{"check", "-c", filepath.Join(dir, name)}
				nodes = sb.Outbounds
			}
			data := mustRead(t, filepath.Join(dir, name))
			output, checkErr := exec.Command(bin, args...).CombinedOutput()
			if err := os.WriteFile(filepath.Join(dir, core+"-raw-check.log"), output, 0600); err != nil {
				t.Fatal(err)
			}
			t.Logf("run=%s raw_sha256=%x validation_ok=%t", runID, sha256.Sum256(data), checkErr == nil)
			if checkErr != nil {
				t.Fatal("original configuration rejected; inspect private native log")
			}
			for i, node := range nodes {
				if node["server"] == nil {
					continue
				}
				t.Run(fmt.Sprintf("node-%d", i), func(t *testing.T) {
					port := freePort(t)
					cfg := map[string]any{}
					if core == "sing-box" {
						cfg = map[string]any{"log": map[string]any{"level": "warn"}, "inbounds": []any{map[string]any{"type": "mixed", "listen": "127.0.0.1", "listen_port": port}}, "outbounds": []any{node}, "route": map[string]any{"final": node["tag"]}}
					} else {
						cfg = map[string]any{"mixed-port": port, "allow-lan": false, "bind-address": "127.0.0.1", "mode": "rule", "log-level": "warning", "proxies": []any{node}, "rules": []string{"MATCH," + node["name"].(string)}}
					}
					path := filepath.Join(t.TempDir(), "runtime.json")
					b, _ := json.Marshal(cfg)
					if err := os.WriteFile(path, b, 0600); err != nil {
						t.Fatal(err)
					}
					args := []string{"-d", dir, "-f", path}
					if core == "sing-box" {
						args = []string{"run", "-c", path}
					}
					log, err := os.OpenFile(filepath.Join(dir, fmt.Sprintf("%s-node-%d.log", core, i)), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
					if err != nil {
						t.Fatal(err)
					}
					defer log.Close()
					cmd := exec.Command(bin, args...)
					cmd.Stdout, cmd.Stderr = log, log
					if err := cmd.Start(); err != nil {
						t.Fatal(err)
					}
					defer func() { cmd.Process.Kill(); cmd.Wait() }()
					eventually(t, 5*time.Second, "client loopback listener", func() bool {
						c, e := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 100*time.Millisecond)
						if e != nil {
							return false
						}
						c.Close()
						return true
					})
					proxyURL, _ := url.Parse(fmt.Sprintf("socks5://127.0.0.1:%d", port))
					transport := &http.Transport{Proxy: http.ProxyURL(proxyURL), DisableKeepAlives: true}
					defer transport.CloseIdleConnections()
					client := &http.Client{Transport: transport, Timeout: 15 * time.Second}
					start := time.Now()
					response, err := client.Get(target)
					if err != nil {
						t.Errorf("run=%s TCP failed: %v", runID, err)
						return
					}
					body, err := io.ReadAll(io.LimitReader(response.Body, 4096))
					response.Body.Close()
					var address struct {
						IP string `json:"ip"`
					}
					_ = json.Unmarshal(body, &address)
					t.Logf("run=%s protocol=%v TCP_status=%d elapsed_ms=%d egress=%s", runID, node["type"], response.StatusCode, time.Since(start).Milliseconds(), address.IP)
					if err != nil || response.StatusCode != 200 {
						t.Error("probe HTTP response failed")
					}
					query := dnsmessage.Message{Header: dnsmessage.Header{ID: uint16(port), RecursionDesired: true}, Questions: []dnsmessage.Question{{Name: dnsmessage.MustNewName("example.com."), Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}}}
					packet, _ := query.Pack()
					reply, elapsed, err := socks5UDP(port, "1.1.1.1:53", packet, 5*time.Second)
					if err != nil {
						t.Logf("run=%s UDP unavailable elapsed_ms=%d; see node capability", runID, elapsed.Milliseconds())
						return
					}
					answer := new(dnsmessage.Message)
					if answer.Unpack(reply) != nil || answer.ID != query.ID || answer.RCode != dnsmessage.RCodeSuccess {
						t.Error("UDP DNS response invalid")
					} else {
						t.Logf("run=%s UDP passed answers=%d", runID, len(answer.Answers))
					}
				})
			}
		})
	}
}
