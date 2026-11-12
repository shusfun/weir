package center

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shusfun/weir/internal/policydata"
	"github.com/shusfun/weir/internal/protection"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/resource"
	"github.com/shusfun/weir/internal/store/ent/setting"
	"github.com/shusfun/weir/packages/contracts"
)

const protectionPolicyKey = "protection-policy"
const protectionActionKind = "protection-action"

func resolveProtectionPolicy(doc policydata.Document) (policydata.View, protection.Policy, error) {
	var result protection.Policy
	var defaults policydata.Values
	b, _ := json.Marshal(protection.Default())
	_ = json.Unmarshal(b, &defaults)
	view, err := policydata.Resolve(defaults, doc, func(values policydata.Values) error {
		b, _ := json.Marshal(values)
		if err := contracts.DecodeStrict(b, &result, 64<<10); err != nil {
			return err
		}
		return result.Validate()
	})
	return view, result, err
}

func (s *Server) protectionPolicy(ctx context.Context) (policydata.View, protection.Policy, error) {
	doc := policydata.Empty()
	row, err := s.Store.Ent.Setting.Query().Where(setting.KeyEQ(protectionPolicyKey)).Only(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return policydata.View{}, protection.Policy{}, err
	}
	if err == nil {
		b, _ := json.Marshal(row.Value)
		if err = contracts.DecodeStrict(b, &doc, 64<<10); err != nil {
			return policydata.View{}, protection.Policy{}, err
		}
	}
	return resolveProtectionPolicy(doc)
}

func (s *Server) protectionRoutes(admin *gin.RouterGroup) {
	admin.GET("/defense/policy/history", func(c *gin.Context) {
		rows, err := s.Store.Ent.Resource.Query().Where(resource.KindEQ("protection-policy-version")).Order(ent.Desc(resource.FieldID)).Limit(100).All(c.Request.Context())
		if err != nil {
			failInternal(c, 503, "protection_policy_unavailable", err)
			return
		}
		items := make([]gin.H, 0, len(rows))
		for _, row := range rows {
			var doc policydata.Document
			raw, _ := json.Marshal(row.Data["document"])
			if err = contracts.DecodeStrict(raw, &doc, 64<<10); err == nil {
				_, _, err = resolveProtectionPolicy(doc)
			}
			if err != nil {
				failInternal(c, 503, "protection_policy_unavailable", err)
				return
			}
			items = append(items, gin.H{"revision": row.Key, "document": doc, "created_at": row.CreatedAt})
		}
		c.JSON(200, gin.H{"items": items})
	})
	admin.GET("/defense/policy", func(c *gin.Context) {
		v, p, err := s.protectionPolicy(c.Request.Context())
		if err != nil {
			failInternal(c, 503, "protection_policy_unavailable", err)
			return
		}
		actions, err := s.protectionActions(c.Request.Context())
		if err != nil {
			failInternal(c, 503, "protection_policy_unavailable", err)
			return
		}
		count := 0
		for _, action := range actions {
			if action.automaticOrigin() && action.CreatedAt.After(time.Now().Add(-time.Hour)) {
				count++
			}
		}
		c.JSON(200, gin.H{"policy": v, "defaults": protection.Default(), "hourly_used": count, "hourly_limit": p.HourlyLimit, "window": "rolling_1h"})
	})
	admin.POST("/defense/policy/draft", func(c *gin.Context) {
		var req struct {
			Text string `json:"text"`
		}
		if !bindWorker(c, &req, 128<<10) {
			return
		}
		d, err := s.submitActionDraft(c.Request.Context(), "dsh", "修改自动安全处置开关或保护边界，需管理员确认", []DraftStep{{Action: "defense.policy.update", Args: map[string]any{"text": req.Text}}})
		if err != nil {
			failDetail(c, 422, "protection_policy_invalid", err.Error(), nil)
			return
		}
		c.JSON(201, d)
	})
	admin.GET("/defense/automatic", s.listProtectionActions)
	admin.POST("/defense/automatic/:id/revoke", s.revokeProtectionAction)
	admin.POST("/defense/automatic/:id/promote", func(c *gin.Context) {
		d, err := s.submitActionDraft(c.Request.Context(), "dsh", "将安全临时处置转为永久，需管理员确认", []DraftStep{{Action: "defense.action.promote", Args: map[string]any{"action_id": c.Param("id")}}})
		if err != nil {
			failDetail(c, 422, "protection_promotion_invalid", err.Error(), nil)
			return
		}
		c.JSON(201, d)
	})
}

var protectionCaps = []Capability{
	{Name: "defense.policy.update", Title: "修改自动安全处置护栏", Kind: "draft", Risk: "danger", Last: true,
		Purpose: "修改自动收紧开关、保护名单和有界次数期限，始终由管理员确认。",
		Params:  []Param{{Key: "text", Label: "策略文件（JSON）", Kind: "text", Required: true}},
		plan: func(ctx context.Context, s *Server, p *planCtx, st *DraftStep) error {
			before, _, err := s.protectionPolicy(ctx)
			if err != nil {
				return err
			}
			text, _ := st.Args["text"].(string)
			var doc policydata.Document
			if err = contracts.DecodeStrict([]byte(text), &doc, 64<<10); err != nil {
				return err
			}
			after, _, err := resolveProtectionPolicy(doc)
			if err != nil {
				return err
			}
			if after.Revision == before.Revision {
				return errors.New("protection policy unchanged")
			}
			p.versions[protectionPolicyKey] = before.Revision
			st.Result = map[string]any{"expected_revision": before.Revision}
			b, _ := json.MarshalIndent(before.Document, "", "  ")
			a, _ := json.MarshalIndent(doc, "", "  ")
			st.Diff = fullProfileDiff(string(b), string(a))
			st.Preview = []string{"仅安全收紧可自动生效；优化、放宽及 AI 自修改始终需确认", "关闭总开关后已有自动处置也暂停执行；所有记录保留", "增加上限、移除保护项或启用自动操作会扩大自动处置权限，请核对完整差异"}
			return nil
		},
		exec: func(ctx context.Context, s *Server, x *execCtx, st *DraftStep) (func(context.Context) error, error) {
			text, _ := st.Args["text"].(string)
			var doc policydata.Document
			if err := contracts.DecodeStrict([]byte(text), &doc, 64<<10); err != nil {
				return nil, err
			}
			expected, _ := st.Result["expected_revision"].(string)
			return nil, s.saveProtectionPolicy(ctx, doc, expected, x.actor.GetInt("user_id"))
		}},
}

func (s *Server) saveProtectionPolicy(ctx context.Context, doc policydata.Document, expected string, actor int) error {
	after, _, err := resolveProtectionPolicy(doc)
	if err != nil {
		return err
	}
	return s.Store.Lock.Do(ctx, "automatic-protection", func(ctx context.Context) error {
		before, _, err := s.protectionPolicy(ctx)
		if err != nil {
			return err
		}
		if before.Revision != expected {
			return errors.New("protection policy changed; preview again")
		}
		tx, err := s.Store.Ent.Tx(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		for _, v := range []policydata.View{before, after} {
			if err = tx.Resource.Create().SetKind("protection-policy-version").SetKey(v.Revision).SetData(map[string]any{"document": v.Document, "trace_id": taskTrace(ctx), "actor": actor}).OnConflictColumns(resource.FieldKind, resource.FieldKey).Ignore().Exec(ctx); err != nil {
				return err
			}
		}
		if err = tx.Setting.Create().SetKey(protectionPolicyKey).SetValue(toMap(doc)).OnConflictColumns(setting.FieldKey).UpdateNewValues().Exec(ctx); err != nil {
			return err
		}
		if _, err = tx.Audit.Create().SetActor(actor).SetAction("defense.policy.apply").SetTarget(after.Revision).SetResult("ok").SetDetail(map[string]any{"trace_id": taskTrace(ctx), "previous": before.Revision}).Save(ctx); err != nil {
			return err
		}
		return tx.Commit()
	})
}
