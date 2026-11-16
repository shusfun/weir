//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
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

func TestOutboundBridgesNative(t *testing.T) {
	for _, helper := range []string{"sing-box", "mihomo"} {
		t.Run(helper, func(t *testing.T) {
			dir := t.TempDir()
			p := gate.Paths{CoresDir: filepath.Join(dir, "cores"), KernelsDir: filepath.Join(dir, "kernels")}
			if err := os.MkdirAll(p.KernelsDir, 0755); err != nil {
				t.Fatal(err)
			}
			for _, spec := range []struct{ name, env string }{{"xray", "WEIR_E2E_XRAY"}, {helper, map[string]string{"sing-box": "WEIR_E2E_SINGBOX", "mihomo": "WEIR_E2E_MIHOMO"}[helper]}} {
				body, err := os.ReadFile(os.Getenv(spec.env))
				if err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(filepath.Join(p.KernelsDir, spec.name+"-fixture"), body, 0755); err != nil {
					t.Fatal(err)
				}
			}
			var hits atomic.Int64
			observeDone := make(chan struct{})
			web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/observe" {
					_, _ = io.WriteString(w, strings.Repeat("x", 1024))
					w.(http.Flusher).Flush()
					select {
					case <-r.Context().Done():
					case <-observeDone:
					}
					return
				}
				hits.Add(1)
				_, _ = io.WriteString(w, "via-exit")
			}))
			t.Cleanup(func() { close(observeDone); web.CloseClientConnections(); web.Close() })
			exitPort, exitHits := socksServer(t)
			builtin := kernels.Builtin()
			driver := func(name string) string {
				_, text, err := kernelconf.Resolve(builtin[name], func(key string) (string, bool) { v, ok := builtin[key]; return v, ok })
				if err != nil {
					t.Fatal(err)
				}
				return text
			}
			entryPort, bridgePort := freePort(t), freePort(t)
			cfg := contracts.GateConfig{
				Ports:       contracts.PortPlan{XrayStats: freePort(t), SingboxStats: freePort(t)},
				Drivers:     map[string]string{"xray": driver("xray-26.3"), "sing-box": driver("sing-box-1.12"), "mihomo": driver("mihomo-1")},
				Inbounds:    []contracts.Inbound{{ID: 1, Tag: "entry", Kernel: "xray", Protocol: "socks", Port: entryPort, Network: "tcp"}},
				Credentials: []contracts.Credential{{ID: 1, InboundID: 1, StatsKey: "c1", UserKey: "u1", UUID: "entry-user", Password: "entry-password"}},
				Outbounds:   []contracts.Outbound{{ID: 10, Tag: "exit", Protocol: "socks", Address: "127.0.0.1", Port: exitPort, ClientOnly: helper == "mihomo", Client: map[string]any{"type": "socks5", "server": "127.0.0.1", "port": exitPort, "udp": true}}},
				Routes:      []contracts.Route{{ID: 1, InboundTag: "entry", OutboundTag: "exit"}},
				Bridges:     []contracts.OutboundBridge{{FromKernel: "xray", OutboundTag: "exit", Kernel: helper, Port: bridgePort, Username: "bridge-user", Password: "bridge-password-at-least-24-characters"}},
			}
			observer := contracts.ConnectionObserver{Port: freePort(t), Secret: strings.Repeat("s", 32)}
			cfg.Observers = map[string]contracts.ConnectionObserver{helper: observer}
			request := func(c contracts.GateConfig) gate.ApplyRequest {
				out, err := kernelconf.Build(c, kernelconf.Options{})
				if err != nil {
					t.Fatal(err)
				}
				body := out.Singbox
				if helper == "mihomo" {
					body = out.Mihomo
				}
				return gate.ApplyRequest{Kernels: map[string]string{"xray": "fixture", helper: "fixture"}, Configs: map[string][]byte{"xray": out.Xray, helper: body}, Args: out.Args, StatsPorts: map[string]int{"xray": c.Ports.XrayStats, "sing-box": c.Ports.SingboxStats}, HealthPorts: map[string][]int{"xray": {c.Ports.XrayStats}, helper: {c.Bridges[0].Port, observer.Port}}, Ports: []int{entryPort, observer.Port, c.Bridges[0].Port, c.Ports.XrayStats, c.Ports.SingboxStats}}
			}
			h := gate.NewHelper(p)
			t.Cleanup(func() { h.Sup.Stop("xray"); h.Sup.Stop(helper) })
			req := request(cfg)
			if res := h.Apply(t.Context(), req); !res.OK {
				t.Fatal(res.Error)
			}
			fetch := func(port int, authenticated bool) error {
				proxy, _ := url.Parse(fmt.Sprintf("socks5://127.0.0.1:%d", port))
				if authenticated {
					proxy.User = url.UserPassword("entry-user", "entry-password")
				}
				tr := &http.Transport{Proxy: http.ProxyURL(proxy), DisableKeepAlives: true}
				defer tr.CloseIdleConnections()
				client := &http.Client{Timeout: 3 * time.Second, Transport: tr}
				resp, err := client.Get(web.URL)
				if err != nil {
					return err
				}
				defer resp.Body.Close()
				body, err := io.ReadAll(resp.Body)
				if err == nil && string(body) != "via-exit" {
					return fmt.Errorf("unexpected response")
				}
				return err
			}
			if err := fetch(entryPort, true); err != nil {
				t.Fatalf("entry request failed: %v; %s logs: %v", err, helper, h.Sup.Logs(helper))
			}
			if exitHits.Load() != 1 || hits.Load() != 1 {
				t.Fatal("traffic did not use the requested exit")
			}
			if err := fetch(bridgePort, false); err == nil {
				t.Fatal("auxiliary listener accepted unauthenticated request")
			}
			proxy, _ := url.Parse(fmt.Sprintf("socks5://entry-user:entry-password@127.0.0.1:%d", entryPort))
			tr := &http.Transport{Proxy: http.ProxyURL(proxy)}
			defer tr.CloseIdleConnections()
			response, err := (&http.Client{Transport: tr, Timeout: 5 * time.Second}).Get(web.URL + "/observe")
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			readSnapshot := func(auth bool) (int, map[string]any) {
				req, _ := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d/connections", observer.Port), nil)
				if auth {
					req.Header.Set("Authorization", "Bearer "+observer.Secret)
				}
				res, err := (&http.Client{Timeout: time.Second}).Do(req)
				if err != nil {
					t.Fatal(err)
				}
				defer res.Body.Close()
				var doc map[string]any
				if res.StatusCode == 200 {
					if err = json.NewDecoder(res.Body).Decode(&doc); err != nil {
						t.Fatal(err)
					}
				}
				return res.StatusCode, doc
			}
			if code, _ := readSnapshot(false); code != 401 {
				t.Fatal("native API lacks authentication", code)
			}
			code, snapshot := readSnapshot(true)
			if code != 200 {
				t.Fatal("native observer rejected", code)
			}
			connections, _ := snapshot["connections"].([]any)
			if len(connections) == 0 {
				t.Fatal("active connection missing from native snapshot")
			}
			identified := false
			for _, row := range connections {
				m, _ := row.(map[string]any)["metadata"].(map[string]any)
				if m["inboundName"] == "weir-bridge-1" || helper == "sing-box" && m["type"] == "socks/weir-bridge-1" {
					identified = true
				}
			}
			if !identified {
				t.Fatalf("native inbound metadata unavailable: %v", snapshot)
			}
			response.Body.Close()
			h.Sup.Stop(helper)
			before := hits.Load()
			if err := fetch(entryPort, true); err == nil || hits.Load() != before {
				t.Fatal("auxiliary failure leaked traffic through direct")
			}
			if res := h.Apply(t.Context(), req); !res.OK {
				t.Fatal(res.Error)
			}
			cfg.Bridges[0].Port = freePort(t)
			bad := request(cfg)
			bad.HealthPorts[helper] = []int{freePort(t)}
			if res := h.Apply(t.Context(), bad); res.OK {
				t.Fatal("unhealthy candidate accepted")
			}
			if err := fetch(entryPort, true); err != nil {
				t.Fatal("rollback did not restore path", err)
			}
			evidence(t, "outbound-bridge-"+helper, map[string]any{"authenticated_loopback": true, "exit_connections": exitHits.Load(), "no_direct_on_stop": true, "whole_batch_rollback": true})
		})
	}
}
