package center

import (
	"testing"
	"time"

	"github.com/shusfun/weir/internal/kernels"
	"github.com/shusfun/weir/packages/contracts"
	"gopkg.in/yaml.v3"
)

func TestSourceNodeAsExitFollowsRefreshAndKeepsLastGood(t *testing.T) {
	s, admin, g := setupGate(t)
	ctx := t.Context()
	// 这台 Gate 上要有节点，才知道用到哪些内核、能否自动切换。
	mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": int(g.id), "preset": "vless-reality", "skip_probe": true}, 200)
	src, err := s.Store.Ent.SubSource.Create().SetName("机场").SetURL("https://example.com/sub").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	gid := itoa(int(g.id))
	// 订阅源还没拉过：保存成功但提示原因，出口暂时为丢弃（不会变成直连）。
	o := mustJSON(t, admin, "POST", "/api/v1/admin/gates/"+gid+"/outbounds", map[string]any{"tag": "hk-pool", "protocol": "source", "source_id": src.ID, "mode": "filter", "match": "^HK"}, 200)
	if o["warning"] == nil {
		t.Fatalf("unresolved exit must warn: %v", o)
	}
	find := func(tag string) contracts.Outbound {
		for _, ob := range g.config(0).Outbounds {
			if ob.Tag == tag {
				return ob
			}
		}
		return contracts.Outbound{}
	}
	if ob := find("hk-pool"); ob.Protocol != "blackhole" {
		t.Fatalf("never resolved = %+v", ob)
	}
	mustJSON(t, admin, "POST", "/api/v1/admin/gates/"+gid+"/outbounds", map[string]any{"tag": "bad", "protocol": "source", "source_id": src.ID, "mode": "filter", "match": "("}, 400)

	// 订阅源刷新后：一组的全部可用成员都下发，由内核自动选择与切换（跳过不能作出口的协议）。
	cache := kvCache{s}
	cache.Store(ctx, src.ID, []string{
		"hysteria2://pw@198.51.100.9:443?sni=a.example.com#HK-QUIC",
		"trojan://pw1@198.51.100.10:443?sni=b.example.com#HK-01",
		"trojan://pw2@198.51.100.11:443?sni=c.example.com#HK-02",
		"trojan://pw3@198.51.100.12:443?sni=d.example.com#JP-01",
	})
	s.RefreshSourceExits(ctx)
	ob := find("hk-pool")
	if ob.Protocol != "group" || len(ob.Members) != 2 || ob.Members[0].Address != "198.51.100.10" || ob.Members[1].Address != "198.51.100.11" {
		t.Fatalf("group = %+v", ob)
	}
	// 刷新后没有匹配：保留上一次可用的。
	cache.Store(ctx, src.ID, []string{"trojan://pw4@198.51.100.13:443?sni=e.example.com#JP-02"})
	s.RefreshSourceExits(ctx)
	if ob := find("hk-pool"); len(ob.Members) != 2 {
		t.Fatalf("must keep last good = %+v", ob)
	}
	// 单个节点：名称完全一致，得到普通出口。
	mustJSON(t, admin, "POST", "/api/v1/admin/gates/"+gid+"/outbounds", map[string]any{"tag": "jp", "protocol": "source", "source_id": src.ID, "mode": "node", "match": "JP-02"}, 200)
	if ob := find("jp"); ob.Protocol != "trojan" || ob.Address != "198.51.100.13" {
		t.Fatalf("single node exit = %+v", ob)
	}
	// 一组超过 50 个：保存报错，提示收紧条件。
	many := []string{}
	for i := 0; i < 51; i++ {
		many = append(many, "trojan://p@198.51.100.20:"+itoa(1000+i)+"?sni=x.example.com#BIG-"+itoa(i))
	}
	cache.Store(ctx, src.ID, many)
	if w := admin.do("POST", "/api/v1/admin/gates/"+gid+"/outbounds", map[string]any{"tag": "big", "protocol": "source", "source_id": src.ID, "mode": "filter", "match": "^BIG"}, nil); decode(t, w)["warning"] == nil {
		t.Fatal("more than 50 members must be reported")
	}
}

func TestSourceGroupNeedsKernelSupport(t *testing.T) {
	s, admin, g := setupGate(t)
	ctx := t.Context()
	mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": int(g.id), "preset": "vless-reality", "skip_probe": true}, 200)
	src, _ := s.Store.Ent.SubSource.Create().SetName("机场").SetURL("https://example.com/sub").Save(ctx)
	// 保存一个驱动：去掉 features，表示这个 Xray 版本不支持自动切换。
	saveDriver(t, s, "xray-26.3", func(m map[string]any) { m["features"] = []any{} })
	w := admin.do("POST", "/api/v1/admin/gates/"+itoa(int(g.id))+"/outbounds", map[string]any{"tag": "pool", "protocol": "source", "source_id": src.ID, "mode": "filter", "match": "."}, nil)
	if body := decode(t, w); w.Code != 409 || body["error"].(map[string]any)["code"] != "group_unsupported" {
		t.Fatalf("unsupported kernel = %d %v", w.Code, body)
	}
	// 单个节点不需要自动切换，照常保存。
	mustJSON(t, admin, "POST", "/api/v1/admin/gates/"+itoa(int(g.id))+"/outbounds", map[string]any{"tag": "one", "protocol": "source", "source_id": src.ID, "mode": "node", "match": "HK-01"}, 200)
}

func TestChainStepThreeSourceAndUserRestriction(t *testing.T) {
	s, admin, g := setupGate(t)
	ctx := t.Context()
	src, _ := s.Store.Ent.SubSource.Create().SetName("机场").SetURL("https://example.com/sub").Save(ctx)
	kvCache{s}.Store(ctx, src.ID, []string{"trojan://pw1@198.51.100.10:443?sni=b.example.com#HK-01", "trojan://pw2@198.51.100.11:443?sni=c.example.com#HK-02"})
	// 管理员建节点，第 ③ 步选订阅源里的一组：全部成员下发。
	n := mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": int(g.id), "preset": "vless-reality", "skip_probe": true,
		"chain": map[string]any{"kind": "source", "source_id": src.ID, "mode": "filter", "match": "^HK"}}, 200)
	chainOf := n["chain"].(map[string]any)
	if chainOf["kind"] != "source" || len(chainOf["source"].(map[string]any)["resolved"].([]any)) != 2 {
		t.Fatalf("node chain = %v", chainOf)
	}
	var group contracts.Outbound
	for _, ob := range g.config(0).Outbounds {
		if ob.Tag == "nodechain-"+itoa(int(n["id"].(float64))) {
			group = ob
		}
	}
	if group.Protocol != "group" || len(group.Members) != 2 {
		t.Fatalf("node chain outbound = %+v", group)
	}

	// 用户自建：只能选分配给他的订阅组里出现的节点，不能直接选订阅源。
	u := mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "chain@example.test", "allow_chain": true}, 200)
	password := u["password"].(string)
	uid := int(u["id"].(float64))
	mine := mustJSON(t, admin, "POST", "/api/v1/admin/misub/profiles", map[string]any{"name": "给小明", "source_ids": []int{src.ID}}, 200)
	other := mustJSON(t, admin, "POST", "/api/v1/admin/misub/profiles", map[string]any{"name": "别人的", "source_ids": []int{src.ID}}, 200)
	mustJSON(t, admin, "PUT", "/api/v1/admin/misub/profiles/"+itoa(int(mine["id"].(float64)))+"/grants", map[string]any{"user_ids": []int{uid}}, 200)
	alice := login(t, s, "chain@example.test", password)
	entry := int(n["id"].(float64))
	body := func(exit map[string]any) map[string]any {
		return map[string]any{"name": "经机场", "entry_node_id": entry, "exit": exit}
	}
	if w := alice.do("POST", "/api/v1/me/chains", body(map[string]any{"kind": "source", "source_id": src.ID, "mode": "node", "match": "HK-01"}), nil); w.Code != 422 {
		t.Fatalf("user must not pick a raw source: %d %s", w.Code, w.Body.String())
	}
	if w := alice.do("POST", "/api/v1/me/chains", body(map[string]any{"kind": "source", "profile_id": int(other["id"].(float64)), "mode": "node", "match": "HK-01"}), nil); w.Code != 422 {
		t.Fatalf("user must not pick an ungranted group: %d %s", w.Code, w.Body.String())
	}
	// 订阅组里的节点名是处理后的名称（默认加地区旗帜），用户在页面上看到、按这个名称选。
	_ = s.Store.Ent.Gate.UpdateOneID(int(g.id)).SetLastSeenAt(nowUTC()).Exec(ctx)
	g.serveActions(t, s, nil)
	ch := mustJSON(t, alice, "POST", "/api/v1/me/chains", body(map[string]any{"kind": "source", "profile_id": int(mine["id"].(float64)), "mode": "node", "match": "🇭🇰 HK-02"}), 200)
	if ch["exit_kind"] != "source" {
		t.Fatalf("user chain = %v", ch)
	}
	// 可选范围：用户只看到分给他的订阅组；管理员看到全部订阅源。
	mineList := mustJSON(t, alice, "GET", "/api/v1/me/chain-sources", nil, 200)["items"].([]any)
	if len(mineList) != 1 || mineList[0].(map[string]any)["kind"] != "profile" || len(mineList[0].(map[string]any)["nodes"].([]any)) != 2 {
		t.Fatalf("user chain sources = %v", mineList)
	}
	if all := mustJSON(t, admin, "GET", "/api/v1/admin/chain-sources", nil, 200)["items"].([]any); len(all) != 1 || all[0].(map[string]any)["kind"] != "source" {
		t.Fatalf("admin chain sources = %v", all)
	}
}

// writeProfile 在中心数据目录写一份覆盖用的版本档案（与内置档案同名时替换它）。
// saveDriver 保存一个驱动版本（覆盖同名内置驱动），edit 改动展开前的原文。
func saveDriver(t *testing.T, s *Server, name string, edit func(m map[string]any)) {
	t.Helper()
	var m map[string]any
	if err := yaml.Unmarshal([]byte(kernels.Builtin()[name]), &m); err != nil {
		t.Fatal(err)
	}
	edit(m)
	b, _ := yaml.Marshal(m)
	if _, err := s.Store.Ent.KernelDriver.Create().SetName(name).SetKernel(m["kernel"].(string)).SetRevision(1).SetText(string(b)).Save(t.Context()); err != nil {
		t.Fatal(err)
	}
	s.dropDriverCache()
}

func nowUTC() time.Time { return time.Now().UTC() }
