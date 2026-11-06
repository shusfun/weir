//go:build e2e

package e2e

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"testing"
	"time"
)

// Explicitly enabled disposable fixture for ego-browser acceptance. All kernels
// and paths remain in the normal e2e environment and are removed on termination.
func TestResourcesBrowserFixture(t *testing.T) {
	if os.Getenv("WEIR_E2E_BROWSER_FIXTURE") != "1" {
		t.Skip("interactive browser fixture is disabled")
	}
	e := setup(t)
	assets := os.Getenv("WEIR_E2E_CONSOLE")
	if assets == "" {
		t.Fatal("WEIR_E2E_CONSOLE must point to the built console")
	}
	e.s.Assets = os.DirFS(assets)
	e.seedSingbox()
	e.s.StartBackground(e.ctx)
	e.startGate("资源验收 VPS")
	e.s.Store.Ent.Gate.UpdateOneID(e.gateID).SetPublicHost("127.0.0.1").ExecX(e.ctx)
	proxyPort, _ := socksServer(t)
	direct := e.post("/api/v1/admin/nodes", map[string]any{"gate_id": e.gateID, "preset": "ss2022", "name": "入口直连", "skip_probe": true}, nil)
	e.post("/api/v1/admin/nodes", map[string]any{"gate_id": e.gateID, "preset": "ss2022", "name": "入口链式", "skip_probe": true, "exit": map[string]any{"kind": "socks5", "address": "127.0.0.1", "port": proxyPort, "remark": "出口备注：家庭线路"}}, nil)
	e.post("/api/v1/admin/nodes", map[string]any{"gate_id": e.gateID, "preset": "vless-reality", "name": "入口 Xray", "skip_probe": true, "settings": map[string]any{"reality_dest": localDest(t), "reality_server_names": "e2e.test"}}, nil)
	u := e.post("/api/v1/admin/users", map[string]any{"email": "resource-user@e2e.test", "password": "ResourceUser2026", "allow_chain": true, "all_nodes": true}, nil)
	e.applied("entry fixture")
	entryGate := e.gateID
	e.startGate("出口 VPS")
	e.s.Store.Ent.Gate.UpdateOneID(e.gateID).SetPublicHost("127.0.0.1").ExecX(e.ctx)
	relay := e.post("/api/v1/admin/nodes", map[string]any{"gate_id": e.gateID, "preset": "vless-reality", "name": "出口节点", "skip_probe": true, "settings": map[string]any{"reality_dest": localDest(t), "reality_server_names": "e2e.test"}}, nil)
	e.post(fmt.Sprintf("/api/v1/admin/users/%d/chains", int(u["id"].(float64))), map[string]any{"name": "手动链备注", "entry_node_id": direct["id"], "exit": map[string]any{"kind": "node", "node_id": relay["id"]}, "skip_probe": true}, nil)
	e.appliedGate(entryGate, "manual chain fixture")
	e.applied("browser fixture")
	upstream, _ := url.Parse(e.url)
	proxy := &httputil.ReverseProxy{Rewrite: func(r *httputil.ProxyRequest) {
		r.SetURL(upstream)
		r.Out.Host = r.In.Host
	}}
	srv := &http.Server{Addr: ":19810", Handler: proxy, ReadHeaderTimeout: 5 * time.Second}
	t.Cleanup(func() { srv.Close() })
	t.Log("browser fixture ready on :19810")
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		t.Fatal(err)
	}
}
