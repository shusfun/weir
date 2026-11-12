package center

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/gin-gonic/gin"
	"github.com/shusfun/weir/internal/policydata"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/resource"
	"github.com/shusfun/weir/internal/store/ent/setting"
	"github.com/shusfun/weir/packages/contracts"
)

const workerPolicyKey = "worker-policy"

func resolveWorkerPolicy(doc policydata.Document) (policydata.View, contracts.WorkerBudget, error) {
	var defaults policydata.Values
	raw, _ := json.Marshal(contracts.DefaultWorkerBudget())
	_ = json.Unmarshal(raw, &defaults)
	var budget contracts.WorkerBudget
	view, err := policydata.Resolve(defaults, doc, func(v policydata.Values) error {
		raw, _ := json.Marshal(v)
		if err := contracts.DecodeStrict(raw, &budget, 4096); err != nil {
			return err
		}
		return budget.Validate()
	})
	return view, budget, err
}

func (s *Server) workerPolicy(ctx context.Context) (policydata.View, contracts.WorkerBudget, error) {
	doc := policydata.Empty()
	row, err := s.Store.Ent.Setting.Query().Where(setting.KeyEQ(workerPolicyKey)).Only(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return policydata.View{}, contracts.WorkerBudget{}, err
	}
	if err == nil {
		raw, _ := json.Marshal(row.Value)
		if err = contracts.DecodeStrict(raw, &doc, 16<<10); err != nil {
			return policydata.View{}, contracts.WorkerBudget{}, err
		}
	}
	return resolveWorkerPolicy(doc)
}

func (s *Server) workerPolicyRoutes(admin *gin.RouterGroup) {
	admin.GET("/workers/policy/history", func(c *gin.Context) {
		rows, err := s.Store.Ent.Resource.Query().Where(resource.KindEQ("worker-policy-version")).Order(ent.Desc(resource.FieldID)).Limit(100).All(c.Request.Context())
		if err != nil {
			failInternal(c, 503, "worker_policy_unavailable", err)
			return
		}
		items := make([]gin.H, 0, len(rows))
		for _, row := range rows {
			var doc policydata.Document
			raw, _ := json.Marshal(row.Data["document"])
			if err = contracts.DecodeStrict(raw, &doc, 16<<10); err == nil {
				_, _, err = resolveWorkerPolicy(doc)
			}
			if err != nil {
				failInternal(c, 503, "worker_policy_unavailable", err)
				return
			}
			items = append(items, gin.H{"revision": row.Key, "document": doc, "created_at": row.CreatedAt})
		}
		c.JSON(200, gin.H{"items": items})
	})
	admin.GET("/workers/policy", func(c *gin.Context) {
		view, _, err := s.workerPolicy(c.Request.Context())
		if err != nil {
			failInternal(c, 503, "worker_policy_unavailable", err)
			return
		}
		c.JSON(200, view)
	})
	admin.POST("/workers/policy/draft", func(c *gin.Context) {
		var req struct {
			Text string `json:"text"`
		}
		if !bindWorker(c, &req, 32<<10) {
			return
		}
		d, err := s.submitActionDraft(c.Request.Context(), "dsh", "调整 VPS 后台任务资源预算，需管理员确认", []DraftStep{{Action: "worker.policy.update", Args: map[string]any{"text": req.Text}}})
		if err != nil {
			failDetail(c, 422, "worker_policy_invalid", err.Error(), nil)
			return
		}
		c.JSON(201, d)
	})
}

var workerPolicyCaps = []Capability{{
	Name: "worker.policy.update", Title: "修改 VPS 后台任务预算", Kind: "draft", Risk: "danger", Last: true,
	Purpose: "有界修改后台并发与所有网络任务共用的正文带宽预算；大型下载始终单并发，修改需管理员确认。",
	Params:  []Param{{Key: "text", Label: "任务预算文件（JSON）", Kind: "text", Required: true}},
	plan: func(ctx context.Context, s *Server, p *planCtx, step *DraftStep) error {
		before, _, err := s.workerPolicy(ctx)
		if err != nil {
			return err
		}
		text, _ := step.Args["text"].(string)
		var doc policydata.Document
		if err = contracts.DecodeStrict([]byte(text), &doc, 16<<10); err != nil {
			return err
		}
		after, _, err := resolveWorkerPolicy(doc)
		if err != nil {
			return err
		}
		if before.Revision == after.Revision {
			return errors.New("worker policy unchanged")
		}
		p.versions[workerPolicyKey] = before.Revision
		step.Result = map[string]any{"expected_revision": before.Revision}
		old, _ := json.MarshalIndent(before.Document, "", "  ")
		next, _ := json.MarshalIndent(doc, "", "  ")
		step.Diff = fullProfileDiff(string(old), string(next))
		step.Preview = []string{"并发 1–32，默认 1；大型下载单并发", "带宽按每台 VPS 的后台 HTTP 收发正文合计限制，范围 64 KiB/s–128 MiB/s", "不影响代理连接；配置同步、心跳、计量不计入此预算", "降低并发不会中止已有任务，后续领取使用新预算"}
		return nil
	},
	exec: func(ctx context.Context, s *Server, x *execCtx, step *DraftStep) (func(context.Context) error, error) {
		text, _ := step.Args["text"].(string)
		expected, _ := step.Result["expected_revision"].(string)
		var doc policydata.Document
		if err := contracts.DecodeStrict([]byte(text), &doc, 16<<10); err != nil {
			return nil, err
		}
		after, _, err := resolveWorkerPolicy(doc)
		if err != nil {
			return nil, err
		}
		err = s.Store.Lock.Do(ctx, "worker-policy", func(ctx context.Context) error {
			before, _, err := s.workerPolicy(ctx)
			if err != nil {
				return err
			}
			if before.Revision != expected {
				return errors.New("worker policy changed; preview again")
			}
			tx, err := s.Store.Ent.Tx(ctx)
			if err != nil {
				return err
			}
			defer tx.Rollback()
			for _, view := range []policydata.View{before, after} {
				if err = tx.Resource.Create().SetKind("worker-policy-version").SetKey(view.Revision).SetData(map[string]any{"document": view.Document, "trace_id": taskTrace(ctx)}).OnConflictColumns(resource.FieldKind, resource.FieldKey).Ignore().Exec(ctx); err != nil {
					return err
				}
			}
			if err = tx.Setting.Create().SetKey(workerPolicyKey).SetValue(toMap(doc)).OnConflictColumns(setting.FieldKey).UpdateNewValues().Exec(ctx); err != nil {
				return err
			}
			if _, err = tx.Audit.Create().SetActor(x.actor.GetInt("user_id")).SetAction("worker.policy.apply").SetTarget(after.Revision).SetResult("ok").SetDetail(map[string]any{"trace_id": taskTrace(ctx), "previous": before.Revision}).Save(ctx); err != nil {
				return err
			}
			return tx.Commit()
		})
		if err == nil {
			_ = s.Store.Notifications.Publish(ctx, "workers")
		}
		return nil, err
	},
}}
