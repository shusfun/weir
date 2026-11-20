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
	"encoding/json"
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

	"github.com/shusfun/weir/internal/store/ent/resource"
	"github.com/shusfun/weir/packages/contracts"
)

func browserFixtureCertificate(t *testing.T, host string) (tls.Certificate, []byte, []byte, []byte) {
	t.Helper()
	var certPEM, keyPEM, root []byte
	if directory := os.Getenv("WEIR_E2E_BROWSER_TLS_DIR"); directory != "" {
		read := func(name string) []byte {
			data, err := os.ReadFile(filepath.Join(directory, name))
			if err != nil {
				t.Fatal(err)
			}
			return data
		}
		certPEM, keyPEM, root = read("server.pem"), read("server.key"), read("ca.pem")
	} else {
		now := time.Now()
		caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Isolated browser CA"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(24 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
		caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
		if err != nil {
			t.Fatal(err)
		}
		root = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		leaf := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "Isolated browser test"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(23 * time.Hour), IPAddresses: []net.IP{net.ParseIP(host), net.ParseIP("127.0.0.1")}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
		der, err := x509.CreateCertificate(rand.Reader, leaf, ca, &key.PublicKey, caKey)
		if err != nil {
			t.Fatal(err)
		}
		keyDER, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			t.Fatal(err)
		}
		certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
		keyPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	}
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(root) {
		t.Fatal("invalid browser fixture CA")
	}
	intermediates := x509.NewCertPool()
	for _, der := range pair.Certificate[1:] {
		certificate, err := x509.ParseCertificate(der)
		if err != nil {
			t.Fatal(err)
		}
		intermediates.AddCert(certificate)
	}
	for _, name := range []string{host, "127.0.0.1"} {
		if _, err := leaf.Verify(x509.VerifyOptions{Roots: roots, Intermediates: intermediates, DNSName: name}); err != nil {
			t.Fatalf("browser fixture certificate for %s: %v", name, err)
		}
	}
	if contracts.BrowserTestCertificateHash(leaf, time.Now()) == "" {
		t.Fatal("browser fixture requires a valid short-lived P-256 leaf")
	}
	return pair, certPEM, keyPEM, root
}

func TestNodeTestBrowserFixture(t *testing.T) {
	if os.Getenv("WEIR_E2E_NODE_BROWSER") != "1" {
		t.Skip("interactive fixture disabled")
	}
	e := setup(t)
	e.s.AllowPrivateSources = true
	sourceURL, davURL := browserUpstreams(t)

	browserOrigin := os.Getenv("WEIR_E2E_BROWSER_ORIGIN")
	if browserOrigin == "" {
		t.Fatal("isolated browser origin required")
	}
	e.s.Assets = os.DirFS(os.Getenv("WEIR_E2E_CONSOLE"))
	e.seedSingbox()
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
	tlsCert, certPEM, keyPEM, root := browserFixtureCertificate(t, host)
	rootPath := filepath.Join(e.dir, "browser-ca.pem")
	if err := os.WriteFile(rootPath, root, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SSL_CERT_FILE", rootPath)
	if out := os.Getenv("WEIR_E2E_BROWSER_DIR"); out != "" {
		if err := os.WriteFile(filepath.Join(out, "ca.pem"), root, 0600); err != nil {
			t.Fatal(err)
		}
	}
	router := e.s.Router()
	secure := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Make real download activity observable while navigating and refreshing.
		if r.URL.Path == "/api/v1/node-test/download" {
			time.Sleep(3 * time.Second)
		}
		router.ServeHTTP(w, r)
	}))
	_ = secure.Listener.Close()
	listener, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
	if err != nil {
		t.Fatal(err)
	}
	secure.Listener = listener
	secure.TLS = &tls.Config{Certificates: []tls.Certificate{tlsCert}}
	secure.StartTLS()
	t.Cleanup(secure.Close)
	// Native clients may use their own resolver, so the echo endpoint must not
	// depend on the container's hosts file. The certificate authenticates this IP.
	origin := "https://" + net.JoinHostPort(host, fmt.Sprint(secure.Listener.Addr().(*net.TCPAddr).Port))
	e.s.SetOriginForTest(origin)
	e.startGate("node-tests-browser")
	browserGateID := e.gateID
	eventually(t, 20*time.Second, "browser test capabilities received", func() bool {
		capabilities, err := e.s.Store.Tasks.Capabilities(e.ctx, browserGateID)
		return err == nil && capabilities.ConfigFeatures[contracts.ConfigBrowserTest] > 0 &&
			capabilities.ConfigFeatures[contracts.ConfigFirewall] > 0
	})
	cert := e.post("/api/v1/admin/certs", map[string]any{"name": "Browser fixture certificate", "domain": "127.0.0.1", "cert_pem": string(certPEM), "key_pem": string(keyPEM)}, nil)
	// Use the production configuration service, then pin only fixture ports to
	// the Docker mappings. This keeps certificate and capability checks real.
	raw, err := json.Marshal(map[string]any{"enabled": true, "cert_id": cert["id"], "host": "127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest("PUT", e.url+fmt.Sprintf("/api/v1/admin/gates/%d/browser-test", browserGateID), bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := e.admin.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var configured struct {
		Config contracts.BrowserTestPlan `json:"config"`
		Error  struct {
			Code   string `json:"code"`
			Detail string `json:"detail"`
		} `json:"error"`
	}
	decodeErr := json.NewDecoder(resp.Body).Decode(&configured)
	resp.Body.Close()
	if resp.StatusCode != 200 || decodeErr != nil {
		t.Fatalf("browser configuration: %d %s %s %v", resp.StatusCode, configured.Error.Code, configured.Error.Detail, decodeErr)
	}
	row := e.s.Store.Ent.Resource.Query().Where(resource.KindEQ("browser-test-config"), resource.KeyEQ(strconv.Itoa(browserGateID))).OnlyX(e.ctx)
	var saved struct {
		Plan contracts.BrowserTestPlan `json:"plan"`
		Key  string                    `json:"key"`
	}
	data, err := json.Marshal(row.Data)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	saved.Plan.Port, saved.Plan.RTCPort, saved.Plan.Origin = 22443, 22444, browserOrigin
	data, err = json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	var updated map[string]any
	if err = json.Unmarshal(data, &updated); err != nil {
		t.Fatal(err)
	}
	e.s.Store.Ent.Resource.UpdateOne(row).SetData(updated).ExecX(e.ctx)
	e.s.Store.Ent.Gate.UpdateOneID(browserGateID).AddConfigRevision(1).ExecX(e.ctx)
	publicHost := "127.0.0.1"
	if os.Getenv("WEIR_E2E_PUBLIC_NETWORK") == "1" {
		publicHost = host
	}
	e.s.Store.Ent.Gate.UpdateOneID(e.gateID).SetPublicHost(publicHost).ExecX(e.ctx)
	e.post("/api/v1/admin/nodes", map[string]any{"gate_id": e.gateID, "preset": "ss2022", "name": "下载与 UDP 实测", "skip_probe": true}, nil)
	e.post("/api/v1/admin/nodes", map[string]any{"gate_id": e.gateID, "preset": "hysteria2", "name": "Hysteria2 外部检查", "skip_probe": true}, nil)
	e.post("/api/v1/admin/nodes", map[string]any{"gate_id": e.gateID, "preset": "http-local", "name": "HTTP 实测", "skip_probe": true}, nil)
	e.post("/api/v1/admin/users", map[string]any{"unlimited": true, "email": "node-user@e2e.test", "password": "NodeUser2026", "all_nodes": true, "allow_chain": true}, nil)
	e.post("/api/v1/admin/users", map[string]any{"unlimited": true, "email": "empty-user@e2e.test", "password": "EmptyUser2026", "all_nodes": false}, nil)
	e.applied("node browser fixture")
	e.startGate("第二台 VPS")
	e.s.Store.Ent.Gate.UpdateOneID(e.gateID).SetPublicHost(publicHost).ExecX(e.ctx)
	e.post("/api/v1/admin/nodes", map[string]any{"gate_id": e.gateID, "preset": "ss2022", "name": "跨 VPS 成员", "skip_probe": true}, nil)
	e.applied("second gate browser fixture")
	e.s.StartBackground(e.ctx)
	proxyPort, _ := socksServerAt(t, host)
	if out := os.Getenv("WEIR_E2E_BROWSER_DIR"); out != "" {
		data, err := json.Marshal(map[string]any{"run_id": os.Getenv("WEIR_E2E_RUN_ID"), "gate_id": browserGateID, "origin": browserOrigin, "endpoint": "https://127.0.0.1:22443", "source_url": sourceURL, "webdav_url": davURL, "proxy_host": host, "proxy_port": proxyPort})
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(out, "fixture.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
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
	finished := make(chan struct{})
	defer close(finished)
	go func() {
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for {
			select {
			case <-finished:
				return
			case <-tick.C:
				var receipt struct {
					RunID string `json:"run_id"`
				}
				data, err := os.ReadFile(filepath.Join(os.Getenv("WEIR_E2E_BROWSER_DIR"), "result.json"))
				if err == nil && json.Unmarshal(data, &receipt) == nil && receipt.RunID == os.Getenv("WEIR_E2E_RUN_ID") && receipt.RunID != "" {
					_ = srv.Close()
					return
				}
			}
		}
	}()
	t.Log("node browser fixture ready on :19810", e.dir)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		t.Fatal(err)
	}
}
