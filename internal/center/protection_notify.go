package center

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/resource"
	"github.com/shusfun/weir/packages/contracts"
)

// The action itself is the notification outbox. A crash after delivery can
// cause a duplicate notification, but never another policy modification.
func (s *Server) retryProtectionNotifications(ctx context.Context, now time.Time) error {
	actions, err := s.protectionActions(ctx)
	if err != nil {
		return err
	}
	var failures []error
	for _, action := range actions {
		if !action.automaticOrigin() || action.Notification == "sent" {
			continue
		}
		if action.NotificationAttempts >= 5 {
			failures = append(failures, errors.New("automatic protection notification exhausted; inspect security activity"))
			continue
		}
		if action.NotificationNext != nil && action.NotificationNext.After(now) {
			continue
		}
		if err := s.deliverProtectionNotification(ctx, action.ID); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (s *Server) deliverProtectionNotification(ctx context.Context, id string) error {
	return s.Store.Lock.Do(ctx, "protection-notification:"+id, func(ctx context.Context) error {
		var action protectionAction
		claimed := false
		err := s.Store.Lock.Do(ctx, "automatic-protection", func(ctx context.Context) error {
			row, err := s.Store.Ent.Resource.Query().Where(resource.KindIn(protectionActionKind, "protection-permanent"), resource.KeyEQ(id)).Only(ctx)
			if err != nil {
				return err
			}
			raw, _ := json.Marshal(row.Data)
			if err = contracts.DecodeStrict(raw, &action, 64<<10); err != nil {
				return err
			}
			if action.Notification == "sent" || action.NotificationAttempts >= 5 || action.NotificationNext != nil && action.NotificationNext.After(time.Now()) {
				return nil
			}
			action.Notification, action.NotificationAttempts = "pending", action.NotificationAttempts+1
			next := time.Now().UTC().Add(time.Minute << (action.NotificationAttempts - 1))
			action.NotificationNext = &next
			if err = row.Update().SetData(toMap(action)).Exec(ctx); err != nil {
				return err
			}
			claimed = true
			return nil
		})
		if err != nil || !claimed {
			return err
		}
		sendCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		deliveryErr := s.Notify(sendCtx, fmt.Sprintf("Weir 安全临时收紧记录 %s 已产生。请在观测 → 安全核对当前状态，可撤销；临时处置最长 24 小时自动失效。", id))
		cancel()
		err = s.Store.Lock.Do(ctx, "automatic-protection", func(ctx context.Context) error {
			row, err := s.Store.Ent.Resource.Query().Where(resource.KindIn(protectionActionKind, "protection-permanent"), resource.KeyEQ(id)).Only(ctx)
			if ent.IsNotFound(err) {
				return nil
			}
			if err != nil {
				return err
			}
			data := row.Data
			if deliveryErr == nil {
				data["notification"] = "sent"
				delete(data, "notification_next")
			} else {
				data["notification"] = "failed"
			}
			return row.Update().SetData(data).Exec(ctx)
		})
		return errors.Join(deliveryErr, err)
	})
}
