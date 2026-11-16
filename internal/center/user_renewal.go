package center

import (
	"context"
	"time"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/link"
)

type renewalLink struct {
	ID     int        `json:"id"`
	Name   string     `json:"name"`
	EndsAt *time.Time `json:"ends_at"`
}

type renewalResult struct {
	Extended  []renewalLink `json:"extended"`
	Preserved []renewalLink `json:"preserved"`
}

func (s *Server) updateUserPlan(ctx context.Context, uid int, plan planRequest, passwordHash string) (u *ent.User, result renewalResult, err error) {
	finish := operationStage(ctx, "user.plan.update", "user_id", uid)
	defer func() { finish(err) }()
	result = renewalResult{Extended: []renewalLink{}, Preserved: []renewalLink{}}
	err = s.Store.Lock.Do(ctx, "cred-sync", func(ctx context.Context) error {
		tx, e := s.Store.Ent.Tx(ctx)
		if e != nil {
			return e
		}
		defer tx.Rollback()
		old, e := tx.User.Get(ctx, uid)
		if e != nil {
			return e
		}
		storeCopy := *s.Store
		storeCopy.Ent = tx.Client()
		server := &Server{Store: &storeCopy}
		upd := old.Update()
		if e = server.applyPlan(ctx, upd.Mutation(), plan); e != nil {
			return e
		}
		if passwordHash != "" {
			upd.SetPasswordHash(passwordHash)
		}
		u, e = upd.Save(ctx)
		if e != nil {
			return e
		}
		if len(plan.ExpiresAt) > 0 {
			links, e := tx.Link.Query().Where(link.UserIDEQ(uid)).Order(ent.Asc(link.FieldID)).All(ctx)
			if e != nil {
				return e
			}
			// Only a renewal of a finite period is linked. Independent, unlimited,
			// and already customized link periods remain untouched.
			renewed := old.ExpiresAt != nil && u.ExpiresAt != nil && u.ExpiresAt.After(*old.ExpiresAt)
			for _, l := range links {
				if renewed && l.EndsAt != nil && l.EndsAt.Equal(*old.ExpiresAt) {
					// The predicate protects a concurrently customized link as well.
					n, e := tx.Link.Update().Where(link.IDEQ(l.ID), link.EndsAtEQ(*old.ExpiresAt)).SetEndsAt(*u.ExpiresAt).Save(ctx)
					if e != nil {
						return e
					}
					if n == 1 {
						result.Extended = append(result.Extended, renewalLink{l.ID, l.Name, u.ExpiresAt})
						continue
					}
				}
				result.Preserved = append(result.Preserved, renewalLink{l.ID, l.Name, l.EndsAt})
			}
		}
		if e = tx.Commit(); e != nil {
			return e
		}
		u.Unwrap()
		return nil
	})
	return
}
