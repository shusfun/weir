package control

import (
	"context"
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
)

// Exercise converted subscriptions through the pinned real core, including
// authentication failures. Everything listens on loopback and uses test keys.
func TestProxyProtocolLiveClient(t *testing.T) {
	binary := os.Getenv("MIGATE_SINGBOX_BINARY")
	if binary == "" {
		t.Skip("set MIGATE_SINGBOX_BINARY for real core acceptance")
	}
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "migate-proxy-verified") }))
	defer target.Close()
	certificate := httptest.NewTLSServer(http.NotFoundHandler())
	defer certificate.Close()
	dir := t.TempDir()
	certFile := filepath.Join(dir, "test-cert.pem")
	keyFile := filepath.Join(dir, "test-key.pem")
	leaf := certificate.TLS.Certificates[0]
	write := func(path string, data []byte) {
		t.Helper()
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leaf.Certificate[0]}))
	keyDER, err := x509.MarshalPKCS8PrivateKey(leaf.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	write(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}))
	port := func() int {
		t.Helper()
		l, e := net.Listen("tcp", "127.0.0.1:0")
		if e != nil {
			t.Fatal(e)
		}
		p := l.Addr().(*net.TCPAddr).Port
		l.Close()
		return p
	}
	start := func(name string, cfg map[string]any, listenPort int) func() {
		t.Helper()
		file := filepath.Join(dir, name+".json")
		b, e := json.Marshal(cfg)
		if e != nil {
			t.Fatal(e)
		}
		write(file, b)
		cmd := exec.Command(binary, "run", "-c", file)
		if e = cmd.Start(); e != nil {
			t.Fatal(e)
		}
		stop := func() { cmd.Process.Kill(); cmd.Wait() }
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
			c, e := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", listenPort), 100*time.Millisecond)
			if e == nil {
				c.Close()
				return stop
			}
			time.Sleep(30 * time.Millisecond)
		}
		stop()
		t.Fatal("test core listener did not start")
		return nil
	}
	for _, scenario := range []string{"http", "https", "socks5", "anytls", "2022-blake3-aes-128-gcm", "2022-blake3-aes-256-gcm", "2022-blake3-chacha20-poly1305", "2022-blake3-aes-128-gcm-multiuser", "2022-blake3-aes-256-gcm-multiuser"} {
		t.Run(scenario, func(t *testing.T) {
			scheme := strings.TrimSuffix(scenario, "-multiuser")
			multiuser := scheme != scenario
			serverPort := port()
			kind := scheme
			if scheme == "https" {
				kind = "http"
			}
			if scheme == "socks5" {
				kind = "socks"
			}
			in := map[string]any{"type": kind, "tag": "server", "listen": "127.0.0.1", "listen_port": serverPort, "users": []any{map[string]any{"username": "us@er", "password": "pa:ss"}}}
			keySize := 32
			if strings.Contains(scheme, "128") {
				keySize = 16
			}
			if strings.HasPrefix(scheme, "2022-") {
				in["type"] = "shadowsocks"
				in["method"] = scheme
				in["password"] = base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", keySize)))
				delete(in, "users")
				if multiuser {
					in["users"] = []any{map[string]any{"name": "test", "password": base64.StdEncoding.EncodeToString([]byte(strings.Repeat("y", keySize)))}}
				}
			}
			if scheme == "anytls" {
				in["users"] = []any{map[string]any{"name": "test", "password": "pa:ss"}}
			}
			if scheme == "https" || scheme == "anytls" {
				in["tls"] = map[string]any{"enabled": true, "certificate_path": certFile, "key_path": keyFile}
			}
			stop := start(scheme+"-server", map[string]any{"inbounds": []any{in}, "outbounds": []any{map[string]any{"type": "direct"}}}, serverPort)
			defer stop()
			for _, valid := range []bool{true, false} {
				password := "pa:ss"
				if !valid {
					password = "wrong"
				}
				u := url.URL{Scheme: scheme, Host: fmt.Sprintf("127.0.0.1:%d", serverPort), User: url.UserPassword("us@er", password)}
				if scheme == "anytls" {
					u.User = url.User(password)
				}
				if strings.HasPrefix(scheme, "2022-") {
					letter := "x"
					if !valid {
						letter = "y"
					}
					u.Scheme = "ss"
					u.User = url.UserPassword(scheme, base64.StdEncoding.EncodeToString([]byte(strings.Repeat(letter, keySize))))
					if multiuser {
						letter = "y"
						if !valid {
							letter = "z"
						}
						secret := in["password"].(string) + ":" + base64.StdEncoding.EncodeToString([]byte(strings.Repeat(letter, keySize)))
						u.User = url.UserPassword(scheme, secret)
					}
				}
				b, e := singboxOutput([]string{u.String()})
				if e != nil {
					t.Fatal(e)
				}
				var cfg map[string]any
				json.Unmarshal(b, &cfg)
				out := cfg["outbounds"].([]any)[1].(map[string]any)
				if tls, ok := out["tls"].(map[string]any); ok {
					tls["certificate_path"] = certFile
				}
				clientPort := port()
				cfg["inbounds"] = []any{map[string]any{"type": "http", "listen": "127.0.0.1", "listen_port": clientPort}}
				stopClient := start(fmt.Sprintf("%s-client-%t", scheme, valid), cfg, clientPort)
				proxy, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", clientPort))
				transport := &http.Transport{Proxy: http.ProxyURL(proxy)}
				client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				req, _ := http.NewRequestWithContext(ctx, "GET", target.URL, nil)
				resp, requestErr := client.Do(req)
				body := []byte{}
				if requestErr == nil {
					body, _ = io.ReadAll(resp.Body)
					resp.Body.Close()
				}
				cancel()
				transport.CloseIdleConnections()
				stopClient()
				if valid && (requestErr != nil || string(body) != "migate-proxy-verified") {
					t.Fatalf("real converted client failed: %v", requestErr)
				}
				if !valid && string(body) == "migate-proxy-verified" {
					t.Fatal("incorrect credential accepted")
				}
			}
		})
	}
}
