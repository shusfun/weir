package center

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/gin-gonic/gin"
	"github.com/shusfun/weir/internal/store/ent"
	planrow "github.com/shusfun/weir/internal/store/ent/accountplan"
	"github.com/shusfun/weir/packages/contracts"
)

func parseAccountPlanDocument(text string) (accountPlanDocument, error) {
	var doc accountPlanDocument
	err := contracts.DecodeStrict([]byte(text), &doc, 64<<10)
	if err != nil {
		return doc, err
	}
	return doc, doc.normalize()
}

var accountPlanCaps = []Capability{
	{Name: "list_account_plans", Title: "账户套餐", Kind: "read", Purpose: "读取套餐、版本及发放内容，不返回邀请码或兑换码。", read: func(ctx context.Context, s *Server, _ map[string]any) (any, error) {
		return s.Store.Ent.AccountPlan.Query().Order(ent.Desc(planrow.FieldID)).Limit(1000).All(ctx)
	}},
	{Name: "account.plan.update", Title: "修改账户套餐", Kind: "draft", Risk: "danger", Object: "account_plan", Last: true, Purpose: "提交完整套餐 JSON，管理员确认完整差异后生效。仅影响后续签发，已生成码保留原快照；不允许自动执行。", Params: []Param{
		{Key: "plan_id", Label: "套餐", Kind: "ref", Ref: "account_plan", Required: true},
		{Key: "text", Label: "套餐文件（JSON）", Kind: "text", Required: true, Hint: "完整 name、enabled、grant；grant 包含 quota（字节）、unlimited、days、all_nodes、node_ids、profile_ids。"},
	}, plan: func(ctx context.Context, s *Server, p *planCtx, st *DraftStep) error {
		before, err := s.Store.Ent.AccountPlan.Get(ctx, intArg(st.Args, "plan_id"))
		if err != nil {
			return err
		}
		text, _ := st.Args["text"].(string)
		doc, err := parseAccountPlanDocument(text)
		if err != nil {
			return err
		}
		if err = validateGrantResources(ctx, s.Store.Ent, doc.Grant); err != nil {
			return err
		}
		old := accountPlanDocument{Name: before.Name, Enabled: before.Enabled, Grant: before.Grant}
		if version(old) == version(doc) {
			return errors.New("account plan is unchanged")
		}
		a, _ := json.MarshalIndent(old, "", "  ")
		b, _ := json.MarshalIndent(doc, "", "  ")
		st.Diff = fullProfileDiff(string(a), string(b))
		st.Result = map[string]any{"revision": before.Revision}
		st.Preview = []string{"仅修改后续签发的套餐；已签发码及已发放授权不变", "套餐修改必须由管理员确认，不进入自动安全收紧流程"}
		return nil
	}, exec: func(ctx context.Context, s *Server, x *execCtx, st *DraftStep) (func(context.Context) error, error) {
		text, _ := st.Args["text"].(string)
		doc, err := parseAccountPlanDocument(text)
		if err != nil {
			return nil, err
		}
		code, out := invoke(x, s.putAccountPlan, "PUT", gin.Params{{Key: "id", Value: itoa(intArg(st.Args, "plan_id"))}}, map[string]any{"name": doc.Name, "enabled": doc.Enabled, "grant": doc.Grant, "revision": st.Result["revision"]})
		if code >= 300 {
			return nil, handlerError(code, out)
		}
		return nil, nil
	}},
}
