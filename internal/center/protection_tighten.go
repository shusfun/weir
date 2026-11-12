package center

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/shusfun/weir/internal/protection"
)

func tighteningValue(args map[string]any) (json.RawMessage, error) {
	text, ok := args["value"].(string)
	if !ok || len(text) > 8192 || !json.Valid([]byte(text)) {
		return nil, errors.New("value must be a bounded JSON number or string array")
	}
	return json.RawMessage(text), nil
}

var protectionTightenCaps = []Capability{
	{Name: "defense.tighten", Title: "临时收紧安全策略", Kind: "draft", Risk: "danger", Last: true, Sources: []string{"dsh", "analysis"},
		Purpose: "降低发码限额或验证触发阈值、提高本地验证难度、追加邮箱拒绝名单。仅绑定安全分析任务时可能由宿主临时执行；其他来源需管理员确认。不能修改 AI 自身规则。",
		Params: []Param{
			{Key: "event_id", Label: "安全事件", Kind: "ref", Ref: "event", Required: true},
			{Key: "field", Label: "安全策略字段", Kind: "text", Required: true},
			{Key: "value", Label: "新值或追加项（JSON）", Kind: "text", Required: true, Hint: "数值如 10；名单追加项如 [\"@example.com\"]"},
			{Key: "minutes", Label: "生效时长", Kind: "int", Unit: "分钟", Required: true, Min: num(5), Max: num(1440)},
		},
		plan: func(ctx context.Context, s *Server, p *planCtx, st *DraftStep) error {
			e, err := s.loadEvent(ctx, intArg(st.Args, "event_id"))
			if err != nil {
				return err
			}
			if e.Status == "dismissed" {
				return errors.New("security event dismissed")
			}
			view, err := s.accountPolicy(ctx)
			if err != nil {
				return err
			}
			_, guard, err := s.protectionPolicy(ctx)
			if err != nil {
				return err
			}
			field, _ := st.Args["field"].(string)
			value, err := tighteningValue(st.Args)
			if err != nil {
				return err
			}
			_, err = protection.Tighten(view.Effective, field, value, guard)
			if err != nil {
				return err
			}
			p.versions[accountPolicyKey] = view.Revision
			st.Title = "收紧安全策略 " + field
			st.Preview = []string{fmt.Sprintf("安全事件 #%d；临时生效 %s，到期恢复底层策略", e.ID, duration(intArg(st.Args, "minutes"))), "受保护名单、总开关、每小时操作上限由服务端执行；优化和 AI 自修改不适用"}
			st.Diff = fullProfileDiff(string(view.Effective[field]), string(value))
			return nil
		},
		exec: func(ctx context.Context, s *Server, x *execCtx, st *DraftStep) (func(context.Context) error, error) {
			value, err := tighteningValue(st.Args)
			if err != nil {
				return nil, err
			}
			field, _ := st.Args["field"].(string)
			return nil, s.applyConfirmedTightening(ctx, intArg(st.Args, "event_id"), field, value, intArg(st.Args, "minutes"), x.actor.GetInt("user_id"))
		}},
}
