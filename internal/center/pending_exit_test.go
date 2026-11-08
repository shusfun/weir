package center

import (
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/store/ent/audit"
	"github.com/shusfun/weir/internal/store/ent/chain"
	"github.com/shusfun/weir/packages/contracts"
)

func TestPendingExitEncryptionAtomicApplyAndRevocation(t *testing.T) {
	s, admin, g := setupGate(t)
	ctx := t.Context()
	var fail atomic.Bool
	g.serveActions(t, s, map[string]func(contracts.Action) contracts.ActionResult{
		contracts.ActionChainProbe: func(contracts.Action) contracts.ActionResult {
			if fail.Load() {
				return contracts.ActionResult{Error: "协议握手失败"}
			}
			return contracts.ActionResult{OK: true}
		},
	})
	n := mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": g.id, "preset": "ss2022", "skip_probe": true}, 200)
	nid := int(n["id"].(float64))
	u := mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "pending@example.test", "all_nodes": true, "allow_chain": true}, 200)
	uid := int(u["id"].(float64))
	owner := login(t, s, "pending@example.test", u["password"].(string))
	exit := map[string]any{"kind": "socks5", "address": "203.0.113.6", "port": 1080, "password": "candidate-secret"}
	fail.Store(true)
	for _, tc := range []struct {
		kind, path string
		body       map[string]any
	}{
		{"node", "/api/v1/admin/nodes/" + itoa(nid), map[string]any{"exit": exit}},
		{"chain", "/api/v1/me/chains", map[string]any{"name": "待验证链", "entry_node_id": nid, "exit": exit}},
		{"type", "/api/v1/proxy/types/ss2022/exit", map[string]any{"revision": 0, "exit": exit}},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			client, method := owner, "POST"
			if tc.kind == "node" {
				client, method = admin, "PATCH"
			}
			if tc.kind == "type" {
				method = "PUT"
			}
			w := client.do(method, tc.path, tc.body, nil)
			if w.Code != 202 {
				t.Fatalf("候选未保存：%d %s", w.Code, w.Body)
			}
			pview := decode(t, w)["pending"].(map[string]any)
			id := pview["id"].(string)
			p, err := s.readPendingExit(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			raw, _, _ := s.Store.KV.Get(ctx, "pending-exit:"+id)
			if strings.Contains(raw, "candidate-secret") || strings.Contains(w.Body.String(), "candidate-secret") {
				t.Fatal("候选秘密泄漏")
			}
			if s.Store.Ent.Node.GetX(ctx, nid).ExitID != int(n["exit_id"].(float64)) || s.Store.Ent.Chain.Query().Where(chain.UserIDEQ(uid)).CountX(ctx) != 0 {
				t.Fatal("未通过候选提前生效")
			}
			// 恢复任务必须重新检查权限，旧候选不会在撤权后生效。
			if tc.kind == "chain" {
				s.Store.Ent.User.UpdateOneID(uid).SetAllowChain(false).ExecX(ctx)
				p.Status = "waiting"
				if err = s.savePendingExit(ctx, p); err != nil {
					t.Fatal(err)
				}
				s.resumePendingExits(ctx)
				p, _ = s.readPendingExit(ctx, id)
				if p.Status != "stale" {
					t.Fatal(p.Status, p.Reason)
				}
				s.Store.Ent.User.UpdateOneID(uid).SetAllowChain(true).ExecX(ctx)
				return
			}
			mustJSON(t, client, "DELETE", "/api/v1/proxy/pending-exits/"+id+"?dryRun=true", map[string]any{"revision": p.Revision}, 200)
			unchanged, _ := s.readPendingExit(ctx, id)
			if unchanged.Status != p.Status {
				t.Fatal("预演修改候选")
			}
			mustJSON(t, client, "DELETE", "/api/v1/proxy/pending-exits/"+id, map[string]any{"revision": p.Revision + 1}, 409)
			if tc.kind == "node" {
				trace := "96378d39d6914d6f99530b17b0f79763"
				w := client.do("POST", "/api/v1/proxy/pending-exits/"+id+"/retry", map[string]any{"revision": p.Revision}, map[string]string{"traceparent": "00-" + trace + "-1111111111111111-01"})
				if w.Code != 202 {
					t.Fatal(w.Code, w.Body)
				}
				a := s.Store.Ent.Audit.Query().Where(audit.ActionEQ("exit.retry"), audit.TargetEQ("candidate:"+id)).OnlyX(ctx)
				if a.Detail["trace_id"] != trace || a.Actor != 1 {
					t.Fatal("重试操作关联缺失", a)
				}
				for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
					p, _ = s.readPendingExit(ctx, id)
					if p.Status != "waiting" {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("候选重试未完成")
					}
				}
				mustJSON(t, client, "DELETE", "/api/v1/proxy/pending-exits/"+id, map[string]any{"revision": p.Revision}, 200)
				p, _ = s.readPendingExit(ctx, id)
				if len(p.Body) > 0 || p.Status != "discarded" {
					t.Fatal("候选放弃后保留秘密")
				}
				return
			}
			fail.Store(false)
			p.Status = "waiting"
			if err = s.savePendingExit(ctx, p); err != nil {
				t.Fatal(err)
			}
			s.resumePendingExits(ctx)
			p, _ = s.readPendingExit(ctx, id)
			if p.Status != "applied" {
				t.Fatal(p.Status, p.Reason)
			}
			cs := s.Store.Ent.Chain.Query().Where(chain.UserIDEQ(uid)).AllX(ctx)
			if len(cs) != 1 || loadPolicy(s.Store.Ent.User.GetX(ctx, uid)).Revision != 1 {
				t.Fatal("类型出口未整体生效")
			}
			b, _ := json.Marshal(p.view())
			if strings.Contains(string(b), "candidate-secret") || len(p.Body) > 0 {
				t.Fatal("已应用候选未回收秘密")
			}
		})
	}
}

func TestPendingTypeExitAllNodesAndPermissions(t *testing.T) {
	s, admin, g := setupGate(t)
	ctx := t.Context()
	var failing atomic.Bool
	var calls atomic.Int32
	g.serveActions(t, s, map[string]func(contracts.Action) contracts.ActionResult{
		contracts.ActionChainProbe: func(contracts.Action) contracts.ActionResult {
			if failing.Load() && calls.Add(1)%2 == 0 {
				return contracts.ActionResult{Error: "第二个入口握手失败"}
			}
			return contracts.ActionResult{OK: true}
		},
	})
	for range 2 {
		mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": g.id, "preset": "ss2022", "skip_probe": true}, 200)
	}
	u := mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "atomic-type@example.test", "all_nodes": true, "allow_chain": true}, 200)
	uid := int(u["id"].(float64))
	owner := login(t, s, "atomic-type@example.test", u["password"].(string))
	exit := func(address string) map[string]any {
		return map[string]any{"kind": "socks5", "address": address, "port": 1080}
	}
	mustJSON(t, owner, "PUT", "/api/v1/proxy/types/ss2022/exit", map[string]any{"revision": 0, "exit": exit("203.0.113.6")}, 200)
	before := s.Store.Ent.Chain.Query().Where(chain.UserIDEQ(uid)).AllX(ctx)
	if len(before) != 2 {
		t.Fatal("类型出口未覆盖两个入口")
	}
	failing.Store(true)
	w := owner.do("PUT", "/api/v1/proxy/types/ss2022/exit", map[string]any{"revision": 1, "exit": exit("203.0.113.7")}, nil)
	if w.Code != 202 {
		t.Fatalf("未保留失败候选：%d %s", w.Code, w.Body)
	}
	id := decode(t, w)["pending"].(map[string]any)["id"].(string)
	p, err := s.readPendingExit(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	for _, ch := range before {
		if s.Store.Ent.Chain.GetX(ctx, ch.ID).ExitID != ch.ExitID {
			t.Fatal("候选未全通过却替换了部分出口")
		}
	}
	if loadPolicy(s.Store.Ent.User.GetX(ctx, uid)).Revision != 1 {
		t.Fatal("失败候选推进了生效版本")
	}
	mustJSON(t, owner, "GET", "/api/v1/proxy/pending-exits?all=true", nil, 403)
	all := mustJSON(t, admin, "GET", "/api/v1/proxy/pending-exits?all=true", nil, 200)
	if len(all["items"].([]any)) != 1 {
		t.Fatal("管理员未看到其他用户的候选")
	}
	mustJSON(t, owner, "POST", "/api/v1/proxy/pending-exits/"+id+"/retry?dryRun=true", map[string]any{"revision": p.Revision + 1}, 409)
	mustJSON(t, owner, "POST", "/api/v1/proxy/pending-exits/"+id+"/retry?dryRun=true", map[string]any{"revision": p.Revision}, 200)
	failing.Store(false)
	p.Status = "waiting"
	if err := s.savePendingExit(ctx, p); err != nil {
		t.Fatal(err)
	}
	s.resumePendingExits(ctx)
	p, _ = s.readPendingExit(ctx, id)
	if p.Status != "applied" || loadPolicy(s.Store.Ent.User.GetX(ctx, uid)).Revision != 2 {
		t.Fatal("补测未整体应用", p.Status, p.Reason)
	}
	for _, ch := range s.Store.Ent.Chain.Query().Where(chain.UserIDEQ(uid)).AllX(ctx) {
		if s.Store.Ent.Exit.GetX(ctx, ch.ExitID).Address != "203.0.113.7" {
			t.Fatal("整体切换后仍有旧出口")
		}
	}
}
