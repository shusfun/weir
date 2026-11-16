package center

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"net"
	"os"
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
	a.POST("/state", s.agentState)
	a.POST("/heartbeat/v2", s.agentHeartbeat)
	a.POST("/meter", s.agentMeter)
	a.GET("/country-networks", s.agentCountryNetworks)
	a.POST("/live", s.agentLive)
	a.POST("/browser-tests", s.agentBrowserTests)
	a.POST("/poll", s.agentPoll)
	a.POST("/tasks/:id/:operation", s.agentTaskUpdate)
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
	s.browserTestRoutes(auth, admin)
	s.workerObservationRoutes(admin)
	s.certRoutes(admin)
	s.nodeCertRoutes(admin)
	s.nodeRoutes(admin)
	s.exitRoutes(auth, admin)
	s.aiRoutes(admin)
	s.routingRoutes(admin)
	s.domainRoutes(admin)
	s.kernelAdminRoutes(admin)
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
	if st.AppliedRevision > 0 && st.FailedRevision == 0 && st.ConflictPort == 0 && st.Error == "" {
		if err = s.acknowledgeUoT(ctx, g.ID, st.AppliedRevision); err != nil {
			failInternal(c, 500, "uot_ack_failed", err)
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
	s.agentHeartbeatV2(c, hb)
}

// 空状态表示当前没有更新操作。
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

// enqueueAction retains the operation ID while the leased task is its only execution path.
func (s *Server) enqueueAction(ctx context.Context, gid int, kind string, payload map[string]any, ttl time.Duration) (*ent.GateAction, error) {
	caps, err := s.Store.Tasks.Capabilities(ctx, gid)
	if err != nil {
		return nil, err
	}
	if caps.Tasks[kind] != contracts.TaskSchemaVersion(kind) {
		return nil, errNoWorker
	}
	return s.enqueueWorkerTask(ctx, gid, kind, payload, ttl)
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
