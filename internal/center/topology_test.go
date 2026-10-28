package center

import "testing"

func TestGateTopologyUsersNodesRoutesExits(t *testing.T) {
	_, admin, g := setupGate(t)
	gid := itoa(int(g.id))
	n := mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": int(g.id), "preset": "ss2022", "skip_probe": true}, 200)
	u := mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "topo@example.test", "name": "小明"}, 200)
	mustJSON(t, admin, "POST", "/api/v1/admin/gates/"+gid+"/outbounds", map[string]any{"tag": "home", "name": "家宽", "protocol": "socks", "address": "198.51.100.30", "port": 1080}, 200)
	mustJSON(t, admin, "POST", "/api/v1/admin/gates/"+gid+"/routes", map[string]any{"outbound_tag": "home", "domain": "example.org", "priority": 10}, 200)

	v := mustJSON(t, admin, "GET", "/api/v1/admin/gates/"+gid+"/topology", nil, 200)
	kinds, links := map[string]int{}, map[string]string{}
	for _, raw := range v["nodes"].([]any) {
		m := raw.(map[string]any)
		kinds[m["kind"].(string)]++
		if l, _ := m["link"].(string); l != "" {
			links[m["id"].(string)] = l
		}
	}
	if kinds["user"] != 1 || kinds["node"] != 1 || kinds["route"] != 1 || kinds["exit"] != 2 {
		t.Fatalf("topology nodes = %v", v["nodes"])
	}
	uid, nid := itoa(int(u["id"].(float64))), itoa(int(n["id"].(float64)))
	if links["user-"+uid] != "/users?user="+uid {
		t.Fatalf("user link = %v", links)
	}
	want := map[string]bool{"user-" + uid + ">node-" + nid: false, "node-" + nid + ">exit-direct": false}
	for _, raw := range v["edges"].([]any) {
		e := raw.(map[string]any)
		key := e["from"].(string) + ">" + e["to"].(string)
		if _, ok := want[key]; ok {
			want[key] = true
		}
	}
	for k, ok := range want {
		if !ok {
			t.Fatalf("missing edge %s in %v", k, v["edges"])
		}
	}
}
