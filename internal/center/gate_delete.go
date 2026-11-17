package center

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/budget"
	"github.com/shusfun/weir/internal/store/ent/certificate"
	"github.com/shusfun/weir/internal/store/ent/chain"
	"github.com/shusfun/weir/internal/store/ent/credential"
	entexit "github.com/shusfun/weir/internal/store/ent/exit"
	"github.com/shusfun/weir/internal/store/ent/exitbinding"
	"github.com/shusfun/weir/internal/store/ent/gate"
	"github.com/shusfun/weir/internal/store/ent/gateaction"
	"github.com/shusfun/weir/internal/store/ent/node"
	"github.com/shusfun/weir/internal/store/ent/ratesample"
	"github.com/shusfun/weir/internal/store/ent/resource"
	"github.com/shusfun/weir/internal/store/ent/route"
	"github.com/shusfun/weir/internal/store/ent/setting"
)

const deletedGateKind = "deleted-gate"

var errGateDeleteStale = errors.New("gate_delete_preview_stale")
var errGateDeleteActive = errors.New("gate_delete_requires_revocation")

type gateDeleteClaim struct {
	GateID   int       `json:"gate_id"`
	Actor    int       `json:"actor"`
	Revision string    `json:"revision"`
	Expires  time.Time `json:"expires"`
}

type gateDeleteObject struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type gateDeletePlan struct {
	Gate                                      gateDeleteObject   `json:"gate"`
	Nodes                                     []gateDeleteObject `json:"nodes"`
	Chains                                    []gateDeleteObject `json:"chains"`
	Exits                                     []gateDeleteObject `json:"exits"`
	BlockedExits                              []gateDeleteObject `json:"blocked_exits"`
	Routes                                    []int              `json:"routes"`
	Users                                     []int              `json:"users"`
	Links                                     []int              `json:"links"`
	Credentials                               []int              `json:"credentials"`
	Certificates                              []gateDeleteObject `json:"certificates"`
	Laya                                      bool               `json:"laya"`
	certificates                              []*ent.Certificate
	laya                                      *ent.Setting
	Revision                                  string `json:"-"`
	nodeIDs, chainIDs, exitIDs, affectedGates []int
	users                                     []*ent.User
	links                                     []*ent.Link
}

// Keep the preview and commit on the same database snapshot. Only identifiers
// and names leave the server; the encrypted claim also binds the actor and age.
func gateDeletionPlan(ctx context.Context, db *ent.Client, gid int) (*gateDeletePlan, error) {
	g, err := db.Gate.Get(ctx, gid)
	if err != nil {
		return nil, err
	}
	if g.Status != gate.StatusRevoked && g.LastSeenAt != nil {
		return nil, errGateDeleteActive
	}
	p := &gateDeletePlan{Gate: gateDeleteObject{g.ID, g.Name}, Nodes: []gateDeleteObject{}, Chains: []gateDeleteObject{}, Exits: []gateDeleteObject{}, BlockedExits: []gateDeleteObject{}, Routes: []int{}, Users: []int{}, Links: []int{}, Credentials: []int{}, Certificates: []gateDeleteObject{}}
	nodes, err := db.Node.Query().Where(node.GateIDEQ(gid)).Order(ent.Asc(node.FieldID)).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, n := range nodes {
		p.Nodes = append(p.Nodes, gateDeleteObject{n.ID, n.Name})
		p.nodeIDs = append(p.nodeIDs, n.ID)
	}
	exits, err := db.Exit.Query().Where(entexit.Or(entexit.GateIDEQ(gid), entexit.ExitNodeIDIn(p.nodeIDs...))).Order(ent.Asc(entexit.FieldID)).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, e := range exits {
		name := e.Remark
		if name == "" {
			name = exitKindLabel(string(e.Kind)) + " #" + itoa(e.ID)
		}
		if exitGateID(e) == gid {
			p.Exits = append(p.Exits, gateDeleteObject{e.ID, name})
			p.exitIDs = append(p.exitIDs, e.ID)
		} else if e.ExitNodeID != nil {
			p.BlockedExits = append(p.BlockedExits, gateDeleteObject{e.ID, name})
			bound, berr := db.ExitBinding.Query().Where(exitbinding.ExitIDEQ(e.ID)).All(ctx)
			if berr != nil {
				return nil, berr
			}
			for _, b := range bound {
				id := b.GateID
				if id != gid && !slices.Contains(p.affectedGates, id) {
					p.affectedGates = append(p.affectedGates, id)
				}
			}
		}
	}
	chains, err := db.Chain.Query().Where(chain.Or(chain.EntryNodeIDIn(p.nodeIDs...), chain.ExitIDIn(p.exitIDs...))).Order(ent.Asc(chain.FieldID)).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, ch := range chains {
		p.Chains = append(p.Chains, gateDeleteObject{ch.ID, ch.Name})
		p.chainIDs = append(p.chainIDs, ch.ID)
	}
	p.Routes, err = db.Route.Query().Where(route.Or(route.GateIDEQ(gid), route.NodeIDIn(p.nodeIDs...), route.ExitIDIn(p.exitIDs...))).Order(ent.Asc(route.FieldID)).IDs(ctx)
	if err != nil {
		return nil, err
	}
	if p.Routes == nil {
		p.Routes = []int{}
	}
	internalKeys := []string{}
	for _, eid := range p.exitIDs {
		internalKeys = append(internalKeys, exitCredentialKey(eid))
	}
	creds, err := db.Credential.Query().Where(credential.Or(credential.NodeIDIn(p.nodeIDs...), credential.ChainIDIn(p.chainIDs...), credential.And(credential.InternalEQ(true), credential.StatsKeyIn(internalKeys...)))).Order(ent.Asc(credential.FieldID)).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, cr := range creds {
		p.Credentials = append(p.Credentials, cr.ID)
		if !slices.Contains(p.nodeIDs, cr.NodeID) {
			n, e := db.Node.Get(ctx, cr.NodeID)
			if e != nil {
				return nil, e
			}
			if n.GateID != gid && !slices.Contains(p.affectedGates, n.GateID) {
				p.affectedGates = append(p.affectedGates, n.GateID)
			}
		}
	}
	users, err := db.User.Query().All(ctx)
	if err != nil {
		return nil, err
	}
	for _, u := range users {
		if overlapsIDs(u.NodeIds, p.nodeIDs) {
			p.users = append(p.users, u)
			p.Users = append(p.Users, u.ID)
		}
	}
	links, err := db.Link.Query().All(ctx)
	if err != nil {
		return nil, err
	}
	for _, l := range links {
		if overlapsIDs(l.NodeIds, p.nodeIDs) || overlapsIDs(l.ChainIds, p.chainIDs) {
			p.links = append(p.links, l)
			p.Links = append(p.Links, l.ID)
		}
	}
	certs, err := db.Certificate.Query().Where(certificate.GateIDEQ(gid)).Order(ent.Asc(certificate.FieldID)).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, cert := range certs {
		p.Certificates = append(p.Certificates, gateDeleteObject{cert.ID, cert.Name})
		p.certificates = append(p.certificates, cert)
	}
	laya, err := db.Setting.Query().Where(setting.KeyEQ("laya")).Only(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return nil, err
	}
	if laya != nil {
		var config layaSettings
		b, err := json.Marshal(laya.Value)
		if err != nil {
			return nil, err
		}
		if err = json.Unmarshal(b, &config); err != nil {
			return nil, err
		}
		if config.Mode == "gate" && config.GateID == gid {
			p.Laya = true
			p.laya = laya
		}
	}
	slices.Sort(p.Users)
	slices.Sort(p.Links)
	slices.Sort(p.affectedGates)
	// Include selection contents, not only affected counts, to reject stale
	// confirmations after another administrator changes an authorization.
	selections := map[string]any{}
	for _, u := range p.users {
		selections["user:"+itoa(u.ID)] = u.NodeIds
	}
	for _, l := range p.links {
		selections["link:"+itoa(l.ID)] = []any{l.NodeIds, l.ChainIds}
	}
	p.Revision = version([]any{p, g.Status, g.LastSeenAt, g.ConfigRevision, nodes, exits, chains, selections, certs, p.laya})
	return p, nil
}

func overlapsIDs(values, remove []int) bool {
	for _, id := range values {
		if slices.Contains(remove, id) {
			return true
		}
	}
	return false
}

func withoutIDs(values, remove []int) []int {
	out := make([]int, 0, len(values))
	for _, id := range values {
		if !slices.Contains(remove, id) {
			out = append(out, id)
		}
	}
	return out
}

func gateDeleteFailure(c *gin.Context, err error) {
	switch {
	case errors.Is(err, errGateDeleteActive):
		failDetail(c, 409, err.Error(), "这台 VPS 曾连接中心，请先撤销身份再删除", nil)
	case errors.Is(err, errGateDeleteStale):
		failDetail(c, 409, err.Error(), "影响范围已变化或预览已过期，请重新预览", nil)
	case ent.IsNotFound(err):
		failure(c, 404, "gate_not_found")
	default:
		failInternal(c, 503, "gate_delete_failed", err)
	}
}

func (s *Server) previewGateDeletion(c *gin.Context) {
	gid, ok := subParam(c, "id")
	if !ok {
		return
	}
	ctx := c.Request.Context()
	var outcome error
	finish := operationStage(ctx, "gate.delete.preview", "gate_id", gid)
	defer func() { finish(outcome) }()
	tx, err := s.Store.Ent.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		outcome = err
		gateDeleteFailure(c, err)
		return
	}
	defer tx.Rollback()
	plan, err := gateDeletionPlan(ctx, tx.Client(), gid)
	if err != nil {
		outcome = err
		gateDeleteFailure(c, err)
		return
	}
	claim := gateDeleteClaim{GateID: gid, Actor: c.GetInt("user_id"), Revision: plan.Revision, Expires: time.Now().Add(10 * time.Minute)}
	raw, err := json.Marshal(claim)
	var token string
	if err == nil {
		token, err = s.Crypto.Encrypt(string(raw))
	}
	if err != nil {
		outcome = err
		gateDeleteFailure(c, err)
		return
	}
	c.JSON(200, gin.H{"impact": plan, "preview_token": token, "expires_at": claim.Expires})
}

func (s *Server) deleteGate(c *gin.Context) {
	gid, ok := subParam(c, "id")
	if !ok {
		return
	}
	var req struct {
		PreviewToken string `json:"preview_token"`
	}
	if !bind(c, &req) {
		return
	}
	ctx := c.Request.Context()
	var outcome error
	finish := operationStage(ctx, "gate.delete", "gate_id", gid, "actor_id", c.GetInt("user_id"))
	defer func() { finish(outcome) }()
	raw, err := s.Crypto.Decrypt(req.PreviewToken)
	var claim gateDeleteClaim
	if err != nil || json.Unmarshal([]byte(raw), &claim) != nil || claim.GateID != gid || claim.Actor != c.GetInt("user_id") || !time.Now().Before(claim.Expires) {
		outcome = errGateDeleteStale
		gateDeleteFailure(c, outcome)
		return
	}
	var plan *gateDeletePlan
	err = s.Store.Lock.Do(ctx, "cred-sync", func(ctx context.Context) error {
		return s.Store.Lock.Do(ctx, "gate-network:"+itoa(gid), func(ctx context.Context) error {
			return s.Store.Lock.Do(ctx, "laya-settings", func(ctx context.Context) error {
				tx, err := s.Store.Ent.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
				if err != nil {
					return err
				}
				defer tx.Rollback()
				plan, err = gateDeletionPlan(ctx, tx.Client(), gid)
				if err != nil {
					return err
				}
				if plan.Revision != claim.Revision {
					return errGateDeleteStale
				}
				stage := operationStage(ctx, "gate.delete.references", "gate_id", gid)
				err = s.applyGateDeletion(ctx, tx, plan, c)
				stage(err)
				if err != nil {
					return err
				}
				return tx.Commit()
			})
		})
	})
	outcome = err
	if err != nil {
		gateDeleteFailure(c, err)
		return
	}
	// Invalidate pending probes only after commit. KV cleanup cannot roll back
	// SQL; missing nodes already prevent late results from restoring references.
	warnings := []string{}
	s.nodeProbes.Lock()
	for _, id := range plan.nodeIDs {
		if job := s.nodeProbes.jobs[id]; job != "" {
			_ = s.jobs.Cancel(jobViewer{admin: true}, job)
			delete(s.nodeProbes.jobs, id)
		}
		if err := s.Store.KV.Del(ctx, pendingNodeProbePrefix+itoa(id)); err != nil {
			warnings = append(warnings, "节点实测队列清理待重试")
		}
		if err := s.Store.KV.Del(ctx, nodeTestKey(id)); err != nil {
			warnings = append(warnings, "节点实测缓存清理待重试")
		}
	}
	s.nodeProbes.Unlock()
	for _, id := range append(plan.affectedGates, gid) {
		s.waiters.notify(id)
		if err := s.Store.Notifications.Publish(ctx, "gate:"+itoa(id)); err != nil {
			warnings = append(warnings, "配置已更新，等待 VPS 下次轮询领取")
		}
	}
	c.JSON(200, gin.H{"deleted": true, "impact": plan, "warnings": warnings})
}

func (s *Server) applyGateDeletion(ctx context.Context, tx *ent.Tx, p *gateDeletePlan, c *gin.Context) error {
	gid := p.Gate.ID
	// A retained identity tombstone distinguishes an explicit deletion from a
	// database outage. The old device must still receive a revocation response.
	if err := tx.Resource.Create().SetKind(deletedGateKind).SetKey(itoa(gid)).SetData(map[string]any{"deleted_at": time.Now().UTC(), "trace_id": taskTrace(ctx)}).Exec(ctx); err != nil {
		return err
	}
	// Retain certificate material and IDs used by other nodes. Only the
	// removed device binding and its renewal capability disappear.
	for _, cert := range p.certificates {
		update := tx.Certificate.UpdateOneID(cert.ID).ClearGateID()
		if cert.Source == certificate.SourceAcme || cert.Source == certificate.SourcePanel {
			update.SetStatus(certificate.StatusFailed).SetLastError("VPS 已删除，原设备证书管理不可用；现有证书材料保留")
		}
		if err := update.Exec(ctx); err != nil {
			return err
		}
	}
	if p.laya != nil {
		value := p.laya.Value
		value["enabled"], value["gate_id"] = false, 0
		if err := tx.Setting.UpdateOneID(p.laya.ID).SetValue(value).AddRevision(1).Exec(ctx); err != nil {
			return err
		}
	}
	for _, e := range p.BlockedExits {
		if err := tx.Exit.UpdateOneID(e.ID).SetKind(entexit.KindBlackhole).ClearExitNodeID().SetEnabled(true).SetRemark("原落地 VPS 已删除，流量已拦截").Exec(ctx); err != nil {
			return err
		}
	}
	for _, u := range p.users {
		if err := tx.User.UpdateOneID(u.ID).SetNodeIds(withoutIDs(u.NodeIds, p.nodeIDs)).Exec(ctx); err != nil {
			return err
		}
	}
	for _, l := range p.links {
		if err := tx.Link.UpdateOneID(l.ID).SetNodeIds(withoutIDs(l.NodeIds, p.nodeIDs)).SetChainIds(withoutIDs(l.ChainIds, p.chainIDs)).Exec(ctx); err != nil {
			return err
		}
	}
	journals, err := tx.Resource.Query().Where(resource.KindEQ("credential-sync")).All(ctx)
	if err != nil {
		return err
	}
	for _, row := range journals {
		var journal struct {
			Gates []int `json:"gates"`
		}
		if err := decodeStrict(row.Data, &journal); err != nil {
			return err
		}
		if slices.Contains(journal.Gates, gid) {
			if err := tx.Resource.UpdateOneID(row.ID).SetData(map[string]any{"gates": withoutIDs(journal.Gates, []int{gid})}).Exec(ctx); err != nil {
				return err
			}
		}
	}
	if _, err := tx.Credential.Delete().Where(credential.IDIn(p.Credentials...)).Exec(ctx); err != nil {
		return err
	}
	if _, err := tx.Chain.Delete().Where(chain.IDIn(p.chainIDs...)).Exec(ctx); err != nil {
		return err
	}
	if _, err := tx.Route.Delete().Where(route.IDIn(p.Routes...)).Exec(ctx); err != nil {
		return err
	}
	if _, err := tx.Node.Delete().Where(node.IDIn(p.nodeIDs...)).Exec(ctx); err != nil {
		return err
	}
	if _, err := tx.ExitBinding.Delete().Where(exitbinding.Or(exitbinding.GateIDEQ(gid), exitbinding.ExitIDIn(p.exitIDs...))).Exec(ctx); err != nil {
		return err
	}
	if _, err := tx.Exit.Delete().Where(entexit.IDIn(p.exitIDs...)).Exec(ctx); err != nil {
		return err
	}
	if _, err := tx.Budget.Delete().Where(budget.GateIDEQ(gid)).Exec(ctx); err != nil {
		return err
	}
	if _, err := tx.GateAction.Delete().Where(gateaction.GateIDEQ(gid)).Exec(ctx); err != nil {
		return err
	}
	if _, err := tx.RateSample.Delete().Where(ratesample.Or(ratesample.NodeIDIn(p.nodeIDs...), ratesample.ChainIDIn(p.chainIDs...))).Exec(ctx); err != nil {
		return err
	}
	if _, err := tx.Resource.Delete().Where(resource.KindIn(gateTrafficKind, gateTrafficConfigKind), resource.KeyEQ(itoa(gid))).Exec(ctx); err != nil {
		return err
	}
	if _, err := tx.Resource.Delete().Where(resource.KindIn(ruleMeterKind, ruleDailyKind, domainDailyKind, domainBaselineKind, domainCoverageKind, browserConfigKind, browserSessionKind), resource.OwnerIDEQ(gid)).Exec(ctx); err != nil {
		return err
	}
	if _, err := tx.Resource.Delete().Where(resource.KindIn(outboundBridgeKind, domainObserverKind), resource.KeyEQ(itoa(gid))).Exec(ctx); err != nil {
		return err
	}
	uotRows, err := tx.Resource.Query().Where(resource.KindEQ(uotResourceKind)).All(ctx)
	if err != nil {
		return err
	}
	for _, row := range uotRows {
		var transport uotEndpoint
		if err = decodeResource(row, &transport); err != nil {
			return err
		}
		if transport.GateID == gid || transport.EntryID == gid || slices.Contains(p.nodeIDs, transport.NodeID) || slices.Contains(p.exitIDs, transport.ExitID) {
			if err = tx.Resource.DeleteOne(row).Exec(ctx); err != nil {
				return err
			}
			if transport.GateID != gid {
				if err = tx.Gate.UpdateOneID(transport.GateID).AddConfigRevision(1).Exec(ctx); err != nil {
					return err
				}
			}
		}
	}
	if _, err := tx.Gate.Update().Where(gate.IDIn(p.affectedGates...)).AddConfigRevision(1).Save(ctx); err != nil {
		return err
	}
	if err := tx.Gate.DeleteOneID(gid).Exec(ctx); err != nil {
		return err
	}
	return tx.Audit.Create().SetActor(c.GetInt("user_id")).SetAction("gate.delete").SetTarget("gate:" + itoa(gid)).SetResult("ok").SetIP(c.ClientIP()).SetDetail(traceDetail(c)).Exec(ctx)
}

func (s *Server) gateDeleted(ctx context.Context, id int) (bool, error) {
	return s.Store.Ent.Resource.Query().Where(resource.KindEQ(deletedGateKind), resource.KeyEQ(itoa(id))).Exist(ctx)
}

func (s *Server) rejectDeletedGate(c *gin.Context, id int) bool {
	deleted, err := s.gateDeleted(c.Request.Context(), id)
	if err != nil {
		failInternal(c, 503, "auth_unavailable", err)
		return true
	}
	if deleted {
		failure(c, 403, "gate_revoked")
		return true
	}
	return false
}

// A no-op update locks the identity until the caller's transaction commits.
// Deletion and creation must not commit a new deployment against a removed VPS.
func lockActiveGates(ctx context.Context, db *ent.Client, ids ...int) error {
	ids = dedupe(ids)
	slices.Sort(ids)
	for _, id := range ids {
		n, err := db.Gate.Update().Where(gate.IDEQ(id), gate.StatusEQ(gate.StatusActive)).SetStatus(gate.StatusActive).Save(ctx)
		if err != nil {
			return err
		}
		if n != 1 {
			return errors.New("gate_not_active")
		}
	}
	return nil
}
