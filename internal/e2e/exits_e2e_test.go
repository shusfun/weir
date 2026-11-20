//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/shusfun/weir/internal/center"
	"github.com/shusfun/weir/internal/gate"
	"github.com/shusfun/weir/internal/store"
)

func (e *env) patch(path string, body any) map[string]any {
	e.t.Helper()
	raw, _ := json.Marshal(body)
	req, _ := http.NewRequest("PATCH", e.url+path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	resp, err := e.admin.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	if resp.StatusCode != 200 {
		e.t.Fatalf("PATCH %s = %d %v", path, resp.StatusCode, result)
	}
	return result
}

// 同一个出口由节点和规则引用；用真实客户端验证默认出口、选择性分流、拦截与用户链路的优先级。
func TestUnifiedExitsAndSelectiveRouting(t *testing.T) {
	if os.Getenv("WEIR_E2E_PUBLIC_NETWORK") != "1" {
		t.Skip("用户链路只允许公网地址，需 WEIR_E2E_NETWORK 指定 TEST-NET 容器网络")
	}
	var router http.Handler
	secure := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { router.ServeHTTP(w, r) }))
	secure.EnableHTTP2 = true
	secure.StartTLS()
	t.Cleanup(secure.Close)
	// 保留公共 CA，同时让临时客户端和 Gate 信任这次隔离中心的证书。
	bundle, err := os.ReadFile("/etc/ssl/certs/ca-certificates.crt")
	if err != nil {
		t.Fatal(err)
	}
	bundle = append(bundle, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: secure.Certificate().Raw})...)
	caFile := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(caFile, bundle, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SSL_CERT_FILE", caFile)
	e := setup(t)
	router = e.s.Router()
	e.url = secure.URL
	e.admin.Transport = secure.Client().Transport
	e.s.Store.Ent.Setting.Create().SetKey("center-origin").SetValue(map[string]any{"origin": secure.URL}).SaveX(e.ctx)
	sb := e.seedSingbox()
	e.startGate("exits")
	gid := e.gateID
	e.s.Store.Ent.Gate.UpdateOneID(gid).SetPublicHost("127.0.0.1").ExecX(e.ctx)
	proxyPort, hits := socksServer(t)
	proxy := map[string]any{"kind": "socks5", "address": "127.0.0.1", "port": proxyPort, "remark": "shared"}
	n1 := e.post("/api/v1/admin/nodes", map[string]any{"gate_id": gid, "preset": "ss2022", "exit": proxy, "skip_probe": true}, nil)
	eid := int(n1["exit_id"].(float64))
	n2 := e.post("/api/v1/admin/nodes", map[string]any{"gate_id": gid, "preset": "ss2022", "exit_id": eid, "skip_probe": true}, nil)
	direct := e.post("/api/v1/admin/nodes", map[string]any{"gate_id": gid, "preset": "ss2022", "skip_probe": true}, nil)
	linked := e.post("/api/v1/admin/nodes", map[string]any{"gate_id": gid, "preset": "ss2022", "exit": map[string]any{"kind": "link", "link": fmt.Sprintf("socks5://127.0.0.1:%d#linked", proxyPort)}, "skip_probe": true}, nil)
	uid := int(e.post("/api/v1/admin/users", map[string]any{"unlimited": true, "email": "exit@e2e.test", "password": "E2eUser1", "allow_chain": true}, nil)["id"].(float64))
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("exit")) }))
	t.Cleanup(target.Close)
	e.applied("shared exits and direct node")
	client := func(n map[string]any) int { return e.runSingboxClient(sb, e.subOutbound(uid, int(n["id"].(float64)))) }
	p1, p2, pd, pl := client(n1), client(n2), client(direct), client(linked)
	for i, p := range []int{p1, p2, pd, pl} {
		eventually(t, 15*time.Second, "client startup", func() bool { return fetchVia(p, target.URL, 4) == nil })
		before := hits.Load()
		if err := fetchVia(p, target.URL, 4); err != nil {
			t.Fatal(err)
		}
		if (hits.Load() > before) != (i != 2) {
			t.Fatalf("client %d default exit incorrect", i)
		}
	}
	base := fmt.Sprintf("/api/v1/admin/gates/%d/routes", gid)
	rule := e.post(base, map[string]any{"exit_id": eid, "ip": "127.0.0.1/32", "priority": 10}, nil)
	e.applied("selective proxy route")
	before := hits.Load()
	if err := fetchVia(pd, target.URL, 4); err != nil || hits.Load() <= before {
		t.Fatalf("selective route did not use shared exit: %v", err)
	}
	block := e.post("/api/v1/admin/exits", map[string]any{"kind": "blackhole", "remark": "blocked"}, nil)
	e.patch(base+"/"+strconv.Itoa(int(rule["id"].(float64))), map[string]any{"exit_id": block["id"]})
	e.applied("selective block")
	if err := fetchVia(pd, target.URL, 4); err == nil {
		t.Fatal("blackhole route allowed traffic")
	}
	// 管理员代建的用户链路仍属于该用户，走另一台 VPS 并越过管理员的选择性拦截。
	e.startGate("exit-relay")
	relayID := e.gateID
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		t.Fatal(err)
	}
	publicHost := ""
	for _, addr := range addresses {
		ip, _, _ := net.ParseCIDR(addr.String())
		if ip != nil && ip.To4() != nil && !ip.IsLoopback() {
			publicHost = ip.String()
			break
		}
	}
	if publicHost == "" {
		t.Fatal("缺少 TEST-NET 容器地址")
	}
	e.s.Store.Ent.Gate.UpdateOneID(relayID).SetPublicHost(publicHost).ExecX(e.ctx)
	relay := e.post("/api/v1/admin/nodes", map[string]any{"gate_id": relayID, "preset": "vless-reality", "skip_probe": true, "settings": map[string]any{"reality_dest": strings.TrimPrefix(secure.URL, "https://"), "reality_server_names": "example.com"}}, nil)
	e.appliedGate(relayID, "relay before exit verification")
	address := net.JoinHostPort(publicHost, fmt.Sprint(relay["port"]))
	c, err := net.DialTimeout("tcp", address, 3*time.Second)
	if err != nil {
		t.Fatalf("隔离网络节点端口 %s 不可达：%v", address, err)
	}
	_ = c.Close()
	ch := e.post(fmt.Sprintf("/api/v1/admin/users/%d/chains", uid), map[string]any{"name": "relay exit", "entry_node_id": direct["id"], "exit": map[string]any{"kind": "node", "node_id": relay["id"]}, "skip_probe": true}, nil)
	if p, pending := ch["pending"]; pending {
		t.Fatalf("真实出口未通过验证：%v", p)
	}
	e.appliedGate(gid, "user link takes precedence")
	e.appliedGate(relayID, "internal credentials")
	pc := e.runSingboxClient(sb, e.subOutbound(uid, int(direct["id"].(float64)), int(ch["id"].(float64))))
	eventually(t, 15*time.Second, "user chain before block", func() bool { return fetchVia(pc, target.URL, 4) == nil })
	before = hits.Load()
	if err := fetchVia(pc, target.URL, 4); err != nil || hits.Load() != before {
		t.Fatalf("user link did not take precedence: %v", err)
	}
}

type failingRevocationCleanup struct {
	store.KV
	calls int
}

func (kv *failingRevocationCleanup) Del(context.Context, ...string) error {
	kv.calls++
	return errors.New("injected deletion cache cleanup failure")
}

func TestExitRevocationSurvivesCenterRestart(t *testing.T) {
	e := setup(t)
	singbox := e.seedSingbox()
	var gateRouter atomic.Value
	gateRouter.Store(e.s.Router())
	gateServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gateRouter.Load().(http.Handler).ServeHTTP(w, r)
	}))
	t.Cleanup(gateServer.Close)
	e.gateCenter = gateServer.URL

	var gates []int
	var paths []gate.Paths
	var stopAgents []context.CancelFunc
	for _, name := range []string{"revoke-first", "revoke-second"} {
		e.startGate(name)
		e.s.Store.Ent.Gate.UpdateOneID(e.gateID).SetPublicHost("127.0.0.1").ExecX(e.ctx)
		gates = append(gates, e.gateID)
		paths = append(paths, e.paths)
		stopAgents = append(stopAgents, e.stopAgent)
	}
	proxyPort, _ := socksServer(t)
	exit := e.post("/api/v1/admin/exits", map[string]any{"kind": "socks5", "remark": "revocation fixture", "address": "127.0.0.1", "port": proxyPort}, nil)
	node := func(gateID int, exitID any) int {
		fields := map[string]any{"gate_id": gateID, "preset": "ss2022", "skip_probe": true}
		if exitID != nil {
			fields["exit_id"] = exitID
		}
		return int(e.post("/api/v1/admin/nodes", fields, nil)["id"].(float64))
	}
	first, second, kept := node(gates[0], exit["id"]), node(gates[1], exit["id"]), node(gates[1], nil)
	user := e.post("/api/v1/admin/users", map[string]any{"unlimited": true, "email": "revoke@e2e.test"}, nil)
	userID := int(user["id"].(float64))
	for _, gateID := range gates {
		e.appliedGate(gateID, "original credentials applied")
	}
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("test")) }))
	t.Cleanup(target.Close)
	firstPort := e.runSingboxClient(singbox, e.subOutbound(userID, first))
	secondPort := e.runSingboxClient(singbox, e.subOutbound(userID, second))
	keptPort := e.runSingboxClient(singbox, e.subOutbound(userID, kept))
	for _, port := range []int{firstPort, secondPort, keptPort} {
		eventually(t, 15*time.Second, "original client connects", func() bool { return fetchVia(port, target.URL, 4) == nil })
	}
	for _, stop := range stopAgents {
		stop()
	}

	path := e.url + "/api/v1/admin/exits/" + strconv.Itoa(int(exit["id"].(float64)))
	status, preview := call(t, e.admin, "GET", path+"/delete-preview", nil)
	if status != 200 {
		t.Fatalf("delete preview status = %d", status)
	}
	kv := &failingRevocationCleanup{KV: e.s.Store.KV}
	e.s.Store.KV = kv
	status, _ = call(t, e.admin, "DELETE", path, map[string]any{"preview_token": preview["preview_token"]})
	e.s.Store.KV = kv.KV
	if status != 204 || kv.calls == 0 {
		t.Fatalf("delete with cleanup failure = %d, cache cleanup calls = %d", status, kv.calls)
	}
	for _, gateID := range gates {
		row := e.s.Store.Ent.Gate.GetX(e.ctx, gateID)
		if row.ConfigRevision <= row.AppliedRevision {
			t.Fatalf("revocation not pending for offline Gate %d", gateID)
		}
	}

	configPath := e.s.Path
	e.s.Close()
	config, err := center.LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	e.s, err = center.New(config, configPath, fstest.MapFS{"index.html": {Data: []byte("ok")}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.s.Close)
	gateRouter.Store(e.s.Router())
	for _, path := range paths {
		e.paths = path
		e.runAgent()
	}
	for _, gateID := range gates {
		e.appliedGate(gateID, "revocation applied after center restart")
	}
	for _, port := range []int{firstPort, secondPort} {
		if err := fetchVia(port, target.URL, 4); err == nil {
			t.Fatal("removed credentials still pass traffic")
		}
	}
	if err := fetchVia(keptPort, target.URL, 4); err != nil {
		t.Fatalf("unrelated credential stopped working: %v", err)
	}
}
