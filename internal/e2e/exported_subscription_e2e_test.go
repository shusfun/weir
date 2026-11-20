//go:build e2e

package e2e

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
	"gopkg.in/yaml.v3"

	"github.com/shusfun/weir/internal/gate"
	"github.com/shusfun/weir/packages/contracts"
)

func TestExportedSubscriptionRealClients(t *testing.T) {
	dir := os.Getenv("WEIR_E2E_SUBSCRIPTION_DIR")
	target := os.Getenv("WEIR_E2E_PROBE_URL")
	dnsAddress := "1.1.1.1:53"
	var roots *x509.CertPool
	var helper *gate.Helper
	dnsName := "example.com."
	if dir == "" {
		dir, target, dnsAddress, roots, helper = seedExportedSubscriptionFixture(t)
		dnsName = "subscription-export.e2e.dns."
	}
	parsed, err := url.Parse(target)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil {
		t.Fatal("an explicit HTTPS probe URL is required")
	}
	runID := time.Now().UTC().Format("20060102T150405Z")
	t.Logf("run=%s stage=begin", runID)
	defer func() { t.Logf("run=%s stage=complete failed=%t", runID, t.Failed()) }()
	var sb struct {
		Outbounds []map[string]any `json:"outbounds"`
	}
	raw := mustRead(t, filepath.Join(dir, "singbox-explicit.txt"))
	if json.Unmarshal(raw, &sb) != nil {
		t.Fatal("invalid sing-box export")
	}
	var provider struct {
		Proxies []map[string]any `yaml:"proxies"`
	}
	if yaml.Unmarshal(mustRead(t, filepath.Join(dir, "clash-nodes.yaml")), &provider) != nil {
		t.Fatal("invalid Clash provider export")
	}
	for _, core := range []string{"mihomo", "sing-box"} {
		t.Run(core, func(t *testing.T) {
			bin := os.Getenv("WEIR_E2E_MIHOMO")
			name := "clash-explicit.txt"
			args := []string{"-t", "-d", dir, "-f", filepath.Join(dir, name)}
			nodes := provider.Proxies
			if core == "sing-box" {
				bin = os.Getenv("WEIR_E2E_SINGBOX")
				name = "singbox-explicit.txt"
				args = []string{"check", "-c", filepath.Join(dir, name)}
				nodes = sb.Outbounds
			}
			data := mustRead(t, filepath.Join(dir, name))
			if roots != nil && core == "mihomo" {
				seedSubscriptionGeoIP(t, dir)
			}
			output, checkErr := exec.Command(bin, args...).CombinedOutput()
			if err := os.WriteFile(filepath.Join(dir, core+"-raw-check.log"), output, 0600); err != nil {
				t.Fatal(err)
			}
			t.Logf("run=%s raw_sha256=%x validation_ok=%t", runID, sha256.Sum256(data), checkErr == nil)
			if checkErr != nil {
				t.Fatal("original configuration rejected; inspect private native log")
			}
			tested := 0
			for i, node := range nodes {
				if node["server"] == nil {
					continue
				}
				tested++
				t.Run(fmt.Sprintf("node-%d", i), func(t *testing.T) {
					summary := map[string]any{"name": node["name"], "type": node["type"], "server": node["server"], "port": node["port"], "cipher": node["cipher"], "udp": node["udp"]}
					t.Logf("run=%s client=%s node_summary=%v", runID, core, summary)
					port := freePort(t)
					cfg := map[string]any{}
					if core == "sing-box" {
						cfg = map[string]any{"log": map[string]any{"level": "warn"}, "inbounds": []any{map[string]any{"type": "mixed", "listen": "127.0.0.1", "listen_port": port}}, "outbounds": []any{node}, "route": map[string]any{"final": node["tag"]}}
					} else {
						cfg = map[string]any{"mixed-port": port, "allow-lan": false, "bind-address": "127.0.0.1", "mode": "rule", "log-level": "debug", "proxies": []any{node}, "rules": []string{"MATCH," + node["name"].(string)}}
					}
					path := filepath.Join(t.TempDir(), "runtime.json")
					b, _ := json.Marshal(cfg)
					if err := os.WriteFile(path, b, 0600); err != nil {
						t.Fatal(err)
					}
					args := []string{"-d", dir, "-f", path}
					if core == "sing-box" {
						args = []string{"run", "-c", path}
					}
					log, err := os.OpenFile(filepath.Join(dir, fmt.Sprintf("%s-node-%d.log", core, i)), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
					if err != nil {
						t.Fatal(err)
					}
					defer log.Close()
					cmd := exec.Command(bin, args...)
					cmd.Stdout, cmd.Stderr = log, log
					if err := cmd.Start(); err != nil {
						t.Fatal(err)
					}
					defer func() {
						cmd.Process.Kill()
						cmd.Wait()
						if !t.Failed() {
							return
						}
						output, _ := os.ReadFile(log.Name())
						text := string(output)
						if len(text) > 8192 {
							text = text[len(text)-8192:]
						}
						t.Logf("run=%s client=%s type=%v diagnostics=%v log=%q", runID, core, node["type"], nativeFailureSummary(string(output)), text)
						if helper != nil {
							t.Logf("run=%s gate=sing-box diagnostics=%v", runID, nativeFailureSummary(strings.Join(helper.Sup.Logs("sing-box"), "\n")))
						}
					}()
					eventually(t, 5*time.Second, "client loopback listener", func() bool {
						c, e := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 100*time.Millisecond)
						if e != nil {
							return false
						}
						c.Close()
						return true
					})
					proxyURL, _ := url.Parse(fmt.Sprintf("socks5://127.0.0.1:%d", port))
					transport := &http.Transport{Proxy: http.ProxyURL(proxyURL), DisableKeepAlives: true,
						TLSClientConfig: &tls.Config{RootCAs: roots}}
					defer transport.CloseIdleConnections()
					client := &http.Client{Transport: transport, Timeout: 15 * time.Second}
					start := time.Now()
					var response *http.Response
					var body []byte
					var fetchErr error
					eventually(t, 10*time.Second, "native TCP path", func() bool {
						response, fetchErr = client.Get(target)
						if fetchErr != nil {
							return false
						}
						body, fetchErr = io.ReadAll(io.LimitReader(response.Body, 4096))
						response.Body.Close()
						return fetchErr == nil && response.StatusCode == http.StatusOK
					})
					if response == nil || fetchErr != nil || response.StatusCode != http.StatusOK {
						t.Errorf("run=%s TCP failed: %v", runID, fetchErr)
						return
					}
					var address struct {
						IP string `json:"ip"`
					}
					_ = json.Unmarshal(body, &address)
					t.Logf("run=%s protocol=%v TCP_status=%d elapsed_ms=%d egress=%s", runID, node["type"], response.StatusCode, time.Since(start).Milliseconds(), address.IP)
					if fetchErr != nil || response.StatusCode != 200 {
						t.Error("probe HTTP response failed")
					}
					query := dnsmessage.Message{Header: dnsmessage.Header{ID: uint16(port), RecursionDesired: true}, Questions: []dnsmessage.Question{{Name: dnsmessage.MustNewName(dnsName), Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}}}
					packet, _ := query.Pack()
					reply, elapsed, err := socks5UDP(port, dnsAddress, packet, 5*time.Second)
					if err != nil {
						if roots != nil {
							t.Fatalf("fixture UDP transfer failed: %v", err)
						}
						t.Logf("run=%s UDP unavailable elapsed_ms=%d; see node capability", runID, elapsed.Milliseconds())
						return
					}
					answer := new(dnsmessage.Message)
					if err := answer.Unpack(reply); err != nil {
						t.Fatalf("run=%s UDP DNS decode failed: %v", runID, err)
					}
					if !answer.Response || answer.ID != query.ID || answer.RCode != dnsmessage.RCodeSuccess || len(answer.Answers) == 0 {
						t.Fatalf("run=%s UDP DNS response invalid: rcode=%s answers=%d", runID, answer.RCode, len(answer.Answers))
					}
					if roots != nil {
						record, ok := answer.Answers[0].Body.(*dnsmessage.AResource)
						if !ok || record.A != [4]byte{127, 0, 0, 1} || answer.Answers[0].Header.Name != query.Questions[0].Name {
							t.Fatalf("run=%s UDP DNS answer did not come from the fixture", runID)
						}
					}
					t.Logf("run=%s UDP passed answers=%d", runID, len(answer.Answers))
				})
			}
			if tested == 0 {
				t.Fatal("exported subscription contains no testable nodes")
			}
		})
	}
}

func nativeFailureSummary(output string) map[string]int {
	output = strings.ToLower(output)
	counts := map[string]int{"lines": strings.Count(output, "\n")}
	for _, hint := range []string{"refused", "i/o timeout", "no route", "eof", "closed", "reset by peer", "invalid", "decrypt", "authenticate", "salt", "loopback", "match", "rule", "proxy connection"} {
		if count := strings.Count(output, hint); count > 0 {
			counts[hint] = count
		}
	}
	return counts
}

func seedExportedSubscriptionFixture(t *testing.T) (string, string, string, *x509.CertPool, *gate.Helper) {
	t.Helper()
	e := setup(t)
	e.seedSingbox()
	helper := e.startGate("native-exports")
	eventually(t, 20*time.Second, "gate core capability", func() bool {
		capabilities, err := e.s.Store.Tasks.Capabilities(e.ctx, e.gateID)
		return err == nil && capabilities.Tasks[contracts.TaskCoreInstall] > 0
	})
	e.s.Store.Ent.Gate.UpdateOneID(e.gateID).SetPublicHost("127.0.0.1").ExecX(e.ctx)
	node := e.post("/api/v1/admin/nodes", map[string]any{"gate_id": e.gateID, "preset": "ss2022", "skip_probe": true}, nil)
	account := e.post("/api/v1/admin/users", map[string]any{"email": "export@e2e.test", "unlimited": true,
		"node_ids": []any{node["id"]}}, nil)
	e.applied("configuration for subscription export")
	profiles := e.post(fmt.Sprintf("/api/v1/admin/users/%d/links", int(account["id"].(float64))),
		map[string]any{"name": "native export", "node_ids": []any{node["id"]}}, nil)
	basePath := path.Join("/sub", profiles["token"].(string))
	dir := t.TempDir()
	for filename, suffix := range map[string]string{
		"singbox-explicit.txt": "?target=sing-box",
		"clash-explicit.txt":   "?target=clash",
		"clash-nodes.yaml":     "/provider/single",
	} {
		response, err := e.admin.Get(e.url + basePath + suffix)
		if err != nil {
			t.Fatalf("%s export request failed", filename)
		}
		raw, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil || response.StatusCode != 200 {
			t.Fatalf("%s export status=%d trace=%s", filename, response.StatusCode, response.Header.Get("X-Trace-Id"))
		}
		if err := os.WriteFile(filepath.Join(dir, filename), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ip":"127.0.0.1"}`))
	}))
	t.Cleanup(target.Close)
	roots := x509.NewCertPool()
	roots.AddCert(target.Certificate())
	dns, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dns.Close() })
	lab := &dnsLab{t: t, answer: net.ParseIP("127.0.0.1")}
	go lab.serveUDP(dns, "exports")
	return dir, target.URL, dns.LocalAddr().String(), roots, helper
}
