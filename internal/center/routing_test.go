package center

import (
	"fmt"
	"testing"
)

func TestOutboundsAndRoutes(t *testing.T) {
	_, admin, g := setupGate(t)
	gid := int(g.id)
	mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": gid, "preset": "vless-reality", "skip_probe": true}, 200)
	base := fmt.Sprintf("/api/v1/admin/gates/%d", gid)

	// 系统保留标签、不支持的协议、无地址的 SOCKS5 都拒绝。
	mustJSON(t, admin, "POST", base+"/outbounds", map[string]any{"tag": "direct", "protocol": "freedom"}, 400)
	mustJSON(t, admin, "POST", base+"/outbounds", map[string]any{"tag": "chain-1", "protocol": "freedom"}, 400)
	mustJSON(t, admin, "POST", base+"/outbounds", map[string]any{"tag": "x", "protocol": "wireguard"}, 400)
	mustJSON(t, admin, "POST", base+"/outbounds", map[string]any{"tag": "home", "protocol": "socks"}, 400)

	ob := mustJSON(t, admin, "POST", base+"/outbounds", map[string]any{"tag": "home", "name": "家宽", "protocol": "socks", "address": "203.0.113.20", "port": 1080, "username": "u", "password": "p"}, 200)
	if ob["password"] != "p" {
		t.Fatalf("admin must see the outbound password: %v", ob)
	}
	oid := int(ob["id"].(float64))
	mustJSON(t, admin, "POST", base+"/outbounds", map[string]any{"tag": "home", "protocol": "freedom"}, 409)

	// 规则只能指向已启用的出口或 direct。
	mustJSON(t, admin, "POST", base+"/routes", map[string]any{"outbound_tag": "nope", "domain": "netflix.com"}, 400)
	mustJSON(t, admin, "POST", base+"/routes", map[string]any{"outbound_tag": "home", "domain": "bad domain"}, 400)
	rt := mustJSON(t, admin, "POST", base+"/routes", map[string]any{"outbound_tag": "home", "domain": "domain:netflix.com,\ngeosite:disney", "priority": 5}, 200)
	if rt["domain"] != "domain:netflix.com,geosite:disney" {
		t.Fatalf("route domain = %v", rt["domain"])
	}

	cfg := g.config(0)
	var hasOut, hasRoute bool
	for _, o := range cfg.Outbounds {
		hasOut = hasOut || o.Tag == "home" && o.Password == "p" && o.Port == 1080
	}
	for _, r := range cfg.Routes {
		hasRoute = hasRoute || r.OutboundTag == "home" && r.Priority == adminRouteBase+5
	}
	if !hasOut || !hasRoute {
		t.Fatalf("gate config outbounds=%+v routes=%+v", cfg.Outbounds, cfg.Routes)
	}

	// 被规则引用的出口不能停用、改名或删除。
	mustJSON(t, admin, "PATCH", fmt.Sprintf("%s/outbounds/%d", base, oid), map[string]any{"tag": "home", "protocol": "socks", "address": "203.0.113.20", "port": 1080, "enabled": false}, 409)
	mustJSON(t, admin, "PATCH", fmt.Sprintf("%s/outbounds/%d", base, oid), map[string]any{"tag": "home2", "protocol": "socks", "address": "203.0.113.20", "port": 1080}, 409)
	mustJSON(t, admin, "DELETE", fmt.Sprintf("%s/outbounds/%d", base, oid), nil, 409)
	mustJSON(t, admin, "DELETE", fmt.Sprintf("%s/routes/%d", base, int(rt["id"].(float64))), nil, 204)
	mustJSON(t, admin, "DELETE", fmt.Sprintf("%s/outbounds/%d", base, oid), nil, 204)
	for _, o := range g.config(0).Outbounds {
		if o.Tag == "home" {
			t.Fatal("deleted outbound still delivered")
		}
	}
}
