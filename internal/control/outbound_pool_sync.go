package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/imzyb/MiGate/ent"
	"github.com/imzyb/MiGate/ent/resource"
	"github.com/imzyb/MiGate/internal/db"
)

func buildPoolChange(ctx context.Context, tx *ent.Tx, r *ent.Resource, nodes []db.Outbound, selection []string) (db.PoolChange, error) {
	change := db.PoolChange{PoolID: int64(r.ID), Previous: []db.PoolMember{}, Next: []db.Outbound{}}
	outs, routes, err := poolResources(ctx, tx, r.OwnerID)
	if err != nil {
		return change, err
	}
	old := map[string]db.Outbound{}
	for _, out := range outs {
		if out.Source == db.OutboundSourceSubscription && out.SubscriptionID == int64(r.ID) {
			change.Previous = append(change.Previous, db.PoolMember{ID: out.ID, Digest: db.PoolMemberDigest(out)})
			old[out.SubscriptionIdentity] = out
		}
	}
	selected := map[string]bool{}
	for _, identity := range selection {
		if selected[identity] {
			return change, errors.New("duplicate_pool_selection")
		}
		selected[identity] = true
	}
	for _, out := range nodes {
		if !selected[out.SubscriptionIdentity] {
			continue
		}
		delete(selected, out.SubscriptionIdentity)
		out.SubscriptionID = int64(r.ID)
		if existing, found := old[out.SubscriptionIdentity]; found {
			out.ID, out.Tag, out.Sort = existing.ID, existing.Tag, existing.Sort
		} else {
			key := fmt.Sprintf("%d:%s", r.ID, out.SubscriptionIdentity)
			identity, e := tx.Resource.Query().Where(resource.KindEQ("pool-node-id"), resource.KeyEQ(key)).Only(ctx)
			if ent.IsNotFound(e) {
				identity, e = tx.Resource.Create().SetKind("pool-node-id").SetKey(key).SetOwnerID(r.OwnerID).SetData(map[string]any{}).Save(ctx)
			}
			if e != nil {
				return change, errors.New("pool_identity_failed")
			}
			out.ID = 2000000000 + int64(identity.ID)
			out.Tag = fmt.Sprintf("pool-%d-%d", r.ID, identity.ID)
		}
		change.Next = append(change.Next, out)
	}
	if len(selected) > 0 {
		return change, errors.New("pool_selection_changed")
	}
	return change, change.Validate(outs, routes)
}

func queuePoolChange(ctx context.Context, tx *ent.Tx, r *ent.Resource, change db.PoolChange, selection []string, enabled, remove bool) (*ent.Task, error) {
	b, _ := json.Marshal(change)
	var payload map[string]any
	json.Unmarshal(b, &payload)
	t, err := tx.Task.Create().SetDeviceID(r.OwnerID).SetOperation("outbound.pool.apply").SetKey(randomToken(16)).SetPayload(payload).SetExpiresAt(time.Now().Add(24 * time.Hour)).SetLeasedUntil(time.Unix(0, 0)).Save(ctx)
	if err == nil {
		t, err = tx.Task.UpdateOne(t).SetRevision(int64(t.ID)).Save(ctx)
	}
	if err != nil {
		return nil, err
	}
	r.Data["version"], r.Data["status"], r.Data["task_id"] = int(number(r.Data["version"]))+1, "pending", t.ID
	r.Data["pending_selected"], r.Data["pending_enabled"], r.Data["pending_delete"] = selection, enabled, remove
	delete(r.Data, "preview")
	delete(r.Data, "preview_digest")
	_, err = tx.Resource.UpdateOne(r).SetData(r.Data).Save(ctx)
	return t, err
}

func (s *Server) syncPool(ctx context.Context, id int, force bool) error {
	tx, err := s.Store.Tx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	r, err := tx.Resource.Get(ctx, id)
	if err != nil || r.Kind != "outbound-pool" {
		return errors.New("pool_not_found")
	}
	d, err := lockPoolDevice(ctx, tx, r.OwnerID)
	if err != nil || !poolCapable(d) {
		return errors.New("agent_capability_required")
	}
	// Lock after the device, exactly as explicit apply. Re-read configuration.
	r, err = tx.Resource.UpdateOneID(id).SetOwnerID(r.OwnerID).Save(ctx)
	if err != nil {
		return err
	}
	if r.Data["auto_refresh"] != true || r.Data["auto_armed"] != true || r.Data["enabled"] != true || r.Data["status"] == "pending" {
		return nil
	}
	next, _ := time.Parse(time.RFC3339, text(r.Data["next_sync"]))
	if !force && time.Now().Before(next) {
		return nil
	}
	if pending, e := pendingPoolConfiguration(ctx, tx, r.OwnerID, false); e != nil || pending {
		return errors.New("pool_configuration_pending")
	}
	r.Data["last_sync_check"], r.Data["next_sync"] = time.Now().UTC().Format(time.RFC3339), time.Now().Add(time.Minute).UTC().Format(time.RFC3339)
	// Failures preserve applied inventory and leave an explicit bounded status.
	fail := func(reason string) error {
		r.Data["sync_error"] = reason
		_, e := tx.Resource.UpdateOne(r).SetData(r.Data).Save(ctx)
		if e != nil {
			return e
		}
		return tx.Commit()
	}
	src, err := tx.Resource.Get(ctx, int(number(r.Data["source_id"])))
	if err != nil || src.Kind != "sources" {
		return fail("source_not_found")
	}
	src, err = tx.Resource.UpdateOne(src).SetOwnerID(src.OwnerID).Save(ctx)
	if err != nil {
		return err
	}
	links, stale, err := sourceLinks(src.Data, time.Now())
	if err != nil || stale || len(links) > 500 {
		return fail("fresh_bounded_source_required")
	}
	nodes := []db.Outbound{}
	selection := []string{}
	seen := map[string]bool{}
	unsupported := 0
	for _, link := range links {
		out, e := poolOutbound(link)
		if e != nil {
			unsupported++
			continue
		}
		if seen[out.SubscriptionIdentity] {
			continue
		}
		seen[out.SubscriptionIdentity] = true
		nodes = append(nodes, out)
		selection = append(selection, out.SubscriptionIdentity)
	}
	r.Data["sync_unsupported"] = unsupported
	if len(nodes) == 0 {
		return fail("pool_no_supported_nodes")
	}
	b, _ := json.Marshal(nodes)
	sourceDigest := digest(string(b))
	if text(r.Data["sync_source_digest"]) == sourceDigest {
		r.Data["sync_error"] = ""
		_, err = tx.Resource.UpdateOne(r).SetData(r.Data).Save(ctx)
		if err != nil {
			return err
		}
		return tx.Commit()
	}
	change, err := buildPoolChange(ctx, tx, r, nodes, selection)
	if err != nil {
		return fail(err.Error())
	}
	r.Data["pending_source_digest"] = sourceDigest
	r.Data["sync_error"] = ""
	t, err := queuePoolChange(ctx, tx, r, change, selection, true, false)
	if err != nil {
		return err
	}
	_, err = tx.Audit.Create().SetActor(0).SetAction("outbound.pool.auto").SetTarget(fmt.Sprint(t.ID)).SetResult("queued").SetIP("system").Save(ctx)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Server) syncDuePools(ctx context.Context) {
	after := 0
	for ctx.Err() == nil {
		rows, err := s.Store.Resource.Query().Where(resource.KindEQ("outbound-pool"), resource.IDGT(after)).Order(ent.Asc("id")).Limit(100).All(ctx)
		if err != nil || len(rows) == 0 {
			return
		}
		for _, r := range rows {
			after = r.ID
			if r.Data["auto_refresh"] == true && r.Data["enabled"] == true {
				_ = s.syncPool(ctx, r.ID, false)
			}
		}
	}
}
