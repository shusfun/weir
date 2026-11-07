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
	"testing"
	"time"

	"github.com/shusfun/weir/internal/kernels"
	"github.com/shusfun/weir/internal/nodeprobe"
	"github.com/shusfun/weir/packages/contracts"
)

// 从实际创建入口验收，不跳过创建前实测；TLS 使用显式信任的隔离测试 CA。
func TestAllPresetsCreationAndTraffic(t *testing.T) {
	sb := os.Getenv("WEIR_E2E_SINGBOX")
	if sb == "" {
		t.Fatal("real sing-box required")
	}
	e := setup(t)
	data, err := os.ReadFile(sb)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = (kernels.Cache{Dir: filepath.Join(e.dir, "center-data", "kernels")}).Put("sing-box", "1.14.4", "amd64", data); err != nil {
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
	for _, preset := range presets {
		t.Run(preset, func(t *testing.T) {
			parent := e.t
			e.t = t
			defer func() { e.t = parent }()
			body := map[string]any{"gate_id": e.gateID, "preset": preset, "name": preset}
			if preset == "vless-ws-tls" || preset == "vmess-ws-tls" || preset == "trojan-tls" {
				body["cert_id"] = cert["id"]
			}
			created := e.post("/api/v1/admin/nodes", body, nil)
			nodeIDs[preset] = int(created["id"].(float64))
			tested, ok := created["tested"].(map[string]any)
			if !ok || tested["done"] != true || tested["stage"] != "connect" {
				t.Fatalf("creation was not tested: %v", tested)
			}
			e.applied(preset)
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
	blob := bytes.Repeat([]byte("w"), 256<<10)
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
	target.Start()
	defer target.Close()
	// The HTTPS target must remain reachable through WireGuard, which cannot
	// reach the remote host's loopback interface.
	hosts, err := os.OpenFile("/etc/hosts", os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = fmt.Fprintf(hosts, "\n%s node-center.e2e.test\n", host)
	_ = hosts.Close()
	if err != nil {
		t.Fatal(err)
	}
	tlsCert, err := tls.X509KeyPair([]byte(certPEM), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kd}))
	if err != nil {
		t.Fatal(err)
	}
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
			cfg := map[string]any{"log": map[string]any{"level": "warn"}, "inbounds": []any{map[string]any{"type": "mixed", "listen": "127.0.0.1", "listen_port": port}}, "outbounds": doc.Outbounds, "endpoints": doc.Endpoints, "route": map[string]any{"final": tags[preset]}}
			body, _ := json.Marshal(cfg)
			path := filepath.Join(e.dir, "client-"+preset+".json")
			if err := os.WriteFile(path, body, 0600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(sb, "run", "-c", path)
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
				if err := fetchVia(port, target.URL, len(blob)); err != nil {
					t.Fatalf("probe displaced WireGuard client: %v", err)
				}
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
	probeURL := "https://" + net.JoinHostPort("node-center.e2e.test", fmt.Sprint(probeTarget.Listener.Addr().(*net.TCPAddr).Port))
	runProbe := func(t *testing.T, outbound map[string]any, udp bool) nodeprobe.Result {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 40*time.Second)
		defer cancel()
		r, err := nodeprobe.Run(ctx, nodeprobe.Options{Binary: sb, Run: []string{"run", "-c", "{config}"}, Check: []string{"check", "-c", "{config}"}, Client: contracts.NodeTestClient{Outbounds: []map[string]any{outbound}, Target: outbound["tag"].(string)}, Target: probeURL, Token: "isolated-probe", UDP: udp, UDPServers: []string{"127.0.0.1:9"}})
		if r.TCP.Status == "unknown" {
			t.Fatalf("test setup failed: %v %+v", err, r)
		}
		return r
	}
	copyOutbound := func(preset string) map[string]any {
		for _, o := range doc.Outbounds {
			if o["tag"] == tags[preset] {
				b, _ := json.Marshal(o)
				var out map[string]any
				_ = json.Unmarshal(b, &out)
				return out
			}
		}
		t.Fatalf("missing outbound %s", preset)
		return nil
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
		o["tls"].(map[string]any)["server_name"] = "wrong-certificate.invalid"
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
		o["server_port"] = l.Addr().(*net.TCPAddr).Port
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
		r, err := nodeprobe.Run(ctx, nodeprobe.Options{Binary: sb, Run: []string{"run", "-c", "{config}"}, Check: []string{"check", "-c", "{config}"}, Client: contracts.NodeTestClient{Outbounds: []map[string]any{o}, Target: o["tag"].(string)}, Target: probeURL + "/stall", Token: "isolated-probe"})
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
