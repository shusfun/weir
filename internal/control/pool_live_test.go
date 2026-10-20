package control

import (
	"bytes"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
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
	"testing"
	"time"

	"github.com/imzyb/MiGate/internal/db"
	"github.com/imzyb/MiGate/internal/singbox"
)

func livePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func liveCore(t *testing.T, binary, name string, cfg any, port int) func() {
	t.Helper()
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), name+".json")
	if err := os.WriteFile(file, b, 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(binary, "check", "-c", file).CombinedOutput(); err != nil {
		t.Fatalf("%s configuration rejected: %s", name, out)
	}
	cmd := exec.Command(binary, "run", "-c", file)
	var logs bytes.Buffer
	cmd.Stderr = &logs
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	stop := func() { cmd.Process.Kill(); cmd.Wait() }
	t.Cleanup(stop)
	if port == 0 {
		time.Sleep(150 * time.Millisecond)
		return stop
	}
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 50*time.Millisecond)
		if err == nil {
			c.Close()
			return stop
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatal(name + " listener unavailable")
	return stop
}

func liveProxyRequest(port int, target string) (string, error) {
	u, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", port))
	transport := &http.Transport{Proxy: http.ProxyURL(u)}
	defer transport.CloseIdleConnections()
	client := http.Client{Transport: transport, Timeout: 3 * time.Second}
	r, err := client.Get(target)
	if err != nil {
		return "", err
	}
	defer r.Body.Close()
	b, err := io.ReadAll(io.LimitReader(r.Body, 1024))
	return string(b), err
}

// This uses the actual managed-outbound builders (not a hand-written client).
// Non-default options, trust and incorrect credentials traverse the real core.
func TestPoolManagedAdvancedLive(t *testing.T) {
	binary := os.Getenv("MIGATE_SINGBOX_BINARY")
	if binary == "" {
		t.Skip("set MIGATE_SINGBOX_BINARY for real managed core tests")
	}
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "managed-pool-verified") }))
	defer target.Close()
	certServer := httptest.NewTLSServer(http.NotFoundHandler())
	defer certServer.Close()
	cert := certServer.TLS.Certificates[0]
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Certificate[0]})
	keyDER, err := x509.MarshalPKCS8PrivateKey(cert.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	certPath, keyPath := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	os.WriteFile(certPath, certPEM, 0600)
	os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0600)
	for _, protocol := range []string{"hysteria2", "tuic", "anytls", "vmess"} {
		t.Run(protocol, func(t *testing.T) {
			serverPort, clientPort := livePort(t), livePort(t)
			uuid := "f25f6387-2c31-43cb-a2a3-e71fc2e194ed"
			in := map[string]any{"type": protocol, "listen": "127.0.0.1", "listen_port": serverPort, "users": []any{map[string]any{"name": "test", "password": "test-only-password"}}, "tls": map[string]any{"enabled": true, "certificate_path": certPath, "key_path": keyPath}}
			settings := db.OutboundSettings{TLS: true, Security: "tls", SNI: "example.com", TrustedCertificate: string(certPEM)}
			if protocol == "hysteria2" {
				settings.Obfs = "salamander"
				settings.ObfsPassword = "test-only-obfs"
				in["obfs"] = map[string]any{"type": "salamander", "password": settings.ObfsPassword}
			}
			if protocol == "tuic" {
				settings.CongestionControl = "bbr"
				settings.ZeroRTT = true
				settings.ALPN = []string{"h3"}
				in["users"] = []any{map[string]any{"uuid": uuid, "password": "test-only-password"}}
				in["congestion_control"] = "bbr"
				in["zero_rtt_handshake"] = true
				in["tls"].(map[string]any)["alpn"] = []string{"h3"}
			}
			if protocol == "anytls" {
				settings.Fingerprint = "chrome"
			}
			if protocol == "vmess" {
				settings = db.OutboundSettings{Network: "tcp", Security: "none", Method: "chacha20-poly1305"}
				delete(in, "tls")
				in["users"] = []any{map[string]any{"uuid": uuid}}
			}
			serverCfg := map[string]any{"inbounds": []any{in}, "outbounds": []any{map[string]any{"type": "direct"}}}
			ready := serverPort
			if protocol == "hysteria2" || protocol == "tuic" {
				ready = 0
			}
			liveCore(t, binary, "server", serverCfg, ready)
			cases := []string{"valid", "credentials", "hostname"}
			if protocol == "vmess" {
				cases = cases[:2]
			}
			for _, scenario := range cases {
				valid := scenario == "valid"
				options := settings
				if scenario == "hostname" {
					options.SNI = "wrong.invalid"
				}
				raw, _ := json.Marshal(options)
				out := db.Outbound{ID: 1, Tag: "pool-live", Protocol: protocol, Address: "127.0.0.1", Port: serverPort, Username: uuid, Password: "test-only-password", SettingsJSON: string(raw), Enabled: true}
				if scenario == "credentials" {
					if protocol == "vmess" {
						out.Username = "3c102e15-e38e-493f-b61a-1ec47c6489ad"
					} else {
						out.Password = "incorrect"
					}
				}
				if _, err := db.ManagedOutboundSettings(out); err != nil {
					t.Fatal(err)
				}
				built, err := singbox.BuildConfigWithOutbounds(nil, []db.Outbound{out}, nil)
				if err != nil {
					t.Fatal(err)
				}
				body, _ := json.Marshal(built)
				var cfg map[string]any
				json.Unmarshal(body, &cfg)
				cfg["inbounds"] = []any{map[string]any{"type": "http", "listen": "127.0.0.1", "listen_port": clientPort}}
				cfg["route"] = map[string]any{"final": db.GeneratedOutboundTag(db.CoreSingbox, 1, out.Tag)}
				stop := liveCore(t, binary, "client-"+scenario, cfg, clientPort)
				bodyText, err := liveProxyRequest(clientPort, target.URL)
				stop()
				if valid && (err != nil || bodyText != "managed-pool-verified") {
					t.Fatalf("actual managed %s connection failed: %v", protocol, err)
				}
				if !valid && bodyText == "managed-pool-verified" {
					t.Fatal("incorrect managed credentials accepted")
				}
			}
		})
	}
}

func TestWireGuardActualTunnel(t *testing.T) {
	binary := os.Getenv("MIGATE_WIREGUARD_BINARY")
	if binary == "" {
		t.Skip("local test client requires with_wireguard and with_gvisor")
	}
	serverKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	clientKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	encode := func(b []byte) string { return base64.StdEncoding.EncodeToString(b) }
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "wireguard-tunnel-verified") }))
	defer target.Close()
	serverPort, clientPort := livePort(t), livePort(t)
	liveCore(t, binary, "wireguard-server", map[string]any{"endpoints": []any{map[string]any{"type": "wireguard", "tag": "wg-server", "system": false, "address": []string{"10.77.0.1/32"}, "private_key": encode(serverKey.Bytes()), "listen_port": serverPort, "peers": []any{map[string]any{"public_key": encode(clientKey.PublicKey().Bytes()), "allowed_ips": []string{"10.77.0.2/32"}}}}}, "outbounds": []any{map[string]any{"type": "direct", "tag": "direct"}}, "route": map[string]any{"final": "direct"}}, 0)
	u := url.URL{Scheme: "wireguard", Host: fmt.Sprintf("127.0.0.1:%d", serverPort), User: url.User(encode(clientKey.Bytes())), Fragment: "actual-wireguard"}
	u.RawQuery = url.Values{"publickey": {encode(serverKey.PublicKey().Bytes())}, "address": {"10.77.0.2/32"}, "allowedips": {"0.0.0.0/0"}, "mtu": {"1280"}}.Encode()
	body, err := singboxOutput([]string{u.String()})
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	json.Unmarshal(body, &cfg)
	cfg["inbounds"] = []any{map[string]any{"type": "http", "listen": "127.0.0.1", "listen_port": clientPort}}
	liveCore(t, binary, "wireguard-client", cfg, clientPort)
	tunnelTarget := strings.Replace(target.URL, "127.0.0.1", "10.77.0.1", 1)
	text, err := liveProxyRequest(clientPort, tunnelTarget)
	if err != nil || text != "wireguard-tunnel-verified" {
		t.Fatalf("actual WireGuard tunnel failed: %v", err)
	}
}
