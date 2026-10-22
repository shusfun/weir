//go:build e2e

package e2e

import (
	"bytes"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
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
	"sync/atomic"
	"testing"
	"time"

	"github.com/imzyb/MiGate/internal/kernels"
)

func (e *env) get(path string, header map[string]string) ([]byte, http.Header) {
	e.t.Helper()
	req, _ := http.NewRequest("GET", e.url+path, nil)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := e.admin.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		e.t.Fatalf("GET %s = %d %s", path, resp.StatusCode, b)
	}
	return b, resp.Header
}

// fetchVia 经本机 SOCKS5（客户端内核的入站）下载并核对长度。
func fetchVia(port int, target string, size int) error {
	proxy, _ := url.Parse(fmt.Sprintf("socks5://127.0.0.1:%d", port))
	hc := &http.Client{Timeout: 8 * time.Second, Transport: &http.Transport{Proxy: http.ProxyURL(proxy), DisableKeepAlives: true}}
	resp, err := hc.Get(target)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	n, err := io.Copy(io.Discard, resp.Body)
	if err == nil && n != int64(size) {
		err = fmt.Errorf("short body %d", n)
	}
	return err
}

// socksServer 是一个最小的无认证 SOCKS5 服务（模拟家宽出口），记录经过它的连接数。
func socksServer(t *testing.T) (int, *atomic.Int64) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	var count atomic.Int64
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 262)
				if _, err := io.ReadFull(c, buf[:2]); err != nil {
					return
				}
				io.ReadFull(c, buf[:buf[1]])
				c.Write([]byte{5, 0})
				if _, err := io.ReadFull(c, buf[:4]); err != nil {
					return
				}
				var host string
				switch buf[3] {
				case 1:
					io.ReadFull(c, buf[:4])
					host = net.IP(buf[:4]).String()
				case 3:
					io.ReadFull(c, buf[:1])
					n := int(buf[0])
					io.ReadFull(c, buf[:n])
					host = string(buf[:n])
				case 4:
					io.ReadFull(c, buf[:16])
					host = net.IP(buf[:16]).String()
				}
				io.ReadFull(c, buf[:2])
				up, err := net.Dial("tcp", net.JoinHostPort(host, strconv.Itoa(int(binary.BigEndian.Uint16(buf[:2])))))
				if err != nil {
					c.Write([]byte{5, 1, 0, 1, 0, 0, 0, 0, 0, 0})
					return
				}
				defer up.Close()
				count.Add(1)
				c.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0})
				go io.Copy(up, c)
				io.Copy(c, up)
			}(c)
		}
	}()
	return l.Addr().(*net.TCPAddr).Port, &count
}

// runSingboxClient 以订阅里的一个出口启动 sing-box 客户端，返回本机 SOCKS 端口。
func (e *env) runSingboxClient(bin string, outbound map[string]any) int {
	e.t.Helper()
	port := freePort(e.t)
	tag, _ := outbound["tag"].(string)
	cfg := map[string]any{
		"log":       map[string]any{"level": "warn"},
		"inbounds":  []any{map[string]any{"type": "mixed", "listen": "127.0.0.1", "listen_port": port}},
		"outbounds": []any{outbound},
		"route":     map[string]any{"final": tag},
	}
	b, _ := json.Marshal(cfg)
	path := filepath.Join(e.dir, fmt.Sprintf("client-%d.json", port))
	os.WriteFile(path, b, 0600)
	cmd := exec.Command(bin, "run", "-c", path)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	return port
}

// 四步建节点 → 一个窗口建用户 → 订阅 → 真实 sing-box 客户端逐个连接（REALITY / SS2022 / Hysteria2 / TUIC /
// 节点链经家宽 SOCKS5 / 用户链经另一节点），并核对按用户计量。
func TestWizardSubscriptionRealClients(t *testing.T) {
	sb := os.Getenv("MIGATE_E2E_SINGBOX")
	if sb == "" {
		t.Skip("MIGATE_E2E_SINGBOX not set")
	}
	e := setup(t)
	data, err := os.ReadFile(sb)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = (kernels.Cache{Dir: filepath.Join(e.dir, "center-data", "kernels")}).Put("sing-box", "1.14.3", "amd64", data); err != nil {
		t.Fatal(err)
	}
	e.startGate("hk")
	ctx := e.ctx
	g := e.s.Store.Ent.Gate.GetX(ctx, e.gateID)
	if g.PublicHost != "127.0.0.1" {
		e.s.Store.Ent.Gate.UpdateOneID(g.ID).SetPublicHost("127.0.0.1").ExecX(ctx)
	}

	dest := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("dest")) }))
	dest.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	dest.StartTLS()
	t.Cleanup(dest.Close)
	homePort, homeHits := socksServer(t)

	node := func(body map[string]any) map[string]any {
		body["gate_id"], body["skip_probe"] = e.gateID, true
		return e.post("/api/v1/admin/nodes", body, nil)
	}
	reality := node(map[string]any{"preset": "vless-reality", "name": "R", "settings": map[string]any{"reality_dest": dest.Listener.Addr().String(), "reality_server_names": "e2e.test"}})
	node(map[string]any{"preset": "ss2022", "name": "SS"})
	hy2 := node(map[string]any{"preset": "hysteria2", "name": "HY2"})
	node(map[string]any{"preset": "tuic", "name": "TUIC"})
	node(map[string]any{"preset": "ss2022", "name": "HOME", "chain": map[string]any{"kind": "socks5", "address": "127.0.0.1", "port": homePort}})
	node(map[string]any{"preset": "ss2022", "name": "VIA-R", "chain": map[string]any{"kind": "node", "node_id": reality["id"]}})

	u := e.post("/api/v1/admin/users", map[string]any{"email": "user@e2e.test", "password": "E2eUser1"}, nil)
	uid := int(u["id"].(float64))
	e.post(fmt.Sprintf("/api/v1/admin/users/%d/chains", uid), map[string]any{"name": "USER-CHAIN", "entry_node_id": hy2["id"], "exit": map[string]any{"kind": "node", "node_id": reality["id"]}, "skip_probe": true}, nil)
	var detail map[string]any
	b, _ := e.get(fmt.Sprintf("/api/v1/admin/users/%d", uid), nil)
	json.Unmarshal(b, &detail)
	token := detail["link_items"].([]any)[0].(map[string]any)["token"].(string)
	e.applied("nodes and user")

	body, header := e.get("/sub/"+token, map[string]string{"User-Agent": "sing-box/1.14.3"})
	if header.Get("Subscription-Userinfo") == "" {
		t.Fatal("missing Subscription-Userinfo")
	}
	var doc struct {
		Outbounds []map[string]any `json:"outbounds"`
	}
	if err = json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("sing-box subscription: %v\n%s", err, body)
	}
	blob := bytes.Repeat([]byte("m"), 1<<20)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write(blob) }))
	t.Cleanup(target.Close)

	seen := map[string]bool{}
	for _, ob := range doc.Outbounds {
		if ob["type"] == "selector" {
			continue
		}
		tag := ob["tag"].(string)
		seen[tag] = true
		port := e.runSingboxClient(sb, ob)
		before := homeHits.Load()
		eventually(t, 25*time.Second, "proxied download via "+tag, func() bool { return fetchVia(port, target.URL, len(blob)) == nil })
		if tag == "HOME" && homeHits.Load() == before {
			t.Fatal("HOME node traffic did not leave through the home SOCKS5 exit")
		}
		t.Logf("%s ok", tag)
	}
	for _, want := range []string{"R", "SS", "HY2", "TUIC", "HOME", "VIA-R", "USER-CHAIN"} {
		if !seen[want] {
			t.Fatalf("subscription missing %s: %v", want, seen)
		}
	}
	eventually(t, 20*time.Second, "per-user metering", func() bool {
		return e.s.Store.Ent.User.GetX(ctx, uid).Used >= int64(len(blob))*int64(len(seen))
	})
	t.Logf("metered %d bytes for %d nodes", e.s.Store.Ent.User.GetX(ctx, uid).Used, len(seen))
}
