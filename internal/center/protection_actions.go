package center

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shusfun/weir/internal/policydata"
	"github.com/shusfun/weir/internal/protection"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/resource"
	"github.com/shusfun/weir/packages/contracts"
)

type securityRunKey struct{}
type securityRunGrant struct {
	RunID   string    `json:"run_id"`
	EventID int       `json:"event_id"`
	TraceID string    `json:"trace_id"`
	Until   time.Time `json:"until"`
}

func (s *Server) securityRunForToken(ctx context.Context, token string) (securityRunGrant, bool) {
	var grant securityRunGrant
	if s.Store == nil || len(token) != 64 {
		return grant, false
	}
	raw, ok, err := s.Store.KV.Get(ctx, "security-run:"+digest(token))
	if err != nil || !ok || contracts.DecodeStrict([]byte(raw), &grant, 2048) != nil || !grant.Until.After(time.Now()) || !validTraceHex(grant.TraceID, 32) {
		return securityRunGrant{}, false
	}
	return grant, true
}

type protectionAction struct {
	Permanent            bool            `json:"permanent"`
	Automatic            bool            `json:"automatic"`
	ID                   string          `json:"id"`
	EventID              int             `json:"event_id"`
	RunID                string          `json:"run_id"`
	TraceID              string          `json:"trace_id"`
	PolicyRevision       string          `json:"policy_revision"`
	Kind                 string          `json:"kind"`
	IP                   string          `json:"ip,omitempty"`
	Field                string          `json:"field,omitempty"`
	Value                json.RawMessage `json:"value,omitempty"`
	Reason               string          `json:"reason"`
	CreatedAt            time.Time       `json:"created_at"`
	Until                time.Time       `json:"until"`
	RevokedAt            *time.Time      `json:"revoked_at,omitempty"`
	Notification         string          `json:"notification"`
	NotificationAttempts int             `json:"notification_attempts,omitempty"`
	NotificationNext     *time.Time      `json:"notification_next,omitempty"`
	EffectiveState       string          `json:"effective_state,omitempty"`
	SuspendedReason      string          `json:"suspended_reason,omitempty"`
}

// RunID is immutable, including after promotion or revocation: neither action
// may refund the originating automatic operation's hourly allowance.
func (a protectionAction) automaticOrigin() bool { return a.Automatic || a.RunID != "" }

func automaticRequestGrant(ctx context.Context, d *Draft) (securityRunGrant, bool) {
	grant, ok := ctx.Value(securityRunKey{}).(securityRunGrant)
	if d.Source != "analysis" || !ok || grant.EventID < 1 || grant.RunID == "" || !grant.Until.After(time.Now()) || taskTrace(ctx) != grant.TraceID || len(d.Steps) != 1 {
		return grant, false
	}
	step := d.Steps[0]
	return grant, (step.Action == "defense.block" || step.Action == "defense.tighten") && intArg(step.Args, "event_id") == grant.EventID
}

func protectionReceipt(d *Draft, action protectionAction) {
	d.ID, d.Explain, d.CreatedAt = action.ID, action.Reason, action.CreatedAt
	args := map[string]any{"event_id": action.EventID}
	if !action.Permanent {
		args["minutes"] = int(action.Until.Sub(action.CreatedAt) / time.Minute)
	}
	if action.Kind == "tighten" {
		args["field"], args["value"] = action.Field, string(action.Value)
	}
	d.Steps = []DraftStep{{Action: "defense." + action.Kind, Title: "已有安全处置", Args: args}}
	d.Status = "auto_receipt"
}

// Replays return an immutable receipt before previewing a new change. The
// receipt is not an assertion that a revoked, expired or paused rule is active.
func (s *Server) existingProtectionReceipt(ctx context.Context, d *Draft) (bool, error) {
	grant, ok := automaticRequestGrant(ctx, d)
	if !ok {
		return false, nil
	}
	actions, err := s.protectionActions(ctx)
	if err != nil {
		return false, err
	}
	for _, action := range actions {
		if action.EventID == grant.EventID && action.RunID == grant.RunID {
			protectionReceipt(d, action)
			return true, nil
		}
	}
	return false, nil
}

func (s *Server) protectionActions(ctx context.Context) ([]protectionAction, error) {
	rows, err := s.Store.Ent.Resource.Query().Where(resource.Or(
		resource.And(resource.KindEQ(protectionActionKind), resource.CreatedAtGTE(time.Now().Add(-24*time.Hour))),
		resource.KindEQ("protection-permanent"),
	)).Order(ent.Asc(resource.FieldID)).Limit(10000).All(ctx)
	if err != nil {
		return nil, err
	}
	if len(rows) >= 10000 {
		return nil, errors.New("protection action capacity exceeded")
	}
	out := []protectionAction{}
	for _, row := range rows {
		var value protectionAction
		b, _ := json.Marshal(row.Data)
		if contracts.DecodeStrict(b, &value, 64<<10) != nil {
			return nil, errors.New("invalid automatic protection record")
		}
		out = append(out, value)
	}
	return out, nil
}

func (s *Server) effectiveAccountPolicy(ctx context.Context) (policydata.View, error) {
	view, err := s.accountPolicy(ctx)
	if err != nil {
		return view, err
	}
	_, policy, err := s.protectionPolicy(ctx)
	if err != nil {
		return view, err
	}
	actions, err := s.protectionActions(ctx)
	if err != nil {
		return view, err
	}
	for _, action := range actions {
		if action.Kind != "tighten" || action.Automatic && !policy.Enabled || action.RevokedAt != nil || !action.Permanent && !action.Until.After(time.Now()) {
			continue
		}
		values, e := protection.Tighten(view.Effective, action.Field, action.Value, policy)
		if e != nil {
			continue
		} // A later administrator policy or protection can supersede this tightening.
		view.Effective = values
		view.Sources[action.Field] = "temporary:" + action.ID
	}
	view.Revision = settingsHash(map[string]any{"base": view.Revision, "effective": view.Effective})
	return view, nil
}

func (s *Server) tryAutomaticProtection(ctx context.Context, d *Draft) (bool, error) {
	grant, ok := automaticRequestGrant(ctx, d)
	if !ok {
		return false, nil
	}
	step := d.Steps[0]
	applied := false
	err := s.Store.Lock.Do(ctx, "automatic-protection", func(ctx context.Context) error {
		view, policy, err := s.protectionPolicy(ctx)
		if err != nil {
			return err
		}
		event, err := s.loadEvent(ctx, grant.EventID)
		if err != nil {
			return err
		}
		if event.TraceID != grant.TraceID || time.Since(event.CreatedAt) > 24*time.Hour {
			return nil
		}
		actions, err := s.protectionActions(ctx)
		if err != nil {
			return err
		}
		used := 0
		for _, action := range actions {
			if action.EventID == grant.EventID && action.RunID == grant.RunID {
				protectionReceipt(d, action)
				applied = true
				return nil
			}
			if action.automaticOrigin() && action.CreatedAt.After(time.Now().Add(-time.Hour)) {
				used++
			}
		}
		if !policy.Enabled || used >= policy.HourlyLimit || event.Status != "analyzing" {
			return nil
		}
		minutes := intArg(step.Args, "minutes")
		if minutes < 5 || minutes > policy.MaxMinutes {
			return nil
		}
		now := time.Now().UTC()
		action := protectionAction{Automatic: true, ID: d.ID, EventID: event.ID, RunID: grant.RunID, TraceID: grant.TraceID, PolicyRevision: view.Revision, Reason: d.Explain, CreatedAt: now, Until: now.Add(time.Duration(minutes) * time.Minute), Notification: "pending"}
		if step.Action == "defense.block" {
			if policy.ProtectsIP(event.SourceIP) {
				return nil
			}
			action.Kind, action.IP = "block", event.SourceIP
		} else {
			action.Kind = "tighten"
			action.Field, _ = step.Args["field"].(string)
			action.Value, err = tighteningValue(step.Args)
			if err != nil {
				return nil
			}
			base, err := s.effectiveAccountPolicy(ctx)
			if err != nil {
				return err
			}
			if _, err = protection.Tighten(base.Effective, action.Field, action.Value, policy); err != nil {
				return nil
			}
		}
		tx, err := s.Store.Ent.Tx(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if _, err = tx.Resource.Create().SetKind(protectionActionKind).SetKey(action.ID).SetData(toMap(action)).Save(ctx); err != nil {
			return err
		}
		if _, err = tx.Audit.Create().SetAction("defense.auto.apply").SetTarget(action.ID).SetResult("ok").SetDetail(map[string]any{"trace_id": grant.TraceID, "event_id": grant.EventID, "until": action.Until, "kind": action.Kind, "field": action.Field}).Save(ctx); err != nil {
			return err
		}
		event.Status, event.Analysis = "auto_applied", "安全临时收紧已由宿主护栏批准，可撤销"
		n, err := tx.Resource.Update().Where(resource.KindEQ(eventKind), resource.KeyEQ(itoa(event.ID))).SetData(toMap(event)).Save(ctx)
		if err != nil {
			return err
		}
		if n != 1 {
			return errors.New("security event disappeared during automatic protection")
		}
		if err = tx.Commit(); err != nil {
			return err
		}
		applied = true
		return nil
	})
	if applied {
		if d.Status == "auto_receipt" {
			return true, err
		}
		d.Status = "auto_applied"
		if event, e := s.loadEvent(ctx, grant.EventID); e == nil {
			s.tasks.note("observation-producer", s.recordSecurityObservation(ctx, event))
		}
		s.notifyConsole("defense", d.ID)
	}
	return applied, err
}

func (s *Server) automaticBlocked(r *http.Request) (bool, error) {
	_, policy, err := s.protectionPolicy(r.Context())
	if err != nil {
		return false, err
	}
	ip := clientIP(r)
	if policy.ProtectsIP(ip) {
		return false, nil
	}
	actions, err := s.protectionActions(r.Context())
	if err != nil {
		return false, err
	}
	for _, action := range actions {
		if action.Kind == "block" && action.IP == ip && (!action.Automatic || policy.Enabled) && action.RevokedAt == nil && (action.Permanent || action.Until.After(time.Now())) {
			return true, nil
		}
	}
	return false, nil
}

func (s *Server) listProtectionActions(c *gin.Context) {
	actions, err := s.protectionActions(c.Request.Context())
	if err != nil {
		failInternal(c, 503, "protection_actions_unavailable", err)
		return
	}
	_, policy, err := s.protectionPolicy(c.Request.Context())
	if err != nil {
		failInternal(c, 503, "protection_policy_unavailable", err)
		return
	}
	base, err := s.accountPolicy(c.Request.Context())
	if err != nil {
		failInternal(c, 503, "account_policy_unavailable", err)
		return
	}
	for i := range actions {
		a := &actions[i]
		switch {
		case a.RevokedAt != nil:
			a.EffectiveState = "revoked"
		case !a.Permanent && !a.Until.After(time.Now()):
			a.EffectiveState = "expired"
		case a.Automatic && !policy.Enabled:
			a.EffectiveState, a.SuspendedReason = "suspended", "自动安全处置总开关已关闭"
		case a.Kind == "block" && policy.ProtectsIP(a.IP):
			a.EffectiveState, a.SuspendedReason = "suspended", "来源位于保护名单"
		case a.Kind == "tighten":
			values, e := protection.Tighten(base.Effective, a.Field, a.Value, policy)
			if e != nil {
				a.EffectiveState, a.SuspendedReason = "suspended", e.Error()
			} else {
				a.EffectiveState = "active"
				base.Effective = values
			}
		default:
			a.EffectiveState = "active"
		}
	}
	c.JSON(200, gin.H{"items": actions})
}

func (s *Server) applyConfirmedTightening(ctx context.Context, eventID int, field string, value json.RawMessage, minutes, actor int) error {
	return s.Store.Lock.Do(ctx, "automatic-protection", func(ctx context.Context) error {
		view, err := s.accountPolicy(ctx)
		if err != nil {
			return err
		}
		_, policy, err := s.protectionPolicy(ctx)
		if err != nil {
			return err
		}
		if minutes < 5 || minutes > 1440 {
			return errors.New("invalid tightening duration")
		}
		if _, err = protection.Tighten(view.Effective, field, value, policy); err != nil {
			return err
		}
		now := time.Now().UTC()
		action := protectionAction{ID: randomToken(12), Kind: "tighten", EventID: eventID, TraceID: taskTrace(ctx), Field: field, Value: value, CreatedAt: now, Until: now.Add(time.Duration(minutes) * time.Minute), Notification: "confirmed"}
		tx, err := s.Store.Ent.Tx(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if _, err = tx.Resource.Create().SetKind(protectionActionKind).SetKey(action.ID).SetData(toMap(action)).Save(ctx); err != nil {
			return err
		}
		if _, err = tx.Audit.Create().SetActor(actor).SetAction("defense.tighten.confirm").SetTarget(action.ID).SetResult("ok").SetDetail(map[string]any{"trace_id": action.TraceID}).Save(ctx); err != nil {
			return err
		}
		return tx.Commit()
	})
}

func (s *Server) revokeProtectionAction(c *gin.Context) {
	id := c.Param("id")
	if !validTraceHex(id, 24) {
		failure(c, 404, "protection_action_not_found")
		return
	}
	err := s.Store.Lock.Do(c.Request.Context(), "automatic-protection", func(ctx context.Context) error {
		row, err := s.Store.Ent.Resource.Query().Where(resource.KindIn(protectionActionKind, "protection-permanent"), resource.KeyEQ(id)).Only(ctx)
		if err != nil {
			return err
		}
		var action protectionAction
		b, _ := json.Marshal(row.Data)
		if err = contracts.DecodeStrict(b, &action, 64<<10); err != nil {
			return err
		}
		if action.RevokedAt != nil {
			return nil
		}
		now := time.Now().UTC()
		action.RevokedAt = &now
		tx, err := s.Store.Ent.Tx(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if err = tx.Resource.UpdateOneID(row.ID).SetData(toMap(action)).Exec(ctx); err != nil {
			return err
		}
		if _, err = tx.Audit.Create().SetActor(c.GetInt("user_id")).SetAction("defense.auto.revoke").SetTarget(id).SetResult("ok").SetDetail(map[string]any{"trace_id": taskTrace(ctx)}).Save(ctx); err != nil {
			return err
		}
		return tx.Commit()
	})
	if ent.IsNotFound(err) {
		failure(c, 404, "protection_action_not_found")
		return
	}
	if err != nil {
		failInternal(c, 503, "protection_revoke_failed", err)
		return
	}
	s.notifyConsole("defense", id)
	c.Status(204)
}
