//go:build e2e

package e2e

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/store/ent/credential"
)

func TestNodeTestBrowserFixture(t *testing.T) {
	if os.Getenv("WEIR_E2E_NODE_BROWSER") != "1" {
		t.Skip("interactive fixture disabled")
	}
	e := setup(t)
	e.s.Assets = os.DirFS(os.Getenv("WEIR_E2E_CONSOLE"))
	e.seedSingbox()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Isolated browser CA"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	root := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	rootPath := filepath.Join(e.dir, "browser-ca.pem")
	if err := os.WriteFile(rootPath, root, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SSL_CERT_FILE", rootPath)
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), DNSNames: []string{"node-center.e2e.test"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err = x509.CreateCertificate(rand.Reader, leaf, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	kd, _ := x509.MarshalECPrivateKey(key)
	tlsCert, err := tls.X509KeyPair(append(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), root...), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kd}))
	if err != nil {
		t.Fatal(err)
	}
	addrs, _ := net.InterfaceAddrs()
	host := ""
	for _, a := range addrs {
		ip, _, err := net.ParseCIDR(a.String())
		if err == nil && ip.To4() != nil && !ip.IsLoopback() {
			host = ip.String()
			break
		}
	}
	if host == "" {
		t.Fatal("container IPv4 required")
	}
	hosts, err := os.OpenFile("/etc/hosts", os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fmt.Fprintf(hosts, "\n%s node-center.e2e.test\n", host)
	_ = hosts.Close()
	router := e.s.Router()
	secure := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Make real download activity observable while navigating and refreshing.
		if r.URL.Path == "/api/v1/node-test/download" {
			time.Sleep(3 * time.Second)
		}
		router.ServeHTTP(w, r)
	}))
	_ = secure.Listener.Close()
	secure.Listener, err = net.Listen("tcp", net.JoinHostPort(host, "0"))
	if err != nil {
		t.Fatal(err)
	}
	secure.TLS = &tls.Config{Certificates: []tls.Certificate{tlsCert}}
	secure.StartTLS()
	t.Cleanup(secure.Close)
	origin := "https://" + net.JoinHostPort("node-center.e2e.test", fmt.Sprint(secure.Listener.Addr().(*net.TCPAddr).Port))
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(root)
	hc := &http.Client{Jar: e.admin.Jar, Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}, Timeout: 30 * time.Second}
	for _, op := range []struct{ method, path, body string }{
		{"POST", "/api/v1/auth/login", `{"email":"admin@e2e.test","password":"E2eAdmin1"}`},
		{"PUT", "/api/v1/admin/center-origin", fmt.Sprintf(`{"origin":%q}`, origin)},
	} {
		req, _ := http.NewRequest(op.method, origin+op.path, bytes.NewBufferString(op.body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		resp, err := hc.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("%s = %d", op.path, resp.StatusCode)
		}
	}
	e.startGate("node-tests-browser")
	e.s.Store.Ent.Gate.UpdateOneID(e.gateID).SetPublicHost("127.0.0.1").ExecX(e.ctx)
	e.post("/api/v1/admin/nodes", map[string]any{"gate_id": e.gateID, "preset": "ss2022", "name": "下载与 UDP 实测", "skip_probe": true}, nil)
	e.post("/api/v1/admin/nodes", map[string]any{"gate_id": e.gateID, "preset": "hysteria2", "name": "Hysteria2 外部检查", "skip_probe": true}, nil)
	bad := e.post("/api/v1/admin/nodes", map[string]any{"gate_id": e.gateID, "preset": "http-local", "name": "认证失败恢复", "skip_probe": true}, nil)
	e.post("/api/v1/admin/users", map[string]any{"unlimited": true, "email": "node-user@e2e.test", "password": "NodeUser2026", "all_nodes": true, "allow_chain": true}, nil)
	e.post("/api/v1/admin/users", map[string]any{"unlimited": true, "email": "empty-user@e2e.test", "password": "EmptyUser2026", "all_nodes": false}, nil)
	e.applied("node browser fixture")
	e.startGate("第二台 VPS")
	e.s.Store.Ent.Gate.UpdateOneID(e.gateID).SetPublicHost("127.0.0.1").ExecX(e.ctx)
	e.post("/api/v1/admin/nodes", map[string]any{"gate_id": e.gateID, "preset": "ss2022", "name": "跨 VPS 成员", "skip_probe": true}, nil)
	e.applied("second gate browser fixture")
	e.s.StartBackground(e.ctx)
	e.s.Store.Ent.Credential.Update().Where(credential.NodeIDEQ(int(bad["id"].(float64)))).SetPassword("invalid-browser-fixture-password").ExecX(e.ctx)
	upstream, _ := url.Parse(e.url)
	proxy := &httputil.ReverseProxy{Rewrite: func(r *httputil.ProxyRequest) { r.SetURL(upstream); r.Out.Host = r.In.Host }}
	srv := &http.Server{Addr: ":19810", Handler: proxy, ReadHeaderTimeout: 5 * time.Second}
	t.Cleanup(func() { _ = srv.Close() })
	seconds, _ := strconv.Atoi(os.Getenv("WEIR_E2E_BROWSER_SECONDS"))
	if seconds < 1 || seconds > 3600 {
		seconds = 900
	}
	timer := time.AfterFunc(time.Duration(seconds)*time.Second, func() { _ = srv.Close() })
	defer timer.Stop()
	t.Log("node browser fixture ready on :19810", e.dir)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		t.Fatal(err)
	}
}
