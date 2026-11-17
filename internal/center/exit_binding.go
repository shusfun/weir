package center

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shusfun/weir/internal/nodeprobe"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/chain"
	entexit "github.com/shusfun/weir/internal/store/ent/exit"
	"github.com/shusfun/weir/internal/store/ent/exitbinding"
	"github.com/shusfun/weir/internal/store/ent/node"
	"github.com/shusfun/weir/internal/store/ent/route"
)

func exitGateID(e *ent.Exit) int {
	if e == nil || e.GateID == nil {
		return 0
	}
	return *e.GateID
}

func exitProbeLocationOwner(owner, location string, gateID int) string {
	if location == "center" {
		return owner + ":center"
	}
	return owner + ":gate:" + itoa(gateID)
}

func sourceExitGateOwner(owner string, gateID int) string {
	return owner + ":gate:" + itoa(gateID)
}

type exitApplyResult struct {
	GateID int    `json:"gate_id"`
	Name   string `json:"name,omitempty"`
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

func (s *Server) connectionDigest(spec ExitSpec) string {
	next := spec
	next.Remark = ""
	raw, _ := json.Marshal(next)
	return digest(string(raw))
}

func (s *Server) bindingSpec(b *ent.ExitBinding) (ExitSpec, error) {
	if b == nil {
		return ExitSpec{}, errors.New("binding_missing")
	}
	spec := ExitSpec{Kind: string(b.Kind), Remark: b.Remark, Address: b.Address, Port: b.Port, Username: b.Username}
	var err error
	if spec.Password, err = s.reveal(b.Password); err != nil {
		return spec, err
	}
	if spec.Link, err = s.reveal(b.Link); err != nil {
		return spec, err
	}
	if b.ExitNodeID != nil {
		spec.NodeID = *b.ExitNodeID
	}
	sp := specOf(b.Source)
	spec.SourceID, spec.ProfileID, spec.Mode, spec.Match = sp.SourceID, sp.ProfileID, sp.Mode, sp.Match
	spec.Identity, spec.InsecureIdentities = sp.Identity, sp.InsecureIdentities
	return spec, nil
}

func sameConnection(a, b ExitSpec) bool {
	a.Remark, b.Remark = "", ""
	return reflect.DeepEqual(a, b)
}

func (s *Server) writeExitBinding(ctx context.Context, db *ent.Client, exitID, gateID int, spec ExitSpec, status exitbinding.Status, reason string, resolved json.RawMessage) error {
	pw, err := s.seal(spec.Password)
	if err != nil {
		return err
	}
	link, err := s.seal(strings.TrimSpace(spec.Link))
	if err != nil {
		return err
	}
	create := db.ExitBinding.Create().
		SetExitID(exitID).SetGateID(gateID).SetStatus(status).SetReason(reason).
		SetDesiredDigest(s.connectionDigest(spec)).SetKind(exitbinding.Kind(spec.Kind)).
		SetRemark(strings.TrimSpace(spec.Remark)).SetAddress(strings.Trim(strings.TrimSpace(spec.Address), "[]")).
		SetPort(spec.Port).SetUsername(spec.Username).SetPassword(pw).SetLink(link)
	if status == exitbinding.StatusApplied {
		create.SetVerifiedAt(time.Now().UTC())
	}
	if spec.Kind == "node" && spec.NodeID != 0 {
		create.SetExitNodeID(spec.NodeID)
	}
	if spec.Kind == "source" {
		if len(resolved) == 0 {
			return errors.New("订阅源出口缺少已验证的解析快照")
		}
		create.SetSource(spec.source().toMap()).SetResolved(resolved)
	} else if spec.Kind == "link" {
		create.SetSource(map[string]any{"insecure_identities": validConfirmations(spec.Link, spec.InsecureIdentities)})
	}
	if err = create.OnConflictColumns(exitbinding.FieldExitID, exitbinding.FieldGateID).UpdateNewValues().Exec(ctx); err != nil {
		return err
	}
	upd := db.ExitBinding.Update().Where(exitbinding.ExitIDEQ(exitID), exitbinding.GateIDEQ(gateID))
	if spec.Kind != "source" {
		upd.ClearResolved()
	}
	if spec.Kind != "node" {
		upd.ClearExitNodeID()
	}
	if spec.Kind != "source" && spec.Kind != "link" {
		upd.ClearSource()
	}
	return upd.Exec(ctx)
}

func (s *Server) exitServes(ctx context.Context, e *ent.Exit, entryID int) bool {
	return e != nil && (exitGateID(e) == entryID || s.exitBound(ctx, e.ID, entryID))
}

func (s *Server) exitBound(ctx context.Context, exitID, gateID int) bool {
	b, err := s.exitBinding(ctx, exitID, gateID)
	return err == nil && b != nil
}

func (s *Server) exitBinding(ctx context.Context, exitID, gateID int) (*ent.ExitBinding, error) {
	b, err := s.Store.Ent.ExitBinding.Query().Where(exitbinding.ExitIDEQ(exitID), exitbinding.GateIDEQ(gateID)).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	return b, err
}

func (s *Server) referencingGates(ctx context.Context, exitID int) ([]int, error) {
	ids := []int{}
	ns, err := s.Store.Ent.Node.Query().Where(node.ExitIDEQ(exitID)).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, n := range ns {
		ids = append(ids, n.GateID)
	}
	chs, err := s.Store.Ent.Chain.Query().Where(chain.ExitIDEQ(exitID)).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, ch := range chs {
		n, err := s.Store.Ent.Node.Get(ctx, ch.EntryNodeID)
		if err != nil {
			return nil, err
		}
		ids = append(ids, n.GateID)
	}
	rs, err := s.Store.Ent.Route.Query().Where(route.ExitIDEQ(exitID)).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, r := range rs {
		ids = append(ids, r.GateID)
	}
	return dedupe(ids), nil
}

func (s *Server) exitsUsedOnGate(ctx context.Context, gid int) (map[int]bool, error) {
	used := map[int]bool{}
	ns, err := s.Store.Ent.Node.Query().Where(node.GateIDEQ(gid)).All(ctx)
	if err != nil {
		return nil, err
	}
	nodeIDs := []int{}
	for _, n := range ns {
		nodeIDs = append(nodeIDs, n.ID)
		if n.ExitID != 0 {
			used[n.ExitID] = true
		}
	}
	if len(nodeIDs) > 0 {
		chs, err := s.Store.Ent.Chain.Query().Where(chain.EntryNodeIDIn(nodeIDs...)).All(ctx)
		if err != nil {
			return nil, err
		}
		for _, ch := range chs {
			used[ch.ExitID] = true
		}
	}
	rs, err := s.Store.Ent.Route.Query().Where(route.GateIDEQ(gid)).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, r := range rs {
		used[r.ExitID] = true
	}
	direct, err := s.Store.Ent.Exit.Query().Where(entexit.GateIDEQ(gid), entexit.TagEQ("direct")).Only(ctx)
	if err == nil {
		used[direct.ID] = true
	} else if !ent.IsNotFound(err) {
		return nil, err
	}
	return used, nil
}

func (s *Server) saveLocatedProbe(ctx context.Context, owner, location string, gateID int, p *exitProbe) error {
	if p == nil || !p.OK && p.At.IsZero() {
		return nil
	}
	p.Location = location
	if location == "gate" {
		p.GateID = gateID
	}
	return s.saveExitProbe(ctx, exitProbeLocationOwner(owner, location, gateID), p)
}

func (s *Server) loadLocatedProbe(ctx context.Context, owner, location string, gateID int) (exitProbe, bool) {
	return s.loadExitProbe(ctx, exitProbeLocationOwner(owner, location, gateID))
}

// attachMatchingProbe 只挂上指定 VPS 的实测，避免目录里的另一处结果冒充这条路径。
func (s *Server) attachMatchingProbe(ctx context.Context, v gin.H, exitID, gateID int) {
	if v == nil {
		return
	}
	delete(v, "probe")
	if snapshot, ok := s.loadGateExit(ctx, exitID, gateID); ok {
		v["resolved"] = storedExitView(snapshot)
	}
	if gateID <= 0 {
		return
	}
	if p, ok := s.loadLocatedProbe(ctx, exitOwner(exitID), "gate", gateID); ok {
		v["probe"] = p
	}
}

func (s *Server) catalogSpec(ctx context.Context, spec ExitSpec, owner *int, admin bool) error {
	if spec.Kind == "freedom" {
		return errors.New("system_exit")
	}
	if owner != nil && (spec.Kind == "freedom" || spec.Kind == "blackhole") {
		return errors.New("exit_not_allowed")
	}
	cc := chainCtx{admin: admin}
	if owner != nil {
		cc.userID = *owner
	}
	switch spec.Kind {
	case "node":
		n, err := s.Store.Ent.Node.Get(ctx, spec.NodeID)
		if err != nil {
			return err
		}
		if !n.Enabled {
			return errors.New("chain_exit_unavailable")
		}
		return nil
	case "socks5", "http", "https", "link":
		_, err := s.resolveExitSpec(ctx, 0, spec, owner != nil, cc)
		return err
	case "source":
		if err := s.checkSourceAccess(ctx, spec.source(), cc); err != nil {
			return err
		}
		_, err := s.resolveSpec(ctx, nil, nil, spec.source(), owner != nil)
		return err
	case "blackhole":
		if !admin {
			return errors.New("exit_not_allowed")
		}
		return nil
	default:
		return errors.New("invalid_chain")
	}
}

func (s *Server) verifyExitOnGate(ctx context.Context, g *ent.Gate, self int, owner *int, spec ExitSpec, admin, probe bool, kernel string) (*exitProbe, error) {
	cc := chainCtx{kernel: kernel, admin: admin, gate: g}
	if owner != nil {
		cc.userID = *owner
	}
	return s.validateExitSpec(ctx, g, self, spec, owner != nil, probe, cc)
}

func (s *Server) applyExitOnGate(ctx context.Context, e *ent.Exit, g *ent.Gate, spec ExitSpec, admin bool) (result exitApplyResult) {
	result = exitApplyResult{GateID: g.ID, Name: g.Name, Status: "kept"}
	var outcome error
	finish := operationStage(ctx, "exit.apply", "exit.id", e.ID, "gate.id", g.ID)
	defer func() { finish(outcome) }()
	current, err := s.exitBinding(ctx, e.ID, g.ID)
	if err != nil {
		outcome = err
		result.Status, result.Reason = "failed", "读取生效配置失败："+redact(err.Error())
		return
	}
	fail := func(err error) exitApplyResult {
		outcome = err
		result.Reason = exitVerificationReason(redact(err.Error()))
		status := exitbinding.StatusFailed
		if missingMihomo(err) {
			result.Status, status = "waiting", exitbinding.StatusWaiting
		} else if current == nil {
			result.Status = "failed"
		}
		if current != nil {
			if updateErr := s.Store.Ent.ExitBinding.UpdateOneID(current.ID).SetStatus(status).SetReason(truncate(result.Reason, 500)).Exec(ctx); updateErr != nil {
				outcome = errors.Join(err, updateErr)
				result.Status = "failed"
				result.Reason += "；记录失败状态失败：" + redact(updateErr.Error())
			}
		}
		return result
	}
	if current != nil {
		applied, err := s.bindingSpec(current)
		if err != nil {
			return fail(err)
		}
		if current.Status == exitbinding.StatusApplied && sameConnection(applied, spec) && (spec.Kind != "source" || len(current.Resolved) > 0) {
			result.Status = "applied"
			return
		}
	}
	if err := s.checkGateKernels(ctx, g, spec); err != nil {
		return fail(err)
	}
	p, err := s.verifyExitOnGate(ctx, g, 0, e.OwnerUserID, spec, admin, true, "")
	if err != nil {
		return fail(err)
	}
	tx, err := s.Store.Ent.Tx(ctx)
	if err != nil {
		return fail(err)
	}
	// 绑定、解析快照与配置版本一起提交；任一步失败，Gate 仍读取旧版本。
	if err = s.bindApplied(ctx, tx.Client(), e.ID, g.ID, spec, p); err == nil {
		err = tx.Gate.UpdateOneID(g.ID).AddConfigRevision(1).Exec(ctx)
	}
	if err != nil {
		return fail(errors.Join(err, tx.Rollback()))
	}
	if err = tx.Commit(); err != nil {
		return fail(err)
	}
	s.recordRevision(ctx, g.ID, g.ConfigRevision, g.ConfigRevision+1)
	s.waiters.notify(g.ID)
	if err = s.Store.Notifications.Publish(ctx, "gate:"+itoa(g.ID)); err != nil {
		s.tasks.note("exit-notify-"+itoa(g.ID), err)
	}
	if err = s.saveLocatedProbe(ctx, exitOwner(e.ID), "gate", g.ID, p); err != nil {
		s.tasks.note("exit-probe-save-"+itoa(e.ID), err)
	}
	result.Status = "applied"
	return
}

func (s *Server) applyExitToUsers(ctx context.Context, e *ent.Exit, spec ExitSpec, admin bool) ([]exitApplyResult, error) {
	gates, err := s.referencingGates(ctx, e.ID)
	if err != nil {
		return nil, err
	}
	out := []exitApplyResult{}
	for _, gid := range gates {
		g, err := s.Store.Ent.Gate.Get(ctx, gid)
		if err != nil {
			out = append(out, exitApplyResult{GateID: gid, Status: "kept", Reason: "找不到这台 VPS"})
			continue
		}
		out = append(out, s.applyExitOnGate(ctx, e, g, spec, admin))
	}
	return out, nil
}

func (s *Server) probeExitFromCenter(ctx context.Context, spec ExitSpec, publicOnly bool) (*exitProbe, error) {
	if spec.Kind == "node" {
		return nil, errors.New("exit_probe_unsupported: 另一台 VPS 的出口要由使用它的入口 VPS 实测，中心测不到这段内部转发")
	}
	if spec.Kind == "freedom" || spec.Kind == "blackhole" {
		return nil, errors.New("probe_unsupported")
	}
	cc := chainCtx{admin: !publicOnly}
	ex, err := s.resolveExitSpec(ctx, 0, spec, publicOnly, cc)
	if err != nil {
		return nil, err
	}
	bin, driver, err := deployedProbeBinary(s, ctx, "mihomo")
	if err != nil {
		return nil, err
	}
	proxies := []map[string]any{}
	for i, link := range ex.Links {
		if i >= 5 || link == "" {
			break
		}
		proxy, err := clientOutbound(link)
		if err != nil {
			return nil, err
		}
		proxy["name"] = fmt.Sprintf("exit-%d", i+1)
		proxies = append(proxies, proxy)
	}
	if len(proxies) == 0 {
		return nil, errors.New("出口缺少可实测的客户端配置")
	}
	finish := operationStage(ctx, "exit.probe.measure", "location", "center", "echo", probeHost(s.probeTarget(ctx)), "download", s.probeDownload(ctx) != "")
	measured, err := measureExitProxy(ctx, nodeprobe.MeasureOptions{
		Binary: bin, Run: driver.Run, Check: driver.Check, Proxy: proxies[0],
		Echo: s.probeTarget(ctx), Download: s.probeDownload(ctx), PublicOnly: publicOnly,
	})
	finish(err)
	if err != nil {
		return nil, err
	}
	p := &exitProbe{
		OK: true, Mode: "proxy", LatencyMS: measured.LatencyMS, ExitIP: measured.ExitIP,
		Bytes: measured.Bytes, DownloadMS: measured.DownloadMS, DownloadError: measured.DownloadError,
		Note: "中心实测不代表任何 VPS 可用", At: time.Now().UTC(), Location: "center",
	}
	s.finishProbe(ctx, p)
	return p, nil
}

// measureExitProxy 是中心本机实测。测试可替换它，避免依赖已下载的 mihomo。
var measureExitProxy = nodeprobe.Measure

func (s *Server) noteExitApplyRetry(c *gin.Context, exitID int) error {
	v, ok := c.Get("pending_exit_request")
	if !ok {
		return nil
	}
	p := v.(pendingExit)
	p.Status = "waiting"
	p.Reason = "mihomo 正在下载，完成后自动验证"
	p.ExitID = exitID
	return s.savePendingExit(c.Request.Context(), p)
}

func (s *Server) bindingsNeedApply(ctx context.Context, e *ent.Exit, spec ExitSpec) (bool, error) {
	gates, err := s.referencingGates(ctx, e.ID)
	if err != nil {
		return false, err
	}
	for _, gid := range gates {
		b, err := s.exitBinding(ctx, e.ID, gid)
		if err != nil {
			return false, err
		}
		if b == nil || b.Status != exitbinding.StatusApplied {
			return true, nil
		}
		applied, err := s.bindingSpec(b)
		if err != nil {
			return false, err
		}
		if !sameConnection(applied, spec) {
			return true, nil
		}
	}
	return false, nil
}

func (s *Server) bindApplied(ctx context.Context, db *ent.Client, exitID, gateID int, spec ExitSpec, probe *exitProbe) error {
	if exitID == 0 || gateID == 0 {
		return nil
	}
	e, err := db.Exit.Get(ctx, exitID)
	if err != nil {
		return err
	}
	if e.Tag == "direct" || e.Kind == entexit.KindFreedom {
		return nil
	}
	if spec.Kind == "" {
		spec, err = s.exitSpec(e)
		if err != nil {
			return err
		}
	}
	var resolved json.RawMessage
	if probe != nil {
		resolved = probe.resolved
	}
	return s.writeExitBinding(ctx, db, exitID, gateID, spec, exitbinding.StatusApplied, "", resolved)
}

func (s *Server) exitUsageView(ctx context.Context, e *ent.Exit) ([]gin.H, []exitProbe, objectState, error) {
	if e.Tag == "direct" {
		g, _ := s.Store.Ent.Gate.Get(ctx, exitGateID(e))
		return nil, nil, exitObjectState(e, g, exitProbe{}, false), nil
	}
	gates, err := s.referencingGates(ctx, e.ID)
	if err != nil {
		return nil, nil, objectState{}, err
	}
	uses := []gin.H{}
	probes := []exitProbe{}
	for _, gid := range gates {
		g, err := s.Store.Ent.Gate.Get(ctx, gid)
		name := "VPS " + itoa(gid)
		if err == nil {
			name = g.Name
		}
		b, err := s.exitBinding(ctx, e.ID, gid)
		if err != nil {
			return nil, nil, objectState{}, err
		}
		item := gin.H{"gate_id": gid, "name": name, "status": "unknown", "reason": "这台 VPS 还没有验证通过的生效配置"}
		if b != nil {
			item["status"] = b.Status
			item["reason"] = b.Reason
			if b.Status == exitbinding.StatusApplied && b.Reason == "" {
				item["reason"] = "已替换为当前连接参数"
			}
			if b.Status == exitbinding.StatusFailed || b.Status == exitbinding.StatusWaiting {
				item["status"] = "kept"
				if b.Reason == "" {
					item["reason"] = "验证未通过，仍使用上一次生效配置"
				}
			}
			if b.VerifiedAt != nil {
				item["verified_at"] = b.VerifiedAt
			}
		}
		if p, ok := s.loadLocatedProbe(ctx, exitOwner(e.ID), "gate", gid); ok {
			item["probe"] = p
			probes = append(probes, p)
		}
		uses = append(uses, item)
	}
	centerOK := false
	var centerAt *time.Time
	if p, ok := s.loadLocatedProbe(ctx, exitOwner(e.ID), "center", 0); ok {
		probes = append(probes, p)
		centerOK = p.OK
		centerAt = &p.At
	}
	applied, kept, unknown := 0, 0, 0
	for _, item := range uses {
		switch item["status"] {
		case "applied":
			applied++
		case "kept", "failed", "waiting":
			kept++
		default:
			unknown++
		}
	}
	return uses, probes, exitUsageState(e, applied, kept, unknown, centerOK, centerAt), nil
}

func (s *Server) userMayProbeGate(ctx context.Context, uid, gid int) error {
	u, err := s.Store.Ent.User.Get(ctx, uid)
	if err != nil || !u.AllowChain {
		return errors.New("chain_disabled")
	}
	allowed, err := s.userNodeIDs(ctx, u)
	if err != nil {
		return err
	}
	for _, n := range allowed {
		if n.GateID == gid && n.Enabled {
			return nil
		}
	}
	return errors.New("entry_not_allowed")
}
