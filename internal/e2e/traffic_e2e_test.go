//go:build e2e

package e2e

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"gopkg.in/yaml.v3"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/acme/acmetest"
	"github.com/shusfun/weir/internal/center"
	"github.com/shusfun/weir/internal/kernels"
	entgate "github.com/shusfun/weir/internal/store/ent/gate"
	"github.com/shusfun/weir/packages/contracts"
)

func TestDNSResolutionRealKernels(t *testing.T) {
	e := setup(t)
	sb := e.seedSingbox()
	facts := e.kernelFacts(sb)
	evidence(t, "dns-baseline", facts)
	lab := startDNSLab(t)
	e.startGate("dns")
	e.s.Store.Ent.Gate.UpdateOneID(e.gateID).SetPublicHost("127.0.0.1").ExecX(e.ctx)
	dest := localTLS(t)
	_, destPort, _ := net.SplitHostPort(dest)
	xnode := e.post("/api/v1/admin/nodes", map[string]any{"gate_id": e.gateID, "preset": "vless-reality", "name": "dns-x", "skip_probe": true, "settings": map[string]any{"reality_dest": lab.reality + ":" + destPort, "reality_server_names": "e2e.test"}}, nil)
	snode := e.post("/api/v1/admin/nodes", map[string]any{"gate_id": e.gateID, "preset": "ss2022", "name": "dns-s", "skip_probe": true}, nil)
	user := e.quotaUser("dns@e2e.test")
	uid := int(user["id"].(float64))
	e.appliedDump("dns nodes")
	e.requireCredential(uid, int(xnode["id"].(float64)), int(snode["id"].(float64)))
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("dns-ok")) }))
	t.Cleanup(web.Close)
	_, webPort, _ := net.SplitHostPort(web.Listener.Addr().String())
	lab.answer = net.ParseIP("127.0.0.1")

	t.Run("xray-direct-prefers-doh", func(t *testing.T) {
		name := lab.unique("xdirect")
		before := lab.snapshot()
		port := e.runSingboxClient(sb, e.subOutbound(uid, int(xnode["id"].(float64))))
		err := proxyGET(port, name, webPort, len("dns-ok"))
		got := lab.since(before)
		evidence(t, "xray-doh", map[string]any{"name": name, "err": errString(err), "queries": got, "facts": facts})
		if err != nil {
			t.Fatal(err)
		}
		if saw(got, name, "system") || !saw(got, name, "doh") || saw(got, name, "udp") {
			cfg, _ := os.ReadFile(filepath.Join(e.paths.CoresDir, "xray.json"))
			t.Fatalf("xray direct query path = %v accepts=%v config=%s", got, lab.accepts(), dnsSnippet(cfg))
		}
	})
	t.Run("singbox-direct-prefers-doh", func(t *testing.T) {
		name := lab.unique("sdirect")
		before := lab.snapshot()
		port := e.runSingboxClient(sb, e.subOutbound(uid, int(snode["id"].(float64))))
		err := proxyGET(port, name, webPort, len("dns-ok"))
		got := lab.since(before)
		evidence(t, "singbox-doh", map[string]any{"name": name, "err": errString(err), "queries": got, "version": e.singboxVersion})
		if err != nil {
			t.Fatal(err)
		}
		if saw(got, name, "system") || !saw(got, name, "doh") || saw(got, name, "udp") {
			t.Fatalf("sing-box direct query path = %v", got)
		}
	})
	t.Run("reality-uses-system-dns", func(t *testing.T) {
		got := lab.matching(lab.reality)
		evidence(t, "reality-system-dns", map[string]any{"name": lab.reality, "queries": got})
		if !saw(got, lab.reality, "system") {
			t.Fatalf("REALITY dest was not resolved by system DNS: %v", got)
		}
		if saw(got, lab.reality, "doh") || saw(got, lab.reality, "udp") {
			t.Fatalf("REALITY dest used kernel DNS: %v", got)
		}
	})
	t.Run("doh-failure-falls-back", func(t *testing.T) {
		lab.dohFail.Store(true)
		t.Cleanup(func() { lab.dohFail.Store(false) })
		name := lab.unique("fallback")
		before := lab.snapshot()
		port := e.runSingboxClient(sb, e.subOutbound(uid, int(snode["id"].(float64))))
		err := proxyGET(port, name, webPort, len("dns-ok"))
		got := lab.since(before)
		evidence(t, "doh-fallback", map[string]any{"name": name, "err": errString(err), "queries": got, "singbox": e.singboxVersion, "note": "sing-box 1.12 配置没有 evaluate 回退；本夹具二进制是 1.14 区间，1.12 真实流量未覆盖"})
		if err != nil {
			t.Fatal(err)
		}
		if !saw(got, name, "udp") {
			t.Fatalf("DoH failure did not reach plain DNS: %v", got)
		}
	})
	t.Run("all-resolvers-fail", func(t *testing.T) {
		lab.dohFail.Store(true)
		lab.udpFail.Store(true)
		t.Cleanup(func() { lab.dohFail.Store(false); lab.udpFail.Store(false) })
		name := lab.unique("fail")
		before := lab.snapshot()
		port := e.runSingboxClient(sb, e.subOutbound(uid, int(snode["id"].(float64))))
		start := time.Now()
		err := proxyGET(port, name, webPort, len("dns-ok"))
		elapsed := time.Since(start)
		got := lab.since(before)
		evidence(t, "dns-all-fail", map[string]any{"name": name, "err": errString(err), "elapsed_ms": elapsed.Milliseconds(), "queries": got})
		if err == nil || strings.Contains(err.Error(), "connection refused") {
			t.Fatalf("resolver failure was not observed through a live client: %v queries=%v", err, got)
		}
		if !saw(got, name, "doh") && !saw(got, name, "udp") && !saw(got, name, "doh-tcp") {
			t.Fatalf("failure did not reach a test resolver: %v queries=%v", err, got)
		}
		if elapsed > 12*time.Second {
			t.Fatalf("failure was not exposed within 12s: %s after %s", err, elapsed)
		}
	})
	evidence(t, "dns-version-capability", map[string]any{
		"xray": facts["xray"], "singbox": facts["singbox"],
		"singbox_1_12_config": "final stays on DoH; no evaluate fallback. No 1.12 binary in this fixture, real 1.12 traffic uncovered",
		"singbox_1_14_config": "evaluate falls through to UDP DNS",
	})
}

func TestUDPExitRealKernels(t *testing.T) {
	e := setup(t)
	sb := e.seedSingbox()
	evidence(t, "udp-baseline", e.kernelFacts(sb))
	probe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	t.Cleanup(probe.Close)
	oldURL, oldEvery := contracts.GroupProbeURL, contracts.GroupProbeEvery
	contracts.GroupProbeURL, contracts.GroupProbeEvery = probe.URL, "2s"
	t.Cleanup(func() { contracts.GroupProbeURL, contracts.GroupProbeEvery = oldURL, oldEvery })
	caFile, originPort, _ := e.httpsCenter("udp.e2e.test")
	installTrust(t, mustRead(t, caFile))
	origin := fmt.Sprintf("https://udp.e2e.test:%d", originPort)
	e.s.SetOriginForTest(origin)
	if conn, err := tls.Dial("tcp", fmt.Sprintf("udp.e2e.test:%d", originPort), nil); err != nil {
		t.Fatalf("probe origin is not trusted by this process (SSL_CERT_FILE=%s): %v", os.Getenv("SSL_CERT_FILE"), err)
	} else {
		conn.Close()
	}
	// 内核 DoH 不读 /etc/hosts。探测目标 udp.e2e.test 必须由内核实际使用的解析器回答，否则链式探测和节点测试会在出口解析处断开。
	lab := startDNSLab(t)
	lab.alias("udp.e2e.test", net.ParseIP("127.0.0.1"))
	e.startGate("udp")
	e.s.Store.Ent.Gate.UpdateOneID(e.gateID).SetPublicHost("127.0.0.1").ExecX(e.ctx)
	echo := udpEcho(t)
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("tcp-ok")) }))
	t.Cleanup(web.Close)
	user := e.quotaUser("udp@e2e.test")
	uid := int(user["id"].(float64))
	udpSocks := udpSocksServer(t, true)
	tcpSocks := udpSocksServer(t, false)
	httpProxy := httpProxyServer(t)

	dest := localTLS(t)
	reality := map[string]any{"reality_dest": dest, "reality_server_names": "e2e.test"}
	direct := e.post("/api/v1/admin/nodes", map[string]any{"gate_id": e.gateID, "preset": "ss2022", "name": "udp-direct", "skip_probe": true}, nil)
	httpNode := e.post("/api/v1/admin/nodes", map[string]any{"gate_id": e.gateID, "preset": "vless-reality", "name": "udp-http", "skip_probe": true, "settings": reality, "exit": map[string]any{"kind": "http", "address": "127.0.0.1", "port": httpProxy.port}}, nil)
	if httpNode["id"] == nil || direct["id"] == nil {
		t.Fatalf("node create missing id direct=%v http=%v", direct, httpNode)
	}
	e.applied("udp base nodes")
	e.requireCredential(uid, int(direct["id"].(float64)), int(httpNode["id"].(float64)))

	t.Run("direct", func(t *testing.T) {
		bindSub(e, t)
		client := e.runSingboxClient(sb, e.subOutbound(uid, int(direct["id"].(float64))))
		waitListen(t, client)
		if err := fetchVia(client, web.URL, len("tcp-ok")); err != nil {
			t.Fatal(err)
		}
		nonce := []byte("udp-direct-" + strconv.FormatInt(time.Now().UnixNano(), 10))
		before := echo.count.Load()
		reply, elapsed, err := socks5UDP(client, echo.addr, nonce, 3*time.Second)
		evidence(t, "udp-direct", map[string]any{"elapsed_ms": elapsed.Milliseconds(), "err": errString(err), "echo_delta": echo.count.Load() - before, "matched": bytes.Equal(reply, nonce)})
		if err != nil || !bytes.Equal(reply, nonce) || echo.count.Load() <= before {
			t.Fatalf("direct UDP failed: %v reply=%q echo %d→%d", err, reply, before, echo.count.Load())
		}
	})
	t.Run("http-rejects", func(t *testing.T) {
		bindSub(e, t)
		var logs bytes.Buffer
		client := e.runSingboxClientDebug(sb, e.subOutbound(uid, int(httpNode["id"].(float64))), &logs)
		waitListen(t, client)
		if err := fetchVia(client, web.URL, len("tcp-ok")); err != nil {
			t.Fatalf("TCP should still work: %v", err)
		}
		nonce := []byte("udp-http-" + strconv.FormatInt(time.Now().UnixNano(), 10))
		before, hops := echo.count.Load(), httpProxy.hits.Load()
		elapsed, err := waitUDPReject(client, echo.addr, nonce, &logs)
		evidence(t, "udp-http-reject", map[string]any{"elapsed_ms": elapsed.Milliseconds(), "err": errString(err), "echo_delta": echo.count.Load() - before, "proxy_delta": httpProxy.hits.Load() - hops})
		if !udpRejectVisible(err, elapsed) || echo.count.Load() != before {
			t.Fatalf("HTTP exit did not reject UDP visibly: err=%v elapsed=%s echo %d→%d", err, elapsed, before, echo.count.Load())
		}
		body, _ := e.get("/sub/"+e.userToken(uid)+"/provider/single", map[string]string{"User-Agent": "clash.meta"})
		var provider struct {
			Proxies []struct {
				Port int   `yaml:"port"`
				UDP  *bool `yaml:"udp"`
			} `yaml:"proxies"`
		}
		if err := yaml.Unmarshal(body, &provider); err != nil {
			t.Fatal(err)
		}
		found := false
		for _, proxy := range provider.Proxies {
			if proxy.Port == int(httpNode["port"].(float64)) {
				found = true
				if proxy.UDP == nil || *proxy.UDP {
					t.Fatal("HTTP exit capability lost in provider")
				}
			}
		}
		if !found {
			t.Fatal("HTTP node missing from provider")
		}

	})
	t.Run("socks-measured", func(t *testing.T) {
		bindSub(e, t)
		good := e.post("/api/v1/admin/exits", map[string]any{"gate_id": e.gateID, "kind": "socks5", "address": "127.0.0.1", "port": udpSocks.port, "remark": "udp-yes"}, nil)
		bad := e.post("/api/v1/admin/exits", map[string]any{"gate_id": e.gateID, "kind": "socks5", "address": "127.0.0.1", "port": tcpSocks.port, "remark": "udp-no"}, nil)
		e.applied("socks probes")
		if udpOf(good) != "supported" || udpOf(bad) != "unsupported" {
			t.Fatalf("measured socks udp = %v / %v", udpOf(good), udpOf(bad))
		}
		yes := e.post("/api/v1/admin/nodes", map[string]any{"gate_id": e.gateID, "preset": "ss2022", "name": "udp-socks", "skip_probe": true, "exit_id": int(good["id"].(float64))}, nil)
		no := e.post("/api/v1/admin/nodes", map[string]any{"gate_id": e.gateID, "preset": "vless-reality", "name": "udp-socks-no", "skip_probe": true, "settings": reality, "exit_id": int(bad["id"].(float64))}, nil)
		e.applied("socks nodes")
		yesClient := e.runSingboxClient(sb, e.subOutbound(uid, int(yes["id"].(float64))))
		waitListen(t, yesClient)
		nonce := []byte("udp-socks-" + strconv.FormatInt(time.Now().UnixNano(), 10))
		before, relay := echo.count.Load(), udpSocks.udp.Load()
		reply, elapsed, err := socks5UDP(yesClient, echo.addr, nonce, 3*time.Second)
		evidence(t, "udp-socks-yes", map[string]any{"elapsed_ms": elapsed.Milliseconds(), "err": errString(err), "echo_delta": echo.count.Load() - before, "relay_delta": udpSocks.udp.Load() - relay})
		if err != nil || !bytes.Equal(reply, nonce) || udpSocks.udp.Load() <= relay || echo.count.Load() <= before {
			t.Fatalf("SOCKS UDP path missed the relay or echo: %v relay %d→%d echo %d→%d", err, relay, udpSocks.udp.Load(), before, echo.count.Load())
		}
		var noLogs bytes.Buffer
		noClient := e.runSingboxClientDebug(sb, e.subOutbound(uid, int(no["id"].(float64))), &noLogs)
		waitListen(t, noClient)
		if err := fetchVia(noClient, web.URL, len("tcp-ok")); err != nil {
			t.Fatalf("TCP via UDP-incapable SOCKS: %v", err)
		}
		before = echo.count.Load()
		elapsed, err = waitUDPReject(noClient, echo.addr, []byte("nope"), &noLogs)
		evidence(t, "udp-socks-no", map[string]any{"elapsed_ms": elapsed.Milliseconds(), "err": errString(err), "echo_delta": echo.count.Load() - before})
		if !udpRejectVisible(err, elapsed) || echo.count.Load() != before {
			t.Fatalf("incapable SOCKS did not reject UDP quickly: err=%v elapsed=%s echo=%d", err, elapsed, echo.count.Load())
		}
	})
	t.Run("chain-and-group", func(t *testing.T) {
		bindSub(e, t)
		entry := int(direct["gate_id"].(float64))
		probedYes := e.post("/api/v1/admin/exits", map[string]any{"gate_id": entry, "kind": "socks5", "address": "127.0.0.1", "port": udpSocks.port, "remark": "group-yes"}, nil)
		probedNo := e.post("/api/v1/admin/exits", map[string]any{"gate_id": entry, "kind": "socks5", "address": "127.0.0.1", "port": tcpSocks.port, "remark": "group-no"}, nil)
		e.applied("group member probes")
		if udpOf(probedYes) != "supported" || udpOf(probedNo) != "unsupported" {
			t.Fatalf("group member probes = %v / %v", udpOf(probedYes), udpOf(probedNo))
		}
		e.startGate("udp-hop")
		hop := e.gateID
		e.s.Store.Ent.Gate.UpdateOneID(hop).SetPublicHost("127.0.0.1").ExecX(e.ctx)
		dest := localTLS(t)
		reality := map[string]any{"reality_dest": dest, "reality_server_names": "e2e.test"}
		landing := e.post("/api/v1/admin/nodes", map[string]any{"gate_id": hop, "preset": "vless-reality", "name": "landing", "skip_probe": true, "settings": reality}, nil)
		blocked := e.post("/api/v1/admin/nodes", map[string]any{"gate_id": hop, "preset": "vless-reality", "name": "blocked-landing", "skip_probe": true, "settings": reality, "exit": map[string]any{"kind": "http", "address": "127.0.0.1", "port": httpProxy.port}}, nil)
		e.appliedGate(hop, "landing nodes")
		e.gateID = entry
		chainOK := e.post("/api/v1/admin/nodes", map[string]any{"gate_id": entry, "preset": "ss2022", "name": "chain-ok", "skip_probe": true, "exit": map[string]any{"kind": "node", "node_id": landing["id"]}}, nil)
		chainNo := e.post("/api/v1/admin/nodes", map[string]any{"gate_id": entry, "preset": "ss2022", "name": "chain-no", "skip_probe": true, "exit": map[string]any{"kind": "node", "node_id": blocked["id"]}}, nil)
		e.s.AllowPrivateSources = true
		src := e.post("/api/v1/admin/subscriptions/sources", map[string]any{"name": "udp-pool", "url": serveLinks(t, []string{
			fmt.Sprintf("socks5://127.0.0.1:%d#UDP-YES", udpSocks.port),
			fmt.Sprintf("socks5://127.0.0.1:%d#UDP-NO", tcpSocks.port),
		})}, nil)
		sid := int(src["id"].(float64))
		e.post(fmt.Sprintf("/api/v1/admin/subscriptions/sources/%d/refresh", sid), map[string]any{}, nil)
		partial := e.post("/api/v1/admin/nodes", map[string]any{"gate_id": e.gateID, "preset": "ss2022", "name": "group-partial", "skip_probe": true, "exit": map[string]any{"kind": "source", "source_id": sid, "mode": "filter", "match": "^UDP"}}, nil)
		noneSrc := e.post("/api/v1/admin/subscriptions/sources", map[string]any{"name": "http-pool", "url": serveLinks(t, []string{
			fmt.Sprintf("http://127.0.0.1:%d#HTTP-A", httpProxy.port),
			fmt.Sprintf("http://127.0.0.1:%d#HTTP-B", httpProxy.port),
		})}, nil)
		noneID := int(noneSrc["id"].(float64))
		e.post(fmt.Sprintf("/api/v1/admin/subscriptions/sources/%d/refresh", noneID), map[string]any{}, nil)
		none := e.post("/api/v1/admin/nodes", map[string]any{"gate_id": e.gateID, "preset": "ss2022", "name": "group-none", "skip_probe": true, "exit": map[string]any{"kind": "source", "source_id": noneID, "mode": "filter", "match": "^HTTP"}}, nil)
		e.applied("chain and groups")
		okClient := e.runSingboxClient(sb, e.subOutbound(uid, int(chainOK["id"].(float64))))
		waitListen(t, okClient)
		nonce := []byte("chain-" + strconv.FormatInt(time.Now().UnixNano(), 10))
		before := echo.count.Load()
		reply, elapsed, err := socks5UDP(okClient, echo.addr, nonce, 5*time.Second)
		evidence(t, "udp-chain-ok", map[string]any{"elapsed_ms": elapsed.Milliseconds(), "err": errString(err), "echo_delta": echo.count.Load() - before})
		if err != nil || !bytes.Equal(reply, nonce) || echo.count.Load() <= before {
			t.Fatalf("chain to a UDP-capable node failed: %v", err)
		}
		var noLogs bytes.Buffer
		noClient := e.runSingboxClientDebug(sb, e.subOutbound(uid, int(chainNo["id"].(float64))), &noLogs)
		waitListen(t, noClient)
		before = echo.count.Load()
		elapsed, err = waitUDPReject(noClient, echo.addr, []byte("chain-no"), &noLogs)
		evidence(t, "udp-chain-no", map[string]any{"elapsed_ms": elapsed.Milliseconds(), "err": errString(err), "echo_delta": echo.count.Load() - before})
		if !udpRejectVisible(err, elapsed) || echo.count.Load() != before {
			t.Fatalf("chain to UDP-incapable node did not reject: err=%v elapsed=%s", err, elapsed)
		}
		partClient := e.runSingboxClient(sb, e.subOutbound(uid, int(partial["id"].(float64))))
		waitListen(t, partClient)
		if err := fetchVia(partClient, web.URL, len("tcp-ok")); err != nil {
			t.Fatalf("TCP via partial group: %v", err)
		}
		before, relay := echo.count.Load(), udpSocks.udp.Load()
		dead := tcpSocks.udp.Load()
		reply, elapsed, err = socks5UDP(partClient, echo.addr, []byte("group-yes"), 3*time.Second)
		evidence(t, "udp-group-partial", map[string]any{"elapsed_ms": elapsed.Milliseconds(), "err": errString(err), "yes_delta": udpSocks.udp.Load() - relay, "no_delta": tcpSocks.udp.Load() - dead, "echo_delta": echo.count.Load() - before})
		if err != nil || !bytes.Equal(reply, []byte("group-yes")) || udpSocks.udp.Load() <= relay || tcpSocks.udp.Load() != dead {
			t.Fatalf("group UDP did not stay on the capable member: %v yes %d no %d", err, udpSocks.udp.Load()-relay, tcpSocks.udp.Load()-dead)
		}
		var noneLogs bytes.Buffer
		noneClient := e.runSingboxClientDebug(sb, e.subOutbound(uid, int(none["id"].(float64))), &noneLogs)
		waitListen(t, noneClient)
		if err := fetchVia(noneClient, web.URL, len("tcp-ok")); err != nil {
			t.Fatalf("TCP via UDP-incapable group: %v", err)
		}
		before = echo.count.Load()
		elapsed, err = waitUDPReject(noneClient, echo.addr, []byte("group-no"), &noneLogs)
		evidence(t, "udp-group-none", map[string]any{"elapsed_ms": elapsed.Milliseconds(), "err": errString(err), "echo_delta": echo.count.Load() - before})
		if !udpRejectVisible(err, elapsed) || echo.count.Load() != before {
			t.Fatalf("all-incapable group did not reject UDP: err=%v elapsed=%s", err, elapsed)
		}
	})
	t.Run("node-test-unsupported", func(t *testing.T) {
		bindSub(e, t)
		_, stop := e.pairHTTPS("udp-node-test", origin, caFile)
		t.Cleanup(stop)
		eventually(t, 15*time.Second, "non-loopback gate", func() bool {
			g, err := e.s.Store.Ent.Gate.Query().Where(entgate.NameEQ("udp-node-test")).Only(e.ctx)
			return err == nil && g.LastSeenAt != nil && g.CenterURL == origin
		})
		g, err := e.s.Store.Ent.Gate.Query().Where(entgate.NameEQ("udp-node-test")).Only(e.ctx)
		if err != nil {
			t.Fatal(err)
		}
		e.s.Store.Ent.Gate.UpdateOneID(g.ID).SetPublicHost("127.0.0.1").ExecX(e.ctx)
		node := e.post("/api/v1/admin/nodes", map[string]any{"gate_id": g.ID, "preset": "ss2022", "name": "node-test-http", "skip_probe": true, "exit": map[string]any{"kind": "http", "address": "127.0.0.1", "port": httpProxy.port}}, nil)
		e.gateID = g.ID
		e.appliedDump("node-test http exit")
		raw, _ := e.get(fmt.Sprintf("/api/v1/admin/nodes/%d", int(node["id"].(float64))), nil)
		evidence(t, "node-test-record", map[string]any{"node": json.RawMessage(raw)})
		result := e.post(fmt.Sprintf("/api/v1/admin/nodes/%d/test", int(node["id"].(float64))), map[string]any{}, nil)
		testResult, _ := result["test"].(map[string]any)
		udp, _ := testResult["udp"].(map[string]any)
		evidence(t, "node-test-unsupported", map[string]any{"status": testResult["status"], "tcp": testResult["tcp"], "udp": udp, "center": origin, "gate_id": g.ID})
		if udp["status"] != "not_applicable" || udp["why"] != "出口不支持" || testResult["status"] != "passed" {
			t.Fatalf("node test should pass with UDP 出口不支持: %v", testResult)
		}
	})
}

func TestCertRenewalRealKernels(t *testing.T) {
	e := setup(t)
	sb := e.seedSingbox()
	evidence(t, "cert-baseline", e.kernelFacts(sb))
	ca1, ca2 := acmetest.New(t), acmetest.New(t)
	restore := center.SetACMEDirectoryForTest(func(center.NodeCertSettings) string { return ca1.Directory() })
	t.Cleanup(restore)
	ca1.Fetch = func(identifier, token string) (string, error) { return http01(identifier, token) }
	ca2.Fetch = ca1.Fetch
	e.s.Store.Ent.Setting.Create().SetKey("node-certs").SetValue(map[string]any{"email": "certs@e2e.test"}).SaveX(e.ctx)
	e.startGate("cert")
	e.s.Store.Ent.Gate.UpdateOneID(e.gateID).SetPublicHost("127.0.0.1").ExecX(e.ctx)
	eventually(t, 15*time.Second, "port report", func() bool {
		g, err := e.s.Store.Ent.Gate.Get(e.ctx, e.gateID)
		return err == nil && g.ListeningPorts != nil
	})
	user := e.quotaUser("cert@e2e.test")
	uid := int(user["id"].(float64))
	trojan := e.post("/api/v1/admin/nodes", map[string]any{"gate_id": e.gateID, "preset": "trojan-tls", "name": "cert-x", "skip_probe": true}, nil)
	hy2 := e.post("/api/v1/admin/nodes", map[string]any{"gate_id": e.gateID, "preset": "hysteria2", "name": "cert-h", "skip_probe": true}, nil)
	e.appliedDump("tls nodes")
	e.requireCredential(uid, int(trojan["id"].(float64)), int(hy2["id"].(float64)))
	certID := int(trojan["cert_id"].(float64))
	if int(hy2["cert_id"].(float64)) != certID {
		t.Fatalf("hysteria2 did not prefer the trusted cert: %v vs %v", hy2["cert_id"], certID)
	}
	link := e.shareLink(uid, int(hy2["id"].(float64)))
	if strings.Contains(link, "pinSHA256") || strings.Contains(link, "insecure=1") {
		t.Fatalf("trusted cert subscription pinned or skipped verification: %s", redactLink(link))
	}
	installTrust(t, ca1)
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("cert-ok")) }))
	t.Cleanup(web.Close)
	xport := e.runSingboxClient(sb, e.subOutbound(uid, int(trojan["id"].(float64))))
	hport := e.runSingboxClient(sb, e.subOutbound(uid, int(hy2["id"].(float64))))
	eventually(t, 20*time.Second, "initial tls transfer", func() bool {
		return fetchVia(xport, web.URL, len("cert-ok")) == nil && fetchVia(hport, web.URL, len("cert-ok")) == nil
	})
	drip := dripServer(t)
	live := hold(t, xport, drip.URL)
	eventually(t, 10*time.Second, "held connection", func() bool { return live.got.Load() > 0 })
	oldFP := certFileFP(t, e.paths.CertsDir, certID)
	rev, hash := e.revision(), kernelHash(t, e.paths.CoresDir)
	pids := helperRunning(t, e.paths.Socket)
	evidence(t, "cert-before", map[string]any{"cert_id": certID, "fingerprint": oldFP, "revision": rev, "hash": hash, "pids": pids})

	center.SetACMEDirectoryForTest(func(center.NodeCertSettings) string { return ca2.Directory() })
	renewed := e.post(fmt.Sprintf("/api/v1/admin/certs/%d/renew", certID), map[string]any{}, nil)
	evidence(t, "cert-renew-accepted", map[string]any{"cert_id": certID, "fingerprint": renewed["fingerprint"], "source": renewed["source"], "status": renewed["status"], "file": oldFP})
	var newFP string
	eventually(t, 90*time.Second, "cert file replaced", func() bool {
		newFP = certFileFP(t, e.paths.CertsDir, certID)
		return newFP != "" && newFP != oldFP
	})
	time.Sleep(1500 * time.Millisecond)
	if e.revision() != rev || kernelHash(t, e.paths.CoresDir) != hash || !samePIDs(pids, helperRunning(t, e.paths.Socket)) || live.errString() != "" || live.got.Load() == 0 {
		t.Fatalf("renewal changed runtime rev %d→%d hash %s→%s pids %v→%v held %s bytes %d", rev, e.revision(), hash, kernelHash(t, e.paths.CoresDir), pids, helperRunning(t, e.paths.Socket), live.errString(), live.got.Load())
	}
	evidence(t, "cert-file-replaced", map[string]any{"old": oldFP, "new": newFP, "revision": rev, "pids": pids, "held_bytes": live.got.Load(), "renew_id": renewed["id"]})

	t.Run("xray-new-handshake", func(t *testing.T) {
		bound := 70 * time.Minute
		if raw := os.Getenv("WEIR_E2E_CERT_BOUND"); raw != "" {
			if d, err := time.ParseDuration(raw); err == nil {
				bound = d
			}
		}
		addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(int(trojan["port"].(float64))))
		start := time.Now()
		var served string
		for {
			served = peerFP(addr)
			if served == newFP {
				break
			}
			if time.Since(start) > bound {
				t.Fatalf("xray still serves %s after %s; new file is %s", served, time.Since(start).Round(time.Second), newFP)
			}
			t.Logf("xray cert poll elapsed=%s served=%s want=%s", time.Since(start).Round(time.Second), served, newFP)
			time.Sleep(15 * time.Second)
		}
		evidence(t, "xray-new-handshake", map[string]any{"elapsed_ms": time.Since(start).Milliseconds(), "served": served, "bound": bound.String()})
	})
	t.Run("hysteria2-new-handshake", func(t *testing.T) {

		installTrust(t, ca2)
		fresh := e.runSingboxClient(sb, e.subOutbound(uid, int(hy2["id"].(float64))))
		if err := fetchVia(fresh, web.URL, len("cert-ok")); err != nil {
			t.Fatalf("new hysteria2 handshake did not get the renewed cert: %v", err)
		}
		evidence(t, "hysteria2-new-handshake", map[string]any{"result": "passed", "note": "client trusted only the renewal CA"})

	})

	t.Run("self-signed-fallback", func(t *testing.T) {
		e.startGate("cert-self")
		selfID := e.gateID
		e.s.Store.Ent.Gate.UpdateOneID(selfID).SetPublicHost("127.0.0.1").ExecX(e.ctx)
		busy, err := net.Listen("tcp", ":80")
		if err != nil {
			conn, dialErr := net.DialTimeout("tcp", "127.0.0.1:80", time.Second)
			if dialErr != nil {
				t.Fatalf("port 80 must be occupied for fallback: listen=%v dial=%v", err, dialErr)
			}
			conn.Close()
		} else {
			t.Cleanup(func() { busy.Close() })
		}
		node := e.post("/api/v1/admin/nodes", map[string]any{"gate_id": selfID, "preset": "hysteria2", "name": "self", "skip_probe": true}, nil)
		e.appliedGate(selfID, "self-signed hysteria2")
		row, err := e.s.Store.Ent.Certificate.Get(e.ctx, int(node["cert_id"].(float64)))
		if err != nil || row.Source != "self_signed" || row.LastError == "" {
			t.Fatalf("self-signed fallback = %+v %v", row, err)
		}
		pinned := e.shareLink(uid, int(node["id"].(float64)))
		if !strings.Contains(pinned, "pinSHA256=") {
			t.Fatalf("self-signed subscription missing fingerprint: %s", redactLink(pinned))
		}
		evidence(t, "self-signed-fallback", map[string]any{"reason": row.LastError, "pinned": true})
	})
}

func (e *env) appliedDump(what string) {
	e.t.Helper()
	deadline := time.Now().Add(2 * time.Minute)
	var last string
	for time.Now().Before(deadline) {
		g, err := e.s.Store.Ent.Gate.Get(e.ctx, e.gateID)
		if err == nil && g.AppliedRevision == g.ConfigRevision && g.AppliedRevision > 0 {
			return
		}
		if err == nil {
			last = fmt.Sprintf("rev %d applied %d failed %d err %q", g.ConfigRevision, g.AppliedRevision, g.FailedRevision, g.LastError)
		}
		time.Sleep(300 * time.Millisecond)
	}
	e.t.Fatalf("timed out waiting for %s (%s)", what, last)
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func (e *env) kernelFacts(singbox string) map[string]any {
	e.t.Helper()
	xd, _, _ := (kernels.Drivers{}).Select("xray", e.xrayVersion)
	sd, _, _ := (kernels.Drivers{}).Select("sing-box", e.singboxVersion)
	xv, _ := exec.Command(e.xray, "version").CombinedOutput()
	sv, _ := exec.Command(singbox, "version").CombinedOutput()
	return map[string]any{
		"xray":    map[string]any{"binary": strings.TrimSpace(string(xv)), "registered": e.xrayVersion, "digest": e.xrayDigest, "driver": xd.Name, "driver_range": xd.MinVersion + ".." + xd.MaxVersion},
		"singbox": map[string]any{"binary": strings.TrimSpace(string(sv)), "registered": e.singboxVersion, "digest": e.singboxDigest, "driver": sd.Name, "driver_range": sd.MinVersion + ".." + sd.MaxVersion, "fixture_dir": filepath.Base(filepath.Dir(singbox))},
	}
}

func evidence(t *testing.T, scenario string, fields map[string]any) {
	t.Helper()
	fields["scenario"], fields["at"] = scenario, time.Now().UTC().Format(time.RFC3339Nano)
	b, _ := json.Marshal(fields)
	t.Log(string(b))
	f, err := os.OpenFile("/t/traffic-evidence.jsonl", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err == nil {
		_, _ = f.Write(append(b, '\n'))
		_ = f.Close()
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (e *env) quotaUser(email string) map[string]any {
	e.t.Helper()
	user := e.post("/api/v1/admin/users", map[string]any{"email": email, "password": "E2eUser1", "unlimited": true}, nil)
	detail := map[string]any{}
	b, _ := e.get("/api/v1/admin/users/"+strconv.Itoa(int(user["id"].(float64))), nil)
	json.Unmarshal(b, &detail)
	if detail["unlimited"] != true {
		e.t.Fatalf("test user has no quota: %v", detail["quota_bytes"])
	}
	return user
}

func (e *env) requireCredential(uid int, nodeIDs ...int) {
	e.t.Helper()
	for _, id := range nodeIDs {
		ob := e.subOutbound(uid, id)
		secret, _ := ob["password"].(string)
		if i := strings.LastIndex(secret, ":"); i >= 0 {
			secret = secret[i+1:]
		}
		if secret == "" {
			secret, _ = ob["uuid"].(string)
		}
		if secret == "" {
			e.t.Fatalf("subscription outbound for node %d has no credential", id)
		}
		found := false
		for _, name := range []string{"xray.json", "sing-box.json"} {
			b, err := os.ReadFile(filepath.Join(e.paths.CoresDir, name))
			if err == nil && bytes.Contains(b, []byte(secret)) {
				found = true
			}
		}
		if !found {
			e.t.Fatalf("server config does not contain the ordinary user's credential for node %d", id)
		}
	}
	evidence(e.t, "credential-present", map[string]any{"user_id": uid, "nodes": nodeIDs, "present": true})
}

func (e *env) userToken(uid int) string {
	e.t.Helper()
	b, _ := e.get("/api/v1/admin/users/"+strconv.Itoa(uid), nil)
	var detail map[string]any
	json.Unmarshal(b, &detail)
	return detail["link_items"].([]any)[0].(map[string]any)["token"].(string)
}

func (e *env) shareLink(uid, nodeID int) string {
	e.t.Helper()
	b, _ := e.get("/api/v1/admin/users/"+strconv.Itoa(uid), nil)
	var detail struct {
		Links []struct {
			Entries []center.Entry `json:"entries"`
		} `json:"link_items"`
	}
	json.Unmarshal(b, &detail)
	for _, link := range detail.Links {
		for _, entry := range link.Entries {
			if entry.NodeID == nodeID {
				return entry.URI
			}
		}
	}
	e.t.Fatalf("missing share link for node %d", nodeID)
	return ""
}

func redactLink(link string) string {
	if i := strings.Index(link, "@"); i > 0 {
		scheme := strings.Index(link, "://")
		if scheme >= 0 && scheme < i {
			return link[:scheme+3] + "[redacted]" + link[i:]
		}
	}
	return "[redacted]"
}

func proxyGET(port int, host, portText string, size int) error {
	var conn net.Conn
	var err error
	deadline := time.Now().Add(8 * time.Second)
	for {
		conn, err = net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 300*time.Millisecond)
		if err == nil || time.Now().After(deadline) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(8 * time.Second))
	req := fmt.Sprintf("GET http://%s:%s/x HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", host, portText, host)
	if _, err = conn.Write([]byte(req)); err != nil {
		return err
	}
	body, err := io.ReadAll(conn)
	if err != nil {
		return err
	}
	if !bytes.Contains(body, []byte("200")) {
		return fmt.Errorf("proxy status %q", firstLine(body))
	}
	if !bytes.Contains(body, []byte("dns-ok")) && size > 0 {
		return fmt.Errorf("missing body")
	}
	return nil
}

func firstLine(b []byte) string {
	line, _, _ := bytes.Cut(b, []byte("\n"))
	if len(line) > 180 {
		line = line[:180]
	}
	return string(line)
}

type dnsHit struct {
	Name string `json:"name"`
	Via  string `json:"via"`
	Peer string `json:"peer,omitempty"`
}

type dnsLab struct {
	t       *testing.T
	reality string
	answer  net.IP
	aliases map[string]net.IP
	dohFail atomic.Bool
	udpFail atomic.Bool
	mu      sync.Mutex
	hits    []dnsHit
	tcp     []string
	n       int
}

func startDNSLab(t *testing.T) *dnsLab {
	t.Helper()
	for _, ip := range []string{"1.1.1.1", "8.8.8.8"} {
		if err := addLoopIP(ip); err != nil && !errors.Is(err, syscall.EEXIST) {
			t.Fatal(err)
		}
	}
	ca, cert := ipCert(t, []net.IP{net.ParseIP("1.1.1.1"), net.ParseIP("8.8.8.8")})
	pemBytes := caPEM(ca)
	installTrust(t, pemBytes)
	trustSystem(t, pemBytes)
	lab := &dnsLab{t: t, reality: "reality-" + strconv.FormatInt(time.Now().UnixNano(), 36) + ".e2e.dns", answer: net.ParseIP("127.0.0.1")}
	sys, err := net.ListenPacket("udp", "127.0.0.53:53")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sys.Close() })
	go lab.serveUDP(sys, "system")
	oldResolv, err := os.ReadFile("/etc/resolv.conf")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile("/etc/resolv.conf", []byte("nameserver 127.0.0.53\noptions timeout:1 attempts:1\n"), 0644); err != nil {
		t.Fatalf("cannot separate system DNS from kernel DNS: %v", err)
	}
	t.Cleanup(func() { _ = os.WriteFile("/etc/resolv.conf", oldResolv, 0644) })
	for _, ip := range []string{"1.1.1.1", "8.8.8.8"} {
		ln, err := tls.Listen("tcp", ip+":443", &tls.Config{Certificates: []tls.Certificate{cert}, NextProtos: []string{"h2", "http/1.1"}})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { ln.Close() })
		go http.Serve(acceptNote{Listener: ln, note: func(addr string) { lab.noteAccept(addr) }}, lab.doh(ip))
		pc, err := net.ListenPacket("udp", ip+":53")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { pc.Close() })
		go lab.serveUDP(pc, "udp")
	}
	return lab
}

func (l *dnsLab) alias(name string, ip net.IP) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.aliases == nil {
		l.aliases = map[string]net.IP{}
	}
	l.aliases[strings.TrimSuffix(name, ".")] = ip
}

func (l *dnsLab) aliasIP(name string) (net.IP, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	ip, ok := l.aliases[strings.TrimSuffix(name, ".")]
	return ip, ok
}

func (l *dnsLab) unique(prefix string) string {
	l.n++
	return fmt.Sprintf("%s-%d-%d.e2e.dns", prefix, l.n, time.Now().UnixNano())
}

type acceptNote struct {
	net.Listener
	note func(string)
}

func (l acceptNote) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err == nil && l.note != nil {
		l.note(c.RemoteAddr().String())
	}
	return c, err
}

func (l *dnsLab) noteAccept(peer string) {
	l.mu.Lock()
	l.tcp = append(l.tcp, peer)
	l.mu.Unlock()
	l.note("tcp", "doh-tcp", peer)
}

func (l *dnsLab) accepts() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.tcp...)
}

func dnsSnippet(cfg []byte) string {
	var doc map[string]any
	if json.Unmarshal(cfg, &doc) != nil {
		return ""
	}
	b, _ := json.Marshal(doc["dns"])
	if len(b) > 500 {
		b = b[:500]
	}
	return string(b)
}

func trustSystem(t *testing.T, extra []byte) {
	t.Helper()
	path := "/etc/ssl/certs/ca-certificates.crt"
	base, err := os.ReadFile(path)
	if err != nil || bytes.Contains(base, extra) {
		return
	}
	if err = os.WriteFile(path, append(base, extra...), 0644); err != nil {
		t.Logf("system trust bundle is not writable: %v", err)
	}
}

func (l *dnsLab) note(name, via, peer string) {
	l.mu.Lock()
	l.hits = append(l.hits, dnsHit{Name: name, Via: via, Peer: peer})
	l.mu.Unlock()
}

func (l *dnsLab) snapshot() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.hits)
}

func (l *dnsLab) since(n int) []dnsHit {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := append([]dnsHit(nil), l.hits[n:]...)
	return out
}

func (l *dnsLab) matching(name string) []dnsHit {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []dnsHit
	for _, h := range l.hits {
		if h.Name == name || strings.HasPrefix(h.Name, name) {
			out = append(out, h)
		}
	}
	return out
}

func saw(hits []dnsHit, name, via string) bool {
	for _, h := range hits {
		if (h.Name == name || strings.HasSuffix(h.Name, name) || strings.Contains(h.Name, name)) && h.Via == via {
			return true
		}
	}
	return false
}

func (l *dnsLab) doh(ip string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q []byte
		if r.Method == http.MethodGet {
			raw := r.URL.Query().Get("dns")
			q, _ = base64.RawURLEncoding.DecodeString(raw)
		} else {
			q, _ = io.ReadAll(io.LimitReader(r.Body, 4096))
		}
		name := dnsQuestion(q)
		if name == "" {
			name = "(unparsed)"
		}
		l.note(name, "doh", r.RemoteAddr)
		if ip, ok := l.aliasIP(name); ok {
			w.Header().Set("Content-Type", "application/dns-message")
			_, _ = w.Write(dnsAnswer(q, ip, 0))
			return
		}
		if strings.HasSuffix(name, ".e2e.dns") {
			if l.dohFail.Load() {
				hj, ok := w.(http.Hijacker)
				if ok {
					conn, _, _ := hj.Hijack()
					if conn != nil {
						conn.Close()
					}
				}
				return
			}
			w.Header().Set("Content-Type", "application/dns-message")
			_, _ = w.Write(dnsAnswer(q, l.answer, 0))
			return
		}
		resp := forwardDNS(q)
		w.Header().Set("Content-Type", "application/dns-message")
		_, _ = w.Write(resp)
		_ = ip
	})
}

func (l *dnsLab) serveUDP(pc net.PacketConn, via string) {
	buf := make([]byte, 1500)
	for {
		n, addr, err := pc.ReadFrom(buf)
		if err != nil {
			return
		}
		q := append([]byte(nil), buf[:n]...)
		name := dnsQuestion(q)
		var resp []byte
		if ip, ok := l.aliasIP(name); ok {
			l.note(name, via, addr.String())
			resp = dnsAnswer(q, ip, 0)
		} else if strings.HasSuffix(name, ".e2e.dns") {
			l.note(name, via, addr.String())
			code := 0
			if l.udpFail.Load() {
				code = 2
			}
			resp = dnsAnswer(q, l.answer, code)
		} else {
			resp = forwardDNS(q)
		}
		_, _ = pc.WriteTo(resp, addr)
	}
}

func dnsQuestion(pkt []byte) string {
	name, _, ok := dnsName(pkt, 12)
	if !ok {
		return ""
	}
	return name
}

func dnsName(pkt []byte, off int) (string, int, bool) {
	var labels []string
	for step := 0; step < 20; step++ {
		if off >= len(pkt) {
			return "", off, false
		}
		n := int(pkt[off])
		if n == 0 {
			return strings.Join(labels, "."), off + 1, true
		}
		if n&0xC0 != 0 {
			return "", off, false
		}
		off++
		if off+n > len(pkt) {
			return "", off, false
		}
		labels = append(labels, string(pkt[off:off+n]))
		off += n
	}
	return "", off, false
}

func dnsAnswer(query []byte, ip net.IP, rcode int) []byte {
	nameEnd, _, ok := dnsNameEnd(query)
	if !ok {
		return query
	}
	out := append([]byte(nil), query[:nameEnd]...)
	out[2], out[3] = 0x81, 0x80|byte(rcode)
	binary.BigEndian.PutUint16(out[6:8], 0)
	if rcode == 0 && ip.To4() != nil {
		binary.BigEndian.PutUint16(out[6:8], 1)
		out = append(out, 0xC0, 0x0C, 0, 1, 0, 1, 0, 0, 0, 30, 0, 4)
		out = append(out, ip.To4()...)
	}
	return out
}

func dnsNameEnd(query []byte) (int, string, bool) {
	name, off, ok := dnsName(query, 12)
	if !ok || off+4 > len(query) {
		return 0, "", false
	}
	return off + 4, name, true
}

func forwardDNS(q []byte) []byte {
	conn, err := net.DialTimeout("udp", "9.9.9.9:53", time.Second)
	if err != nil {
		return dnsAnswer(q, nil, 2)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	_, _ = conn.Write(q)
	buf := make([]byte, 1500)
	n, err := conn.Read(buf)
	if err != nil {
		return dnsAnswer(q, nil, 2)
	}
	return buf[:n]
}

type trustSource interface{ PEM() []byte }

func installTrust(t *testing.T, src any) {
	t.Helper()
	var extra []byte
	switch v := src.(type) {
	case *acmetest.CA:
		extra = v.PEM()
	case []byte:
		extra = v
	default:
		t.Fatalf("unsupported trust source %T", src)
	}
	base, _ := os.ReadFile("/etc/ssl/certs/ca-certificates.crt")
	if prev := os.Getenv("SSL_CERT_FILE"); prev != "" && prev != "/etc/ssl/certs/ca-certificates.crt" {
		if accumulated, err := os.ReadFile(prev); err == nil && len(accumulated) > 0 {
			base = accumulated
		}
	}
	path := filepath.Join(t.TempDir(), "trust.pem")
	if err := os.WriteFile(path, append(base, extra...), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SSL_CERT_FILE", path)
}

func caPEM(c *x509.Certificate) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.Raw})
}

func ipCert(t *testing.T, ips []net.IP) (*x509.Certificate, tls.Certificate) {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "dns lab"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IPAddresses: ips}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pair := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
	return cert, pair
}

func http01(identifier, token string) (string, error) {
	resp, err := http.Get("http://" + identifier + "/.well-known/acme-challenge/" + token)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 512))
	return string(b), err
}

func certFileFP(t *testing.T, dir string, id int) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, strconv.Itoa(id)+".crt"))
	if err != nil {
		return ""
	}
	block, _ := pem.Decode(b)
	if block == nil {
		return ""
	}
	sum := sha256.Sum256(block.Bytes)
	return hex.EncodeToString(sum[:])
}

func peerFP(addr string) string {
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 3 * time.Second}, "tcp", addr, &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		return ""
	}
	defer conn.Close()
	if len(conn.ConnectionState().PeerCertificates) == 0 {
		return ""
	}
	sum := sha256.Sum256(conn.ConnectionState().PeerCertificates[0].Raw)
	return hex.EncodeToString(sum[:])
}

func samePIDs(a, b map[string]int) bool {
	return a["xray"] == b["xray"] && a["sing-box"] == b["sing-box"] && (a["xray"] != 0 || a["sing-box"] != 0)
}

type udpBox struct {
	addr  string
	count atomic.Int64
}

func udpEcho(t *testing.T) *udpBox {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pc.Close() })
	box := &udpBox{addr: pc.LocalAddr().String()}
	go func() {
		buf := make([]byte, 1500)
		for {
			n, addr, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			box.count.Add(1)
			_, _ = pc.WriteTo(append([]byte(nil), buf[:n]...), addr)
		}
	}()
	return box
}

type relayBox struct {
	port int
	hits atomic.Int64
	udp  atomic.Int64
}

func udpSocksServer(t *testing.T, allowUDP bool) *relayBox {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	box := &relayBox{port: ln.Addr().(*net.TCPAddr).Port}
	var relay net.PacketConn
	if allowUDP {
		relay, err = net.ListenPacket("udp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { relay.Close() })
		go relayUDP(relay, &box.udp)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go serveSocks(c, allowUDP, relay, &box.hits)
		}
	}()
	return box
}

func serveSocks(c net.Conn, allowUDP bool, relay net.PacketConn, hits *atomic.Int64) {
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(20 * time.Second))
	buf := make([]byte, 512)
	if _, err := io.ReadFull(c, buf[:2]); err != nil {
		return
	}
	_, _ = io.ReadFull(c, buf[:buf[1]])
	_, _ = c.Write([]byte{5, 0})
	if _, err := io.ReadFull(c, buf[:4]); err != nil {
		return
	}
	if buf[1] == 3 {
		if !allowUDP || relay == nil {
			_, _ = c.Write([]byte{5, 7, 0, 1, 0, 0, 0, 0, 0, 0})
			return
		}
		ip := relay.LocalAddr().(*net.UDPAddr).IP.To4()
		port := relay.LocalAddr().(*net.UDPAddr).Port
		_, _ = c.Write([]byte{5, 0, 0, 1, ip[0], ip[1], ip[2], ip[3], byte(port >> 8), byte(port)})
		io.Copy(io.Discard, c)
		return
	}
	host, port := socksAddr(c, buf)
	up, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), 5*time.Second)
	if err != nil {
		_, _ = c.Write([]byte{5, 5, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}
	defer up.Close()
	hits.Add(1)
	_, _ = c.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0})
	go io.Copy(up, c)
	io.Copy(c, up)
}

func socksAddr(c net.Conn, hdr []byte) (string, int) {
	switch hdr[3] {
	case 1:
		buf := make([]byte, 6)
		io.ReadFull(c, buf)
		return net.IP(buf[:4]).String(), int(binary.BigEndian.Uint16(buf[4:]))
	case 3:
		var n [1]byte
		io.ReadFull(c, n[:])
		buf := make([]byte, int(n[0])+2)
		io.ReadFull(c, buf)
		return string(buf[:n[0]]), int(binary.BigEndian.Uint16(buf[n[0]:]))
	default:
		return "", 0
	}
}

func relayUDP(pc net.PacketConn, count *atomic.Int64) {
	buf := make([]byte, 1500)
	clients := map[string]net.Addr{}
	var mu sync.Mutex
	for {
		n, addr, err := pc.ReadFrom(buf)
		if err != nil {
			return
		}
		pkt := append([]byte(nil), buf[:n]...)
		if len(pkt) < 10 || pkt[2] != 0 {
			continue
		}
		host, port, rest := udpTarget(pkt)
		if host == "" {
			mu.Lock()
			client := clients[addr.String()]
			mu.Unlock()
			if client != nil {
				_, _ = pc.WriteTo(pkt, client)
			}
			continue
		}
		mu.Lock()
		clients[net.JoinHostPort(host, strconv.Itoa(port))] = addr
		mu.Unlock()
		count.Add(1)
		up, err := net.Dial("udp", net.JoinHostPort(host, strconv.Itoa(port)))
		if err != nil {
			continue
		}
		_, _ = up.Write(rest)
		up.SetReadDeadline(time.Now().Add(2 * time.Second))
		m, err := up.Read(buf)
		up.Close()
		if err != nil {
			continue
		}
		reply := append(pkt[:len(pkt)-len(rest)], buf[:m]...)
		_, _ = pc.WriteTo(reply, addr)
	}
}

func udpTarget(pkt []byte) (string, int, []byte) {
	if len(pkt) < 10 {
		return "", 0, nil
	}
	switch pkt[3] {
	case 1:
		if len(pkt) < 10 {
			return "", 0, nil
		}
		return net.IP(pkt[4:8]).String(), int(binary.BigEndian.Uint16(pkt[8:10])), pkt[10:]
	case 3:
		n := int(pkt[4])
		if len(pkt) < 5+n+2 {
			return "", 0, nil
		}
		return string(pkt[5 : 5+n]), int(binary.BigEndian.Uint16(pkt[5+n : 7+n])), pkt[7+n:]
	default:
		return "", 0, nil
	}
}

func httpProxyServer(t *testing.T) *relayBox {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	box := &relayBox{port: ln.Addr().(*net.TCPAddr).Port}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_ = c.SetDeadline(time.Now().Add(20 * time.Second))
				head := make([]byte, 0, 2048)
				tmp := make([]byte, 512)
				for !bytes.Contains(head, []byte("\r\n\r\n")) && len(head) < 8192 {
					n, err := c.Read(tmp)
					if n > 0 {
						head = append(head, tmp[:n]...)
					}
					if err != nil {
						return
					}
				}
				line, _, _ := bytes.Cut(head, []byte("\r\n"))
				fields := strings.Fields(string(line))
				if len(fields) < 2 || fields[0] != "CONNECT" {
					return
				}
				up, err := net.DialTimeout("tcp", fields[1], 8*time.Second)
				if err != nil {
					_, _ = c.Write([]byte("HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\n\r\n"))
					return
				}
				defer up.Close()
				box.hits.Add(1)
				_, _ = c.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
				if i := bytes.Index(head, []byte("\r\n\r\n")); i >= 0 && i+4 < len(head) {
					_, _ = up.Write(head[i+4:])
				}
				_ = c.SetDeadline(time.Time{})
				go io.Copy(up, c)
				io.Copy(c, up)
			}(c)
		}
	}()
	return box
}

func serveLinks(t *testing.T, links []string) string {
	t.Helper()
	body := base64.StdEncoding.EncodeToString([]byte(strings.Join(links, "\n")))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func bindSub(e *env, t *testing.T) {
	parent := e.t
	e.t = t
	t.Cleanup(func() { e.t = parent })
}

func waitListen(t *testing.T, port int) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	var err error
	for {
		conn, dialErr := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 200*time.Millisecond)
		if dialErr == nil {
			conn.Close()
			return
		}
		err = dialErr
		if time.Now().After(deadline) {
			t.Fatalf("local client :%d not listening: %v", port, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func socks5UDP(proxyPort int, dest string, payload []byte, timeout time.Duration) ([]byte, time.Duration, error) {
	start := time.Now()
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(proxyPort)), timeout)
	if err != nil {
		return nil, time.Since(start), err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	if _, err = conn.Write([]byte{5, 1, 0}); err != nil {
		return nil, time.Since(start), err
	}
	buf := make([]byte, 16)
	if _, err = io.ReadFull(conn, buf[:2]); err != nil || buf[1] != 0 {
		return nil, time.Since(start), fmt.Errorf("socks greeting: %v", err)
	}
	if _, err = conn.Write([]byte{5, 3, 0, 1, 0, 0, 0, 0, 0, 0}); err != nil {
		return nil, time.Since(start), err
	}
	if _, err = io.ReadFull(conn, buf[:10]); err != nil {
		return nil, time.Since(start), err
	}
	if buf[1] != 0 {
		return nil, time.Since(start), fmt.Errorf("socks udp associate rejected: %d", buf[1])
	}
	relay := net.JoinHostPort(net.IP(buf[4:8]).String(), strconv.Itoa(int(binary.BigEndian.Uint16(buf[8:10]))))
	pc, err := net.Dial("udp", relay)
	if err != nil {
		return nil, time.Since(start), err
	}
	defer pc.Close()
	host, portText, _ := net.SplitHostPort(dest)
	ip := net.ParseIP(host).To4()
	port, _ := strconv.Atoi(portText)
	pkt := []byte{0, 0, 0, 1, ip[0], ip[1], ip[2], ip[3], byte(port >> 8), byte(port)}
	pkt = append(pkt, payload...)
	if _, err = pc.Write(pkt); err != nil {
		return nil, time.Since(start), err
	}
	// 握手已经完成。VLESS/mux 拒绝会关掉这条控制连接，UDP 套接字本身没有错误包。
	// 不能给控制连接设截止时间，否则超时也会被看成服务端关闭。
	_ = conn.SetDeadline(time.Time{})
	tcpClosed := make(chan struct{})
	go func() {
		_, err := io.Copy(io.Discard, conn)
		if err == nil || !errors.Is(err, net.ErrClosed) {
			close(tcpClosed)
		}
	}()
	_ = pc.SetReadDeadline(time.Now().Add(timeout))
	raw := make([]byte, 1500)
	readDone := make(chan struct{})
	var n int
	var readErr error
	go func() {
		n, readErr = pc.Read(raw)
		close(readDone)
	}()
	select {
	case <-readDone:
		if readErr != nil {
			return nil, time.Since(start), readErr
		}
		_, _, body := udpTarget(raw[:n])
		return body, time.Since(start), nil
	case <-tcpClosed:
		return nil, time.Since(start), errors.New("socks control connection closed")
	}
}

func (e *env) runSingboxClientDebug(bin string, outbound map[string]any, logs io.Writer) int {
	e.t.Helper()
	port := freePort(e.t)
	tag, _ := outbound["tag"].(string)
	cfg := map[string]any{
		"log":       map[string]any{"level": "debug"},
		"inbounds":  []any{map[string]any{"type": "mixed", "listen": "127.0.0.1", "listen_port": port}},
		"outbounds": []any{outbound},
		"route":     map[string]any{"final": tag},
	}
	b, _ := json.Marshal(cfg)
	path := filepath.Join(e.dir, fmt.Sprintf("client-%d.json", port))
	os.WriteFile(path, b, 0600)
	cmd := exec.Command(bin, "run", "-c", path)
	cmd.Stdout, cmd.Stderr = logs, logs
	if err := cmd.Start(); err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	return port
}

// waitUDPReject 记录客户端实际看见拒绝的时间。控制连接关闭、UDP 读错误，
// 或客户端日志里的 connection refused，都算看见；静默等到截止时间不算。
func waitUDPReject(proxyPort int, dest string, payload []byte, logs *bytes.Buffer) (time.Duration, error) {
	start := time.Now()
	errCh := make(chan error, 1)
	go func() {
		_, _, err := socks5UDP(proxyPort, dest, payload, 1200*time.Millisecond)
		errCh <- err
	}()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case err := <-errCh:
			if err == nil {
				return time.Since(start), nil
			}
			return time.Since(start), err
		case <-tick.C:
			if logs != nil && bytes.Contains(logs.Bytes(), []byte("connection refused")) {
				return time.Since(start), errors.New("client observed connection refused")
			}
		case <-deadline.C:
			return time.Since(start), errors.New("silent drop")
		}
	}
}

func udpRejectVisible(err error, elapsed time.Duration) bool {
	if err == nil || elapsed > time.Second {
		return false
	}
	msg := err.Error()
	return !strings.Contains(msg, "silent drop") && !strings.Contains(msg, "i/o timeout")
}

func udpOf(exit map[string]any) string {
	v, _ := exit["udp"].(string)
	return v
}
