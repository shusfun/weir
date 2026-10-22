//go:build e2e

// 端到端测试：在 Linux 容器内用真实 Xray 跑通「中心 → 配对 → 下发 → 真实客户端连接 → 计量 → 额度 / 到期执行」。
// 由 deploy/e2e.py 交叉编译并在 debian 容器中运行；MIGATE_E2E_XRAY 指向 Linux 版 Xray。
package e2e

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"github.com/imzyb/MiGate/internal/center"
	"github.com/imzyb/MiGate/internal/gate"
	"github.com/imzyb/MiGate/internal/kernels"
	"github.com/imzyb/MiGate/internal/store/ent"
	"github.com/imzyb/MiGate/internal/xray"
)

type env struct {
	t      *testing.T
	s      *center.Server
	url    string
	admin  *http.Client
	xray   string
	dir    string
	ctx    context.Context
	gateID int
}

func (e *env) post(path string, body any, header map[string]string) map[string]any {
	e.t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", e.url+path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := e.admin.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	if resp.StatusCode >= 300 {
		e.t.Fatalf("%s = %d %v", path, resp.StatusCode, out)
	}
	return out
}

func eventually(t *testing.T, timeout time.Duration, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func freePort(t *testing.T) int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// setup 启动中心（SQLite）并通过 Web 接口完成初始化，种入 Xray 内核缓存。
func setup(t *testing.T) *env {
	bin := os.Getenv("MIGATE_E2E_XRAY")
	if bin == "" {
		t.Skip("MIGATE_E2E_XRAY not set")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "center", "config.json")
	cfg, _ := center.LoadConfig(path)
	cfg.DataDir = filepath.Join(dir, "center-data")
	token, err := center.Initialize(path, cfg)
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ = center.LoadConfig(path)
	s, err := center.New(cfg, path, fstest.MapFS{"index.html": {Data: []byte("ok")}})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(s.Router())
	t.Cleanup(srv.Close)
	jar, _ := cookiejar.New(nil)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	e := &env{t: t, s: s, url: srv.URL, admin: &http.Client{Jar: jar, Timeout: 30 * time.Second}, xray: bin, dir: dir, ctx: ctx}
	e.post("/api/v1/setup/install", map[string]any{"store": map[string]any{"driver": "sqlite", "sqlite_path": filepath.Join(dir, "center.db")}, "email": "admin@e2e.test", "password": "E2eAdmin1"}, map[string]string{"Authorization": "Bearer " + token})
	data, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = (kernels.Cache{Dir: filepath.Join(cfg.DataDir, "kernels")}).Put("xray", "26.3.27", "amd64", data); err != nil {
		t.Fatal(err)
	}
	return e
}

// startGate 用「添加 Gate」生成的一次性凭证配对，然后运行辅助程序与 Agent（周期调短）。
func (e *env) startGate(name string) {
	inv := e.post("/api/v1/admin/gates/invite", map[string]string{"name": name}, nil)
	gd := filepath.Join(e.dir, name)
	p := gate.Paths{Config: filepath.Join(gd, "agent.json"), Data: filepath.Join(gd, "data"), Socket: filepath.Join("/tmp", "mg-"+name+".sock"),
		CoresDir: filepath.Join(gd, "cores"), CertsDir: filepath.Join(gd, "certs"), KernelsDir: filepath.Join(gd, "kernels"), Root: filepath.Join(gd, "root")}
	go gate.ServeHelper(e.ctx, gate.NewHelper(p))
	eventually(e.t, 5*time.Second, "helper socket", func() bool { _, err := os.Stat(p.Socket); return err == nil })
	if err := gate.Pair(e.ctx, p, e.url, inv["token"].(string), name, ""); err != nil {
		e.t.Fatal(err)
	}
	a, err := gate.NewAgent(p)
	if err != nil {
		e.t.Fatal(err)
	}
	a.MeterEvery, a.HeartbeatEvery, a.ReportEvery = time.Second, time.Second, 1
	go a.Run(e.ctx)
	eventually(e.t, 10*time.Second, "gate heartbeat", func() bool {
		g, err := e.s.Store.Ent.Gate.Query().Only(e.ctx)
		if err == nil && g.LastSeenAt != nil {
			e.gateID = g.ID
			return true
		}
		return false
	})
}

// applied 等待 Gate 应用到中心最新的配置版本。
func (e *env) applied(what string) {
	e.t.Helper()
	eventually(e.t, 30*time.Second, what, func() bool {
		g, err := e.s.Store.Ent.Gate.Get(e.ctx, e.gateID)
		if err == nil && g.LastError != "" {
			e.t.Logf("gate error: %s", g.LastError)
		}
		return err == nil && g.AppliedRevision == g.ConfigRevision && g.AppliedRevision > 0
	})
}

func TestGateRealTrafficQuotaAndExpiry(t *testing.T) {
	e := setup(t)
	e.startGate("hk")
	ctx := e.ctx
	g, _ := e.s.Store.Ent.Gate.Get(ctx, e.gateID)

	// REALITY 伪装目标：本机一个 TLS 1.3 服务。
	dest := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("dest")) }))
	dest.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	dest.StartTLS()
	t.Cleanup(dest.Close)
	priv, pub, err := xray.GenerateRealityKey()
	if err != nil {
		t.Fatal(err)
	}
	port, err := e.s.AllocateNodePort(ctx, g)
	if err != nil {
		t.Fatal(err)
	}
	n := e.s.Store.Ent.Node.Create().SetGateID(g.ID).SetName("香港").SetKernel("xray").SetProtocol("vless").SetPort(port).SetNetwork("tcp").SetSecurity("reality").
		SetSettings(map[string]any{"reality_dest": dest.Listener.Addr().String(), "reality_server_names": "e2e.test", "reality_private_key": priv, "reality_public_key": pub, "reality_short_id": "a1b2c3d4"}).SaveX(ctx)
	u := e.s.Store.Ent.User.Create().SetEmail("u@e2e.test").SetPasswordHash("x").SaveX(ctx)
	link := e.s.Store.Ent.Link.Create().SetUserID(u.ID).SetToken("e2e-link").SaveX(ctx)
	uuid := "6f1c2a8e-3b4d-4c5e-9f60-7a8b9c0d1e2f"
	e.s.Store.Ent.Credential.Create().SetNodeID(n.ID).SetLinkID(link.ID).SetUserID(u.ID).SetStatsKey("c-e2e").SetUUID(uuid).SaveX(ctx)
	if err = e.s.Rebalance(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	e.applied("first config")

	// 真实 Xray 客户端：本机 SOCKS5 入站 → VLESS + REALITY → Gate。
	socks := freePort(t)
	client := map[string]any{
		"log":      map[string]any{"loglevel": "warning"},
		"inbounds": []any{map[string]any{"listen": "127.0.0.1", "port": socks, "protocol": "socks", "settings": map[string]any{"udp": false}}},
		"outbounds": []any{map[string]any{"protocol": "vless", "settings": map[string]any{"vnext": []any{map[string]any{"address": "127.0.0.1", "port": port, "users": []any{map[string]any{"id": uuid, "encryption": "none", "flow": "xtls-rprx-vision"}}}}},
			"streamSettings": map[string]any{"network": "tcp", "security": "reality", "realitySettings": map[string]any{"serverName": "e2e.test", "publicKey": pub, "shortId": "a1b2c3d4", "fingerprint": "chrome"}}}},
	}
	cb, _ := json.Marshal(client)
	cpath := filepath.Join(e.dir, "client.json")
	os.WriteFile(cpath, cb, 0600)
	cmd := exec.Command(e.xray, "run", "-config", cpath)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })

	blob := bytes.Repeat([]byte("m"), 2<<20)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write(blob) }))
	t.Cleanup(target.Close)
	proxy, _ := url.Parse(fmt.Sprintf("socks5://127.0.0.1:%d", socks))
	fetch := func() error {
		hc := &http.Client{Timeout: 8 * time.Second, Transport: &http.Transport{Proxy: http.ProxyURL(proxy), DisableKeepAlives: true}}
		resp, err := hc.Get(target.URL + "/blob")
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		n, err := io.Copy(io.Discard, resp.Body)
		if err == nil && n != int64(len(blob)) {
			err = fmt.Errorf("short body %d", n)
		}
		return err
	}
	eventually(t, 20*time.Second, "first proxied download", func() bool { return fetch() == nil })

	// 计量：中心记到这次下载（≥ 2 MiB）。
	eventually(t, 20*time.Second, "metering", func() bool { return e.s.Store.Ent.User.GetX(ctx, u.ID).Used >= int64(len(blob)) })
	used := e.s.Store.Ent.User.GetX(ctx, u.ID).Used
	t.Logf("metered %d bytes", used)

	// 额度：只剩 1 MiB，再下载 2 MiB 后 Gate 本地停用该用户，新连接失败。
	e.s.Store.Ent.User.UpdateOneID(u.ID).SetQuota(used + 1<<20).ExecX(ctx)
	if err = e.s.Rebalance(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	e.applied("quota config")
	_ = fetch()
	eventually(t, 20*time.Second, "quota enforcement", func() bool { return fetch() != nil })

	// 续额度（不限）后恢复。
	e.s.Store.Ent.User.UpdateOneID(u.ID).SetQuota(0).ExecX(ctx)
	if err = e.s.Rebalance(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	e.applied("unlimited config")
	eventually(t, 20*time.Second, "recovery after renewal", func() bool { return fetch() == nil })

	// 到期：配置里写明 3 秒后到期；到点时中心不下发新配置，由 Gate 本地剔除。
	e.s.Store.Ent.User.UpdateOneID(u.ID).SetExpiresAt(time.Now().Add(3 * time.Second)).ExecX(ctx)
	if err = e.s.Rebalance(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	e.applied("expiry config")
	rev := e.s.Store.Ent.Gate.GetX(ctx, e.gateID).ConfigRevision
	eventually(t, 20*time.Second, "local expiry enforcement", func() bool { return fetch() != nil })
	if after := e.s.Store.Ent.Gate.GetX(ctx, e.gateID).ConfigRevision; after != rev {
		t.Fatalf("expiry should be enforced locally without a new config (revision %d → %d)", rev, after)
	}
	_ = ent.IsNotFound
}
