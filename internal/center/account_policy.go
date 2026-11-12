package center

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shusfun/weir/internal/accountsecurity"
	"github.com/shusfun/weir/internal/policydata"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/resource"
	"github.com/shusfun/weir/internal/store/ent/setting"
	"github.com/shusfun/weir/packages/contracts"
)

const accountPolicyKey = "account-policy"
const accountPolicyVersionKind = "account-policy-version"

func (s *Server) accountPolicy(ctx context.Context) (policydata.View, error) {
	doc := policydata.Empty()
	row, err := s.Store.Ent.Setting.Query().Where(setting.KeyEQ(accountPolicyKey)).Only(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return policydata.View{}, err
	}
	if err == nil {
		b, e := json.Marshal(row.Value)
		if e != nil {
			return policydata.View{}, e
		}
		if e = contracts.DecodeStrict(b, &doc, 64<<10); e != nil {
			return policydata.View{}, e
		}
	}
	return accountsecurity.ResolvePolicy(doc)
}

func (s *Server) saveAccountPolicy(ctx context.Context, doc policydata.Document, expected string, actor int) error {
	ctx = withTrace(ctx, taskTrace(ctx))
	after, err := accountsecurity.ResolvePolicy(doc)
	if err != nil {
		return err
	}
	if err = s.validateHumanProvider(ctx, after.Effective); err != nil {
		return err
	}
	return s.Store.Lock.Do(ctx, "registration-identity", func(ctx context.Context) error {
		before, err := s.accountPolicy(ctx)
		if err != nil {
			return err
		}
		if before.Revision != expected {
			return errors.New("account policy changed; preview again")
		}
		if before.Revision == after.Revision {
			return errors.New("account policy is unchanged")
		}
		tx, err := s.Store.Ent.Tx(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		for _, v := range []policydata.View{before, after} {
			exists, e := tx.Resource.Query().Where(resource.KindEQ(accountPolicyVersionKind), resource.KeyEQ(v.Revision)).Exist(ctx)
			if e != nil {
				return e
			}
			if !exists {
				if _, e = tx.Resource.Create().SetKind(accountPolicyVersionKind).SetKey(v.Revision).SetData(map[string]any{"document": v.Document, "parent": before.Revision, "actor": actor, "trace_id": taskTrace(ctx), "created_at": time.Now().UTC()}).Save(ctx); e != nil {
					return e
				}
			}
		}
		if err = tx.Setting.Create().SetKey(accountPolicyKey).SetValue(toMap(doc)).OnConflictColumns(setting.FieldKey).UpdateNewValues().Exec(ctx); err != nil {
			return err
		}
		if _, err = tx.Audit.Create().SetActor(actor).SetAction("account.policy.apply").SetTarget(after.Revision).SetResult("ok").SetDetail(map[string]any{"trace_id": taskTrace(ctx), "previous": before.Revision}).Save(ctx); err != nil {
			return err
		}
		return tx.Commit()
	})
}

func (s *Server) accountPolicyRoutes(admin *gin.RouterGroup) {
	admin.GET("/settings/account-policy", func(c *gin.Context) {
		v, err := s.accountPolicy(c.Request.Context())
		if err != nil {
			failInternal(c, 503, "account_policy_unavailable", err)
			return
		}
		c.JSON(200, v)
	})
	admin.GET("/settings/account-policy/history", func(c *gin.Context) {
		rows, err := s.Store.Ent.Resource.Query().Where(resource.KindEQ(accountPolicyVersionKind)).Order(ent.Desc(resource.FieldID)).Limit(100).All(c.Request.Context())
		if err != nil {
			failInternal(c, 503, "account_policy_unavailable", err)
			return
		}
		items := make([]gin.H, 0, len(rows))
		for _, row := range rows {
			var doc policydata.Document
			b, err := json.Marshal(row.Data["document"])
			if err == nil {
				err = contracts.DecodeStrict(b, &doc, 64<<10)
			}
			if err == nil {
				_, err = accountsecurity.ResolvePolicy(doc)
			}
			if err != nil {
				failInternal(c, 503, "account_policy_unavailable", err)
				return
			}
			items = append(items, gin.H{"key": row.Key, "data": gin.H{"document": doc, "created_at": row.CreatedAt}})
		}
		c.JSON(200, gin.H{"items": items})
	})
	admin.POST("/settings/account-policy/draft", func(c *gin.Context) {
		var req struct {
			Text string `json:"text"`
		}
		if !bindWorker(c, &req, 128<<10) {
			return
		}
		d, err := s.submitActionDraft(c.Request.Context(), "dsh", "修改账户验证码与发信策略，需管理员确认差异", []DraftStep{{Action: "account.policy.update", Args: map[string]any{"text": req.Text}}})
		if err != nil {
			failDetail(c, 422, "account_policy_invalid", err.Error(), nil)
			return
		}
		s.audit(c, "account.policy.draft", d.ID, "passed")
		c.JSON(201, d)
	})
}

var accountPolicyCaps = []Capability{
	{Name: "get_account_policy", Title: "账户验证码与发信策略", Kind: "read", Purpose: "读取内置、外部、站点三层策略、来源和版本。不返回邮箱、IP 或验证码。", read: func(ctx context.Context, s *Server, _ map[string]any) (any, error) { return s.accountPolicy(ctx) }},
	{Name: "account.policy.update", Title: "修改账户验证码与发信策略", Kind: "draft", Risk: "danger", Last: true, Purpose: "校验有界配置并提交完整差异。放宽和回滚均需管理员确认，不可自动生效。", Params: []Param{{Key: "text", Label: "策略文件（JSON）", Kind: "text", Required: true}},
		plan: func(ctx context.Context, s *Server, p *planCtx, st *DraftStep) error {
			before, err := s.accountPolicy(ctx)
			if err != nil {
				return err
			}
			text, _ := st.Args["text"].(string)
			var doc policydata.Document
			if err = contracts.DecodeStrict([]byte(text), &doc, 64<<10); err != nil {
				return err
			}
			after, err := accountsecurity.ResolvePolicy(doc)
			if err != nil {
				return err
			}
			if before.Revision == after.Revision {
				return errors.New("account policy is unchanged")
			}
			p.versions[accountPolicyKey] = before.Revision
			st.Result = map[string]any{"expected_revision": before.Revision}
			b, _ := json.MarshalIndent(before.Document, "", "  ")
			a, _ := json.MarshalIndent(doc, "", "  ")
			st.Diff = fullProfileDiff(string(b), string(a))
			if err := s.validateHumanProvider(ctx, after.Effective); err != nil {
				return err
			}
			st.Preview = []string{"按 UTC 小时计数发码尝试；邮箱、IP、网段、域名和全站均受限", "修改不撤销已发验证码；已有验证码保留原有效期与尝试上限", "切换人机服务后需取得新挑战；已签发挑战保留原难度与期限，登录始终使用本地 ALTCHA", "历史影响统计未检测；不会将缺失数据当作 0"}
			for name, raw := range after.Effective {
				var old, n float64
				if json.Unmarshal(raw, &n) != nil || json.Unmarshal(before.Effective[name], &old) != nil {
					continue
				}
				lowerLoosens := name == "human_pow_cost" || name == "human_pow_counter" || name == "human_min_seconds"
				if (!lowerLoosens && n > old) || (lowerLoosens && n < old) {
					st.Preview = append(st.Preview, "放宽警告："+name+" 的验证强度降低，或允许的请求量、有效期、尝试次数增加")
				}
			}
			var oldCooldown, newCooldown [3]int
			_ = json.Unmarshal(before.Effective["code_cooldown_seconds"], &oldCooldown)
			_ = json.Unmarshal(after.Effective["code_cooldown_seconds"], &newCooldown)
			for i, n := range newCooldown {
				if n < oldCooldown[i] {
					st.Preview = append(st.Preview, "放宽警告：验证码重发冷却缩短")
					break
				}
			}
			for _, name := range []string{"email_allow_suffixes", "email_deny_suffixes", "email_disposable_exceptions", "email_disposable_additions", "email_alias_rules"} {
				if string(before.Effective[name]) != string(after.Effective[name]) {
					st.Preview = append(st.Preview, "名单或别名规则变化：可能放宽注册限制，请核对完整差异；不会合并或修改已有账户")
					break
				}
			}
			return nil
		},
		exec: func(ctx context.Context, s *Server, x *execCtx, st *DraftStep) (func(context.Context) error, error) {
			text, _ := st.Args["text"].(string)
			var doc policydata.Document
			if err := contracts.DecodeStrict([]byte(text), &doc, 64<<10); err != nil {
				return nil, err
			}
			expected, _ := st.Result["expected_revision"].(string)
			return nil, s.saveAccountPolicy(ctx, doc, expected, x.actor.GetInt("user_id"))
		}},
}
