package center

import (
	"context"
	"errors"
	"fmt"

	"github.com/gin-gonic/gin"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/subgrant"
	"github.com/shusfun/weir/internal/store/ent/subprofile"
	"github.com/shusfun/weir/internal/store/ent/user"
)

// 订阅组分配（SubGrant）的唯一写入口：从订阅组设用户名单、从用户设订阅组名单都走这里。
// 每条分配有自己的订阅令牌；只增删有变化的分配，保留的分配令牌不变，用户手里的订阅地址继续有效。

// grantChange 是一次分配变化：Added / Removed 是对面一侧的编号（按订阅组设时是用户，按用户设时是订阅组）。
type grantChange struct {
	Added   []int
	Removed []int
}

var (
	errGrantProfileNotFound = errors.New("profile_not_found")
	errGrantUserNotFound    = errors.New("user_not_found")
	errGrantUnknownUser     = errors.New("unknown_user")
	errGrantUnknownProfile  = errors.New("unknown_profile")
)

// grantSide 说明从哪一侧设定完整名单。
type grantSide struct {
	byProfile bool
	id        int
}

func (g grantSide) target() string {
	if g.byProfile {
		return "profile:" + itoa(g.id)
	}
	return "user:" + itoa(g.id)
}

// SetProfileUsers 把订阅组的用户名单设为 users（完整名单）。dry 为真时只计算变化。
func (s *Server) SetProfileUsers(ctx context.Context, profileID int, users []int, dry bool) (grantChange, error) {
	return s.replaceGrants(ctx, grantSide{byProfile: true, id: profileID}, users, dry)
}

// SetUserProfiles 把用户的订阅组名单设为 profiles（完整名单）。dry 为真时只计算变化。
func (s *Server) SetUserProfiles(ctx context.Context, userID int, profiles []int, dry bool) (grantChange, error) {
	return s.replaceGrants(ctx, grantSide{id: userID}, profiles, dry)
}

func (s *Server) replaceGrants(ctx context.Context, side grantSide, want []int, dry bool) (change grantChange, err error) {
	finish := operationStage(ctx, "subscription.grants.set", "target", side.target(), "wanted", len(want), "dry_run", dry)
	defer func() {
		finish(err)
	}()
	want = dedupe(want)
	// 两侧共用一把锁：同一对用户与订阅组只会有一条分配。
	err = s.Store.Lock.Do(ctx, "subscription-grants", func(ctx context.Context) error {
		tx, err := s.Store.Ent.Tx(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		var existing []*ent.SubGrant
		if side.byProfile {
			if _, err = tx.SubProfile.Get(ctx, side.id); err != nil {
				if ent.IsNotFound(err) {
					return errGrantProfileNotFound
				}
				return err
			}
			if len(want) > 0 {
				n, err := tx.User.Query().Where(user.IDIn(want...)).Count(ctx)
				if err != nil {
					return err
				}
				if n != len(want) {
					return errGrantUnknownUser
				}
			}
			existing, err = tx.SubGrant.Query().Where(subgrant.ProfileIDEQ(side.id)).All(ctx)
		} else {
			if _, err = tx.User.Get(ctx, side.id); err != nil {
				if ent.IsNotFound(err) {
					return errGrantUserNotFound
				}
				return err
			}
			if len(want) > 0 {
				n, err := tx.SubProfile.Query().Where(subprofile.IDIn(want...)).Count(ctx)
				if err != nil {
					return err
				}
				if n != len(want) {
					return errGrantUnknownProfile
				}
			}
			existing, err = tx.SubGrant.Query().Where(subgrant.UserIDEQ(side.id)).All(ctx)
		}
		if err != nil {
			return err
		}
		other := func(g *ent.SubGrant) int {
			if side.byProfile {
				return g.UserID
			}
			return g.ProfileID
		}
		wanted := make(map[int]bool, len(want))
		for _, id := range want {
			wanted[id] = true
		}
		have := make(map[int]bool, len(existing))
		var removed []*ent.SubGrant
		for _, g := range existing {
			if wanted[other(g)] && !have[other(g)] {
				have[other(g)] = true
				continue
			}
			removed = append(removed, g)
			change.Removed = append(change.Removed, other(g))
		}
		for _, id := range want {
			if !have[id] {
				change.Added = append(change.Added, id)
			}
		}
		if dry {
			return nil
		}
		for _, g := range removed {
			if err = tx.SubGrant.DeleteOne(g).Exec(ctx); err != nil {
				return err
			}
		}
		for _, id := range change.Added {
			create := tx.SubGrant.Create().SetToken(randomToken(24))
			if side.byProfile {
				create.SetProfileID(side.id).SetUserID(id)
			} else {
				create.SetProfileID(id).SetUserID(side.id)
			}
			if _, err = create.Save(ctx); err != nil {
				return err
			}
		}
		return tx.Commit()
	})
	return change, err
}

// applyGrants 是两个分配入口共用的请求流程：试运行预演、写入、审计与错误映射。返回 false 表示已写出错误响应。
func (s *Server) applyGrants(c *gin.Context, side grantSide, want []int) bool {
	ctx := c.Request.Context()
	dry := c.GetBool(dryRunKey)
	change, err := s.replaceGrants(ctx, side, want, dry)
	switch {
	case errors.Is(err, errGrantProfileNotFound), errors.Is(err, errGrantUserNotFound):
		failure(c, 404, err.Error())
		return false
	case errors.Is(err, errGrantUnknownUser), errors.Is(err, errGrantUnknownProfile):
		failure(c, 400, err.Error())
		return false
	case err != nil:
		failInternal(c, 500, "save_failed", err)
		return false
	}
	if dry {
		dryRun(c, dryResult{Preview: grantPreview(side, change)})
		return false
	}
	action := "subscription.profile.grants"
	if !side.byProfile {
		action = "subscription.user.groups"
	}
	s.audit(c, action, side.target(), fmt.Sprintf("ok added=%d removed=%d", len(change.Added), len(change.Removed)))
	return true
}

func grantPreview(side grantSide, change grantChange) []string {
	if len(change.Added) == 0 && len(change.Removed) == 0 {
		return []string{"分配没有变化"}
	}
	var out []string
	if side.byProfile {
		if n := len(change.Added); n > 0 {
			out = append(out, fmt.Sprintf("新分配给 %d 个用户，他们各得到一个订阅地址", n))
		}
		if n := len(change.Removed); n > 0 {
			out = append(out, fmt.Sprintf("取消 %d 个用户的分配，他们的这个订阅地址随即失效", n))
		}
	} else {
		if n := len(change.Added); n > 0 {
			out = append(out, fmt.Sprintf("新分配 %d 个订阅组，各得到一个订阅地址", n))
		}
		if n := len(change.Removed); n > 0 {
			out = append(out, fmt.Sprintf("取消 %d 个订阅组，这些订阅地址随即失效", n))
		}
	}
	return append(out, "其余分配保持不变，订阅地址不变")
}

// setGrants 设定可使用该订阅组的用户（完整列表）。
func (s *Server) setGrants(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	var req struct {
		UserIDs []int `json:"user_ids"`
	}
	if !bind(c, &req) {
		return
	}
	if !s.applyGrants(c, grantSide{byProfile: true, id: id}, req.UserIDs) {
		return
	}
	ctx := c.Request.Context()
	p, err := s.Store.Ent.SubProfile.Get(ctx, id)
	if err != nil {
		failInternal(c, 503, "query_failed", err)
		return
	}
	view, err := s.profileView(ctx, p)
	if err != nil {
		failInternal(c, 503, "query_failed", err)
		return
	}
	c.JSON(200, view)
}

// userGroupsView 列出全部订阅组及该用户是否已分配。
func (s *Server) userGroupsView(ctx context.Context, uid int) (gin.H, error) {
	profiles, err := s.Store.Ent.SubProfile.Query().Order(ent.Asc(subprofile.FieldPosition), ent.Asc(subprofile.FieldID)).All(ctx)
	if err != nil {
		return nil, err
	}
	grants, err := s.Store.Ent.SubGrant.Query().Where(subgrant.UserIDEQ(uid)).All(ctx)
	if err != nil {
		return nil, err
	}
	byProfile := map[int]*ent.SubGrant{}
	for _, g := range grants {
		byProfile[g.ProfileID] = g
	}
	items := make([]gin.H, 0, len(profiles))
	for _, p := range profiles {
		g := byProfile[p.ID]
		item := gin.H{"id": p.ID, "name": p.Name, "enabled": p.Enabled, "expires_at": p.ExpiresAt, "granted": g != nil}
		if g != nil {
			item["last_used_at"] = g.LastUsedAt
		}
		items = append(items, item)
	}
	return gin.H{"items": items}, nil
}

func (s *Server) getUserGroups(c *gin.Context) {
	u, ok := s.subject(c, true)
	if !ok {
		return
	}
	uid := u.ID
	view, err := s.userGroupsView(c.Request.Context(), uid)
	if err != nil {
		failInternal(c, 503, "query_failed", err)
		return
	}
	c.JSON(200, view)
}

// putUserGroups 设定分配给该用户的订阅组（完整列表）。
func (s *Server) putUserGroups(c *gin.Context) {
	u, ok := s.subject(c, true)
	if !ok {
		return
	}
	uid := u.ID
	var req struct {
		ProfileIDs []int `json:"profile_ids"`
	}
	if !bind(c, &req) {
		return
	}
	if !s.applyGrants(c, grantSide{id: uid}, req.ProfileIDs) {
		return
	}
	view, err := s.userGroupsView(c.Request.Context(), uid)
	if err != nil {
		failInternal(c, 503, "query_failed", err)
		return
	}
	c.JSON(200, view)
}
