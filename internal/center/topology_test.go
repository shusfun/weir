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
	if links["node-"+nid] != "/gates?node="+nid {
		t.Fatalf("node link = %v", links)
	}

	// 用户自建链式代理与经同一台 VPS 节点出去的节点都要出现在图上，名字与实际配置一致。
	mustJSON(t, admin, "PATCH", "/api/v1/admin/users/"+uid, map[string]any{"allow_chain": true}, 200)
	ch := mustJSON(t, admin, "POST", "/api/v1/admin/users/"+uid+"/chains", map[string]any{"name": "家里", "entry_node_id": n["id"], "exit": map[string]any{"kind": "socks5", "address": "203.0.113.5", "port": 1080}, "skip_probe": true}, 200)
	rx := mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": int(g.id), "preset": "vless-reality", "skip_probe": true}, 200)
	via := mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": int(g.id), "preset": "ss2022", "skip_probe": true, "chain": map[string]any{"kind": "node", "node_id": rx["id"]}}, 200)
	xid := itoa(int(rx["id"].(float64)))
	v = mustJSON(t, admin, "GET", "/api/v1/admin/gates/"+gid+"/topology", nil, 200)
	byID := map[string]map[string]any{}
	for _, raw := range v["nodes"].([]any) {
		m := raw.(map[string]any)
		byID[m["id"].(string)] = m
	}
	cid := itoa(int(ch["id"].(float64)))
	if c := byID["chain-"+cid]; c == nil || c["kind"] != "route" || byID["exit-chain-"+cid]["sub"] != "203.0.113.5:1080" {
		t.Fatalf("user chain missing from topology: %v", v["nodes"])
	}
	if x := byID["exit-node-"+xid]; x == nil || x["sub"] != "这台 VPS 的节点" || x["link"] != "/gates?node="+xid {
		t.Fatalf("same-VPS exit = %v", x)
	}
	edges := map[string]bool{}
	for _, raw := range v["edges"].([]any) {
		e := raw.(map[string]any)
		edges[e["from"].(string)+">"+e["to"].(string)] = true
	}
	vid := itoa(int(via["id"].(float64)))
	for _, k := range []string{"node-" + nid + ">chain-" + cid, "chain-" + cid + ">exit-chain-" + cid, "node-" + vid + ">exit-node-" + xid} {
		if !edges[k] {
			t.Fatalf("missing edge %s in %v", k, v["edges"])
		}
	}
}
