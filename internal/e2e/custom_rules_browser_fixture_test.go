//go:build e2e

package e2e

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/kernels"
)

// 只在显式启用时提供一次性的真实中心、Gate 与内核，供 ego 验收。
func TestCustomRulesBrowserFixture(t *testing.T) {
	if os.Getenv("WEIR_E2E_CUSTOM_BROWSER") != "1" {
		t.Skip("interactive fixture disabled")
	}
	e := setup(t)
	e.s.Assets = os.DirFS(os.Getenv("WEIR_E2E_CONSOLE"))
	e.seedSingbox()
	if bin := os.Getenv("WEIR_E2E_MIHOMO"); bin != "" {
		data, err := os.ReadFile(bin)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = (kernels.Cache{Dir: filepath.Join(e.dir, "center-data", "kernels")}).Put("mihomo", "1.19.33", "amd64", data); err != nil {
			t.Fatal(err)
		}
	}
	e.startGate("custom-rules-browser")
	e.s.Store.Ent.Gate.UpdateOneID(e.gateID).SetPublicHost("127.0.0.1").ExecX(e.ctx)
	e.post("/api/v1/admin/nodes", map[string]any{"gate_id": e.gateID, "preset": "vless-reality", "name": "清单验收节点", "skip_probe": true, "settings": map[string]any{"reality_dest": localDest(t), "reality_server_names": "e2e.test"}}, nil)
	e.post("/api/v1/admin/users", map[string]any{"email": "custom-user@e2e.test", "password": "CustomUser2026", "all_nodes": true}, nil)
	e.applied("custom browser fixture")
	upstream, _ := url.Parse(e.url)
	proxy := &httputil.ReverseProxy{Rewrite: func(r *httputil.ProxyRequest) { r.SetURL(upstream); r.Out.Host = r.In.Host }}
	srv := &http.Server{Addr: ":19810", Handler: proxy, ReadHeaderTimeout: 5 * time.Second}
	t.Cleanup(func() { srv.Close() })
	t.Log("custom rules browser fixture ready on :19810; data:", e.dir)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		t.Fatal(err)
	}
}
