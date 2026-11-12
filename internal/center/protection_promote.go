package center

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/shusfun/weir/internal/store/ent/resource"
	"github.com/shusfun/weir/packages/contracts"
)

func protectionActionRevision(action protectionAction) string {
	action.Notification = ""
	action.NotificationAttempts = 0
	action.NotificationNext = nil
	action.EffectiveState = ""
	action.SuspendedReason = ""
	return settingsHash(toMap(action))
}

var protectionPromotionCaps = []Capability{{
	Name: "defense.action.promote", Title: "将临时安全处置转为永久", Kind: "draft", Risk: "danger", Last: true,
	Purpose: "永久保留已存在的安全收紧处置，必须由管理员确认；后台不能自行延长期限。",
	Params:  []Param{{Key: "action_id", Label: "安全处置编号", Kind: "text", Required: true}},
	plan: func(ctx context.Context, s *Server, p *planCtx, st *DraftStep) error {
		id, _ := st.Args["action_id"].(string)
		row, err := s.Store.Ent.Resource.Query().Where(resource.KindEQ(protectionActionKind), resource.KeyEQ(id)).Only(ctx)
		if err != nil {
			return err
		}
		var action protectionAction
		b, _ := json.Marshal(row.Data)
		if err = contracts.DecodeStrict(b, &action, 64<<10); err != nil {
			return err
		}
		if action.RevokedAt != nil || !action.Until.After(time.Now()) {
			return errors.New("protection action expired or revoked")
		}
		p.versions["protection-action:"+id] = protectionActionRevision(action)
		st.Result = map[string]any{"expected_revision": protectionActionRevision(action)}
		st.Preview = []string{"取消这条处置的自动到期，必须手动撤销", "仍受保护名单约束；自动处置总开关不撤销管理员确认的永久策略"}
		st.Diff = fullProfileDiff(action.Until.Format(time.RFC3339), "永久，直至管理员撤销")
		return nil
	},
	exec: func(ctx context.Context, s *Server, x *execCtx, st *DraftStep) (func(context.Context) error, error) {
		id, _ := st.Args["action_id"].(string)
		expected, _ := st.Result["expected_revision"].(string)
		return nil, s.Store.Lock.Do(ctx, "automatic-protection", func(ctx context.Context) error {
			row, err := s.Store.Ent.Resource.Query().Where(resource.KindEQ(protectionActionKind), resource.KeyEQ(id)).Only(ctx)
			if err != nil {
				return err
			}
			var action protectionAction
			b, _ := json.Marshal(row.Data)
			if err = contracts.DecodeStrict(b, &action, 64<<10); err != nil {
				return err
			}
			if action.RevokedAt != nil || !action.Until.After(time.Now()) {
				return errors.New("protection action expired or revoked")
			}
			if expected == "" || protectionActionRevision(action) != expected {
				return errors.New("protection action changed; preview again")
			}
			action.Automatic, action.Permanent, action.Until = false, true, time.Time{}
			tx, err := s.Store.Ent.Tx(ctx)
			if err != nil {
				return err
			}
			defer tx.Rollback()
			if err = tx.Resource.UpdateOneID(row.ID).SetKind("protection-permanent").SetData(toMap(action)).Exec(ctx); err != nil {
				return err
			}
			if _, err = tx.Audit.Create().SetActor(x.actor.GetInt("user_id")).SetAction("defense.promote").SetTarget(id).SetResult("ok").SetDetail(map[string]any{"trace_id": taskTrace(ctx)}).Save(ctx); err != nil {
				return err
			}
			if err = tx.Commit(); err != nil {
				return err
			}
			s.notifyConsole("defense", id)
			return nil
		})
	},
}}
