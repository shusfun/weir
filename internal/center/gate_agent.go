package center

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"os"
	"reflect"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/shusfun/weir/internal/buildinfo"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/gateaction"
	"github.com/shusfun/weir/packages/contracts"
)

func (s *Server) gateRoutes(ready, auth, admin *gin.RouterGroup) {
	ready.POST("/agent/pair", s.agentPair)
	ready.POST("/agent/challenge", s.agentChallenge)
	ready.POST("/agent/auth", s.agentAuthenticate)
	a := ready.Group("/agent")
	a.Use(s.agentAuth)
	a.Use(s.agentCommunication)
	a.GET("/config", s.agentConfig)
	a.POST("/state", s.agentState)
	a.POST("/heartbeat", s.agentHeartbeat)
	a.POST("/heartbeat/v2", s.agentHeartbeat)
	a.POST("/meter", s.agentMeter)
	a.POST("/live", s.agentLive)
	a.GET("/actions", s.agentActions)
	a.POST("/poll", s.agentPoll)
	a.POST("/tasks/:id/:operation", s.agentTaskUpdate)
	a.POST("/actions/:id", s.agentActionResult)
	a.GET("/kernels/:kernel/:version", s.agentKernel)
	a.GET("/kernel-assets/:kernel/:version", s.agentKernelArchive)
	a.GET("/release/:version", s.agentRelease)
	a.GET("/certs/:id", s.agentCert)
	a.POST("/panel-secret", s.agentPanelSecret)
	a.GET("/uploads/:id", s.agentUploadState)
	a.PUT("/uploads/:id", s.agentUploadChunk)
	a.POST("/uploads/:id/progress", s.agentUploadProgress)
	a.POST("/self-update", s.agentSelfUpdate)
	s.gateAdminRoutes(admin)
	s.workerObservationRoutes(admin)
	s.certRoutes(admin)
	s.nodeCertRoutes(admin)
	s.nodeRoutes(admin)
	s.exitRoutes(auth, admin)
	s.aiRoutes(admin)
	s.routingRoutes(admin)
	s.kernelAdminRoutes(admin)
}

// agentConfig 长轮询：版本与 since 不同时立即返回完整配置，否则最多等 25 秒后返回 204。
// 编译失败（例如内核尚未下载）返回 409 与原因，Gate 保持上一份已生效配置并稍后重试。
func (s *Server) agentConfig(c *gin.Context) {
	g := gateFrom(c)
	since, _ := strconv.ParseInt(c.Query("since"), 10, 64)
	ctx := c.Request.Context()
	timeout := time.NewTimer(25 * time.Second)
	defer timeout.Stop()
	for {
		wait := s.waiters.wait(g.ID) // 先订阅再读取，避免错过两者之间的版本变化
		cur, err := s.Store.Ent.Gate.Get(ctx, g.ID)
		if err != nil {
			failInternal(c, 503, "config_unavailable", err)
			return
		}
		execSince, execErr := strconv.ParseInt(c.Query("exec_since"), 10, 64)
		if c.Query("exec_since") != "" && execErr == nil && cur.ConfigRevision == since {
			exec, err := s.loadExecution(ctx, g.ID)
			if err != nil {
				failInternal(c, 503, "execution_unavailable", err)
				return
			}
			if exec.Seq != 0 && exec.Seq != execSince {
				cfg, err := s.compileGateConfig(ctx, cur)
				if err != nil {
					failInternal(c, 503, "execution_unavailable", err)
					return
				}
				cfg.ExecutionOnly = true
				cfg.ExecutionSeq = exec.Seq
				cfg.Budgets = exec.Budgets
				cfg.ForceDisconnect = exec.ForceDisconnect
				c.JSON(200, cfg)
				return
			}
		}
		if raw, ok, err := s.Store.KV.Get(ctx, certPushKey(g.ID)); err != nil {
			failInternal(c, 503, "cert_sync_unavailable", err)
			return
		} else if ok && raw != "" && cur.ConfigRevision == since {
			cfg, err := s.compileGateConfig(ctx, cur)
			if err != nil {
				failInternal(c, 503, "cert_sync_unavailable", err)
				return
			}
			cfg.CertOnly = true
			c.JSON(200, cfg)
			return
		}
		if cur.ConfigRevision != since {
			cfg, err := s.compileGateConfig(ctx, cur)
			if err != nil {
				_ = s.Store.Ent.Gate.UpdateOneID(g.ID).SetLastError(err.Error()).Exec(ctx)
				c.AbortWithStatusJSON(409, gin.H{"error": gin.H{"code": "config_unavailable", "message": err.Error()}})
				return
			}
			c.JSON(200, cfg)
			return
		}
		select {
		case <-wait:
		case <-timeout.C:
			c.Status(204)
			return
		case <-ctx.Done():
			return
		}
	}
}

// agentState 记录应用结果；新版本生效后确认对应额度，释放的额度分给其他 Gate。
func (s *Server) agentState(c *gin.Context) {
	var st contracts.GateState
	if !bindWorker(c, &st, contracts.MaxTaskPayload) {
		return
	}
	g, ctx := gateFrom(c), c.Request.Context()
	if st.Apply != nil && !validKernelEvents(st.Apply.Events) {
		failure(c, 400, "invalid_gate_state")
		return
	}
	if st.AppliedRevision < 0 || st.AppliedRevision > g.ConfigRevision || st.FailedRevision < 0 || st.FailedRevision > g.ConfigRevision || len(st.Error) > 8192 || st.ConflictPort < 0 || st.ConflictPort > 65535 {
		failure(c, 400, "invalid_gate_state")
		return
	}
	upd := g.Update().SetLastError(st.Error)
	advanced := st.AppliedRevision > g.AppliedRevision
	if advanced {
		upd.SetAppliedRevision(st.AppliedRevision)
	}
	if st.FailedRevision > 0 {
		upd.SetFailedRevision(st.FailedRevision)
	}
	// 远端 VPS 的服务日志不在中心这台机器上：被拒绝的版本在中心也记一次（同一版本只记一次）。
	if st.FailedRevision > g.FailedRevision {
		slog.Error("gate rejected revision", "gate", g.ID, "revision", st.FailedRevision, "applied", st.AppliedRevision, "err", redact(st.Error))
		_, observeErr := s.enqueueObservation(ctx, observationInput{Point: "gate.kernel", Resource: "gate:" + itoa(g.ID), Text: st.Error, Origin: "center.agentState"})
		s.tasks.note("observation-producer", observeErr)
	}
	g, err := upd.Save(ctx)
	if err != nil {
		failInternal(c, 500, "save_failed", err)
		return
	}
	if st.ConflictPort > 0 {
		if err = s.resolvePortConflict(ctx, g, st.ConflictPort); err != nil {
			_ = g.Update().SetLastError("端口冲突无法自动解决：" + err.Error()).Exec(ctx)
		}
		s.waiters.notify(g.ID)
		_ = s.Store.Notifications.Publish(ctx, "gate:"+itoa(g.ID))
	}
	if st.ExecutionSeq > 0 && st.FailedRevision == 0 && st.Error == "" {
		if err = s.confirmExecution(ctx, g.ID, st.ExecutionSeq); err != nil {
			failInternal(c, 500, "save_failed", err)
			return
		}
	}
	if advanced {
		s.refreshGateReachability(g.ID)
		users, _ := s.confirmBudgets(ctx, g.ID, st.AppliedRevision)
		for _, uid := range users {
			_, _ = s.rebalanceUser(ctx, uid)
		}
	}
	if st.CertSynced {
		if err = s.Store.KV.Del(ctx, certPushKey(g.ID)); err != nil {
			failInternal(c, 500, "save_failed", err)
			return
		}
	}
	if st.Apply != nil {
		trace := st.Apply.TraceID
		if trace == "" {
			if id, _ := traceDetail(c)["trace_id"].(string); id != "" {
				trace = id
			}
		}
		if err = s.appendKernelEvents(ctx, g.ID, st.Apply.Revision, trace, st.Apply.Events); err != nil {
			failInternal(c, 500, "save_failed", err)
			return
		}
	}
	if advanced || st.FailedRevision > 0 || st.ConflictPort > 0 || (st.Apply != nil && st.Apply.Mode != "") {
		result := "ok"
		if st.FailedRevision > 0 || st.ConflictPort > 0 {
			result = "failed"
		}
		detail := traceDetail(c)
		detail["origin"], detail["applied_revision"], detail["failed_revision"], detail["conflict_port"] = "center.agentState", st.AppliedRevision, st.FailedRevision, st.ConflictPort
		if st.Apply != nil {
			detail["mode"], detail["reason"] = st.Apply.Mode, st.Apply.Reason
			if st.Apply.TraceID != "" {
				detail["trace_id"] = st.Apply.TraceID
			}
		}
		_, recordErr := s.Store.Ent.Audit.Create().SetAction("gate.config.apply").SetTarget(itoa(g.ID)).SetResult(result).SetDetail(detail).Save(ctx)
		s.tasks.note("gate-config-record", recordErr)
	}
	c.JSON(200, gin.H{"ok": true})
}

func publicAddress(ip string) string {
	if !publicAddr(net.ParseIP(ip)) {
		return ""
	}
	return ip
}

// agentHeartbeat 记录版本、实际监听端口、运行中的内核与主机观测；统计端口被其他程序占用时换端口。
func validSelfUpdate(su *contracts.SelfUpdateStatus) bool {
	return su != nil && len(su.Error) <= 2000 && len(su.Source) <= 200 && len(su.Version) <= 40 && validUpdateState(su.State)
}

func selfUpdateFields(su *contracts.SelfUpdateStatus) map[string]any {
	return map[string]any{"version": su.Version, "state": su.State, "done": su.Done, "total": su.Total, "speed": su.Speed, "source": su.Source, "error": su.Error, "at": su.At}
}

// agentSelfUpdate 接收 Gate 自更新的进度：心跳 30 秒一次太慢，下载期间单独上报，只写这一个字段。
func (s *Server) agentSelfUpdate(c *gin.Context) {
	var su contracts.SelfUpdateStatus
	if !bind(c, &su) {
		return
	}
	if !validSelfUpdate(&su) {
		failure(c, 400, "invalid_request")
		return
	}
	if err := s.Store.Ent.Gate.UpdateOneID(gateFrom(c).ID).SetSelfUpdate(selfUpdateFields(&su)).Exec(c.Request.Context()); err != nil {
		failInternal(c, 500, "save_failed", err)
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (s *Server) agentHeartbeat(c *gin.Context) {
	var hb contracts.Heartbeat
	if !bindWorker(c, &hb, contracts.MaxTaskPayload) {
		return
	}
	if hb.Protocol != 0 {
		s.agentHeartbeatV2(c, hb)
		return
	}
	g, ctx := gateFrom(c), c.Request.Context()
	ports := make([]map[string]any, 0, len(hb.ListeningPorts))
	for _, p := range hb.ListeningPorts {
		ports = append(ports, map[string]any{"port": p.Port, "proto": p.Proto, "process": p.Process})
	}
	observationsChanged := !reflect.DeepEqual(g.ListeningPorts, ports) || !reflect.DeepEqual(g.KernelsRunning, hb.Kernels)
	arch := hb.Arch
	if arch != "amd64" && arch != "arm64" {
		arch = g.Arch
	}
	upd := g.Update().SetVersion(hb.Version).SetHostname(hb.Hostname).SetArch(arch).SetListeningPorts(ports).SetKernelsRunning(hb.Kernels).SetLastSeenAt(time.Now())
	host := map[string]any{}
	if hb.Host != nil {
		c.Set("observation_id", hb.Host.ObservationID)
		b, _ := json.Marshal(hb.Host)
		_ = json.Unmarshal(b, &host)
	}
	host["node_test_version"] = hb.NodeTestVersion
	upd.SetHost(host)
	if len(hb.Center) <= 255 {
		upd.SetCenterURL(hb.Center)
	}
	// 自更新的进度或失败原因：这次没有就清空（已更新成功或已与中心同版本）。
	if su := hb.SelfUpdate; validSelfUpdate(su) {
		upd.SetSelfUpdate(selfUpdateFields(su))
	} else {
		upd.ClearSelfUpdate()
	}
	// 防火墙：这次没读到时清空（显示「未知」），不沿用旧的结果。
	if fw := hb.Firewall; fw != nil && len(fw.Rules) <= 500 {
		upd.SetFirewall(map[string]any{"tool": fw.Tool, "active": fw.Active, "rules": fw.Rules, "error": fw.Error, "at": time.Now().UTC()})
	} else {
		upd.ClearFirewall()
	}
	if g.PublicHost == "" {
		if ip := publicAddress(c.ClientIP()); ip != "" {
			upd.SetPublicHost(ip)
		}
	}
	first := g.LastSeenAt == nil
	g, err := upd.Save(ctx)
	if err != nil {
		failInternal(c, 500, "save_failed", err)
		return
	}
	if first {
		// 第一次心跳：「复制安装命令」的配对进度据此显示「已上线」
		s.notifyConsole("gate", itoa(g.ID))
	}
	s.absorbPanelReport(ctx, g, hb)
	if observationsChanged && g.AppliedRevision >= g.ConfigRevision {
		s.refreshGateReachability(g.ID)
	}
	s.recordHost(ctx, g.ID, hb.Host, time.Now())
	before := g.ConfigRevision
	if g, err = s.ensureStatsPorts(ctx, g); err != nil {
		failInternal(c, 500, "port_allocation_failed", err)
		return
	}
	if g.ConfigRevision != before {
		s.waiters.notify(g.ID)
		_ = s.Store.Notifications.Publish(ctx, "gate:"+itoa(g.ID))
	}
	c.JSON(200, contracts.HeartbeatReply{HeartbeatProtocol: contracts.HeartbeatProtocol, ConfigRevision: g.ConfigRevision, Center: s.centerOrigin(ctx), Release: s.heartbeatRelease(ctx, g, hb.Version)})
}

// validUpdateState：旧版 Gate 不报 state（只报失败原因），按空值接受。
func validUpdateState(v string) bool {
	return v == "" || v == "downloading" || v == "replacing" || v == "failed"
}

// unexpiredActions 用绝对时间丢掉已过期动作。SQLite 的时间比较不是时区感知的。
func unexpiredActions(items []*ent.GateAction, now time.Time) []*ent.GateAction {
	out := make([]*ent.GateAction, 0, len(items))
	for _, a := range items {
		if a.ExpiresAt.After(now) {
			out = append(out, a)
		}
	}
	return out
}

// agentActions 长轮询待执行动作；过期的动作标记为已过期，不再下发。
func (s *Server) agentActions(c *gin.Context) {
	g, ctx := gateFrom(c), c.Request.Context()
	timeout := time.NewTimer(25 * time.Second)
	defer timeout.Stop()
	for {
		wait := s.actionWaiters.wait(g.ID)
		// SQLite 按字符串比较时间，UTC 与本地时区会对不上。过期与否在读出后用绝对时间判断。
		items, err := s.Store.Ent.GateAction.Query().Where(gateaction.GateIDEQ(g.ID), gateaction.StatusEQ(gateaction.StatusPending)).Order(ent.Asc(gateaction.FieldID)).Limit(10).All(ctx)
		if err != nil {
			failInternal(c, 503, "actions_unavailable", err)
			return
		}
		items = unexpiredActions(items, time.Now())
		if len(items) > 0 {
			out := make([]contracts.Action, 0, len(items))
			err = s.Store.Lock.Do(ctx, "legacy-actions:"+itoa(g.ID), func(ctx context.Context) error {
				if _, e := s.Store.Tasks.Capabilities(ctx, g.ID); e == nil {
					return nil
				} else if !errors.Is(e, sql.ErrNoRows) {
					return e
				}
				for _, a := range items {
					n, e := s.Store.Ent.GateAction.Update().Where(gateaction.IDEQ(a.ID), gateaction.StatusEQ(gateaction.StatusPending)).SetStatus(gateaction.StatusRunning).Save(ctx)
					if e != nil {
						return e
					}
					if n != 1 {
						continue
					}
					out = append(out, contracts.Action{ID: int64(a.ID), Kind: a.Kind, Payload: a.Payload, ExpiresAt: a.ExpiresAt})
				}
				return nil
			})
			if err != nil {
				failInternal(c, 503, "actions_unavailable", err)
				return
			}
			c.JSON(200, out)
			return
		}
		select {
		case <-wait:
		case <-timeout.C:
			c.JSON(200, []contracts.Action{})
			return
		case <-ctx.Done():
			return
		}
	}
}

func (s *Server) agentActionResult(c *gin.Context) {
	var res contracts.ActionResult
	if !bind(c, &res) {
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	g, ctx := gateFrom(c), c.Request.Context()
	a, qerr := s.Store.Ent.GateAction.Query().Where(gateaction.IDEQ(id), gateaction.GateIDEQ(g.ID)).Only(ctx)
	if err != nil || qerr != nil {
		failure(c, 404, "action_not_found")
		return
	}
	if _, queued := a.Payload["protocol"]; queued {
		failure(c, 409, "task_lease_required")
		return
	}
	if a.Status == gateaction.StatusDone || a.Status == gateaction.StatusFailed {
		c.JSON(200, gin.H{"ok": true})
		return
	}
	status := gateaction.StatusDone
	if !res.OK {
		status = gateaction.StatusFailed
	}
	if _, err = a.Update().SetStatus(status).SetResult(res.Result).SetError(res.Error).SetFinishedAt(time.Now()).Save(ctx); err != nil {
		failInternal(c, 500, "save_failed", err)
		return
	}
	s.actionDone.notify(a.ID)
	c.JSON(200, gin.H{"ok": true})
}

// enqueueAction 下发一次性动作并唤醒 Gate 的动作长轮询。
func (s *Server) enqueueAction(ctx context.Context, gid int, kind string, payload map[string]any, ttl time.Duration) (*ent.GateAction, error) {
	if _, err := s.Store.Tasks.Capabilities(ctx, gid); err == nil {
		return s.enqueueWorkerTask(ctx, gid, kind, payload, ttl)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	a, err := s.Store.Ent.GateAction.Create().SetGateID(gid).SetKind(kind).SetPayload(payload).SetExpiresAt(time.Now().Add(ttl)).Save(ctx)
	if err == nil {
		s.actionWaiters.notify(gid)
	}
	return a, err
}

// waitAction 等待动作完成（探测类动作在请求内同步返回结果），超时返回当前状态。
func (s *Server) waitAction(ctx context.Context, id int, timeout time.Duration) (*ent.GateAction, error) {
	deadline := time.Now().Add(timeout)
	for {
		wait := s.actionDone.wait(id)
		a, err := s.Store.Ent.GateAction.Get(ctx, id)
		if err != nil || a.Status == gateaction.StatusDone || a.Status == gateaction.StatusFailed || a.Status == gateaction.StatusExpired || time.Now().After(deadline) {
			return a, err
		}
		select {
		case <-wait:
		case <-time.After(time.Until(deadline)):
		case <-ctx.Done():
			return a, ctx.Err()
		}
	}
}

func (s *Server) agentKernel(c *gin.Context) {
	g := gateFrom(c)
	kernel := c.Param("kernel")
	if !validKernel(kernel) {
		failure(c, 404, "kernel_not_found")
		return
	}
	path, sum, err := s.kernelCache().Get(kernel, c.Param("version"), g.Arch)
	if err != nil {
		failure(c, 404, "kernel_not_found")
		return
	}
	c.Header("X-Kernel-SHA256", sum)
	s.distributeFile(c, path)
}

var selfDigest struct {
	once sync.Once
	sum  string
	err  error
}

// selfSHA256 计算中心自身程序的摘要（Gate 自更新时核对）。
func selfSHA256() (string, string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", "", err
	}
	selfDigest.once.Do(func() {
		f, err := os.Open(exe)
		if err != nil {
			selfDigest.err = err
			return
		}
		defer f.Close()
		h := sha256.New()
		_, selfDigest.err = io.Copy(h, f)
		selfDigest.sum = hex.EncodeToString(h.Sum(nil))
	})
	return exe, selfDigest.sum, selfDigest.err
}

// agentRelease 把与中心同版本的程序提供给 Gate 自更新；不同架构的从 GitHub Release 取（见 gate_release.go）。
func (s *Server) agentRelease(c *gin.Context) {
	g := gateFrom(c)
	if c.Param("version") != buildinfo.Version {
		failure(c, 404, "release_unavailable")
		return
	}
	exe, sum, err := s.gateProgram(c.Request.Context(), g.Arch)
	if err != nil {
		failDetail(c, 503, "release_unavailable", err.Error(), nil)
		return
	}
	c.Header("X-Release-SHA256", sum)
	s.distributeFile(c, exe)
}
