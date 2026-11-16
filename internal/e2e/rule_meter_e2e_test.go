//go:build e2e

package e2e

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/gate"
	"github.com/shusfun/weir/internal/kernelconf"
	"github.com/shusfun/weir/internal/kernels"
	"github.com/shusfun/weir/internal/xray"
	"github.com/shusfun/weir/packages/contracts"
)

func TestRuleMeterNativeIndependentDirectOutlets(t *testing.T) {
	dir := t.TempDir()
	p := gate.Paths{CoresDir: filepath.Join(dir, "cores"), KernelsDir: filepath.Join(dir, "kernels")}
	if err := os.MkdirAll(p.KernelsDir, 0755); err != nil {
		t.Fatal(err)
	}
	bin, err := os.ReadFile(os.Getenv("WEIR_E2E_XRAY"))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(p.KernelsDir, "xray-fixture"), bin, 0755); err != nil {
		t.Fatal(err)
	}
	builtin := kernels.Builtin()
	_, driver, err := kernelconf.Resolve(builtin["xray-26.3"], func(k string) (string, bool) { v, ok := builtin[k]; return v, ok })
	if err != nil {
		t.Fatal(err)
	}
	urls := []string{}
	for i, host := range []string{"127.0.0.1", "127.0.0.2"} {
		body := strings.Repeat("x", (i+1)*16384)
		web := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, body) }))
		ln, err := net.Listen("tcp", host+":0")
		if err != nil {
			t.Fatal(err)
		}
		web.Listener = ln
		web.Start()
		t.Cleanup(web.Close)
		urls = append(urls, web.URL)
	}
	port, stats := freePort(t), freePort(t)
	cfg := contracts.GateConfig{Drivers: map[string]string{"xray": driver}, Ports: contracts.PortPlan{XrayStats: stats}, Inbounds: []contracts.Inbound{{ID: 1, Tag: "entry", Kernel: "xray", Protocol: "socks", Port: port, Network: "tcp"}}, Credentials: []contracts.Credential{{ID: 1, InboundID: 1, StatsKey: "c1", UUID: "u", Password: "p"}}, Outbounds: []contracts.Outbound{{ID: 1 << 40, RuleID: 1, Tag: "rule-1", Protocol: "freedom"}, {ID: (1 << 40) + 1, RuleID: 2, Tag: "rule-2", Protocol: "freedom"}}, Routes: []contracts.Route{{ID: 1, OutboundTag: "rule-1", IP: "127.0.0.1/32", Priority: 1}, {ID: 2, OutboundTag: "rule-2", IP: "127.0.0.2/32", Priority: 2}}}
	out, err := kernelconf.Build(cfg, kernelconf.Options{})
	if err != nil {
		t.Fatal(err)
	}
	h := gate.NewHelper(p)
	t.Cleanup(func() { h.Sup.Stop("xray") })
	if res := h.Apply(t.Context(), gate.ApplyRequest{Kernels: map[string]string{"xray": "fixture"}, Configs: map[string][]byte{"xray": out.Xray}, Args: out.Args, StatsPorts: map[string]int{"xray": stats}, HealthPorts: map[string][]int{"xray": {stats}}, Ports: []int{port, stats}}); !res.OK {
		t.Fatal(res.Error)
	}
	proxy, _ := url.Parse(fmt.Sprintf("socks5://u:p@127.0.0.1:%d", port))
	tr := &http.Transport{Proxy: http.ProxyURL(proxy), DisableKeepAlives: true}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, Timeout: 3 * time.Second}
	for _, target := range urls {
		resp, err := client.Get(target)
		if err != nil {
			t.Fatal(err)
		}
		_, err = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	statsClient, err := xray.NewGRPCStatsClient(t.Context(), fmt.Sprintf("127.0.0.1:%d", stats))
	if err != nil {
		t.Fatal(err)
	}
	defer statsClient.Close()
	names := kernelconf.RuleOutboundNames(cfg)["xray"]
	deadline := time.Now().Add(3 * time.Second)
	for {
		rows, err := statsClient.QueryTrafficStats(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		counts := map[int64]int64{}
		for _, r := range rows {
			if r.ScopeType == "outbound" && names[r.ScopeKey] > 0 {
				counts[names[r.ScopeKey]] += r.Downlink
			}
		}
		if counts[1] >= 16384 && counts[1] < 20000 && counts[2] >= 32768 && counts[2] < 37000 {
			evidence(t, "rule-meter-native", map[string]any{"rule_1_down": counts[1], "rule_2_down": counts[2], "shared_direct_exit": true})
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("rules copied shared total or missing native counts: %v", counts)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
