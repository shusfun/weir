package center

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/chain"
	entexit "github.com/shusfun/weir/internal/store/ent/exit"
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
	spec.Remark = strings.TrimSpace(spec.Remark)
	if utf8.RuneCountInString(spec.Remark) > 100 {
		return nil, errors.New("invalid_remark")
	}
	if spec.Kind == "freedom" && old == nil {
		return s.directExit(ctx, db, gid)
	}
	if spec.Kind == "link" {
		ob, remark, err := parseExitLink(spec.Link)
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
		e, err := db.Exit.Create().SetGateID(gid).SetNillableOwnerUserID(owner).SetKind(entexit.Kind(spec.Kind)).SetTag("pending-" + randomToken(12)).SetPolicyType(policy).Save(ctx)
		if err != nil {
			return nil, err
		}
		id = e.ID
	} else {
		id = old.ID
	}
	m := db.Exit.UpdateOneID(id).SetRemark(spec.Remark).SetKind(entexit.Kind(spec.Kind)).SetAddress(strings.Trim(strings.TrimSpace(spec.Address), "[]")).SetPort(spec.Port).SetUsername(spec.Username).SetPassword(pw).SetLink(link).SetTag("exit-" + itoa(id))
	if spec.Kind == "node" {
		m.SetExitNodeID(spec.NodeID)
	} else {
		m.ClearExitNodeID()
	}
	if spec.Kind == "source" {
		m.SetSource(spec.source().toMap())
	} else {
		m.ClearSource()
	}
	return m.Save(ctx)
}

func (s *Server) validateExitSelection(ctx context.Context, g *ent.Gate, self int, owner *int, id int, spec *ExitSpec, skip, admin bool, kernel string) (*ent.Exit, ExitSpec, *exitProbe, error) {
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
		if e.GateID != g.ID || !reflect.DeepEqual(e.OwnerUserID, owner) || !e.Enabled {
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
	p, err := s.validateExitSpec(ctx, g, self, v, owner != nil, !skip, cc)
	return e, v, p, err
}

func (s *Server) refreshExit(ctx context.Context, e *ent.Exit, p *exitProbe) error {
	s.saveExitProbe(ctx, exitOwner(e.ID), p)
	if e.Kind == entexit.KindSource {
		return s.refreshSourceExit(ctx, exitOwner(e.ID), e.GateID, specOf(e.Source), e.OwnerUserID != nil)
	}
	return nil
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
	v["id"], v["gate_id"], v["owner_user_id"], v["tag"], v["enabled"], v["policy_type"], v["created_at"] = e.ID, e.GateID, e.OwnerUserID, e.Tag, e.Enabled, e.PolicyType, e.CreatedAt
	v["label"] = exitKindLabel(string(e.Kind))
	v["references"], err = s.exitReferences(ctx, e.ID)
	if err != nil {
		return nil, err
	}
	host := e.Address
	if e.Kind == entexit.KindFreedom {
		if g, err := s.Store.Ent.Gate.Get(ctx, e.GateID); err == nil {
			host = g.PublicHost
		}
	}
	if e.ExitNodeID != nil {
		if n, err := s.Store.Ent.Node.Get(ctx, *e.ExitNodeID); err == nil {
			if g, err := s.Store.Ent.Gate.Get(ctx, n.GateID); err == nil {
				host = g.PublicHost
			}
		}
	}
	if e.Kind == entexit.KindSource {
		v["source"] = e.Source
		v["resolved"] = s.exitView(ctx, exitOwner(e.ID))
		if stored, ok := s.loadExit(ctx, exitOwner(e.ID)); ok {
			host = stored.Outbound.Address
			if len(stored.Outbound.Members) > 0 {
				host = stored.Outbound.Members[0].Address
			}
		}
	}
	if p, ok := s.loadExitProbe(ctx, exitOwner(e.ID)); ok {
		v["probe"] = p
		if p.ExitIP != "" {
			host = p.ExitIP
		}
	}
	v["place"] = s.locate(ctx, host)
	return v, nil
}

func (s *Server) exitRoutes(auth, admin *gin.RouterGroup) {
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
		g.POST(b+"/:eid/probe", s.asJob("实测出口", "已取消实测", func(c *gin.Context) { s.probeSavedExit(c, a) }))
	}
}

func (s *Server) exitParam(c *gin.Context, admin bool) (*ent.Exit, bool) {
	id, ok := subParam(c, "eid")
	if !ok {
		return nil, false
	}
	e, err := s.Store.Ent.Exit.Get(c.Request.Context(), id)
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
		q.Where(entexit.GateIDEQ(gid))
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
	c.JSON(200, gin.H{"items": items, "kinds": kinds})
}

func (s *Server) saveExit(c *gin.Context, admin bool) {
	defer s.auditExitFailure(c, "exit.save")
	ctx := c.Request.Context()
	var old *ent.Exit
	if c.Param("eid") != "" {
		var ok bool
		old, ok = s.exitParam(c, admin)
		if !ok {
			return
		}
		if old.Tag == "direct" {
			failure(c, 409, "system_exit")
			return
		}
	}
	var req map[string]json.RawMessage
	if !bind(c, &req) {
		return
	}
	var spec ExitSpec
	gid := 0
	var owner *int
	enabled := true
	if old != nil {
		var err error
		spec, err = s.exitSpec(old)
		if err != nil {
			failInternal(c, 500, "decrypt_failed", err)
			return
		}
		gid, owner, enabled = old.GateID, old.OwnerUserID, old.Enabled
	}
	raw, _ := json.Marshal(req)
	if err := json.Unmarshal(raw, &spec); err != nil {
		failure(c, 400, "invalid_chain")
		return
	}
	if b, ok := req["gate_id"]; ok {
		var id int
		if json.Unmarshal(b, &id) != nil || old != nil && id != gid {
			failure(c, 400, "exit_not_allowed")
			return
		}
		gid = id
	}
	if b, ok := req["enabled"]; ok {
		if json.Unmarshal(b, &enabled) != nil {
			failure(c, 400, "invalid_chain")
			return
		}
	}
	if !admin {
		uid := c.GetInt("user_id")
		owner = &uid
		u, err := s.Store.Ent.User.Get(ctx, uid)
		if err != nil || !u.AllowChain {
			failure(c, 403, "chain_disabled")
			return
		}
		allowed, err := s.userNodeIDs(ctx, u)
		if err != nil {
			failInternal(c, 503, "query_failed", err)
			return
		}
		found := false
		for _, n := range allowed {
			if n.GateID == gid && n.Enabled {
				found = true
			}
		}
		if !found {
			failure(c, 403, "entry_not_allowed")
			return
		}
	}
	if spec.Kind == "freedom" {
		failure(c, 409, "system_exit")
		return
	}
	g, err := s.Store.Ent.Gate.Get(ctx, gid)
	if err != nil {
		failure(c, 404, "gate_not_found")
		return
	}
	refs := []gin.H{}
	if old != nil {
		refs, err = s.exitReferences(ctx, old.ID)
		if err != nil {
			failInternal(c, 503, "query_failed", err)
			return
		}
	}
	if !enabled {
		for _, r := range refs {
			if r["enabled"] == true {
				failDetail(c, 409, "exit_in_use", "先停用或切换引用它的对象", gin.H{"references": refs})
				return
			}
		}
	}
	changed := true
	if old != nil {
		prev, e := s.exitSpec(old)
		if e != nil {
			failInternal(c, 500, "decrypt_failed", e)
			return
		}
		prev.Remark, spec.Remark = "", strings.TrimSpace(spec.Remark)
		next := spec
		next.Remark = ""
		changed = !reflect.DeepEqual(prev, next)
	}
	var p *exitProbe
	if changed {
		if owner != nil {
			u, e := s.Store.Ent.User.Get(ctx, *owner)
			if e != nil {
				failure(c, 404, "user_not_found")
				return
			}
			allowed, e := s.userNodeIDs(ctx, u)
			if e != nil {
				failInternal(c, 503, "query_failed", e)
				return
			}
			if spec.Kind == "node" && allowed[spec.NodeID] == nil {
				failure(c, 403, "exit_not_allowed")
				return
			}
		}
		_, _, p, err = s.validateExitSelection(ctx, g, 0, owner, 0, &spec, false, admin, "")
		if err == nil {
			err = s.checkGateKernels(ctx, g, spec)
		}
		if err == nil {
			for _, r := range refs {
				entryID := 0
				switch r["type"] {
				case "node":
					entryID = r["id"].(int)
				case "chain":
					var ch *ent.Chain
					ch, err = s.Store.Ent.Chain.Get(ctx, r["id"].(int))
					if err != nil {
						break
					}
					entryID = ch.EntryNodeID
				}
				if err != nil {
					break
				}
				if entryID != 0 {
					_, _, _, err = s.validateExitSelection(ctx, g, entryID, owner, 0, &spec, true, admin, "")
					if err != nil {
						break
					}
				}
			}
		}
		if err != nil {
			failErr(c, 422, err)
			return
		}
	}
	if utf8.RuneCountInString(spec.Remark) > 100 {
		failure(c, 400, "invalid_remark")
		return
	}
	if dryRun(c, dryResult{Preview: []string{"保存出口；影响 " + itoa(len(refs)) + " 个引用对象"}}) {
		return
	}
	if !jobCommit(c) {
		return
	}
	tx, err := s.Store.Ent.Tx(ctx)
	if err != nil {
		failInternal(c, 500, "save_failed", err)
		return
	}
	defer tx.Rollback()
	e, err := s.persistExit(ctx, tx.Client(), gid, owner, "", spec, old)
	if err == nil {
		e, err = tx.Exit.UpdateOne(e).SetEnabled(enabled).Save(ctx)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		failInternal(c, 500, "save_failed", err)
		return
	}
	if changed {
		if err = s.refreshExit(ctx, e, p); err != nil {
			failInternal(c, 500, "source_exit_failed", err)
			return
		}
	}
	if err = s.SyncAll(ctx); err == nil {
		_, err = s.bumpGate(ctx, gid)
	}
	if err != nil {
		failInternal(c, 500, "sync_failed", err)
		return
	}
	s.audit(c, "exit.save", exitOwner(e.ID), "ok")
	v, err := s.exitResourceView(ctx, e)
	if err != nil {
		failInternal(c, 503, "query_failed", err)
		return
	}
	c.JSON(200, v)
}

func (s *Server) deleteExit(c *gin.Context, admin bool) {
	defer s.auditExitFailure(c, "exit.delete")
	e, ok := s.exitParam(c, admin)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	if e.Tag == "direct" {
		failure(c, 409, "system_exit")
		return
	}
	refs, err := s.exitReferences(ctx, e.ID)
	if err != nil {
		failInternal(c, 503, "query_failed", err)
		return
	}
	if len(refs) > 0 {
		failDetail(c, 409, "exit_in_use", "请先切换或删除引用对象", gin.H{"references": refs})
		return
	}
	if dryRun(c, dryResult{Preview: []string{"删除出口 " + firstNonEmpty(e.Remark, e.Tag)}}) {
		return
	}
	if err = s.Store.Ent.Exit.DeleteOne(e).Exec(ctx); err != nil {
		failInternal(c, 500, "delete_failed", err)
		return
	}
	_ = s.Store.KV.Del(ctx, exitProbeKey(exitOwner(e.ID)), sourceExitKey(exitOwner(e.ID)))
	if err = s.SyncAll(ctx); err != nil {
		failInternal(c, 500, "sync_failed", err)
		return
	}
	_, _ = s.bumpGate(ctx, e.GateID)
	s.audit(c, "exit.delete", exitOwner(e.ID), "ok")
	c.Status(204)
}

func (s *Server) probeSavedExit(c *gin.Context, admin bool) {
	defer s.auditExitFailure(c, "exit.probe")
	e, ok := s.exitParam(c, admin)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	spec, err := s.exitSpec(e)
	if err != nil {
		failInternal(c, 500, "decrypt_failed", err)
		return
	}
	if spec.Kind == "freedom" || spec.Kind == "blackhole" {
		failure(c, 400, "probe_unsupported")
		return
	}
	g, err := s.Store.Ent.Gate.Get(ctx, e.GateID)
	if err != nil {
		failure(c, 404, "gate_not_found")
		return
	}
	_, _, p, err := s.validateExitSelection(ctx, g, 0, e.OwnerUserID, 0, &spec, false, admin, "")
	if err != nil {
		failErr(c, 422, err)
		return
	}
	s.saveExitProbe(ctx, exitOwner(e.ID), p)
	s.audit(c, "exit.probe", exitOwner(e.ID), "ok")
	c.JSON(200, p)
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
		o, _ = s.exitFromStore(ctx, exitOwner(e.ID), e.Tag, int64(e.ID))
	case entexit.KindNode:
		if e.ExitNodeID == nil {
			return o, errors.New("出口节点缺失")
		}
		o, err = s.exitFor(ctx, e.Tag, *e.ExitNodeID, exitCredentialKey(e.ID))
	default:
		o, err = s.fixedExit(string(e.Kind), e.Address, e.Port, e.Username, e.Password, e.Link)
	}
	o.ID, o.Tag = int64(e.ID), e.Tag
	return o, err
}
