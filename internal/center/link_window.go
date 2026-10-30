package center

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/shusfun/weir/internal/store/ent"
)

// 链接的生效时间：开始、结束都可以不填，都不填就是一直可用。实际可用时段是链接自己的时段与用户到期时间的交集；
// 不在时段里时拉不到节点，Gate 也按同一时段停用这条链接的凭据（已导入的客户端随之不能用）。

// linkWindow 返回链接实际可用的开始与结束时间（nil 表示不限）。l 为空时只看用户到期。
func linkWindow(u *ent.User, l *ent.Link) (*time.Time, *time.Time) {
	end := u.ExpiresAt
	if l == nil {
		return nil, end
	}
	if l.EndsAt != nil && (end == nil || l.EndsAt.Before(*end)) {
		end = l.EndsAt
	}
	return l.StartsAt, end
}

// windowState 是链接此刻的状态：active 可用、not_started 未到开始时间、expired 已结束。
func windowState(u *ent.User, l *ent.Link, now time.Time) string {
	start, end := linkWindow(u, l)
	switch {
	case start != nil && now.Before(*start):
		return "not_started"
	case end != nil && !now.Before(*end):
		return "expired"
	}
	return "active"
}

// optionalTime 解析请求里的时间字段：缺省不改（set=false），null 清空，否则为 RFC 3339 时间。
func optionalTime(raw json.RawMessage) (t *time.Time, set bool, err error) {
	if len(raw) == 0 {
		return nil, false, nil
	}
	if string(raw) == "null" {
		return nil, true, nil
	}
	var v time.Time
	if json.Unmarshal(raw, &v) != nil {
		return nil, true, errors.New("invalid_link_window")
	}
	return &v, true, nil
}

// checkLinkWindow 校验生效时间：开始必须早于结束；结束不能晚于用户到期（管理员给的时间是上限）。
func checkLinkWindow(u *ent.User, start, end *time.Time) error {
	if start != nil && end != nil && !start.Before(*end) {
		return errors.New("link_window_order")
	}
	if u.ExpiresAt != nil {
		if end != nil && end.After(*u.ExpiresAt) {
			return errors.New("link_window_exceeds_plan")
		}
		if start != nil && !start.Before(*u.ExpiresAt) {
			return errors.New("link_window_exceeds_plan")
		}
	}
	return nil
}
