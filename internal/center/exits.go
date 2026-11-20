package center

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/chain"
	entexit "github.com/shusfun/weir/internal/store/ent/exit"
	"github.com/shusfun/weir/internal/store/ent/exitbinding"
	"github.com/shusfun/weir/internal/store/ent/node"
	"github.com/shusfun/weir/internal/store/ent/route"
	"github.com/shusfun/weir/packages/contracts"
)

type exitKindInfo struct {
	Kind        string `json:"kind"`
	Label       string `json:"label"`
	Group       string `json:"group"`
	Description string `json:"description"`
	AdminOnly   bool   `json:"admin_only"`
}

var exitKinds = []exitKindInfo{
	{"freedom", "直接出去", "freedom", "网站看到这台 VPS 的 IP", true},
	{"socks5", "SOCKS5 代理", "proxy", "SOCKS5、HTTP 等代理，网站看到代理的 IP", false},
	{"http", "HTTP 代理", "proxy", "SOCKS5、HTTP 等代理，网站看到代理的 IP", false},
	{"https", "HTTPS 代理", "proxy", "SOCKS5、HTTP 等代理，网站看到代理的 IP", false},
	{"link", "节点链接", "link", "粘贴一条别处的节点，网站看到那个节点的 IP", false},
	{"source", "订阅源节点", "source", "从订阅里挑一个或一组，可自动切换", false},
	{"node", "另一台 VPS", "node", "转到自己的另一台 VPS，网站看到那台的 IP", false},
	{"blackhole", "拦截", "blackhole", "丢弃匹配流量", true},
}

func exitKindLabel(kind string) string {
	for _, k := range exitKinds {
		if k.Kind == kind {
			return k.Label
		}
	}
	return kind
}

func exitOwner(id int) string         { return "exit:" + itoa(id) }
func exitCredentialKey(id int) string { return "exitx-" + itoa(id) }

func (s *Server) exitPolicy(ctx context.Context, id int) string {
	e, err := s.Store.Ent.Exit.Get(ctx, id)
	if err != nil {
		return ""
	}
	return e.PolicyType
}

func (s *Server) exitSpec(e *ent.Exit) (ExitSpec, error) {
	spec := ExitSpec{Kind: string(e.Kind), Remark: e.Remark, Address: e.Address, Port: e.Port, Username: e.Username}
	var err error
	if spec.Password, err = s.reveal(e.Password); err != nil {
		return spec, err
	}
	if spec.Link, err = s.reveal(e.Link); err != nil {
		return spec, err
	}
	if e.ExitNodeID != nil {
		spec.NodeID = *e.ExitNodeID
	}
	sp := specOf(e.Source)
	spec.SourceID, spec.ProfileID, spec.Mode, spec.Match = sp.SourceID, sp.ProfileID, sp.Mode, sp.Match
	spec.Identity, spec.InsecureIdentities = sp.Identity, sp.InsecureIdentities
	return spec, nil
}

func (s *Server) directExit(ctx context.Context, db *ent.Client, gid int) (*ent.Exit, error) {
	e, err := db.Exit.Query().Where(entexit.GateIDEQ(gid), entexit.TagEQ("direct")).Only(ctx)
	if ent.IsNotFound(err) {
		return db.Exit.Create().SetGateID(gid).SetKind(entexit.KindFreedom).SetTag("direct").Save(ctx)
	}
	return e, err
}

// persistExit 只负责持久化；调用方先校验、实测，再在同一事务中绑定引用。
func (s *Server) persistExit(ctx context.Context, db *ent.Client, gid int, owner *int, policy string, spec ExitSpec, old *ent.Exit) (*ent.Exit, error) {
	gateIDs := []int{}
	if gid > 0 {
		gateIDs = append(gateIDs, gid)
	}
	if spec.Kind == "node" {
		n, err := db.Node.Get(ctx, spec.NodeID)
		if err != nil {
			return nil, err
		}
		gateIDs = append(gateIDs, n.GateID)
	}
	if len(gateIDs) > 0 {
		if err := lockActiveGates(ctx, db, gateIDs...); err != nil {
			return nil, err
		}
	}
	spec.Remark = strings.TrimSpace(spec.Remark)
	if utf8.RuneCountInString(spec.Remark) > 100 {
		return nil, errors.New("invalid_remark")
	}
	if spec.Kind == "freedom" && old == nil {
		return s.directExit(ctx, db, gid)
	}
	if spec.Kind == "link" {
		ob, remark, err := parseConfirmedExitLink(spec.Link, spec.InsecureIdentities)
		if err != nil {
			return nil, err
		}
		spec.Address, spec.Port = ob.Address, ob.Port
		if spec.Remark == "" && old == nil {
			spec.Remark = truncate(strings.TrimSpace(remark), 100)
		}
	}
	pw, err := s.seal(spec.Password)
	if err != nil {
		return nil, err
	}
	link, err := s.seal(strings.TrimSpace(spec.Link))
	if err != nil {
		return nil, err
	}
	var id int
	if old == nil {
		create := db.Exit.Create().SetNillableOwnerUserID(owner).SetKind(entexit.Kind(spec.Kind)).SetTag("pending-" + randomToken(12)).SetPolicyType(policy)
		if spec.Kind == "freedom" && gid > 0 {
			create.SetGateID(gid)
		}
		e, err := create.Save(ctx)
		if err != nil {
			return nil, err
		}
		id = e.ID
	} else {
		id = old.ID
	}
	m := db.Exit.UpdateOneID(id).SetRemark(spec.Remark).SetKind(entexit.Kind(spec.Kind)).SetAddress(strings.Trim(strings.TrimSpace(spec.Address), "[]")).SetPort(spec.Port).SetUsername(spec.Username).SetPassword(pw).SetLink(link).SetTag("exit-" + itoa(id))
	if spec.Kind != "freedom" {
		m.ClearGateID()
	}
	if spec.Kind == "node" {
		m.SetExitNodeID(spec.NodeID)
	} else {
		m.ClearExitNodeID()
	}
	if spec.Kind == "source" {
		if spec.Mode == "node" && !validSourceIdentity(spec.Identity) {
			return nil, errors.New("请先确认订阅节点身份")
		}
		m.SetSource(spec.source().toMap())
	} else if spec.Kind == "link" {
		m.SetSource(map[string]any{"insecure_identities": validConfirmations(spec.Link, spec.InsecureIdentities)})
	} else {
		m.ClearSource()
	}
	return m.Save(ctx)
}

func (s *Server) validateExitSelection(ctx context.Context, g *ent.Gate, self int, owner *int, id int, spec *ExitSpec, skip, admin bool, kernel string) (*ent.Exit, ExitSpec, *exitProbe, error) {
	if spec != nil && utf8.RuneCountInString(strings.TrimSpace(spec.Remark)) > 100 {
		return nil, ExitSpec{}, nil, errors.New("invalid_remark")
	}
	if id != 0 && spec != nil {
		return nil, ExitSpec{}, nil, errors.New("ambiguous_exit")
	}
	var e *ent.Exit
	var v ExitSpec
	var err error
	if id != 0 {
		e, err = s.Store.Ent.Exit.Get(ctx, id)
		if err != nil {
			return nil, v, nil, errors.New("exit_not_found")
		}
		if exitGateID(e) != 0 && exitGateID(e) != g.ID || !reflect.DeepEqual(e.OwnerUserID, owner) || !e.Enabled {
			return nil, v, nil, errors.New("exit_not_allowed")
		}
		if e.Tag == "direct" && exitGateID(e) != g.ID {
			return nil, v, nil, errors.New("exit_not_allowed")
		}
		if e.PolicyType != "" && self != 0 {
			n, err := s.Store.Ent.Node.Get(ctx, self)
			if err != nil || s.typeIndex(ctx).of(n).Key != e.PolicyType {
				return nil, v, nil, errors.New("exit_not_allowed")
			}
		}
		if v, err = s.exitSpec(e); err != nil {
			return nil, v, nil, err
		}
	} else if spec != nil {
		v = *spec
	} else {
		v.Kind = "freedom"
	}
	if owner != nil && (v.Kind == "freedom" || v.Kind == "blackhole") {
		return nil, v, nil, errors.New("exit_not_allowed")
	}
	if v.Kind == "" {
		return nil, v, nil, errors.New("invalid_chain")
	}
	if owner != nil {
		u, e := s.Store.Ent.User.Get(ctx, *owner)
		if e != nil || !u.AllowChain {
			return nil, v, nil, errors.New("chain_disabled")
		}
		nodes, e := s.userNodeIDs(ctx, u)
		if e != nil {
			return nil, v, nil, e
		}
		if self != 0 && nodes[self] == nil {
			return nil, v, nil, errors.New("entry_not_allowed")
		}
		if v.Kind == "node" && nodes[v.NodeID] == nil {
			return nil, v, nil, errors.New("exit_not_allowed")
		}
	}
	cc := chainCtx{kernel: kernel, admin: admin, gate: g}
	if owner != nil {
		cc.userID = *owner
	}
	if v.Kind == "source" {
		if err := s.checkSourceAccess(ctx, v.source(), cc); err != nil {
			return nil, v, nil, err
		}
		bound, e := s.bindSourceIdentity(ctx, v.source())
		if e != nil {
			return nil, v, nil, fmt.Errorf("invalid_chain: %v", e)
		}
		v.Identity = bound.Identity
	}
	p, err := s.validateExitSpec(ctx, g, self, v, owner != nil, !skip, cc)
	if err == nil && spec != nil {
		*spec = v
	}
	return e, v, p, err
}

func (s *Server) refreshExit(ctx context.Context, e *ent.Exit, p *exitProbe) error {
	if err := s.invalidateExitDependents(ctx, e.ID); err != nil {
		return err
	}

	if p == nil || !p.OK {
		return s.Store.KV.Del(ctx, exitProbeKey(exitOwner(e.ID)))
	}
	return s.saveLocatedProbe(ctx, exitOwner(e.ID), p.Location, p.GateID, p)
}

func (s *Server) exitReferences(ctx context.Context, id int) ([]gin.H, error) {
	refs := []gin.H{}
	ns, err := s.Store.Ent.Node.Query().Where(node.ExitIDEQ(id)).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, n := range ns {
		refs = append(refs, gin.H{"type": "node", "id": n.ID, "name": n.Name, "enabled": n.Enabled})
	}
	cs, err := s.Store.Ent.Chain.Query().Where(chain.ExitIDEQ(id)).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, c := range cs {
		refs = append(refs, gin.H{"type": "chain", "id": c.ID, "name": c.Name, "owner": c.UserID, "enabled": c.Enabled})
	}
	rs, err := s.Store.Ent.Route.Query().Where(route.ExitIDEQ(id)).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, r := range rs {
		refs = append(refs, gin.H{"type": "route", "id": r.ID, "name": firstNonEmpty(r.Domain, r.IP), "enabled": r.Enabled})
	}
	return refs, nil
}

// 订阅管理与 extapi 共用删除入口，出口引用不会扩大 extapi 的读写权限。
func (s *Server) checkExitSourceDeletion(c *gin.Context, sourceID, profileID int) bool {
	es, err := s.Store.Ent.Exit.Query().Where(entexit.KindEQ(entexit.KindSource)).All(c.Request.Context())
	if err != nil {
		failInternal(c, 503, "query_failed", err)
		return false
	}
	refs := []gin.H{}
	for _, e := range es {
		sp := specOf(e.Source)
		if sourceID > 0 && sp.SourceID == sourceID || profileID > 0 && sp.ProfileID == profileID {
			refs = append(refs, gin.H{"type": "exit", "id": e.ID, "name": firstNonEmpty(e.Remark, e.Tag)})
		}
	}
	bindings, err := s.Store.Ent.ExitBinding.Query().Where(exitbinding.KindEQ(exitbinding.KindSource)).All(c.Request.Context())
	if err != nil {
		failInternal(c, 503, "query_failed", err)
		return false
	}
	for _, b := range bindings {
		sp := specOf(b.Source)
		if sourceID > 0 && sp.SourceID == sourceID || profileID > 0 && sp.ProfileID == profileID {
			refs = append(refs, gin.H{"type": "exit", "id": b.ExitID, "gate_id": b.GateID})
		}
	}
	if len(refs) > 0 {
		failDetail(c, 409, "source_in_use", "先切换或删除引用此来源的出口", gin.H{"references": refs})
		return false
	}
	return true
}

func (s *Server) exitResourceView(ctx context.Context, e *ent.Exit) (gin.H, error) {
	spec, err := s.exitSpec(e)
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(spec)
	v := gin.H{}
	_ = json.Unmarshal(raw, &v)
	v["has_password"], v["has_link"] = spec.Password != "", spec.Link != ""
	delete(v, "password")
	delete(v, "link")
	v["id"], v["gate_id"], v["owner_user_id"], v["tag"], v["enabled"], v["policy_type"], v["created_at"] = e.ID, e.GateID, e.OwnerUserID, e.Tag, e.Enabled, e.PolicyType, e.CreatedAt
	v["label"] = exitKindLabel(string(e.Kind))
	v["references"], err = s.exitReferences(ctx, e.ID)
	if err != nil {
		return nil, err
	}
	if e.Kind == "source" {
		v["source"] = e.Source
		v["resolved"] = s.exitView(ctx, exitOwner(e.ID))
	}
	uses, probes, state, err := s.exitUsageView(ctx, e)
	if err != nil {
		return nil, err
	}
	v["uses"] = uses
	v["probes"] = probes
	v["place"] = s.exitPlace(ctx, e)
	v["object_state"] = state
	v["summary"] = s.exitSummary(ctx, e, "")
	supported, known := s.exitUDP(ctx, e.ID, map[int]bool{})
	switch {
	case !known:
		v["udp"] = "unknown"
	case supported:
		v["udp"] = "supported"
	default:
		v["udp"] = "unsupported"
	}
	return v, nil
}

func (s *Server) exitRoutes(auth, admin *gin.RouterGroup) {
	auth.POST("/me/exit-link-preview", s.previewExitLink)
	admin.POST("/exits/:eid/credentials", s.revealExitCredentials)
	for _, entry := range []struct {
		g     *gin.RouterGroup
		base  string
		admin bool
	}{{admin, "/exits", true}, {auth, "/me/exits", false}} {
		a := entry.admin
		b := entry.base
		g := entry.g
		g.GET(b, func(c *gin.Context) { s.listExits(c, a) })
		g.GET(b+"/:eid", func(c *gin.Context) {
			if e, ok := s.exitParam(c, a); ok {
				v, err := s.exitResourceView(c.Request.Context(), e)
				if err != nil {
					failInternal(c, 503, "query_failed", err)
					return
				}
				c.JSON(200, v)
			}
		})
		g.POST(b, s.asJob("保存出口", "已取消，出口保持原样", func(c *gin.Context) { s.saveExit(c, a) }))
		g.PATCH(b+"/:eid", s.asJob("保存出口", "已取消，出口保持原样", func(c *gin.Context) { s.saveExit(c, a) }))
		g.DELETE(b+"/:eid", func(c *gin.Context) { s.deleteExit(c, a) })
		g.GET(b+"/:eid/delete-preview", func(c *gin.Context) { s.exitDeletePreview(c, a) })
		g.POST(b+"/:eid/probe", s.asJob("实测出口", "已取消实测", func(c *gin.Context) { s.probeSavedExit(c, a) }))
	}
}

func (s *Server) exitParam(c *gin.Context, admin bool) (*ent.Exit, bool) {
	return exitParamWith(c, s.Store.Ent, admin)
}

func exitParamWith(c *gin.Context, db *ent.Client, admin bool) (*ent.Exit, bool) {
	id, ok := subParam(c, "eid")
	if !ok {
		return nil, false
	}
	e, err := db.Exit.Get(c.Request.Context(), id)
	if err != nil || !admin && (e.OwnerUserID == nil || *e.OwnerUserID != c.GetInt("user_id")) {
		failure(c, 404, "exit_not_found")
		return nil, false
	}
	return e, true
}

func (s *Server) listExits(c *gin.Context, admin bool) {
	ctx := c.Request.Context()
	q := s.Store.Ent.Exit.Query()
	if !admin {
		q.Where(entexit.OwnerUserIDEQ(c.GetInt("user_id")))
	}
	if gid, err := strconv.Atoi(c.Query("gate_id")); err == nil && gid > 0 {
		used, err := s.exitsUsedOnGate(ctx, gid)
		if err != nil {
			failInternal(c, 503, "query_failed", err)
			return
		}
		ids := []int{}
		for id := range used {
			ids = append(ids, id)
		}
		if len(ids) == 0 {
			c.JSON(200, gin.H{"items": []gin.H{}, "kinds": []exitKindInfo{}, "directory": []gin.H{}})
			return
		}
		q.Where(entexit.IDIn(ids...))
	}
	es, err := q.Order(ent.Asc(entexit.FieldGateID), ent.Asc(entexit.FieldID)).All(ctx)
	if err != nil {
		failInternal(c, 503, "query_failed", err)
		return
	}
	items := []gin.H{}
	for _, e := range es {
		v, err := s.exitResourceView(ctx, e)
		if err != nil {
			failInternal(c, 503, "query_failed", err)
			return
		}
		items = append(items, v)
	}
	kinds := []exitKindInfo{}
	for _, k := range exitKinds {
		if admin || !k.AdminOnly {
			kinds = append(kinds, k)
		}
	}
	directory, err := s.exitDirectory(ctx, es, items)
	if err != nil {
		failInternal(c, 503, "query_failed", err)
		return
	}
	c.JSON(200, gin.H{"items": items, "kinds": kinds, "directory": directory})
}

func (s *Server) saveExitNow(c *gin.Context, admin bool) {
	defer s.auditExitFailure(c, "exit.save")
	ctx := c.Request.Context()
	var outcome error
	finish := operationStage(ctx, "exit.save", "actor", c.GetInt("user_id"))
	defer func() { finish(outcome) }()
	var old *ent.Exit
	if c.Param("eid") != "" {
		var ok bool
		old, ok = s.exitParam(c, admin)
		if !ok {
			outcome = errors.New("exit_not_found")
			return
		}
		if old.Tag == "direct" {
			outcome = errors.New("system_exit")
			failure(c, 409, "system_exit")
			return
		}
	}
	var req map[string]json.RawMessage
	if !bind(c, &req) {
		outcome = errors.New("invalid_body")
		return
	}
	if _, ok := req["gate_id"]; ok {
		outcome = errors.New("exit_gate_rejected")
		failDetail(c, 400, "exit_gate_rejected", "添加或修改出口不选择入口 VPS；要在某台 VPS 上使用时再验证", nil)
		return
	}
	var spec ExitSpec
	var owner *int
	enabled := true
	if old != nil {
		var err error
		spec, err = s.exitSpec(old)
		if err != nil {
			outcome = err
			failInternal(c, 500, "decrypt_failed", err)
			return
		}
		owner, enabled = old.OwnerUserID, old.Enabled
	}
	raw, _ := json.Marshal(req)
	if err := json.Unmarshal(raw, &spec); err != nil {
		outcome = err
		failure(c, 400, "invalid_chain")
		return
	}
	if b, ok := req["enabled"]; ok {
		if json.Unmarshal(b, &enabled) != nil {
			outcome = errors.New("invalid_chain")
			failure(c, 400, "invalid_chain")
			return
		}
	}
	if !admin {
		uid := c.GetInt("user_id")
		owner = &uid
		u, err := s.Store.Ent.User.Get(ctx, uid)
		if err != nil || !u.AllowChain {
			outcome = errors.New("chain_disabled")
			failure(c, 403, "chain_disabled")
			return
		}
	}
	if spec.Kind == "freedom" {
		outcome = errors.New("system_exit")
		failure(c, 409, "system_exit")
		return
	}
	var err error
	refs := []gin.H{}
	if old != nil {
		refs, err = s.exitReferences(ctx, old.ID)
		if err != nil {
			outcome = err
			failInternal(c, 503, "query_failed", err)
			return
		}
	}
	if !enabled {
		for _, r := range refs {
			if r["enabled"] == true {
				outcome = errors.New("exit_in_use")
				failDetail(c, 409, "exit_in_use", "先停用或切换引用它的对象", gin.H{"references": refs})
				return
			}
		}
	}
	if old != nil && spec.Kind == "source" && spec.Mode == "node" && spec.Identity == "" && reflect.DeepEqual(spec.source(), specOf(old.Source)) {
		bound, e := s.bindStoredSourceIdentity(ctx, exitOwner(old.ID), 0, spec.source())
		if e != nil {
			outcome = e
			failDetail(c, 422, "invalid_chain", e.Error(), nil)
			return
		}
		spec.Identity = bound.Identity
	}
	changed := true
	if old != nil {
		prev, e := s.exitSpec(old)
		if e != nil {
			outcome = e
			failInternal(c, 500, "decrypt_failed", e)
			return
		}
		prev.Remark, spec.Remark = "", strings.TrimSpace(spec.Remark)
		next := spec
		next.Remark = ""
		changed = !reflect.DeepEqual(prev, next)
	}
	if changed {
		if owner != nil && spec.Kind == "node" {
			u, e := s.Store.Ent.User.Get(ctx, *owner)
			if e != nil {
				outcome = e
				failure(c, 404, "user_not_found")
				return
			}
			allowed, e := s.userNodeIDs(ctx, u)
			if e != nil {
				outcome = e
				failInternal(c, 503, "query_failed", e)
				return
			}
			if allowed[spec.NodeID] == nil {
				outcome = errors.New("exit_not_allowed")
				failure(c, 403, "exit_not_allowed")
				return
			}
		}
		if spec.Kind == "source" {
			bound, e := s.bindSourceIdentity(ctx, spec.source())
			if e != nil {
				outcome = e
				failErr(c, 422, e)
				return
			}
			spec.Identity = bound.Identity
		}
		if err = s.catalogSpec(ctx, spec, owner, admin); err != nil {
			outcome = err
			failErr(c, 422, err)
			return
		}
	}
	if utf8.RuneCountInString(spec.Remark) > 100 {
		outcome = errors.New("invalid_remark")
		failure(c, 400, "invalid_remark")
		return
	}
	if dryRun(c, dryResult{Preview: []string{"保存出口资料；已使用的 VPS 验证通过后才替换生效配置"}}) {
		return
	}
	if !jobCommit(c) {
		return
	}
	tx, err := s.Store.Ent.Tx(ctx)
	if err != nil {
		outcome = err
		failInternal(c, 500, "save_failed", err)
		return
	}
	defer tx.Rollback()
	e, err := s.persistExit(ctx, tx.Client(), 0, owner, "", spec, old)
	if err == nil {
		e, err = tx.Exit.UpdateOne(e).SetEnabled(enabled).Save(ctx)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		outcome = err
		failInternal(c, 500, "save_failed", err)
		return
	}
	applications := []exitApplyResult{}
	needApply := changed && len(refs) > 0
	if !needApply {
		needApply, err = s.bindingsNeedApply(ctx, e, spec)
		if err != nil {
			outcome = err
			failInternal(c, 500, "query_failed", err)
			return
		}
	}
	if needApply {
		applications, err = s.applyExitToUsers(ctx, e, spec, admin)
		if err != nil {
			outcome = err
			failInternal(c, 500, "sync_failed", err)
			return
		}
		for _, item := range applications {
			if item.Status == "waiting" {
				if err := s.noteExitApplyRetry(c, e.ID); err != nil {
					outcome = err
					failInternal(c, 500, "save_failed", err)
					return
				}
				break
			}
		}
	}
	s.audit(c, "exit.save", exitOwner(e.ID), "ok")
	v, err := s.exitResourceView(ctx, e)
	if err != nil {
		outcome = err
		failInternal(c, 503, "query_failed", err)
		return
	}
	v["applications"] = applications
	c.JSON(200, v)
}

func (s *Server) deleteExit(c *gin.Context, admin bool) {
	s.deleteExitConfirmed(c, admin, false)
}

func (s *Server) deleteExitConfirmed(c *gin.Context, admin, draft bool) {
	defer s.auditExitFailure(c, "exit.delete")
	ctx := c.Request.Context()
	exitID, _ := strconv.Atoi(c.Param("eid"))
	finish := operationStage(ctx, "exit.delete", "actor", c.GetInt("user_id"), "exit.id", exitID, "code", stageValue(func() slog.Value { return slog.StringValue(c.GetString("error_code")) }))
	defer func() {
		if c.Writer.Status() >= 400 {
			finish(errors.New(c.GetString("error_code")))
			return
		}
		finish(nil)
	}()
	e, ok := s.exitParam(c, admin)
	if !ok {
		return
	}
	if e.Tag == "direct" {
		failure(c, 409, "system_exit")
		return
	}
	var claim exitDeleteClaim
	if !draft {
		var request struct {
			PreviewToken string `json:"preview_token"`
		}
		if c.Request.ContentLength != 0 && !bind(c, &request) {
			return
		}
		raw, err := s.Crypto.Decrypt(request.PreviewToken)
		if err != nil || json.Unmarshal([]byte(raw), &claim) != nil || claim.ExitID != e.ID || claim.Actor != c.GetInt("user_id") || !time.Now().Before(claim.Expires) {
			staleExitDelete(c)
			return
		}
	}
	var plan *exitDeletion
	committed := false
	ctx = WithRevisionCause(ctx, "exit.delete", c.GetInt("user_id"))
	err := s.Store.Lock.Do(ctx, "cred-sync", func(ctx context.Context) error {
		return retryRevocationTransaction(ctx, func() error {
			var txErr error
			plan, committed, txErr = s.deleteExitTransaction(ctx, c, admin, draft, claim)
			return txErr
		})
	})
	if errors.Is(err, errExitDeleteStale) {
		staleExitDelete(c)
		return
	}
	if err != nil {
		failInternal(c, 500, "delete_failed", err)
		return
	}
	if !committed {
		return
	}
	s.notifyRevocations(ctx, plan.gates())
	s.reconcileDeletion(ctx)
	for _, n := range plan.nodes {
		s.logDeletionCleanup(ctx, "node.probe.cleanup", s.forgetNodeProbes(ctx, n.ID))
	}
	_, err = s.pruneGroupMembers(ctx)
	s.logDeletionCleanup(ctx, "proxy.group.prune", err)
	keys, _ := s.Store.KV.Keys(ctx, exitProbeKey(exitOwner(e.ID)), 100)
	keys = append(keys, sourceExitKey(exitOwner(e.ID)))
	_ = s.Store.KV.Del(ctx, keys...)
	s.audit(c, "exit.delete", exitOwner(e.ID), fmt.Sprintf("ok nodes=%d chains=%d routes=%d", len(plan.nodes), len(plan.chains), len(plan.routes)))
	c.Status(204)
}

func (s *Server) deleteExitTransaction(ctx context.Context, c *gin.Context, admin, draft bool, claim exitDeleteClaim) (*exitDeletion, bool, error) {
	tx, err := s.Store.Ent.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	current, ok := exitParamWith(c, tx.Client(), admin)
	if !ok {
		return nil, false, nil
	}
	plan, err := exitDeletionPlan(ctx, tx.Client(), current)
	if err != nil {
		return nil, false, err
	}
	if !draft && (claim.Revision != plan.revision() || !time.Now().Before(claim.Expires)) {
		return nil, false, errExitDeleteStale
	}
	if len(plan.blocked) > 0 || draft && len(plan.objects()) > 0 {
		failDetail(c, 409, "exit_in_use", "请先解除引用，或在代理列表查看和确认级联删除", nil)
		return nil, false, nil
	}
	if dryRun(c, dryResult{Preview: plan.preview(firstNonEmpty(current.Remark, current.Tag))}) {
		return nil, false, nil
	}
	if err = plan.apply(ctx, tx); err != nil {
		return nil, false, err
	}
	if err = tx.Commit(); err != nil {
		return nil, false, err
	}
	return plan, true, nil
}

func staleExitDelete(c *gin.Context) {
	failDetail(c, 409, errExitDeleteStale.Error(), "删除影响已变化或预览已过期，请重新预览后确认", nil)
}

func (s *Server) logDeletionCleanup(ctx context.Context, stage string, err error) {
	if err != nil {
		slog.Warn("deletion follow-up", "stage", stage, "trace", taskTrace(ctx), "err", redact(err.Error()))
	}
}

func (s *Server) reconcileDeletion(ctx context.Context) {
	_, err := s.RequestCredSync(ctx)
	s.logDeletionCleanup(ctx, "cred.sync", err)
}

func (s *Server) probeSavedExit(c *gin.Context, admin bool) {
	defer s.auditExitFailure(c, "exit.probe")
	e, ok := s.exitParam(c, admin)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	var req struct {
		Location string `json:"location"`
		GateID   int    `json:"gate_id"`
	}
	if !bind(c, &req) {
		return
	}
	center := req.Location == "center"
	loc := req.Location
	if req.GateID > 0 {
		loc = "gate"
	}
	var outcome error
	finish := operationStage(ctx, "exit.probe", "exit.id", e.ID, "location", loc, "gate.id", req.GateID, "binding_created", false)
	defer func() { finish(outcome) }()
	if center == (req.GateID > 0) || req.Location != "" && req.Location != "center" {
		outcome = errors.New("invalid_probe_location")
		failure(c, 400, "invalid_probe_location")
		return
	}
	spec, err := s.exitSpec(e)
	if err != nil {
		outcome = err
		failInternal(c, 500, "decrypt_failed", err)
		return
	}
	if spec.Kind == "freedom" || spec.Kind == "blackhole" {
		outcome = errors.New("probe_unsupported")
		failure(c, 400, "probe_unsupported")
		return
	}
	var p *exitProbe
	if center {
		p, err = s.probeExitFromCenter(ctx, spec, e.OwnerUserID != nil)
	} else {
		if !admin {
			if err = s.userMayProbeGate(ctx, c.GetInt("user_id"), req.GateID); err != nil {
				outcome = err
				failErr(c, 403, err)
				return
			}
		}
		g, gerr := s.Store.Ent.Gate.Get(ctx, req.GateID)
		if gerr != nil {
			outcome = gerr
			failure(c, 404, "gate_not_found")
			return
		}
		p, err = s.verifyExitOnGate(ctx, g, 0, e.OwnerUserID, spec, admin, true, "")
	}
	if err != nil {
		outcome = err
		failErr(c, 422, err)
		return
	}
	location := "gate"
	if center {
		location = "center"
	}
	if err = s.saveLocatedProbe(ctx, exitOwner(e.ID), location, req.GateID, p); err != nil {
		outcome = err
		failInternal(c, 500, "save_failed", err)
		return
	}
	s.noteProbeResult(ctx, p)
	s.audit(c, "exit.probe", exitOwner(e.ID), "ok")
	c.JSON(200, p)
}

func (s *Server) noteProbeResult(ctx context.Context, p *exitProbe) {
	if p == nil {
		return
	}
	known := p.Place != nil && p.Place.Known
	finish := operationStage(ctx, "exit.probe.result", "location", p.Location, "gate.id", p.GateID, "exit_ip", p.ExitIP, "place_known", known, "rate_known", p.RateBps != nil, "download_bytes", p.Bytes)
	finish(nil)
}

func (s *Server) auditExitFailure(c *gin.Context, action string) {
	if !c.GetBool(dryRunKey) && c.Writer.Status() >= 400 {
		id, _ := strconv.Atoi(c.Param("eid"))
		s.audit(c, action, exitOwner(id), "failed")
	}
}

func (s *Server) compileExit(ctx context.Context, e *ent.Exit) (contracts.Outbound, error) {
	if !e.Enabled {
		return contracts.Outbound{}, fmt.Errorf("出口 %d 已停用", e.ID)
	}
	var o contracts.Outbound
	var err error
	switch e.Kind {
	case entexit.KindFreedom, entexit.KindBlackhole:
		o.Protocol = string(e.Kind)
	case entexit.KindSource:
		return o, errors.New("订阅源出口必须指定 VPS 生效快照")
	case entexit.KindNode:
		if e.ExitNodeID == nil {
			return o, errors.New("出口节点缺失")
		}
		o, err = s.uotOutbound(ctx, e)
	case entexit.KindLink:
		var spec ExitSpec
		spec, err = s.exitSpec(e)
		if err == nil {
			o, _, err = parseConfirmedExitLink(spec.Link, spec.InsecureIdentities)
		}
	default:
		o, err = s.fixedExit(string(e.Kind), e.Address, e.Port, e.Username, e.Password, e.Link)
	}
	o.ID, o.Tag = int64(e.ID), e.Tag
	return o, err
}

// compileExitForGate 使用这台 VPS 已验证的快照。没有快照时不把尚未验证的期望参数下发。
func (s *Server) compileExitForGate(ctx context.Context, e *ent.Exit, gateID int) (contracts.Outbound, error) {
	if e.Tag == "direct" {
		return s.compileExit(ctx, e)
	}
	b, err := s.exitBinding(ctx, e.ID, gateID)
	if err != nil {
		return contracts.Outbound{}, err
	}
	if b == nil {
		return contracts.Outbound{}, fmt.Errorf("出口 %d 在这台 VPS 上还没有验证通过的生效配置", e.ID)
	}
	applied := appliedExit(e, b)
	if applied.Kind == entexit.KindSource {
		var v storedExit
		if err := json.Unmarshal(b.Resolved, &v); err != nil || v.Outbound.Protocol == "" {
			return contracts.Outbound{}, fmt.Errorf("出口 %d 在这台 VPS 上缺少有效的已验证订阅快照", e.ID)
		}
		v.Outbound.ID, v.Outbound.Tag = int64(e.ID), e.Tag
		return v.Outbound, nil
	}
	return s.compileExit(ctx, applied)
}

func appliedExit(e *ent.Exit, b *ent.ExitBinding) *ent.Exit {
	applied := *e
	applied.Kind = entexit.Kind(b.Kind)
	applied.Remark = b.Remark
	applied.Address = b.Address
	applied.Port = b.Port
	applied.Username = b.Username
	applied.Password = b.Password
	applied.Link = b.Link
	applied.ExitNodeID = b.ExitNodeID
	applied.Source = b.Source
	applied.Enabled = true
	return &applied
}
