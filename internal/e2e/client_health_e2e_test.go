//go:build e2e

package e2e

import (
	"bufio"
	"crypto/tls"
	"crypto/x509"
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
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/subout"
	"github.com/shusfun/weir/packages/contracts"
	"gopkg.in/yaml.v3"
)

// All entry relays terminate at one exit. Targets reject any connection that did
// not originate from that exit, including a client's accidental DIRECT fallback.
type healthNetwork struct {
	targets                [4]*httptest.Server
	entries                [4]*httptest.Server
	modes                  [4]atomic.Int32 // 0 healthy, 1 HTTP 502, 2 reset, 3 timeout, 4 slow payload
	delays                 [4]atomic.Int64
	heads                  [4]atomic.Int64
	leaks, exitConnections atomic.Int64
	ca                     []byte
	items                  []subout.Item
}

func healthTunnel(w http.ResponseWriter, upstream net.Conn) {
	client, buffered, err := w.(http.Hijacker).Hijack()
	if err != nil {
		upstream.Close()
		return
	}
	defer client.Close()
	defer upstream.Close()
	if _, err := buffered.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return
	}
	if err := buffered.Flush(); err != nil {
		return
	}
	done := make(chan struct{})
	go func() { io.Copy(upstream, buffered); upstream.Close(); close(done) }()
	io.Copy(client, upstream)
	client.Close()
	<-done
}

func newHealthNetwork(t *testing.T) *healthNetwork {
	t.Helper()
	n := &healthNetwork{}
	for i := range n.targets {
		i := i
		n.targets[i] = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			host, _, _ := net.SplitHostPort(r.RemoteAddr)
			if host != "127.0.0.2" {
				n.leaks.Add(1)
				http.Error(w, "unexpected source", 403)
				return
			}
			switch n.modes[i].Load() {
			case 1:
				http.Error(w, "unhealthy", 502)
				return
			case 2:
				conn, _, err := w.(http.Hijacker).Hijack()
				if err == nil {
					if tlsConn, ok := conn.(*tls.Conn); ok {
						if tcp, ok := tlsConn.NetConn().(*net.TCPConn); ok {
							tcp.SetLinger(0)
						}
					}
					conn.Close()
				}
				return
			case 3:
				<-r.Context().Done()
				return
			}
			if r.Method == http.MethodHead {
				n.heads[i].Add(1)
				time.Sleep(time.Duration(n.delays[i].Load()) * time.Millisecond)
				w.WriteHeader(204)
				return
			}
			if n.modes[i].Load() == 4 {
				time.Sleep(300 * time.Millisecond)
			}
			fmt.Fprintf(w, "entry-%d", i)
		}))
		t.Cleanup(n.targets[i].Close)
	}
	n.ca = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: n.targets[0].Certificate().Raw})
	exit := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i, err := strconv.Atoi(r.Header.Get("X-Fixture-Entry"))
		if r.Method != http.MethodConnect || err != nil || i < 0 || i >= len(n.targets) {
			http.Error(w, "invalid fixture route", 400)
			return
		}
		dialer := net.Dialer{Timeout: time.Second, LocalAddr: &net.TCPAddr{IP: net.ParseIP("127.0.0.2")}}
		conn, err := dialer.DialContext(r.Context(), "tcp", strings.TrimPrefix(n.targets[i].URL, "https://"))
		if err != nil {
			http.Error(w, "exit unavailable", 502)
			return
		}
		n.exitConnections.Add(1)
		healthTunnel(w, conn)
	}))
	t.Cleanup(exit.Close)
	for i := range n.entries {
		i := i
		n.entries[i] = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodConnect {
				http.Error(w, "CONNECT required", 405)
				return
			}
			conn, err := net.DialTimeout("tcp", strings.TrimPrefix(exit.URL, "http://"), time.Second)
			if err != nil {
				http.Error(w, "exit unavailable", 502)
				return
			}
			fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\nX-Fixture-Entry: %d\r\n\r\n", r.Host, r.Host, i)
			response, err := http.ReadResponse(bufio.NewReader(conn), r)
			if err != nil || response.StatusCode != 200 {
				conn.Close()
				http.Error(w, "exit failed", 502)
				return
			}
			healthTunnel(w, conn)
		}))
		t.Cleanup(n.entries[i].Close)
		n.items = append(n.items, subout.Item{Ref: fmt.Sprintf("chain:%d", i+1), Link: n.entries[i].URL + fmt.Sprintf("#entry-%d", i), UDP: healthBoolPointer(false)})
	}
	return n
}

func healthBoolPointer(value bool) *bool { return &value }

type healthClient struct {
	controller string
	traffic    *http.Client
	control    *http.Client
}

func (c *healthClient) get(path string) map[string]any {
	resp, err := c.control.Get(c.controller + path)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	var value map[string]any
	if json.NewDecoder(resp.Body).Decode(&value) != nil {
		return nil
	}
	return value
}

func (c *healthClient) selected() string {
	v, _ := c.get("/proxies/" + url.PathEscape("链式 · 自动选择"))["now"].(string)
	return v
}

func (c *healthClient) body(target string) (string, error) {
	resp, err := c.traffic.Get(target)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	return string(body), err
}

func (c *healthClient) health() map[string]bool {
	return c.providerHealth("p-group-1")
}

func (c *healthClient) providerHealth(provider string) map[string]bool {
	result := map[string]bool{}
	v := c.get("/providers/proxies/" + provider)
	proxies, _ := v["proxies"].([]any)
	for _, raw := range proxies {
		p := raw.(map[string]any)
		name, _ := p["name"].(string)
		history, _ := p["history"].([]any)
		if len(history) == 0 {
			continue
		}
		alive, _ := p["alive"].(bool)
		// URL-specific health includes expected-status rejection.
		extra, _ := p["extra"].(map[string]any)
		for _, raw := range extra {
			alive = raw.(map[string]any)["alive"].(bool)
		}
		result[name] = alive
	}
	return result
}

func startHealthClient(t *testing.T, n *healthNetwork, interval int, providerMode *atomic.Int32) *healthClient {
	t.Helper()
	plan := &subout.Plan{TestURL: n.targets[0].URL, Groups: []contracts.ProxyGroup{
		{ID: 1, Name: "链式", Kind: "url-test", Members: []string{"chain:1", "chain:2", "chain:3"}, IntervalSeconds: interval, ToleranceMS: 50},
		{ID: 2, Name: "其他组", Kind: "url-test", Members: []string{"chain:4"}, IntervalSeconds: interval, ToleranceMS: 50},
	}}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state := providerMode.Load()
		if r.URL.Path == "/group-2" {
			state = 0 // A healthy out-of-group route must never become a fallback.
		}
		switch state {
		case 1:
			http.Error(w, "provider unavailable", 503)
			return
		case 2:
			fmt.Fprint(w, "proxies: []\n")
			return
		}
		body, err := subout.ClashProvider(n.items, plan, strings.TrimPrefix(r.URL.Path, "/"))
		if err != nil {
			http.Error(w, "fixture render failed", 500)
			return
		}
		w.Write(body)
	}))
	t.Cleanup(provider.Close)
	plan.Providers = func(key string) string { return provider.URL + "/" + key }
	body, _, err := subout.RenderPlan(n.items, "mihomo", plan)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := yaml.Unmarshal(body, &cfg); err != nil {
		t.Fatal(err)
	}
	controller, port := freePort(t), freePort(t)
	cfg["external-controller"], cfg["mixed-port"] = fmt.Sprintf("127.0.0.1:%d", controller), port
	cfg["log-level"], cfg["unified-delay"] = "debug", true
	// Bootstrap only the subscription endpoint directly. The local traffic target
	// must enter PROXY: a blanket LAN exemption would mask an escape.
	providerURL, _ := url.Parse(provider.URL)
	cfg["rules"] = []string{"DST-PORT," + providerURL.Port() + ",DIRECT", "MATCH,PROXY"}
	cfg["profile"] = map[string]any{"store-selected": false}
	dir := t.TempDir()
	file, ca := filepath.Join(dir, "config.json"), filepath.Join(dir, "ca.pem")
	body, _ = json.Marshal(cfg)
	if err := os.WriteFile(file, body, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ca, n.ca, 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Getenv("WEIR_E2E_MIHOMO"), "-d", dir, "-f", file)
	cmd.Env = append(os.Environ(), "SSL_CERT_FILE="+ca)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	proxy, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", port))
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(n.ca)
	transport := &http.Transport{Proxy: http.ProxyURL(proxy), DisableKeepAlives: true, TLSClientConfig: &tls.Config{RootCAs: pool}}
	t.Cleanup(transport.CloseIdleConnections)
	c := &healthClient{controller: fmt.Sprintf("http://127.0.0.1:%d", controller), control: &http.Client{Timeout: time.Second}, traffic: &http.Client{Transport: transport, Timeout: time.Second}}
	eventually(t, 10*time.Second, "isolated client listening", func() bool { return c.get("/version") != nil })
	return c
}

func TestClientHealthRealKernels(t *testing.T) {
	t.Run("cold_https_status_reset_timeout", func(t *testing.T) {
		n := newHealthNetwork(t)
		n.modes[1].Store(1)
		n.modes[2].Store(2)
		var mode atomic.Int32
		c := startHealthClient(t, n, 2, &mode)
		eventually(t, 15*time.Second, "strict HTTPS 204 health", func() bool {
			h := c.health()
			return len(h) == 3 && h["entry-0"] && !h["entry-1"] && !h["entry-2"]
		})
		if body, err := c.body(n.targets[0].URL); err != nil || body != "entry-0" {
			t.Fatalf("cold HTTPS traffic: %q %v", body, err)
		}
		timeoutStarted := time.Now()
		n.modes[2].Store(3)
		eventually(t, 12*time.Second, "timeout recorded", func() bool {
			p := c.get("/providers/proxies/p-group-1")
			proxies, _ := p["proxies"].([]any)
			if len(proxies) != 3 {
				return false
			}
			for _, raw := range proxies {
				p := raw.(map[string]any)
				if p["name"] != "entry-2" {
					continue
				}
				history, _ := p["history"].([]any)
				if len(history) == 0 {
					return false
				}
				last := history[len(history)-1].(map[string]any)
				stamp, _ := time.Parse(time.RFC3339Nano, last["time"].(string))
				return stamp.After(timeoutStarted.Add(5*time.Second)) && !c.health()["entry-2"]
			}
			return false
		})
		previousHeads := n.heads[0].Load()
		n.modes[0].Store(4)
		eventually(t, 12*time.Second, "slow payload still accepts scheduled probes", func() bool { return n.heads[0].Load() > previousHeads && c.health()["entry-0"] })
		started := time.Now()
		body, err := c.body(n.targets[0].URL)
		if err != nil || body != "entry-0" || time.Since(started) < 300*time.Millisecond || !c.health()["entry-0"] {
			t.Fatal("slow payload must not be reported as probe failure")
		}
		n.modes[0].Store(1)
		eventually(t, 12*time.Second, "all members unhealthy", func() bool { h := c.health(); return len(h) == 3 && !h["entry-0"] && !h["entry-1"] && !h["entry-2"] })
		if _, err := c.body(n.targets[0].URL); err == nil {
			t.Fatal("unexpected success with all members failed")
		}
		if n.leaks.Load() != 0 || n.exitConnections.Load() == 0 {
			t.Fatal("traffic escaped fixed exit")
		}
	})
	t.Run("provider_unavailable_and_empty", func(t *testing.T) {
		for _, state := range []int32{1, 2} {
			t.Run(strconv.Itoa(int(state)), func(t *testing.T) {
				n := newHealthNetwork(t)
				var mode atomic.Int32
				mode.Store(state)
				c := startHealthClient(t, n, 2, &mode)
				eventually(t, 10*time.Second, "out-of-group member is healthy", func() bool { return c.providerHealth("p-group-2")["entry-3"] })
				if _, err := c.body(n.targets[0].URL); err == nil {
					t.Fatal("empty provider must reject")
				}
				if n.leaks.Load() != 0 {
					t.Fatal("empty provider fell back to DIRECT")
				}
			})
		}
	})
	t.Run("scheduled_switch_30_seconds", func(t *testing.T) {
		n := newHealthNetwork(t)
		var mode atomic.Int32
		n.delays[0].Store(10)
		n.delays[1].Store(20)
		n.delays[2].Store(30)
		c := startHealthClient(t, n, 30, &mode)
		eventually(t, 15*time.Second, "all candidates healthy", func() bool { h := c.health(); return len(h) == 3 && h["entry-0"] && h["entry-1"] && h["entry-2"] })
		selected := c.selected()
		i, err := strconv.Atoi(strings.TrimPrefix(selected, "entry-"))
		if err != nil || i < 0 || i >= 3 {
			t.Fatalf("unexpected selection %q", selected)
		}
		// The tunnel still connects: only the scheduled probe's five-second
		// timeout marks the route unhealthy, without a manual healthcheck call.
		n.modes[i].Store(3)
		failedAt := time.Now()
		var body string
		eventually(t, 47*time.Second, "scheduled traffic switch without healthcheck API", func() bool {
			body, err = c.body(n.targets[0].URL)
			return err == nil && body != selected && body != "entry-3"
		})
		t.Logf("fault_at=%s recovered_at=%s elapsed_ms=%d previous=%s actual=%s", failedAt.Format(time.RFC3339Nano), time.Now().Format(time.RFC3339Nano), time.Since(failedAt).Milliseconds(), selected, body)
		// A difference below the configured tolerance must not churn the healthy selection.
		stable := c.selected()
		n.modes[i].Store(0)
		deadline := time.Now().Add(42 * time.Second)
		for time.Now().Before(deadline) {
			if c.selected() != stable {
				t.Fatalf("selection churned inside tolerance: %s -> %s", stable, c.selected())
			}
			time.Sleep(time.Second)
		}
		if n.leaks.Load() != 0 {
			t.Fatal("switch escaped fixed exit")
		}
	})
}
