//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/gate"
	"github.com/shusfun/weir/internal/kernels"
	"github.com/shusfun/weir/internal/nodeprobe"
	"github.com/shusfun/weir/packages/contracts"
	"gopkg.in/yaml.v3"
)

// 从实际创建入口验收排队与配置应用，随后使用真实客户端验证业务完成；
// TLS 使用显式信任的隔离测试 CA。
func TestAllPresetsCreationAndTraffic(t *testing.T) {
	sb := os.Getenv("WEIR_E2E_SINGBOX")
	if sb == "" {
		t.Fatal("real sing-box required")
	}
	e := setup(t)
	mh := os.Getenv("WEIR_E2E_MIHOMO")
	data, err := os.ReadFile(sb)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Weir isolated test CA"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	root := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	rootPath := filepath.Join(e.dir, "test-ca.pem")
	if err = os.WriteFile(rootPath, root, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SSL_CERT_FILE", rootPath)
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "127.0.0.1"}, DNSNames: []string{"node-center.e2e.test"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err = x509.CreateCertificate(rand.Reader, leaf, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})) + string(root)
	kd, _ := x509.MarshalECPrivateKey(key)
	cert := e.post("/api/v1/admin/certs", map[string]any{"name": "isolated test certificate", "domain": "127.0.0.1", "cert_pem": certPEM, "key_pem": string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kd}))}, nil)
	e.startGate("protocols")
	e.s.Store.Ent.Gate.UpdateOneID(e.gateID).SetPublicHost("127.0.0.1").ExecX(e.ctx)
	u := e.post("/api/v1/admin/users", map[string]any{"email": "protocols@e2e.test", "all_nodes": true, "quota": 0}, nil)
	presets := []string{"vless-reality", "vless-ws-tls", "vmess-ws-tls", "trojan-tls", "ss2022", "hysteria2", "tuic", "shadowtls", "wireguard", "socks-local", "http-local"}
	nodeIDs := map[string]int{}
	realityDestination := localTLS(t)
	for _, preset := range presets {
		t.Run(preset, func(t *testing.T) {
			parent := e.t
			e.t = t
			defer func() { e.t = parent }()
			if preset == "ss2022" {
				if _, err = (kernels.Cache{Dir: filepath.Join(e.dir, "center-data", "kernels")}).Put("sing-box", "1.14.4", "amd64", data); err != nil {
					t.Fatal(err)
				}
			}
			body := map[string]any{"gate_id": e.gateID, "preset": preset, "name": preset}
			if preset == "vless-reality" {
				body["settings"] = map[string]any{"reality_dest": realityDestination, "reality_server_names": "e2e.test"}
			}
			if preset == "vless-ws-tls" || preset == "vmess-ws-tls" || preset == "trojan-tls" {
				body["cert_id"] = cert["id"]
			}
			created := e.post("/api/v1/admin/nodes", body, nil)
			nodeIDs[preset] = int(created["id"].(float64))
			tested, ok := created["tested"].(map[string]any)
			if !ok || tested["done"] != false || tested["pending"] != true || tested["status"] != "unknown" {
				t.Fatalf("creation did not expose pending verification: %v", tested)
			}
			e.applied(preset)
			if preset == "vless-reality" {
				eventually(t, 10*time.Second, "仅 Xray 的 VPS 上报运行内核", func() bool { return e.s.Store.Ent.Gate.GetX(e.ctx, e.gateID).KernelsRunning["xray"] != "" })
				g := e.s.Store.Ent.Gate.GetX(e.ctx, e.gateID)
				if g.KernelsRunning["xray"] == "" || g.KernelsRunning["sing-box"] != "" || g.KernelsRunning["mihomo"] != "" {
					t.Fatalf("仅 Xray 的 VPS 运行职责错误：%v", g.KernelsRunning)
				}
				if _, _, err := (kernels.Cache{Dir: filepath.Join(e.dir, "center-data", "kernels")}).Get("sing-box", "1.14.4", "amd64"); err == nil {
					t.Fatal("VLESS 实测不应要求 sing-box 缓存")
				}
			}
		})
	}
	if t.Failed() {
		return
	}
	b, _ := e.get(fmt.Sprintf("/api/v1/admin/users/%d", int(u["id"].(float64))), nil)
	var ud map[string]any
	if err = json.Unmarshal(b, &ud); err != nil {
		t.Fatal(err)
	}
	token := ud["link_items"].([]any)[0].(map[string]any)["token"].(string)
	tags := map[string]string{}
	for _, preset := range presets {
		tags[preset] = e.subName(int(u["id"].(float64)), nodeIDs[preset], 0)
	}
	b, _ = e.get("/sub/"+token+"?target=singbox", nil)
	var doc struct {
		Outbounds []map[string]any `json:"outbounds"`
		Endpoints []map[string]any `json:"endpoints"`
	}
	if err = json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Endpoints) != 1 {
		t.Fatal("WireGuard endpoint missing from subscription")
	}
	names := map[string]bool{}
	for _, o := range doc.Outbounds {
		if tag, ok := o["tag"].(string); ok {
			names[tag] = true
		}
	}
	for _, preset := range presets {
		if preset != "wireguard" && !names[tags[preset]] {
			t.Errorf("subscription missing %s", preset)
		}
	}
	b, _ = e.get("/sub/"+token+"/provider/single", nil)
	var mhDoc struct {
		Proxies []map[string]any `yaml:"proxies"`
	}
	if err = yaml.Unmarshal(b, &mhDoc); err != nil {
		t.Fatal(err)
	}
	blob := nodeprobe.Payload("formal-traffic")
	target := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(blob) }))
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		t.Fatal(err)
	}
	host := ""
	for _, a := range addrs {
		ip, _, err := net.ParseCIDR(a.String())
		if err == nil && ip.To4() != nil && !ip.IsLoopback() {
			host = ip.String()
			break
		}
	}
	if host == "" {
		t.Fatal("non-loopback test target required for WireGuard")
	}
	_ = target.Listener.Close()
	target.Listener, err = net.Listen("tcp", net.JoinHostPort(host, "0"))
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	// WireGuard 无法访问远端回环地址，HTTPS 端点使用容器网卡。
	hosts, err := os.OpenFile("/etc/hosts", os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = fmt.Fprintf(hosts, "\n%s node-center.e2e.test\n", host)
	_ = hosts.Close()
	if err != nil {
		t.Fatal(err)
	}
	// 原始 IP 与域名都经过相同的隔离 CA 校验。
	leaf.IPAddresses = append(leaf.IPAddresses, net.ParseIP(host))
	der, err = x509.CreateCertificate(rand.Reader, leaf, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	centerPEM := append(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), root...)
	tlsCert, err := tls.X509KeyPair(centerPEM, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kd}))
	if err != nil {
		t.Fatal(err)
	}
	target.TLS = &tls.Config{Certificates: []tls.Certificate{tlsCert}}
	target.StartTLS()
	center := httptest.NewUnstartedServer(e.s.Router())
	_ = center.Listener.Close()
	center.Listener, err = net.Listen("tcp", net.JoinHostPort(host, "0"))
	if err != nil {
		t.Fatal(err)
	}
	center.TLS = &tls.Config{Certificates: []tls.Certificate{tlsCert}}
	center.StartTLS()
	defer center.Close()
	secureURL := "https://" + net.JoinHostPort("node-center.e2e.test", fmt.Sprint(center.Listener.Addr().(*net.TCPAddr).Port))
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(root)
	secureClient := &http.Client{Jar: e.admin.Jar, Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}, Timeout: 30 * time.Second}
	for _, op := range []struct{ method, path, body string }{
		{"POST", "/api/v1/auth/login", `{"email":"admin@e2e.test","password":"E2eAdmin1"}`},
		{"PUT", "/api/v1/admin/center-origin", fmt.Sprintf(`{"origin":%q}`, secureURL)},
	} {
		req, _ := http.NewRequest(op.method, secureURL+op.path, bytes.NewBufferString(op.body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		resp, err := secureClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("%s: %d", op.path, resp.StatusCode)
		}
	}
	// Gate 改经容器网卡的 HTTPS 地址连接，心跳与实测使用同一隔离 CA。
	e.stopAgent()
	gateConfig, err := gate.LoadConfig(e.paths.Config)
	if err != nil {
		t.Fatal(err)
	}
	gateConfig.Center, gateConfig.CAFile = secureURL, rootPath
	gateJSON, _ := json.Marshal(gateConfig)
	if err = os.WriteFile(e.paths.Config, gateJSON, 0600); err != nil {
		t.Fatal(err)
	}
	e.runAgent()
	defer func() { e.stopAgent() }()
	eventually(t, 10*time.Second, "Gate 经 HTTPS 上报", func() bool {
		return e.s.Store.Ent.Gate.GetX(e.ctx, e.gateID).CenterURL == secureURL
	})
	// 心跳先于重连后的配置应用，不能据此假定 WireGuard 已重新监听。
	e.applied("Gate 经 HTTPS 重连后恢复节点配置")
	for _, p := range mhDoc.Proxies {
		if p["name"] != tags["wireguard"] {
			continue
		}
		for _, endpoint := range []string{target.URL, strings.Replace(target.URL, host, "node-center.e2e.test", 1)} {
			t.Run("wireguard-https-"+strings.TrimPrefix(endpoint, "https://"), func(t *testing.T) {
				r, err := nodeprobe.Run(t.Context(), nodeprobe.Options{Binary: mh, Run: []string{"-f", "{config}"}, Check: []string{"-t", "-f", "{config}"}, Client: contracts.NodeTestClient{Proxies: []map[string]any{p}, Target: tags["wireguard"]}, Target: endpoint, Token: "formal-traffic"})
				if err != nil || r.TCP.Status != "passed" {
					t.Fatalf("WireGuard HTTPS：%+v %v", r, err)
				}
			})
		}
	}
	if t.Failed() {
		return
	}
	for _, preset := range presets {
		t.Run("deployed-"+preset, func(t *testing.T) {
			parent := e.t
			e.t = t
			defer func() { e.t = parent }()
			r := e.post(fmt.Sprintf("/api/v1/admin/nodes/%d/test", nodeIDs[preset]), map[string]any{}, nil)["test"].(map[string]any)
			if r["status"] != "passed" {
				t.Fatalf("deployed test: %+v", r)
			}
			want := "passed"
			if preset == "http-local" || preset == "shadowtls" {
				want = "not_applicable"
			}
			if r["udp"].(map[string]any)["status"] != want || r["tcp"].(map[string]any)["bytes"] != float64(256<<10) {
				t.Fatalf("incomplete result: %+v", r)
			}
		})
	}
	for _, preset := range presets {
		t.Run("traffic-"+preset, func(t *testing.T) {
			port := freePort(t)
			cfg := (contracts.NodeTestClient{Proxies: mhDoc.Proxies, Target: tags[preset]}).Config(port)
			body, _ := json.Marshal(cfg)
			path := filepath.Join(e.dir, "client-"+preset+".json")
			if err := os.WriteFile(path, body, 0600); err != nil {
				t.Fatal(err)
			}
			clientHome := t.TempDir()
			cmd := exec.Command(mh, "-f", path, "-d", clientHome)
			cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
			eventually(t, 25*time.Second, "formal traffic through "+preset, func() bool { return fetchVia(port, target.URL, len(blob)) == nil })
			if preset == "wireguard" {
				parent := e.t
				e.t = t
				defer func() { e.t = parent }()
				r := e.post(fmt.Sprintf("/api/v1/admin/nodes/%d/test", nodeIDs[preset]), map[string]any{}, nil)["test"].(map[string]any)
				if r["status"] != "passed" {
					t.Fatalf("parallel WireGuard test: %v", r)
				}
				e.applied("回收内部测试凭据")
				eventually(t, 25*time.Second, "内部凭据回收后普通 WireGuard 凭据仍可连接", func() bool { return fetchVia(port, target.URL, len(blob)) == nil })
			}
		})
	}
	probeTarget := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/stall" {
			select {
			case <-r.Context().Done():
			case <-time.After(5 * time.Second):
			}
			return
		}
		_, _ = w.Write(nodeprobe.Payload("isolated-probe"))
	}))
	_ = probeTarget.Listener.Close()
	probeTarget.Listener, err = net.Listen("tcp", net.JoinHostPort(host, "0"))
	if err != nil {
		t.Fatal(err)
	}
	probeTarget.TLS = &tls.Config{Certificates: []tls.Certificate{tlsCert}}
	probeTarget.StartTLS()
	defer probeTarget.Close()
	probeURL := "https://" + net.JoinHostPort(host, fmt.Sprint(probeTarget.Listener.Addr().(*net.TCPAddr).Port))
	runProbe := func(t *testing.T, outbound map[string]any, udp bool) nodeprobe.Result {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 40*time.Second)
		defer cancel()
		r, err := nodeprobe.Run(ctx, nodeprobe.Options{Binary: mh, Run: []string{"-f", "{config}"}, Check: []string{"-t", "-f", "{config}"}, Client: contracts.NodeTestClient{Proxies: []map[string]any{outbound}, Target: outbound["name"].(string)}, Target: probeURL, Token: "isolated-probe", UDP: udp, UDPServers: []string{"127.0.0.1:9"}})
		if r.TCP.Status == "unknown" {
			t.Fatalf("test setup failed: %v %+v", err, r)
		}
		return r
	}
	copyOutbound := func(preset string) map[string]any {
		for _, o := range mhDoc.Proxies {
			if o["name"] == tags[preset] {
				b, _ := json.Marshal(o)
				var out map[string]any
				_ = json.Unmarshal(b, &out)
				return out
			}
		}
		t.Fatalf("missing outbound %s", preset)
		return nil
	}
	for _, preset := range []string{"hysteria2", "tuic"} {
		t.Run("udp-filter-"+preset, func(t *testing.T) {
			o := copyOutbound(preset)
			blocked := udpFilter(t, fmt.Sprintf("%v:%v", o["server"], o["port"]), o)
			if r := runProbe(t, o, false); r.TCP.Status != "passed" {
				t.Fatalf("UDP 放行后未连接：%+v", r)
			}
			blocked.Store(true)
			ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
			r, err := nodeprobe.Run(ctx, nodeprobe.Options{Binary: mh, Run: []string{"-f", "{config}"}, Check: []string{"-t", "-f", "{config}"}, Client: contracts.NodeTestClient{Proxies: []map[string]any{o}, Target: o["name"].(string)}, Target: probeURL, Token: "isolated-probe"})
			cancel()
			if r.TCP.Status == "passed" || r.TCP.Why == "" {
				t.Fatalf("UDP 阻断后错误地成功或缺少原因：%+v %v", r, err)
			}
			blocked.Store(false)
			if r := runProbe(t, o, false); r.TCP.Status != "passed" {
				t.Fatalf("UDP 恢复后未连接：%+v", r)
			}
		})
	}
	t.Run("wrong-credentials", func(t *testing.T) {
		o := copyOutbound("trojan-tls")
		o["password"] = "deliberately-invalid"
		if r := runProbe(t, o, false); r.TCP.Status != "failed" {
			t.Fatalf("wrong credential accepted: %+v", r)
		}
	})
	t.Run("wrong-certificate", func(t *testing.T) {
		o := copyOutbound("trojan-tls")
		o["sni"] = "wrong-certificate.invalid"
		if r := runProbe(t, o, false); r.TCP.Status != "failed" {
			t.Fatalf("wrong certificate accepted: %+v", r)
		}
	})
	t.Run("open-port-wrong-protocol", func(t *testing.T) {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer l.Close()
		go func() {
			for {
				c, err := l.Accept()
				if err != nil {
					return
				}
				_ = c.Close()
			}
		}()
		o := copyOutbound("socks-local")
		o["port"] = l.Addr().(*net.TCPAddr).Port
		if r := runProbe(t, o, false); r.TCP.Status != "failed" {
			t.Fatalf("bare TCP accepted as protocol success: %+v", r)
		}
	})
	t.Run("udp-only-failure", func(t *testing.T) {
		if r := runProbe(t, copyOutbound("socks-local"), true); r.TCP.Status != "passed" || r.UDP.Status != "failed" {
			t.Fatalf("UDP failure flattened: %+v", r)
		}
	})
	t.Run("timeout-cleans-client", func(t *testing.T) {
		before, _ := filepath.Glob(filepath.Join(os.TempDir(), "weir-node-probe-*"))
		o := copyOutbound("socks-local")
		ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
		defer cancel()
		r, err := nodeprobe.Run(ctx, nodeprobe.Options{Binary: mh, Run: []string{"-f", "{config}"}, Check: []string{"-t", "-f", "{config}"}, Client: contracts.NodeTestClient{Proxies: []map[string]any{o}, Target: o["name"].(string)}, Target: probeURL + "/stall", Token: "isolated-probe"})
		if !errors.Is(err, context.DeadlineExceeded) || r.TCP.Status == "passed" {
			t.Fatalf("timeout result: %+v %v", r, err)
		}
		after, _ := filepath.Glob(filepath.Join(os.TempDir(), "weir-node-probe-*"))
		if len(after) != len(before) {
			t.Fatalf("temporary client credentials leaked: before %d after %d", len(before), len(after))
		}
		pids, _ := filepath.Glob("/proc/[0-9]*/cmdline")
		for _, path := range pids {
			b, _ := os.ReadFile(path)
			if bytes.Contains(b, []byte("/weir-node-probe-")) {
				t.Fatalf("test client still running: %s", path)
			}
		}
	})
}

// 在真实节点前只转发 UDP；丢弃两个方向的数据验证 QUIC 节点不能绕过 UDP 阻断。
func udpFilter(t *testing.T, target string, outbound map[string]any) *atomic.Bool {
	t.Helper()
	remote, err := net.ResolveUDPAddr("udp", target)
	if err != nil {
		t.Fatal(err)
	}
	upstream, err := net.DialUDP("udp", nil, remote)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		_ = upstream.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close(); _ = upstream.Close() })
	outbound["server"], outbound["port"] = "127.0.0.1", listener.LocalAddr().(*net.UDPAddr).Port
	blocked := new(atomic.Bool)
	var mu sync.Mutex
	var client *net.UDPAddr
	go func() {
		buf := make([]byte, 65536)
		for {
			n, addr, err := listener.ReadFromUDP(buf)
			if err != nil {
				return
			}
			mu.Lock()
			client = addr
			mu.Unlock()
			if !blocked.Load() {
				_, _ = upstream.Write(buf[:n])
			}
		}
	}()
	go func() {
		buf := make([]byte, 65536)
		for {
			n, err := upstream.Read(buf)
			if err != nil {
				return
			}
			mu.Lock()
			addr := client
			mu.Unlock()
			if addr != nil && !blocked.Load() {
				_, _ = listener.WriteToUDP(buf[:n], addr)
			}
		}
	}()
	return blocked
}
