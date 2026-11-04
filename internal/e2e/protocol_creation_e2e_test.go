//go:build e2e

package e2e

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
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
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "127.0.0.1"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
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
		if preset != "wireguard" && !names[preset] {
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
	for _, preset := range presets {
		t.Run("traffic-"+preset, func(t *testing.T) {
			port := freePort(t)
			cfg := map[string]any{"log": map[string]any{"level": "warn"}, "inbounds": []any{map[string]any{"type": "mixed", "listen": "127.0.0.1", "listen_port": port}}, "outbounds": doc.Outbounds, "endpoints": doc.Endpoints, "route": map[string]any{"final": preset}}
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
		})
	}
}
