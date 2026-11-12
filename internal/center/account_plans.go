package center

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shusfun/weir/internal/accountplan"
	"github.com/shusfun/weir/internal/accountsecurity"
	"github.com/shusfun/weir/internal/store"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/accesscode"
	planrow "github.com/shusfun/weir/internal/store/ent/accountplan"
	"github.com/shusfun/weir/internal/store/ent/node"
	"github.com/shusfun/weir/internal/store/ent/resource"
	"github.com/shusfun/weir/internal/store/ent/subgrant"
	"github.com/shusfun/weir/internal/store/ent/subprofile"
	"github.com/shusfun/weir/internal/store/ent/user"
)

func (s *Server) accountPlanRoutes(auth, admin *gin.RouterGroup) {
	admin.GET("/account-plans", s.listAccountPlans)
	admin.POST("/account-plans", s.putAccountPlan)
	admin.PUT("/account-plans/:id", s.putAccountPlan)
	admin.GET("/access-codes", s.listAccessCodes)
	admin.POST("/access-codes", s.issueAccessCodes)
	admin.DELETE("/access-codes/:id", s.revokeAccessCode)
	auth.GET("/me/redemption", s.redemptionStatus)
	auth.GET("/me/redemptions", s.listRedemptions)
	auth.POST("/me/redeem", s.redeemAccessCode)
}

func validateGrantResources(ctx context.Context, client *ent.Client, g accountplan.Grant) error {
	if err := g.Validate(); err != nil {
		return err
	}
	for _, v := range []struct {
		ids   []int
		count func(context.Context) (int, error)
	}{
		{g.NodeIDs, client.Node.Query().Where(node.IDIn(g.NodeIDs...)).Count},
		{g.ProfileIDs, client.SubProfile.Query().Where(subprofile.IDIn(g.ProfileIDs...)).Count},
	} {
		if len(v.ids) > 0 {
			n, err := v.count(ctx)
			if err != nil {
				return err
			}
			if n != len(v.ids) {
				return errors.New("plan_resource_not_found")
			}
		}
	}
	return nil
}

func (s *Server) listAccountPlans(c *gin.Context) {
	rows, err := s.Store.Ent.AccountPlan.Query().Order(ent.Desc(planrow.FieldID)).Limit(1000).All(c.Request.Context())
	if err != nil {
		failInternal(c, 503, "query_failed", err)
		return
	}
	c.JSON(200, gin.H{"items": rows})
}

func (s *Server) putAccountPlan(c *gin.Context) {
	var req struct {
		accountPlanDocument
		Revision int `json:"revision"`
	}
	if !bind(c, &req) {
		return
	}
	if err := req.accountPlanDocument.normalize(); err != nil {
		failure(c, 400, err.Error())
		return
	}
	ctx := c.Request.Context()
	if err := validateGrantResources(ctx, s.Store.Ent, req.Grant); err != nil {
		failure(c, 400, "invalid_plan_grant")
		return
	}
	id := 0
	if c.Param("id") != "" {
		var err error
		id, err = strconv.Atoi(c.Param("id"))
		if err != nil || id < 1 {
			failure(c, 400, "invalid_id")
			return
		}
	}
	if dryRun(c, dryResult{Preview: []string{"保存套餐；已签发的码保留原套餐快照，不随本次编辑变化"}}) {
		return
	}
	var saved *ent.AccountPlan
	err := s.Store.Lock.Do(ctx, "account-plans", func(ctx context.Context) error {
		tx, err := s.Store.Ent.Tx(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if id == 0 {
			saved, err = tx.AccountPlan.Create().SetName(req.Name).SetGrant(req.Grant).SetEnabled(req.Enabled).Save(ctx)
		} else {
			n, e := tx.AccountPlan.Update().Where(planrow.IDEQ(id), planrow.RevisionEQ(req.Revision)).SetName(req.Name).SetGrant(req.Grant).SetEnabled(req.Enabled).AddRevision(1).Save(ctx)
			if e != nil {
				return e
			}
			if n != 1 {
				return errors.New("account_plan_changed")
			}
			saved, err = tx.AccountPlan.Get(ctx, id)
		}
		if err != nil {
			return err
		}
		if _, err = tx.Audit.Create().SetActor(c.GetInt("user_id")).SetAction("account.plan").SetTarget("account_plan:" + itoa(saved.ID)).SetResult("ok").SetIP(c.ClientIP()).SetDetail(traceDetail(c)).Save(ctx); err != nil {
			return err
		}
		return tx.Commit()
	})
	if err != nil {
		if err.Error() == "account_plan_changed" {
			failure(c, 409, err.Error())
		} else {
			failInternal(c, 503, "account_plan_unavailable", err)
		}
		return
	}
	slog.InfoContext(ctx, "account plan saved", "trace", taskTrace(ctx), "plan_id", saved.ID, "revision", saved.Revision)
	c.JSON(200, saved)
}

func (s *Server) issueAccessCodes(c *gin.Context) {
	var req struct {
		Kind      accesscode.Kind `json:"kind"`
		PlanID    *int            `json:"plan_id"`
		Count     int             `json:"count"`
		ExpiresAt *time.Time      `json:"expires_at"`
	}
	if !bind(c, &req) {
		return
	}
	if (req.Kind != accesscode.KindInvite && req.Kind != accesscode.KindRedeem) || req.Count < 1 || req.Count > 100 || (req.Kind == accesscode.KindRedeem && req.PlanID == nil) || (req.ExpiresAt != nil && (!req.ExpiresAt.After(time.Now()) || req.ExpiresAt.After(time.Now().AddDate(10, 0, 0)))) {
		failure(c, 400, "invalid_access_code_request")
		return
	}
	ctx := c.Request.Context()
	tx, err := s.Store.Ent.Tx(ctx)
	if err != nil {
		failInternal(c, 503, "access_code_unavailable", err)
		return
	}
	defer tx.Rollback()
	grant, revision := accountplan.Grant{}, 0
	if req.PlanID != nil {
		plan, e := tx.AccountPlan.Get(ctx, *req.PlanID)
		if e != nil || !plan.Enabled {
			failure(c, 400, "account_plan_not_available")
			return
		}
		grant, revision = plan.Grant, plan.Revision
	}
	if err = validateGrantResources(ctx, tx.Client(), grant); err != nil {
		failure(c, 400, "invalid_plan_grant")
		return
	}
	if dryRun(c, dryResult{Preview: []string{strconv.Itoa(req.Count) + " 个一次性码；明文只在生成后提供一次，保存套餐快照"}}) {
		return
	}
	items := []gin.H{}
	for i := 0; i < req.Count; i++ {
		prefix := "wi_"
		if req.Kind == accesscode.KindRedeem {
			prefix = "wr_"
		}
		raw := prefix + randomToken(24)
		row, e := tx.AccessCode.Create().SetKind(req.Kind).SetDigest(digest(raw)).SetNillablePlanID(req.PlanID).SetPlanRevision(revision).SetGrant(grant).SetNillableExpiresAt(req.ExpiresAt).SetCreatedBy(c.GetInt("user_id")).SetTraceID(taskTrace(ctx)).Save(ctx)
		if e != nil {
			failInternal(c, 503, "access_code_unavailable", e)
			return
		}
		items = append(items, gin.H{"id": row.ID, "code": raw})
	}
	detail := traceDetail(c)
	detail["kind"], detail["count"], detail["plan_id"] = req.Kind, req.Count, req.PlanID
	if _, err = tx.Audit.Create().SetActor(c.GetInt("user_id")).SetAction("account.code.issue").SetTarget(string(req.Kind)).SetResult("ok").SetIP(c.ClientIP()).SetDetail(detail).Save(ctx); err != nil {
		failInternal(c, 503, "access_code_unavailable", err)
		return
	}
	if err = tx.Commit(); err != nil {
		failInternal(c, 503, "access_code_unavailable", err)
		return
	}
	slog.InfoContext(ctx, "account codes issued", "trace", taskTrace(ctx), "count", req.Count, "kind", req.Kind, "plan_id", req.PlanID)
	c.JSON(201, gin.H{"items": items})
}

func (s *Server) revokeAccessCode(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id < 1 {
		failure(c, 400, "invalid_id")
		return
	}
	ctx := c.Request.Context()
	tx, err := s.Store.Ent.Tx(ctx)
	if err != nil {
		failInternal(c, 503, "access_code_unavailable", err)
		return
	}
	defer tx.Rollback()
	n, err := tx.AccessCode.Update().Where(accesscode.IDEQ(id), accesscode.UsedAtIsNil()).SetRevoked(true).Save(ctx)
	if err != nil {
		failInternal(c, 503, "access_code_unavailable", err)
		return
	}
	if n != 1 {
		failure(c, 409, "access_code_not_revocable")
		return
	}
	if _, err = tx.Audit.Create().SetActor(c.GetInt("user_id")).SetAction("account.code.revoke").SetTarget("access_code:" + itoa(id)).SetResult("ok").SetIP(c.ClientIP()).SetDetail(traceDetail(c)).Save(ctx); err == nil {
		err = tx.Commit()
	}
	if err != nil {
		failInternal(c, 503, "access_code_unavailable", err)
		return
	}
	slog.InfoContext(ctx, "account code revoked", "trace", taskTrace(ctx), "code_id", id)
	c.JSON(200, gin.H{"revoked": true})
}

func (s *Server) accessPolicy(c *gin.Context) (accountsecurity.Policy, bool) {
	view, err := s.effectiveAccountPolicy(c.Request.Context())
	if err == nil {
		var p accountsecurity.Policy
		p, err = accountsecurity.ParsePolicy(view.Effective)
		if err == nil {
			return p, true
		}
	}
	failInternal(c, 503, "account_policy_unavailable", err)
	return accountsecurity.Policy{}, false
}

func (s *Server) checkAccessCode(c *gin.Context, kind accesscode.Kind, raw, identity string, p accountsecurity.AccessPolicy) (*ent.AccessCode, bool) {
	ctx := c.Request.Context()
	subjects := []string{"ip:" + digest(c.ClientIP()), "identity:" + digest(identity)}
	var row *ent.AccessCode
	invalid := false
	started := time.Now()
	err := s.Store.Lock.Do(ctx, "account-code-check", func(ctx context.Context) error {
		guard := accountsecurity.AccessGuard{Store: s.Store.KV, Policy: p}
		if err := guard.Check(ctx, subjects); err != nil {
			return err
		}
		if len(raw) == 51 {
			var err error
			row, err = s.Store.Ent.AccessCode.Query().Where(accesscode.DigestEQ(digest(raw)), accesscode.KindEQ(kind), accesscode.Revoked(false), accesscode.UsedAtIsNil(), accesscode.Or(accesscode.ExpiresAtIsNil(), accesscode.ExpiresAtGT(time.Now()))).Only(ctx)
			if err != nil && !ent.IsNotFound(err) {
				return err
			}
		}
		if row == nil {
			invalid = true
			return guard.Failure(ctx, subjects)
		}
		return nil
	})
	outcome := "valid"
	if invalid {
		outcome = "invalid"
	}
	if err != nil {
		outcome = "rejected"
	}
	slog.InfoContext(ctx, "account code verification", "trace", taskTrace(ctx), "kind", kind, "outcome", outcome, "ms", time.Since(started).Milliseconds())
	if err != nil {
		var cd *accountsecurity.CooldownError
		if errors.As(err, &cd) {
			seconds := max(1, int(time.Until(cd.Until).Seconds()+1))
			c.Header("Retry-After", itoa(seconds))
			failDetail(c, 429, "access_code_cooldown", "用码尝试过多，请稍后重试", gin.H{"retry_after": seconds})
		} else {
			failInternal(c, 503, "access_code_unavailable", err)
		}
		return nil, false
	}
	if invalid {
		s.securityEvent(c, "access-code-guessing", "registration", "warning")
		failure(c, 400, "invalid_access_code")
		return nil, false
	}
	return row, true
}

func claimAccessCode(ctx context.Context, tx *ent.Tx, code *ent.AccessCode, uid int) error {
	now := time.Now()
	n, err := tx.AccessCode.Update().Where(accesscode.IDEQ(code.ID), accesscode.KindEQ(code.Kind), accesscode.UsedAtIsNil(), accesscode.Revoked(false), accesscode.Or(accesscode.ExpiresAtIsNil(), accesscode.ExpiresAtGT(now))).SetUsedAt(now).SetUsedBy(uid).Save(ctx)
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("invalid_access_code")
	}
	return nil
}

func applyAccountGrant(ctx context.Context, tx *ent.Tx, u *ent.User, g accountplan.Grant, initial bool) error {
	if err := validateGrantResources(ctx, tx.Client(), g); err != nil {
		return err
	}
	a, err := accountplan.Apply(accountplan.Account{Quota: u.Quota, Unlimited: u.Unlimited, ExpiresAt: u.ExpiresAt, AllNodes: u.AllNodes, NodeIDs: u.NodeIds}, g, time.Now(), initial)
	if err != nil {
		return err
	}
	if err = tx.User.UpdateOneID(u.ID).SetQuota(a.Quota).SetUnlimited(a.Unlimited).SetAllNodes(a.AllNodes).SetNodeIds(a.NodeIDs).SetNillableExpiresAt(a.ExpiresAt).Exec(ctx); err != nil {
		return err
	}
	for _, id := range g.ProfileIDs {
		if err = tx.SubGrant.Create().SetUserID(u.ID).SetProfileID(id).SetToken(linkToken()).OnConflictColumns(subgrant.FieldProfileID, subgrant.FieldUserID).DoNothing().Exec(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) redemptionStatus(c *gin.Context) {
	p, ok := s.accessPolicy(c)
	if ok {
		c.JSON(200, gin.H{"enabled": p.RedeemEnabled})
	}
}

func (s *Server) redeemAccessCode(c *gin.Context) {
	p, ok := s.accessPolicy(c)
	if !ok {
		return
	}
	if !p.RedeemEnabled {
		failure(c, 403, "redemption_disabled")
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if !bind(c, &req) {
		return
	}
	uid := c.GetInt("user_id")
	code, ok := s.checkAccessCode(c, accesscode.KindRedeem, strings.TrimSpace(req.Code), "user:"+itoa(uid), p.AccessPolicy)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	started := time.Now()
	var syncID int
	err := s.Store.Lock.Do(ctx, "registration-identity", func(ctx context.Context) error {
		current, err := s.effectiveAccountPolicy(ctx)
		if err != nil {
			return err
		}
		latest, err := accountsecurity.ParsePolicy(current.Effective)
		if err != nil {
			return err
		}
		if !latest.RedeemEnabled {
			return errors.New("redemption_disabled")
		}
		if version(latest) != version(p) {
			return errors.New("account_policy_changed")
		}
		return s.Store.Lock.Do(ctx, "account-grant:"+itoa(uid), func(ctx context.Context) error {
			tx, err := s.Store.Ent.Tx(ctx)
			if err != nil {
				return err
			}
			defer tx.Rollback()
			q := tx.User.Query().Where(user.IDEQ(uid))
			if s.Store.Driver == store.DriverPostgres {
				q.ForUpdate()
			}
			u, err := q.Only(ctx)
			if err != nil {
				return err
			}
			if err = claimAccessCode(ctx, tx, code, uid); err != nil {
				return err
			}
			if err = applyAccountGrant(ctx, tx, u, code.Grant, false); err != nil {
				return err
			}
			detail := traceDetail(c)
			detail["code_id"], detail["plan_id"], detail["plan_revision"] = code.ID, code.PlanID, code.PlanRevision
			if _, err = tx.Audit.Create().SetActor(uid).SetAction("account.code.redeem").SetTarget("user:" + itoa(uid)).SetResult("ok").SetIP(c.ClientIP()).SetDetail(detail).Save(ctx); err != nil {
				return err
			}
			marker, e := markAccountGrant(ctx, tx, uid, code.ID)
			if e != nil {
				return e
			}
			syncID = marker.ID
			return tx.Commit()
		})
	})
	if err != nil {
		if err.Error() == "redemption_disabled" {
			failure(c, 403, err.Error())
		} else if err.Error() == "account_policy_changed" {
			failure(c, 409, err.Error())
		} else if err.Error() == "invalid_access_code" {
			failure(c, 400, "invalid_access_code")
		} else {
			failInternal(c, 503, "redemption_unavailable", err)
		}
		return
	}
	slog.InfoContext(ctx, "account grant committed", "trace", taskTrace(ctx), "user", uid, "code_id", code.ID, "ms", time.Since(started).Milliseconds())
	// Committed grants are authoritative; reconciliation can be retried without
	// ever consuming the code or issuing the allowance again.
	if err = s.reconcileAccountGrant(ctx, syncID); err != nil {
		c.JSON(202, gin.H{"redeemed": true, "synchronization": "pending", "message": "兑换已入账，VPS 配置同步尚未完成，将自动重试"})
		return
	}
	c.JSON(200, gin.H{"redeemed": true})
}

const accountGrantSyncKind = "account-grant-sync"

func markAccountGrant(ctx context.Context, tx *ent.Tx, uid, codeID int) (*ent.Resource, error) {
	return tx.Resource.Create().SetKind(accountGrantSyncKind).SetKey(itoa(codeID)).SetOwnerID(uid).SetData(map[string]any{"trace_id": taskTrace(ctx)}).Save(ctx)
}

func (s *Server) reconcileAccountGrant(ctx context.Context, id int) error {
	return s.Store.Lock.Do(ctx, "account-grant-sync:"+itoa(id), func(ctx context.Context) error {
		return s.reconcileAccountGrantLocked(ctx, id)
	})
}

func (s *Server) reconcileAccountGrantLocked(ctx context.Context, id int) error {
	r, err := s.Store.Ent.Resource.Get(ctx, id)
	if ent.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if r.Kind != accountGrantSyncKind {
		return errors.New("invalid account reconciliation marker")
	}
	if trace, _ := r.Data["trace_id"].(string); validTraceHex(trace, 32) {
		ctx = withTrace(ctx, trace)
	}
	exists, err := s.Store.Ent.User.Query().Where(user.IDEQ(r.OwnerID)).Exist(ctx)
	if err != nil {
		return err
	}
	if exists {
		if err = s.SyncUser(ctx, r.OwnerID); err != nil {
			slog.WarnContext(ctx, "account grant synchronization pending", "trace", taskTrace(ctx), "user", r.OwnerID, "marker", r.ID, "code", "grant_sync_failed", "err", redact(err.Error()))
			if r.Data["status"] != "pending" {
				return errors.Join(err, s.recordAccountGrantSync(ctx, r, "pending"))
			}
			return err
		}
	}
	err = s.recordAccountGrantSync(ctx, r, "ok")
	if err == nil {
		slog.InfoContext(ctx, "account grant reconciled", "trace", taskTrace(ctx), "user", r.OwnerID, "marker", r.ID)
	}
	return err
}

func (s *Server) recordAccountGrantSync(ctx context.Context, r *ent.Resource, status string) error {
	tx, err := s.Store.Ent.Tx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if status == "ok" {
		n, err := tx.Resource.Delete().Where(resource.IDEQ(r.ID), resource.KindEQ(accountGrantSyncKind)).Exec(ctx)
		if err != nil || n == 0 {
			return err
		}
	} else {
		r.Data["status"] = status
		if err = tx.Resource.UpdateOneID(r.ID).SetData(r.Data).Exec(ctx); err != nil {
			return err
		}
	}
	detail := map[string]any{"trace_id": taskTrace(ctx), "code_id": r.Key, "origin": "center.reconcileAccountGrant"}
	if status == "pending" {
		detail["code"] = "grant_sync_failed"
	}
	if _, err = tx.Audit.Create().SetAction("account.grant.sync").SetTarget("user:" + itoa(r.OwnerID)).SetResult(status).SetDetail(detail).Save(ctx); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Server) ReconcileAccountGrants(ctx context.Context) error {
	rows, err := s.Store.Ent.Resource.Query().Where(resource.KindEQ(accountGrantSyncKind)).Order(ent.Asc(resource.FieldID)).Limit(100).All(ctx)
	if err != nil {
		return err
	}
	var failures []error
	for _, r := range rows {
		if err := s.reconcileAccountGrant(ctx, r.ID); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}
