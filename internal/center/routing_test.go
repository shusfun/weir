package center

import (
	"fmt"
	"testing"

	"github.com/shusfun/weir/packages/contracts"
)

func TestExitsAndRoutes(t *testing.T) {
	s, admin, g := setupGate(t)
	gid := int(g.id)
	mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": gid, "preset": "vless-reality", "skip_probe": true}, 200)
	g.serveActions(t, s, map[string]func(contracts.Action) contracts.ActionResult{
		contracts.ActionChainProbe: func(a contracts.Action) contracts.ActionResult {
			return contracts.ActionResult{OK: true, Result: map[string]any{"mode": "proxy", "latency_ms": 10, "exit_ip": "198.51.100.77"}}
		},
	})
	base := fmt.Sprintf("/api/v1/admin/gates/%d", gid)
	exits := "/api/v1/admin/exits"
	mustJSON(t, admin, "POST", exits, map[string]any{"gate_id": gid, "kind": "freedom"}, 409)
	mustJSON(t, admin, "POST", exits, map[string]any{"gate_id": gid, "kind": "wireguard"}, 422)
	mustJSON(t, admin, "POST", exits, map[string]any{"gate_id": gid, "kind": "socks5"}, 422)
	ob := mustJSON(t, admin, "POST", exits, map[string]any{"gate_id": gid, "kind": "socks5", "remark": "家宽", "address": "203.0.113.20", "port": 1080, "username": "u", "password": "p"}, 200)
	oid := int(ob["id"].(float64))
	tag := "exit-" + itoa(oid)
	if ob["password"] != "p" || ob["tag"] != tag {
		t.Fatalf("exit view = %v", ob)
	}
	stored := s.Store.Ent.Exit.GetX(t.Context(), oid)
	if stored.Password == "p" || stored.Password == "" {
		t.Fatal("exit password was not encrypted")
	}
	path := exits + "/" + itoa(oid)
	mustJSON(t, admin, "POST", base+"/routes", map[string]any{"exit_id": oid}, 400)
	mustJSON(t, admin, "POST", base+"/routes", map[string]any{"exit_id": oid, "domain": "bad domain"}, 400)
	rt := mustJSON(t, admin, "POST", base+"/routes", map[string]any{"exit_id": oid, "domain": "domain:netflix.com,\ngeosite:disney", "priority": 5}, 200)
	rpath := fmt.Sprintf("%s/routes/%d", base, int(rt["id"].(float64)))
	updated := mustJSON(t, admin, "PATCH", rpath, map[string]any{"priority": 7}, 200)
	if updated["domain"] != "domain:netflix.com,geosite:disney" || updated["exit_id"] != float64(oid) {
		t.Fatalf("partial route update cleared fields: %v", updated)
	}
	cfg := g.config(0)
	var hasOut, hasRoute bool
	for _, o := range cfg.Outbounds {
		hasOut = hasOut || o.Tag == tag && o.Protocol == "socks" && o.Password == "p" && o.Port == 1080
	}
	for _, r := range cfg.Routes {
		hasRoute = hasRoute || r.OutboundTag == tag && r.Priority == adminRouteBase+7
	}
	if !hasOut || !hasRoute {
		t.Fatalf("gate config outbounds=%+v routes=%+v", cfg.Outbounds, cfg.Routes)
	}
	mustJSON(t, admin, "PATCH", path, map[string]any{"enabled": false}, 409)
	mustJSON(t, admin, "DELETE", path, nil, 409)
	updated = mustJSON(t, admin, "PATCH", path, map[string]any{"remark": "新备注"}, 200)
	if updated["address"] != "203.0.113.20" || updated["password"] != "p" || updated["tag"] != tag {
		t.Fatalf("partial exit update cleared fields: %v", updated)
	}
	mustJSON(t, admin, "DELETE", rpath, nil, 204)
	mustJSON(t, admin, "DELETE", path, nil, 204)
	for _, o := range g.config(0).Outbounds {
		if o.Tag == tag {
			t.Fatal("deleted exit still delivered")
		}
	}
}
