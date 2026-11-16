//go:build e2e

package e2e

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
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
	"sync/atomic"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/gate"
	"github.com/shusfun/weir/internal/kernelconf"
	"github.com/shusfun/weir/internal/kernels"
	"github.com/shusfun/weir/packages/contracts"
)

func TestSnellSSRNative(t *testing.T) {
	fixtures := os.Getenv("WEIR_E2E_PROTOCOL_FIXTURES")
	if fixtures == "" {
		t.Fatal("run deploy/prepare_protocols.py and set WEIR_E2E_PROTOCOL_FIXTURES; see docs/implementation.html#protocol-fixtures")
	}
	for _, protocol := range []string{"snell", "ssr"} {
		t.Run(protocol, func(t *testing.T) {
			dir := t.TempDir()
			secret := make([]byte, 24)
			if _, err := rand.Read(secret); err != nil {
				t.Fatal(err)
			}
			password := hex.EncodeToString(secret)
			port := freePort(t)
			config := filepath.Join(dir, "server.conf")
			var server *exec.Cmd
			proxy := map[string]any{"name": "exit", "type": protocol, "server": "127.0.0.1", "port": port}
			if protocol == "snell" {
				body := fmt.Sprintf("[snell-server]\nlisten = 127.0.0.1:%d\npsk = %s\nipv6 = false\n", port, password)
				if err := os.WriteFile(config, []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
				server = exec.Command(filepath.Join(fixtures, "snell-server"), "-c", config)
				proxy["version"], proxy["psk"], proxy["udp"] = 4, password, true
			} else {
				body, err := json.Marshal(map[string]any{"server": "127.0.0.1", "server_port": port, "password": password, "method": "aes-128-cfb", "protocol": "auth_sha1_v4", "obfs": "tls1.2_ticket_auth", "timeout": 10, "workers": 1, "forbidden_ip": "", "forbidden_port": ""})
				if err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(config, body, 0600); err != nil {
					t.Fatal(err)
				}
				server = exec.Command("python3", filepath.Join(fixtures, "ssr/shadowsocks/server.py"), "-c", config)
				proxy["cipher"], proxy["password"], proxy["protocol"], proxy["obfs"], proxy["obfs-param"] = "aes-128-cfb", password, "auth_sha1_v4", "tls1.2_ticket_auth", "fixture.test"
			}
			// Do not send server output to the report: some servers echo the PSK.
			log, err := os.Create(filepath.Join(dir, "server.log"))
			if err != nil {
				t.Fatal(err)
			}
			defer log.Close()
			t.Cleanup(func() {
				if t.Failed() {
					data, _ := os.ReadFile(log.Name())
					text := strings.ReplaceAll(string(data), password, "[redacted]")
					if len(text) > 8192 {
						text = text[len(text)-8192:]
					}
					t.Logf("native server diagnostic: %s", text)
				}
			})
			server.Stdout, server.Stderr = log, log
			if err := server.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = server.Process.Kill(); _ = server.Wait() }()
			eventually(t, 45*time.Second, "native server listener", func() bool {
				c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
				if err == nil {
					c.Close()
				}
				return err == nil
			})
			var hits atomic.Int64
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				_, _ = io.WriteString(w, "native-exit:"+r.RemoteAddr)
			}))
			defer target.Close()
			paths := gate.Paths{CoresDir: filepath.Join(dir, "cores"), KernelsDir: filepath.Join(dir, "kernels")}
			if err := os.MkdirAll(paths.KernelsDir, 0755); err != nil {
				t.Fatal(err)
			}
			for _, engine := range []string{"xray", "mihomo"} {
				body, err := os.ReadFile(os.Getenv(map[string]string{"xray": "WEIR_E2E_XRAY", "mihomo": "WEIR_E2E_MIHOMO"}[engine]))
				if err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(filepath.Join(paths.KernelsDir, engine+"-fixture"), body, 0755); err != nil {
					t.Fatal(err)
				}
			}
			builtin := kernels.Builtin()
			driver := func(key string) string {
				_, text, err := kernelconf.Resolve(builtin[key], func(k string) (string, bool) { v, ok := builtin[k]; return v, ok })
				if err != nil {
					t.Fatal(err)
				}
				return text
			}
			entry, bridge, stats := freePort(t), freePort(t), freePort(t)
			cfg := contracts.GateConfig{Ports: contracts.PortPlan{XrayStats: stats}, Drivers: map[string]string{"xray": driver("xray-26.3"), "mihomo": driver("mihomo-1")},
				Inbounds:    []contracts.Inbound{{ID: 1, Tag: "entry", Kernel: "xray", Protocol: "socks", Port: entry, Network: "tcp"}},
				Credentials: []contracts.Credential{{ID: 1, InboundID: 1, StatsKey: "test", UserKey: "test", UUID: "entry-user", Password: "entry-password"}},
				Outbounds:   []contracts.Outbound{{ID: 1, Tag: "exit", Protocol: protocol, Address: "127.0.0.1", Port: port, ClientOnly: true, Client: proxy}},
				Routes:      []contracts.Route{{ID: 1, InboundTag: "entry", OutboundTag: "exit"}},
				Bridges:     []contracts.OutboundBridge{{FromKernel: "xray", OutboundTag: "exit", Kernel: "mihomo", Port: bridge, Username: "bridge-user", Password: "bridge-password-at-least-24-characters"}}}
			helper := gate.NewHelper(paths)
			defer helper.Sup.Stop("xray")
			defer helper.Sup.Stop("mihomo")
			defer func() {
				if !t.Failed() {
					return
				}
				// Capture before Stop removes the process log buffers. Credentials
				// may appear in an upstream diagnostic, including rejected ones.
				redact := strings.NewReplacer(password, "[redacted]", "entry-password", "[redacted]", "bridge-password-at-least-24-characters", "[redacted]", "wrong-isolated-password", "[redacted]")
				for _, engine := range []string{"xray", "mihomo"} {
					text := redact.Replace(strings.Join(helper.Sup.Logs(engine), "\n"))
					if len(text) > 8192 {
						text = text[len(text)-8192:]
					}
					t.Logf("native %s diagnostic: %s", engine, text)
				}
			}()
			apply := func() {
				out, err := kernelconf.Build(cfg, kernelconf.Options{})
				if err != nil {
					t.Fatal(err)
				}
				result := helper.Apply(t.Context(), gate.ApplyRequest{Kernels: map[string]string{"xray": "fixture", "mihomo": "fixture"}, Configs: map[string][]byte{"xray": out.Xray, "mihomo": out.Mihomo}, Args: out.Args, HealthPorts: map[string][]int{"xray": {stats}, "mihomo": {bridge}}})
				if !result.OK {
					t.Fatal(result.Error)
				}
			}
			apply()
			proxyURL, _ := url.Parse(fmt.Sprintf("socks5://entry-user:entry-password@127.0.0.1:%d", entry))
			transport := &http.Transport{Proxy: http.ProxyURL(proxyURL), DisableKeepAlives: true}
			defer transport.CloseIdleConnections()
			client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
			fetch := func() (string, error) {
				res, err := client.Get(target.URL)
				if err != nil {
					return "", err
				}
				defer res.Body.Close()
				body, err := io.ReadAll(res.Body)
				return string(body), err
			}
			body, err := fetch()
			if err != nil || len(body) < len("native-exit:") || hits.Load() != 1 {
				t.Fatalf("native transport failed: %v", err)
			}
			echo := udpEcho(t)
			payload := []byte("native-" + protocol + "-udp")
			udpFetch := func() ([]byte, error) {
				data, _, err := socks5UDP(entry, echo.addr, payload, 3*time.Second, "entry-user", "entry-password")
				return data, err
			}
			if data, err := udpFetch(); err != nil || string(data) != string(payload) {
				t.Fatal("native UDP transport failed", err)
			}
			key := "password"
			if protocol == "snell" {
				key = "psk"
			}
			proxy[key] = "wrong-isolated-password"
			apply()
			before := hits.Load()
			if _, err := fetch(); err == nil || hits.Load() != before {
				t.Fatal("invalid credentials passed or leaked to direct")
			}
			udpBefore := echo.count.Load()
			if _, err := udpFetch(); err == nil || echo.count.Load() != udpBefore {
				t.Fatal("invalid UDP credentials passed or leaked to direct")
			}
			proxy[key] = password
			apply()
			if _, err := fetch(); err != nil {
				t.Fatal("recovery failed", err)
			}
			_ = server.Process.Kill()
			before = hits.Load()
			if _, err := fetch(); err == nil || hits.Load() != before {
				t.Fatal("server failure leaked to direct")
			}
			udpBefore = echo.count.Load()
			if _, err := udpFetch(); err == nil || echo.count.Load() != udpBefore {
				t.Fatal("server failure leaked UDP to direct")
			}
			evidence(t, "native-"+protocol, map[string]any{"native_server": true, "tcp_udp_transferred": true, "observed_exit": body, "wrong_credentials_rejected": true, "no_direct_on_stop": true})
		})
	}
}
