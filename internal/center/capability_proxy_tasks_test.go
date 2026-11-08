package center

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shusfun/weir/packages/contracts"
)

func TestProxyTaskCapabilitiesConfirmAndDryRun(t *testing.T) {
	s, admin, _ := setupGate(t)
	ctx := t.Context()
	u := s.Store.Ent.User.GetX(ctx, 1)
	pol := contracts.ProxyPolicy{Version: 3, Revision: 7, TargetMigration: []string{"旧目标已转为 PROXY"}}
	s.Store.Ent.User.UpdateOne(u).SetProxyPolicy(policyMap(pol)).ExecX(ctx)
	p := pendingExit{ID: "pending-capability", Actor: u.ID, Subject: u.ID, Kind: "node.update", Status: "failed", Revision: 2, At: time.Now().UTC(), Body: json.RawMessage(`{"password":"isolated-secret"}`)}
	if err := s.savePendingExit(ctx, p); err != nil {
		t.Fatal(err)
	}
	m, _ := s.ensureMCP()
	h := s.mcpHandler(m)
	text, failed := toolText(t, h, m.webToken, "get_pending_exits", map[string]any{"user_id": u.ID})
	if failed || !strings.Contains(text, p.ID) || strings.Contains(text, "isolated-secret") {
		t.Fatalf("读取候选状态：%s", text)
	}
	for _, action := range []string{"proxy.exit_retry", "proxy.exit_discard"} {
		text, failed := toolText(t, h, m.webToken, "submit_draft", map[string]any{"explain": "管理候选", "steps": []any{map[string]any{"action": action, "args": map[string]any{"candidate_id": p.ID, "revision": p.Revision}}}})
		if failed {
			t.Fatalf("%s 未注册或预演失败：%s", action, text)
		}
		stored, err := s.readPendingExit(ctx, p.ID)
		if err != nil || stored.Revision != p.Revision || stored.Status != p.Status {
			t.Fatal("提交草稿提前修改候选")
		}
		draft := latestDraft(t, admin)
		if action == "proxy.exit_retry" {
			mustJSON(t, admin, "DELETE", "/api/v1/admin/ai/drafts/"+draft["id"].(string), nil, 204)
		} else {
			mustJSON(t, admin, "POST", "/api/v1/admin/ai/drafts/"+draft["id"].(string)+"/apply", nil, 200)
		}
	}
	p, _ = s.readPendingExit(ctx, p.ID)
	if p.Status != "discarded" || len(p.Body) != 0 {
		t.Fatal("确认后未放弃候选并回收秘密")
	}
	text, failed = toolText(t, h, m.webToken, "submit_draft", map[string]any{"explain": "确认迁移", "steps": []any{map[string]any{"action": "proxy.migration_ack", "args": map[string]any{"user_id": u.ID, "revision": 7}}}})
	if failed {
		t.Fatal(text)
	}
	if len(loadPolicy(s.Store.Ent.User.GetX(ctx, u.ID)).TargetMigration) == 0 {
		t.Fatal("预演提前确认迁移")
	}
	draft := latestDraft(t, admin)
	mustJSON(t, admin, "POST", "/api/v1/admin/ai/drafts/"+draft["id"].(string)+"/apply", nil, 200)
	if len(loadPolicy(s.Store.Ent.User.GetX(ctx, u.ID)).TargetMigration) != 0 {
		t.Fatal("确认后仍有迁移提示")
	}
}

func TestDraftPendingCandidateCompensation(t *testing.T) {
	s, _, _ := setupGate(t)
	ctx := t.Context()
	actor, err := s.systemActor(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, as := range []string{"", "新出口"} {
		p := pendingExit{ID: "pending-undo-" + as, Actor: 1, Subject: 1, Status: "failed", Revision: 1, At: time.Now().UTC(), Body: json.RawMessage(`{"password":"isolated-secret"}`)}
		if err := s.savePendingExit(ctx, p); err != nil {
			t.Fatal(err)
		}
		cap := Capability{op: &opSpec{method: "POST", path: func(context.Context, *Server, map[string]any) (gin.Params, error) { return nil, nil }, handler: func(*Server) gin.HandlerFunc { return func(c *gin.Context) { c.JSON(202, gin.H{"pending": p.view()}) } }}}
		x := &execCtx{actor: actor}
		undo, err := execOp(cap)(ctx, s, x, &DraftStep{As: as, Args: map[string]any{}})
		if as == "" {
			if err != nil || undo == nil || len(x.outputs) != 1 {
				t.Fatalf("未提供补偿操作：%v", err)
			}
			if err = undo(ctx); err != nil {
				t.Fatal(err)
			}
		} else if err == nil {
			t.Fatal("待验证资源被用于后续步骤引用")
		}
		stored, _ := s.readPendingExit(ctx, p.ID)
		if stored.Status != "discarded" || len(stored.Body) > 0 {
			t.Fatal("候选未被补偿回收")
		}
	}
}
