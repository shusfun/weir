//go:build e2e

// 本轮新增功能的容器验收：REALITY 用 443（内核降权运行、只靠 CAP_NET_BIND_SERVICE）、订阅源「一组」由内核自动切换、
// Gate 跟随中心地址切换、单机角色合并为一个进程。由 deploy/e2e.py 在 debian 容器中以 root 运行。
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
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/gate"
	"github.com/shusfun/weir/internal/kernels"
	"github.com/shusfun/weir/packages/contracts"
)

// seedSingbox 把 sing-box 放进中心的内核缓存（版本号与内置档案对应）。
func (e *env) seedSingbox() string {
	e.t.Helper()
	sb := os.Getenv("WEIR_E2E_SINGBOX")
	if sb == "" {
		e.t.Skip("WEIR_E2E_SINGBOX not set")
	}
	data, err := os.ReadFile(sb)
	if err != nil {
		e.t.Fatal(err)
	}
	if _, err = (kernels.Cache{Dir: filepath.Join(e.dir, "center-data", "kernels")}).Put("sing-box", "1.14.3", "amd64", data); err != nil {
		e.t.Fatal(err)
	}
	return sb
}

// helperRunning 经辅助程序的本机套接字读出正在运行的内核进程号。
func helperRunning(t *testing.T, socket string) map[string]int {
	c := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}, Timeout: 5 * time.Second}
	resp, err := c.Get("http://helper/status")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var st struct {
		Running map[string]int `json:"running"`
	}
	json.NewDecoder(resp.Body).Decode(&st)
	return st.Running
}

func procUID(pid int) string {
	b, _ := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(l, "Uid:") {
			return strings.Fields(l)[1]
		}
	}
	return ""
}

func (e *env) gateListens(port int) bool {
	g, err := e.s.Store.Ent.Gate.Get(e.ctx, e.gateID)
	if err != nil {
		return false
	}
	for _, p := range g.ListeningPorts {
		if v, _ := p["port"].(float64); int(v) == port {
			return true
		}
		if v, _ := p["port"].(int); v == port {
			return true
		}
	}
	return false
}

// localDest 是本机 TLS 1.3 站点，作 REALITY 的伪装目标（不依赖外网）。
func localDest(t *testing.T) string {
	dest := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("dest")) }))
	dest.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	dest.StartTLS()
	t.Cleanup(dest.Close)
	return dest.Listener.Addr().String()
}

// PORT-01：REALITY 节点在 443 被其他程序占用时随机分配；443 空出后新建的 REALITY 节点用 443，
// 内核以普通账号运行、只保留 CAP_NET_BIND_SERVICE，并用真实客户端连一次。
func TestRealityPrefers443WithDroppedPrivileges(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("needs root (helper drops privileges for the kernels)")
	}
	if _, err := user.Lookup("weir-e2e"); err != nil {
		if out, err := exec.Command("useradd", "-r", "-M", "-s", "/usr/sbin/nologin", "weir-e2e").CombinedOutput(); err != nil {
			t.Skipf("cannot create an unprivileged account: %v %s", err, out)
		}
	}
	e := setup(t)
	sb := e.seedSingbox()
	e.runAs = "weir-e2e"
	// 内核账号要能进入测试目录（安装器建的目录本来就是这样：内核目录 0755、配置目录属组为内核账号）。
	gd := filepath.Join(e.dir, "p443")
	for _, d := range []string{gd, filepath.Join(gd, "kernels"), filepath.Join(gd, "cores"), filepath.Join(gd, "certs")} {
		os.MkdirAll(d, 0o755)
	}
	for d := gd; d != "/" && d != "."; d = filepath.Dir(d) {
		os.Chmod(d, 0o755)
	}
	// 443 先被「其他程序」占用。
	foreign, err := net.Listen("tcp", ":443")
	if err != nil {
		t.Fatal(err)
	}
	e.startGate("p443")
	e.s.Store.Ent.Gate.UpdateOneID(e.gateID).SetPublicHost("127.0.0.1").ExecX(e.ctx)
	eventually(t, 15*time.Second, "gate reports 443 in use", func() bool { return e.gateListens(443) })
	dest := localDest(t)
	busy := e.post("/api/v1/admin/nodes", map[string]any{"gate_id": e.gateID, "preset": "vless-reality", "name": "R-busy", "skip_probe": true,
		"settings": map[string]any{"reality_dest": dest, "reality_server_names": "e2e.test"}}, nil)
	if busy["port"].(float64) == 443 {
		t.Fatal("443 used by another program must not be taken")
	}
	// 443 空出：Gate 下一次心跳上报后，新建的 REALITY 节点用 443。
	foreign.Close()
	eventually(t, 15*time.Second, "gate reports 443 free", func() bool { return !e.gateListens(443) })
	r := e.post("/api/v1/admin/nodes", map[string]any{"gate_id": e.gateID, "preset": "vless-reality", "name": "R443", "skip_probe": true,
		"settings": map[string]any{"reality_dest": dest, "reality_server_names": "e2e.test"}}, nil)
	if r["port"].(float64) != 443 {
		t.Fatalf("REALITY on a free 443 = %v", r["port"])
	}
	u := e.post("/api/v1/admin/users", map[string]any{"email": "p443@e2e.test", "password": "E2eUser1"}, nil)
	e.applied("REALITY on 443")
	pid := helperRunning(t, filepath.Join("/tmp", "mg-p443.sock"))["xray"]
	acct, _ := user.Lookup("weir-e2e")
	if pid == 0 || procUID(pid) != acct.Uid {
		t.Fatalf("xray pid %d runs as uid %q, want %s", pid, procUID(pid), acct.Uid)
	}
	if c, err := net.DialTimeout("tcp", "127.0.0.1:443", 2*time.Second); err != nil {
		t.Fatalf("xray as %s is not listening on 443: %v", e.runAs, err)
	} else {
		c.Close()
	}
	// 真实客户端经 443 上的 REALITY 节点下载。
	var detail map[string]any
	b, _ := e.get("/api/v1/admin/users/"+strconv.Itoa(int(u["id"].(float64))), nil)
	json.Unmarshal(b, &detail)
	token := detail["link_items"].([]any)[0].(map[string]any)["token"].(string)
	body, _ := e.get("/sub/"+token, map[string]string{"User-Agent": "sing-box/1.14.3"})
	var doc struct {
		Outbounds []map[string]any `json:"outbounds"`
	}
	json.Unmarshal(body, &doc)
	blob := strings.Repeat("p", 256<<10)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte(blob)) }))
	t.Cleanup(target.Close)
	for _, ob := range doc.Outbounds {
		if ob["tag"] == "R443" {
			if ob["server_port"].(float64) != 443 {
				t.Fatalf("subscription port = %v", ob["server_port"])
			}
			port := e.runSingboxClient(sb, ob)
			eventually(t, 25*time.Second, "download via REALITY on 443", func() bool { return fetchVia(port, target.URL, len(blob)) == nil })
			t.Logf("REALITY on 443 as uid %s ok", acct.Uid)
			return
		}
	}
	t.Fatalf("subscription has no R443: %s", body)
}

// member 是一个可以关掉的 SOCKS5 出口（模拟订阅源里的一个节点），记录经过它的连接数。
type member struct {
	port int
	hits *atomic.Int64
	stop func()
}

func newMember(t *testing.T) member {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var hits atomic.Int64
	var conns sync.Map
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			conns.Store(c, true)
			go func(c net.Conn) {
				defer c.Close()
				defer conns.Delete(c)
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
				hits.Add(1)
				c.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0})
				go io.Copy(up, c)
				io.Copy(c, up)
			}(c)
		}
	}()
	stop := func() {
		l.Close()
		conns.Range(func(k, _ any) bool { k.(net.Conn).Close(); return true })
	}
	t.Cleanup(stop)
	return member{port: l.Addr().(*net.TCPAddr).Port, hits: &hits, stop: stop}
}

// CHAIN-03：链式代理第 ③ 步选订阅源里的「一组」：全部成员下发，由内核自动选择；一个成员断开后自动切到另一个。
// Xray（balancer + observatory）与 sing-box（urltest）各验一次。
func TestSourceGroupFailoverInKernels(t *testing.T) {
	e := setup(t)
	sb := e.seedSingbox()
	// 内核的延迟探测改为本机地址、2 秒一次（不依赖外网）。
	probe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))
	t.Cleanup(probe.Close)
	contracts.GroupProbeURL, contracts.GroupProbeEvery = probe.URL, "2s"
	e.startGate("grp")
	e.s.Store.Ent.Gate.UpdateOneID(e.gateID).SetPublicHost("127.0.0.1").ExecX(e.ctx)
	a, b := newMember(t), newMember(t)
	src := e.post("/api/v1/admin/misub/sources", map[string]any{"name": "机场", "url": "https://example.invalid/sub"}, nil)
	sid := int(src["id"].(float64))
	links, _ := json.Marshal([]string{fmt.Sprintf("socks5://127.0.0.1:%d#HK-A", a.port), fmt.Sprintf("socks5://127.0.0.1:%d#HK-B", b.port)})
	if err := e.s.Store.KV.Set(e.ctx, fmt.Sprintf("misub-source:%d", sid), string(links), 0); err != nil {
		t.Fatal(err)
	}
	chainSpec := map[string]any{"kind": "source", "source_id": sid, "mode": "filter", "match": "^HK"}
	dest := localDest(t)
	e.post("/api/v1/admin/nodes", map[string]any{"gate_id": e.gateID, "preset": "vless-reality", "name": "X-POOL", "skip_probe": true, "chain": chainSpec,
		"settings": map[string]any{"reality_dest": dest, "reality_server_names": "e2e.test"}}, nil)
	e.post("/api/v1/admin/nodes", map[string]any{"gate_id": e.gateID, "preset": "ss2022", "name": "S-POOL", "skip_probe": true, "chain": chainSpec}, nil)
	u := e.post("/api/v1/admin/users", map[string]any{"email": "grp@e2e.test", "password": "E2eUser1"}, nil)
	e.applied("source group exits")

	var detail map[string]any
	raw, _ := e.get("/api/v1/admin/users/"+strconv.Itoa(int(u["id"].(float64))), nil)
	json.Unmarshal(raw, &detail)
	token := detail["link_items"].([]any)[0].(map[string]any)["token"].(string)
	body, _ := e.get("/sub/"+token, map[string]string{"User-Agent": "sing-box/1.14.3"})
	var doc struct {
		Outbounds []map[string]any `json:"outbounds"`
	}
	json.Unmarshal(body, &doc)
	blob := strings.Repeat("g", 128<<10)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte(blob)) }))
	t.Cleanup(target.Close)
	clients := map[string]int{}
	for _, ob := range doc.Outbounds {
		if tag, _ := ob["tag"].(string); tag == "X-POOL" || tag == "S-POOL" {
			clients[tag] = e.runSingboxClient(sb, ob)
		}
	}
	if len(clients) != 2 {
		t.Fatalf("subscription outbounds = %v", doc.Outbounds)
	}
	for _, tag := range []string{"X-POOL", "S-POOL"} {
		port := clients[tag]
		before := a.hits.Load() + b.hits.Load()
		eventually(t, 30*time.Second, tag+" download through the group", func() bool { return fetchVia(port, target.URL, len(blob)) == nil })
		if a.hits.Load()+b.hits.Load() == before {
			t.Fatalf("%s traffic did not leave through a group member", tag)
		}
	}
	liveRatesFromKernels(t, e, clients["X-POOL"], target.URL, len(blob))
	// 断开一个成员：内核探测到后自动切到另一个，流量照常。
	used, other := a, b
	if b.hits.Load() > a.hits.Load() {
		used, other = b, a
	}
	used.stop()
	for _, tag := range []string{"X-POOL", "S-POOL"} {
		port := clients[tag]
		before := other.hits.Load()
		eventually(t, 45*time.Second, tag+" failover to the other member", func() bool {
			return fetchVia(port, target.URL, len(blob)) == nil && other.hits.Load() > before
		})
		t.Logf("%s switched to the remaining member", tag)
	}
}

// httpsCenter 让同一个中心经两个 HTTPS 域名提供服务（容器里在 /etc/hosts 指到本机），返回 CA 文件与端口。
func (e *env) httpsCenter(hosts ...string) (string, int, *x509.CertPool) {
	t := e.t
	caKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	caTpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "weir e2e ca"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caDER, _ := x509.CreateCertificate(rand.Reader, caTpl, caTpl, &caKey.PublicKey, caKey)
	ca, _ := x509.ParseCertificate(caDER)
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), DNSNames: hosts, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour), ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	leafDER, _ := x509.CreateCertificate(rand.Reader, leaf, ca, &key.PublicKey, caKey)
	caFile := filepath.Join(e.dir, "ca.pem")
	os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), 0o644)
	srv := httptest.NewUnstartedServer(e.s.Router())
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{leafDER}, PrivateKey: key}}}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	hostsFile, _ := os.ReadFile("/etc/hosts")
	extra := ""
	for _, h := range hosts {
		extra += "127.0.0.1 " + h + "\n"
	}
	if err := os.WriteFile("/etc/hosts", append(hostsFile, []byte(extra)...), 0o644); err != nil {
		t.Skipf("cannot edit /etc/hosts: %v", err)
	}
	t.Cleanup(func() { os.WriteFile("/etc/hosts", hostsFile, 0o644) })
	pool := x509.NewCertPool()
	pool.AddCert(ca)
	return caFile, srv.Listener.Addr().(*net.TCPAddr).Port, pool
}

// httpsAdmin 经某个域名登录控制台（每个域名一份 Cookie）。
func (e *env) httpsAdmin(origin string, pool *x509.CertPool) *http.Client {
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar, Timeout: 30 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}}
	b, _ := json.Marshal(map[string]string{"email": "admin@e2e.test", "password": "E2eAdmin1"})
	req, _ := http.NewRequest("POST", origin+"/api/v1/auth/login", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	resp, err := c.Do(req)
	if err != nil || resp.StatusCode != 200 {
		e.t.Fatalf("login via %s: %v %v", origin, err, resp)
	}
	resp.Body.Close()
	return c
}

func call(t *testing.T, c *http.Client, method, url string, body any) (int, map[string]any) {
	t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(method, url, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// pairHTTPS 用「添加 Gate」的凭证经 HTTPS 中心配对一台 Gate，返回它的设备文件路径与停止函数（停止 = 离线）。
func (e *env) pairHTTPS(name, center, caFile string) (string, func()) {
	t := e.t
	inv := e.post("/api/v1/admin/gates/invite", map[string]string{"name": name}, nil)
	gd := filepath.Join(e.dir, name)
	p := gate.Paths{Config: filepath.Join(gd, "agent.json"), Data: filepath.Join(gd, "data"), Socket: filepath.Join("/tmp", "mg-"+name+".sock"),
		CoresDir: filepath.Join(gd, "cores"), CertsDir: filepath.Join(gd, "certs"), KernelsDir: filepath.Join(gd, "kernels"), Root: filepath.Join(gd, "root")}
	ctx, cancel := context.WithCancel(e.ctx)
	go gate.ServeHelper(ctx, gate.NewHelper(p))
	eventually(t, 5*time.Second, name+" helper socket", func() bool { _, err := os.Stat(p.Socket); return err == nil })
	if err := gate.Pair(ctx, p, center, inv["token"].(string), name, caFile); err != nil {
		t.Fatal(err)
	}
	a, err := gate.NewAgent(p)
	if err != nil {
		t.Fatal(err)
	}
	a.MeterEvery, a.HeartbeatEvery, a.ReportEvery = time.Second, time.Second, 1
	go a.Run(ctx)
	return p.Config, cancel
}

func gateCenter(t *testing.T, config string) string {
	b, _ := os.ReadFile(config)
	var v struct {
		Center string `json:"center"`
	}
	json.Unmarshal(b, &v)
	return v.Center
}

// CENTER-02：中心地址更改后，在线的 Gate 先用新地址完成设备认证再切换（改写设备文件）；
// 离线的 Gate 列在「仍连接旧地址」里，旧地址失效后它回来也连不上，只能重新执行「添加 Gate」。
func TestGateFollowsCenterOrigin(t *testing.T) {
	e := setup(t)
	caFile, port, pool := e.httpsCenter("old.e2e.test", "new.e2e.test")
	oldOrigin, newOrigin := fmt.Sprintf("https://old.e2e.test:%d", port), fmt.Sprintf("https://new.e2e.test:%d", port)
	oldAdmin := e.httpsAdmin(oldOrigin, pool)
	// 第一次经 HTTPS 域名打开控制台：记录地址（不需要重新验证）。
	if code, out := call(t, oldAdmin, "PUT", oldOrigin+"/api/v1/admin/center-origin", map[string]any{"origin": oldOrigin}); code != 200 {
		t.Fatalf("record origin = %d %v", code, out)
	}
	online, _ := e.pairHTTPS("online", oldOrigin, caFile)
	offlineCfg, stopOffline := e.pairHTTPS("offline", oldOrigin, caFile)
	eventually(t, 15*time.Second, "both gates heartbeat via the old origin", func() bool {
		_, v := call(t, oldAdmin, "GET", oldOrigin+"/api/v1/admin/center-origin", nil)
		gs, _ := e.s.Store.Ent.Gate.Query().All(e.ctx)
		n := 0
		for _, g := range gs {
			if g.CenterURL == oldOrigin {
				n++
			}
		}
		return n == 2 && len(v["pending_gates"].([]any)) == 0
	})
	stopOffline()

	// 管理员经新域名访问：只能改成当前正在访问的地址，且要重新验证身份。
	newAdmin := e.httpsAdmin(newOrigin, pool)
	if code, _ := call(t, newAdmin, "PUT", newOrigin+"/api/v1/admin/center-origin", map[string]any{"origin": "https://evil.example.com"}); code != 400 {
		t.Fatalf("arbitrary origin = %d", code)
	}
	if code, _ := call(t, newAdmin, "PUT", newOrigin+"/api/v1/admin/center-origin", map[string]any{"origin": newOrigin, "password": "wrong"}); code != 401 {
		t.Fatalf("change without reauth = %d", code)
	}
	if code, out := call(t, newAdmin, "PUT", newOrigin+"/api/v1/admin/center-origin", map[string]any{"origin": newOrigin, "password": "E2eAdmin1"}); code != 200 {
		t.Fatalf("change origin = %d %v", code, out)
	}
	// 在线的 Gate：新地址认证成功后改写设备文件并切换。
	eventually(t, 20*time.Second, "online gate switches to the new origin", func() bool { return gateCenter(t, online) == newOrigin })
	eventually(t, 10*time.Second, "online gate heartbeats via the new origin", func() bool {
		_, v := call(t, newAdmin, "GET", newOrigin+"/api/v1/admin/center-origin", nil)
		pending := v["pending_gates"].([]any)
		return len(pending) == 1 && pending[0].(map[string]any)["name"] == "offline"
	})
	// 离线的 Gate 仍记着旧地址。
	if gateCenter(t, offlineCfg) != oldOrigin {
		t.Fatalf("offline gate config = %s", gateCenter(t, offlineCfg))
	}
	// 「添加 Gate」命令改用新地址；两次操作都有审计（记录、更改 A → B）。
	if _, inv := call(t, newAdmin, "POST", newOrigin+"/api/v1/admin/gates/invite", map[string]string{"name": "later"}); !strings.Contains(inv["command"].(string), newOrigin) {
		t.Fatalf("invite command = %v", inv["command"])
	}
	audits, _ := e.s.Store.Ent.Audit.Query().All(e.ctx)
	seen := map[string]bool{}
	for _, a := range audits {
		seen[a.Action] = true
	}
	if !seen["center.origin.set"] || !seen["center.origin.change"] {
		t.Fatalf("audit actions = %v", seen)
	}
	// 离线的 Gate 在旧域名还可用时回来：心跳收到新地址，认证成功后切换，「仍连接旧地址」清空。
	a, err := gate.NewAgent(gate.Paths{Config: offlineCfg, Data: filepath.Join(e.dir, "offline", "data"), Socket: filepath.Join("/tmp", "mg-offline.sock"),
		CoresDir: filepath.Join(e.dir, "offline", "cores"), CertsDir: filepath.Join(e.dir, "offline", "certs"), KernelsDir: filepath.Join(e.dir, "offline", "kernels"), Root: filepath.Join(e.dir, "offline", "root")})
	if err != nil {
		t.Fatal(err)
	}
	a.MeterEvery, a.HeartbeatEvery, a.ReportEvery = time.Second, time.Second, 1
	ctx, cancel := context.WithCancel(e.ctx)
	t.Cleanup(cancel)
	go gate.ServeHelper(ctx, gate.NewHelper(gate.Paths{Config: offlineCfg, Data: filepath.Join(e.dir, "offline", "data"), Socket: filepath.Join("/tmp", "mg-offline.sock"),
		CoresDir: filepath.Join(e.dir, "offline", "cores"), CertsDir: filepath.Join(e.dir, "offline", "certs"), KernelsDir: filepath.Join(e.dir, "offline", "kernels"), Root: filepath.Join(e.dir, "offline", "root")}))
	go a.Run(ctx)
	eventually(t, 20*time.Second, "offline gate switches when it comes back", func() bool { return gateCenter(t, offlineCfg) == newOrigin })
	eventually(t, 10*time.Second, "no gate left on the old origin", func() bool {
		_, v := call(t, newAdmin, "GET", newOrigin+"/api/v1/admin/center-origin", nil)
		return len(v["pending_gates"].([]any)) == 0
	})
	t.Logf("both gates followed %s → %s", oldOrigin, newOrigin)
}

// runSingboxDoc 用订阅里的整份 sing-box 配置（含 endpoints、detour 用的辅助出站）启动客户端，最终出口为 final。
func (e *env) runSingboxDoc(bin string, doc map[string]any, final string) int {
	e.t.Helper()
	port := freePort(e.t)
	cfg := map[string]any{"log": map[string]any{"level": "warn"}, "inbounds": []any{map[string]any{"type": "mixed", "listen": "127.0.0.1", "listen_port": port}},
		"outbounds": doc["outbounds"], "route": map[string]any{"final": final}}
	if ep, ok := doc["endpoints"]; ok {
		cfg["endpoints"] = ep
	}
	b, _ := json.Marshal(cfg)
	path := filepath.Join(e.dir, fmt.Sprintf("client-doc-%d.json", port))
	os.WriteFile(path, b, 0o600)
	cmd := exec.Command(bin, "run", "-c", path)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	return port
}

// NODE-02：新增方案（本地 SOCKS / HTTP、ShadowTLS v3 + SS2022、WireGuard）建好后用真实客户端连通；
// 高级字段改过的值生效并通过内核自检；有额度的用户拿不到 WireGuard，并写明原因。
func TestNewPresetsConnectWithRealClients(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("needs root (ShadowTLS handshake site listens on 443)")
	}
	e := setup(t)
	sb := e.seedSingbox()
	// ShadowTLS 的握手站点：本机 443 上的 TLS 1.3 站点（证书由测试 CA 签发），名字指到本机；客户端信任这个 CA。
	caKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	caTpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "weir e2e hs ca"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caDER, _ := x509.CreateCertificate(rand.Reader, caTpl, caTpl, &caKey.PublicKey, caKey)
	ca, _ := x509.ParseCertificate(caDER)
	hsKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	hsDER, _ := x509.CreateCertificate(rand.Reader, &x509.Certificate{SerialNumber: big.NewInt(2), DNSNames: []string{"hs.e2e.test"}, NotBefore: time.Now().Add(-time.Hour),
		NotAfter: time.Now().Add(24 * time.Hour), ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}, ca, &hsKey.PublicKey, caKey)
	caFile := filepath.Join(e.dir, "hs-ca.pem")
	os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), 0o644)
	os.Setenv("SSL_CERT_FILE", caFile)
	t.Cleanup(func() { os.Unsetenv("SSL_CERT_FILE") })
	hostsFile, _ := os.ReadFile("/etc/hosts")
	os.WriteFile("/etc/hosts", append(hostsFile, []byte("127.0.0.1 hs.e2e.test\n")...), 0o644)
	t.Cleanup(func() { os.WriteFile("/etc/hosts", hostsFile, 0o644) })
	hs := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("hs")) }))
	l443, err := net.Listen("tcp", ":443")
	if err != nil {
		t.Fatal(err)
	}
	hs.Listener, hs.TLS = l443, &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{{Certificate: [][]byte{hsDER}, PrivateKey: hsKey}}}
	hs.StartTLS()
	t.Cleanup(hs.Close)
	e.startGate("presets")
	e.s.Store.Ent.Gate.UpdateOneID(e.gateID).SetPublicHost("127.0.0.1").ExecX(e.ctx)
	node := func(body map[string]any) map[string]any {
		body["gate_id"], body["skip_probe"] = e.gateID, true
		return e.post("/api/v1/admin/nodes", body, nil)
	}
	node(map[string]any{"preset": "socks-local", "name": "SOCKS"})
	node(map[string]any{"preset": "http-local", "name": "HTTP"})
	node(map[string]any{"preset": "shadowtls", "name": "STLS", "settings": map[string]any{"tls_sni": "hs.e2e.test"}})
	node(map[string]any{"preset": "wireguard", "name": "WG", "settings": map[string]any{"wg_mtu": 1380}})
	hy := node(map[string]any{"preset": "hysteria2", "name": "HY2-ADV", "settings": map[string]any{"hy2_up_mbps": 50, "hy2_obfs": ""}})
	if st := hy["settings"].(map[string]any); st["hy2_up_mbps"].(float64) != 50 || st["hy2_obfs"] != nil {
		t.Fatalf("advanced fields = %v", st)
	}
	free := e.post("/api/v1/admin/users", map[string]any{"email": "free@e2e.test", "password": "E2eUser1"}, nil)
	limited := e.post("/api/v1/admin/users", map[string]any{"email": "limited@e2e.test", "password": "E2eUser1", "quota": 1 << 30}, nil)
	e.applied("new presets")

	tokenOf := func(u map[string]any) (string, map[string]any) {
		var d map[string]any
		b, _ := e.get("/api/v1/admin/users/"+strconv.Itoa(int(u["id"].(float64))), nil)
		json.Unmarshal(b, &d)
		return d["link_items"].([]any)[0].(map[string]any)["token"].(string), d
	}
	token, _ := tokenOf(free)
	body, _ := e.get("/sub/"+token, map[string]string{"User-Agent": "sing-box/1.14.3"})
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("sing-box subscription: %v\n%s", err, body)
	}
	blob := strings.Repeat("n", 128<<10)
	// 下载目标监听在容器的非回环地址：WireGuard 客户端的用户态协议栈把 127.0.0.0/8 当作自己的回环，不会送进隧道。
	tl, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	target := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte(blob)) }))
	target.Listener = tl
	target.Start()
	t.Cleanup(target.Close)
	targetURL := target.URL
	if addrs, _ := net.InterfaceAddrs(); len(addrs) > 0 {
		for _, a := range addrs {
			if ip, ok := a.(*net.IPNet); ok && ip.IP.To4() != nil && !ip.IP.IsLoopback() {
				targetURL = fmt.Sprintf("http://%s:%d", ip.IP, tl.Addr().(*net.TCPAddr).Port)
				break
			}
		}
	}
	for _, tag := range []string{"SOCKS", "HTTP", "STLS", "WG", "HY2-ADV"} {
		port := e.runSingboxDoc(sb, doc, tag)
		eventually(t, 30*time.Second, "download via "+tag, func() bool { return fetchVia(port, targetURL, len(blob)) == nil })
		t.Logf("%s ok", tag)
	}
	// 有额度的用户：订阅里没有 WireGuard，用户详情写明原因。
	ltoken, ldetail := tokenOf(limited)
	lbody, _ := e.get("/sub/"+ltoken, map[string]string{"User-Agent": "sing-box/1.14.3"})
	if strings.Contains(string(lbody), `"WG"`) {
		t.Fatal("limited user must not get the WireGuard node")
	}
	if w := ldetail["withheld"].([]any); len(w) != 1 || !strings.Contains(w[0].(map[string]any)["reason"].(string), "不限额度") {
		t.Fatalf("withheld = %v", ldetail["withheld"])
	}
}

// ARCH-01：单机角色合并为一个进程——真实的 weir 程序以 `center --serve --local-gate` 运行，本机 Gate 的 Agent
// 在中心进程里；内核仍由 root 辅助程序 `gate helper` 托管。没有单独的 `gate run` 进程。
func TestStandaloneLocalGateInCenterProcess(t *testing.T) {
	bin := os.Getenv("WEIR_E2E_BINARY")
	if bin == "" || os.Geteuid() != 0 {
		t.Skip("WEIR_E2E_BINARY not set or not root")
	}
	dir := t.TempDir()
	cfg := filepath.Join(dir, "center", "config.json")
	agent := "/etc/weir/gate/agent.json"
	os.Remove(agent)
	out, err := exec.Command(bin, "center", "--config", cfg, "--listen", "127.0.0.1:19899", "--init").CombinedOutput()
	if err != nil {
		t.Fatalf("init: %v %s", err, out)
	}
	token := strings.TrimSpace(string(out[strings.LastIndex(string(out), ":")+1:]))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	start := func(args ...string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, bin, args...)
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
		return cmd
	}
	helper := start("gate", "helper")
	centerProc := start("center", "--config", cfg, "--serve", "--listen", "127.0.0.1:19899", "--local-gate", agent)
	base := "http://127.0.0.1:19899"
	eventually(t, 15*time.Second, "center up", func() bool { r, err := http.Get(base + "/healthz"); return err == nil && r.StatusCode == 200 })
	jar, _ := cookiejar.New(nil)
	admin := &http.Client{Jar: jar, Timeout: 30 * time.Second}
	b, _ := json.Marshal(map[string]any{"store": map[string]any{"driver": "sqlite", "sqlite_path": filepath.Join(dir, "center.db")}, "email": "admin@e2e.test", "password": "E2eAdmin1"})
	req, _ := http.NewRequest("POST", base+"/api/v1/setup/install", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	if resp, err := admin.Do(req); err != nil || resp.StatusCode != 201 {
		t.Fatalf("install: %v %v", err, resp)
	}
	code, inv := call(t, admin, "POST", base+"/api/v1/admin/gates/invite", map[string]string{"name": "本机"})
	if code != 201 {
		t.Fatalf("invite = %d %v", code, inv)
	}
	// 配对写出设备文件；中心进程里等着的本机 Agent 随即启动（安装器的本机配对也是这样，配对后不再启动独立服务）。
	if out, err := exec.Command(bin, "gate", "pair", "--config", agent, "--center", base, "--token", inv["token"].(string), "--name", "本机").CombinedOutput(); err != nil {
		t.Fatalf("pair: %v %s", err, out)
	}
	eventually(t, 30*time.Second, "local gate heartbeat from the center process", func() bool {
		_, v := call(t, admin, "GET", base+"/api/v1/admin/gates", nil)
		items, _ := v["items"].([]any)
		return len(items) == 1 && items[0].(map[string]any)["online"] == true
	})
	// 只有中心进程与辅助程序：没有 `gate run`。
	procs, _ := os.ReadDir("/proc")
	for _, p := range procs {
		cmdline, _ := os.ReadFile(filepath.Join("/proc", p.Name(), "cmdline"))
		args := strings.Split(strings.TrimRight(string(cmdline), "\x00"), "\x00")
		if len(args) >= 3 && args[0] == bin && args[1] == "gate" && args[2] == "run" {
			t.Fatalf("found a separate gate agent process: %v", args)
		}
	}
	t.Logf("local gate online inside center pid %d (helper pid %d)", centerProc.Process.Pid, helper.Process.Pid)
}

// listenSockets 读出容器里处于监听的 TCP 套接字：地址是否回环、端口与所属进程。
func listenSockets(t *testing.T) []struct {
	loopback bool
	port     int
	pid      int
} {
	owner := map[string]int{}
	procs, _ := filepath.Glob("/proc/[0-9]*/fd/*")
	for _, fd := range procs {
		if link, err := os.Readlink(fd); err == nil && strings.HasPrefix(link, "socket:[") {
			pid, _ := strconv.Atoi(strings.Split(fd, "/")[2])
			owner[strings.TrimSuffix(strings.TrimPrefix(link, "socket:["), "]")] = pid
		}
	}
	var out []struct {
		loopback bool
		port     int
		pid      int
	}
	for _, name := range []string{"tcp", "tcp6"} {
		b, _ := os.ReadFile("/proc/net/" + name)
		for i, line := range strings.Split(string(b), "\n") {
			f := strings.Fields(line)
			if i == 0 || len(f) < 10 || f[3] != "0A" {
				continue
			}
			addr, portHex, _ := strings.Cut(f[1], ":")
			port, _ := strconv.ParseInt(portHex, 16, 32)
			// IPv4 回环 0100007F；IPv6 回环 ::1 与映射的 ::ffff:127.0.0.1。
			loop := strings.HasSuffix(addr, "7F") && len(addr) == 8 || addr == "00000000000000000000000001000000" || strings.HasSuffix(addr, "FFFF00000100007F")
			out = append(out, struct {
				loopback bool
				port     int
				pid      int
			}{loop, int(port), owner[f[9]]})
		}
	}
	return out
}

// GATE-02 / GATE-03：Gate 只对外开放代理端口、主动连接中心；内核拒绝的配置不应用、原内核照常服务并上报失败；
// Agent 断开时中心显示离线与待下发，恢复后补上新版本。
func TestGateExposureFailedConfigAndOffline(t *testing.T) {
	e := setup(t)
	e.seedSingbox()
	e.startGate("gx")
	e.s.Store.Ent.Gate.UpdateOneID(e.gateID).SetPublicHost("127.0.0.1").ExecX(e.ctx)
	r := e.post("/api/v1/admin/nodes", map[string]any{"gate_id": e.gateID, "preset": "vless-reality", "name": "R", "skip_probe": true,
		"settings": map[string]any{"reality_dest": localDest(t), "reality_server_names": "e2e.test"}}, nil)
	ss := e.post("/api/v1/admin/nodes", map[string]any{"gate_id": e.gateID, "preset": "ss2022", "name": "S", "skip_probe": true}, nil)
	e.post("/api/v1/admin/users", map[string]any{"email": "gx@e2e.test", "password": "E2eUser1"}, nil)
	e.applied("two nodes")
	nodePorts := map[int]bool{int(r["port"].(float64)): true, int(ss["port"].(float64)): true}
	running := helperRunning(t, e.paths.Socket)
	if running["xray"] == 0 || running["sing-box"] == 0 {
		t.Fatalf("kernels running = %v", running)
	}

	// GATE-02：本进程（Agent + 辅助程序）与内核在非回环地址上只监听节点端口；中心只在回环上（测试里的中心）。
	kernelPID := map[int]bool{running["xray"]: true, running["sing-box"]: true}
	exposed := map[int]bool{}
	for _, s := range listenSockets(t) {
		if s.loopback {
			continue
		}
		if s.pid == os.Getpid() {
			t.Fatalf("gate process listens publicly on %d", s.port)
		}
		if kernelPID[s.pid] {
			if !nodePorts[s.port] {
				t.Fatalf("kernel listens publicly on non-node port %d", s.port)
			}
			exposed[s.port] = true
		}
	}
	if len(exposed) != len(nodePorts) {
		t.Fatalf("public kernel ports = %v, want %v", exposed, nodePorts)
	}
	g := e.s.Store.Ent.Gate.GetX(e.ctx, e.gateID)
	for _, s := range listenSockets(t) {
		if !s.loopback && (s.port == g.XrayStatsPort || s.port == g.SingboxStatsPort) {
			t.Fatalf("stats port %d listens publicly", s.port)
		}
	}
	t.Logf("public listeners = node ports %v; stats ports %d/%d loopback only", exposed, g.XrayStatsPort, g.SingboxStatsPort)

	// GATE-03：只有内核会拒绝的参数（REALITY short ID 非十六进制）直接写进库里下发：Gate 自检不通过、不切换，
	// 原 Xray 进程与端口照常服务，中心显示失败与原因。
	rid := int(r["id"].(float64))
	node := e.s.Store.Ent.Node.GetX(e.ctx, rid)
	good := map[string]any{}
	bad := map[string]any{}
	for k, v := range node.Settings {
		good[k], bad[k] = v, v
	}
	bad["reality_short_id"] = "zz"
	e.s.Store.Ent.Node.UpdateOneID(rid).SetSettings(bad).ExecX(e.ctx)
	g = e.s.Store.Ent.Gate.UpdateOneID(e.gateID).AddConfigRevision(1).SaveX(e.ctx)
	var view map[string]any
	gateState := func() map[string]any {
		b, _ := e.get("/api/v1/admin/gates/"+strconv.Itoa(e.gateID), nil)
		view = map[string]any{}
		json.Unmarshal(b, &view)
		return view
	}
	eventually(t, 40*time.Second, "gate reports the rejected config", func() bool { return gateState()["state"] == "failed" })
	if view["last_error"] == "" || view["applied_revision"].(float64) >= float64(g.ConfigRevision) {
		t.Fatalf("failed config view = %v", view)
	}
	if now := helperRunning(t, e.paths.Socket); now["xray"] != running["xray"] {
		t.Fatalf("xray restarted on a rejected config: %v -> %v", running, now)
	}
	if c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", int(r["port"].(float64))), 2*time.Second); err != nil {
		t.Fatalf("old xray stopped serving: %v", err)
	} else {
		c.Close()
	}
	t.Logf("rejected config: %v", view["last_error"])
	e.s.Store.Ent.Node.UpdateOneID(rid).SetSettings(good).ExecX(e.ctx)
	e.s.Store.Ent.Gate.UpdateOneID(e.gateID).AddConfigRevision(1).ExecX(e.ctx)
	// 失败后 Agent 等 30 秒再取下一版本（不反复重试同一份坏配置）。
	eventually(t, 75*time.Second, "fixed config", func() bool { return gateState()["state"] == "applied" })

	// 离线：只停 Agent（内核照常运行）；超过 90 秒没有心跳显示离线，修改节点后是「待生效」而不是已生效。
	e.stopAgent()
	time.Sleep(2 * time.Second)
	e.s.Store.Ent.Gate.UpdateOneID(e.gateID).SetLastSeenAt(time.Now().Add(-2 * time.Minute)).ExecX(e.ctx)
	e.post("/api/v1/admin/nodes", map[string]any{"gate_id": e.gateID, "preset": "ss2022", "name": "S2", "skip_probe": true}, nil)
	if v := gateState(); v["online"] != false || v["state"] != "pending" {
		t.Fatalf("offline gate view = %v", v)
	}
	if c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", int(ss["port"].(float64))), 2*time.Second); err != nil {
		t.Fatalf("kernel must keep serving while the agent is offline: %v", err)
	} else {
		c.Close()
	}
	// 恢复：Agent 重新连上后补上新版本。
	e.runAgent()
	e.applied("config after reconnect")
	if v := gateState(); v["online"] != true || v["state"] != "applied" {
		t.Fatalf("gate after reconnect = %v", v)
	}
}

// KERNEL-02：中心用真实 mihomo -t 检查生成的 Clash 订阅、用 sing-box check 检查 sing-box 订阅（REALITY / SS2022 / Hysteria2）。
func TestCenterChecksSubscriptionsWithRealKernels(t *testing.T) {
	mh := os.Getenv("WEIR_E2E_MIHOMO")
	if mh == "" {
		t.Skip("WEIR_E2E_MIHOMO not set")
	}
	e := setup(t)
	e.seedSingbox()
	data, err := os.ReadFile(mh)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = (kernels.Cache{Dir: filepath.Join(e.dir, "center-data", "kernels")}).Put("mihomo", "1.19.33", "amd64", data); err != nil {
		t.Fatal(err)
	}
	e.startGate("sc")
	e.s.Store.Ent.Gate.UpdateOneID(e.gateID).SetPublicHost("203.0.113.20").ExecX(e.ctx)
	e.post("/api/v1/admin/nodes", map[string]any{"gate_id": e.gateID, "preset": "vless-reality", "name": "R", "skip_probe": true,
		"settings": map[string]any{"reality_dest": localDest(t), "reality_server_names": "e2e.test"}}, nil)
	e.post("/api/v1/admin/nodes", map[string]any{"gate_id": e.gateID, "preset": "ss2022", "name": "S", "skip_probe": true}, nil)
	e.post("/api/v1/admin/nodes", map[string]any{"gate_id": e.gateID, "preset": "hysteria2", "name": "H", "skip_probe": true}, nil)
	u := e.post("/api/v1/admin/users", map[string]any{"email": "sc@e2e.test", "password": "E2eUser1"}, nil)
	var detail map[string]any
	b, _ := e.get("/api/v1/admin/users/"+strconv.Itoa(int(u["id"].(float64))), nil)
	json.Unmarshal(b, &detail)
	token := detail["link_items"].([]any)[0].(map[string]any)["token"].(string)
	for _, ua := range []string{"clash.meta/1.19", "SFA/1.14"} {
		if body, _ := e.get("/sub/"+token, map[string]string{"User-Agent": ua}); len(body) == 0 {
			t.Fatalf("empty %s subscription", ua)
		}
	}
	results := map[string]map[string]any{}
	eventually(t, 60*time.Second, "both subscription checks", func() bool {
		b, _ := e.get("/api/v1/admin/sub-checks", nil)
		var v struct {
			Items []map[string]any `json:"items"`
		}
		json.Unmarshal(b, &v)
		for _, it := range v.Items {
			results[it["kernel"].(string)] = it
		}
		return results["mihomo"] != nil && results["sing-box"] != nil
	})
	for _, k := range []string{"mihomo", "sing-box"} {
		if results[k]["status"] != "passed" {
			t.Fatalf("%s check = %v", k, results[k])
		}
		t.Logf("%s %v: %v", k, results[k]["version"], results[k]["status"])
	}
}

// liveRatesFromKernels：经一组出口下载时，链路页的实时数据来自真实内核的计数——
// 路径（凭据）与出口（一组）都有速率，且标出这一刻流量最大的成员（驱动打开了按出口计量，Gate 把内核标签换回了配置标签）。
func liveRatesFromKernels(t *testing.T, e *env, port int, url string, size int) {
	t.Helper()
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
				_ = fetchVia(port, url, size)
			}
		}
	}()
	var ev struct {
		Paths  map[string]struct{ Up, Down float64 } `json:"paths"`
		Exits  map[string]struct{ Up, Down float64 } `json:"exits"`
		Active map[string]int                        `json:"active"`
	}
	eventually(t, 40*time.Second, "live rates from kernel counters", func() bool {
		raw, err := e.s.LiveSnapshot(e.ctx, e.gateID, 10*time.Second)
		if err != nil || json.Unmarshal(raw, &ev) != nil {
			return false
		}
		moving := 0.0
		for _, r := range ev.Paths {
			moving += r.Down + r.Up
		}
		exit := 0.0
		for id, r := range ev.Exits {
			if strings.HasPrefix(id, "nodechain:") {
				exit += r.Down + r.Up
			}
		}
		active := false
		for _, i := range ev.Active {
			active = active || i >= 0
		}
		t.Logf("live frame: paths %.0f B/s, group exits %.0f B/s, active %v", moving, exit, ev.Active)
		return moving > 0 && exit > 0 && active
	})
}
