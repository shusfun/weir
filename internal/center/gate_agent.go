package center

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"os"
	"runtime"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/imzyb/MiGate/internal/buildinfo"
	"github.com/imzyb/MiGate/internal/store/ent"
	"github.com/imzyb/MiGate/internal/store/ent/gateaction"
	"github.com/imzyb/MiGate/packages/contracts"
)

func (s *Server) gateRoutes(ready, admin *gin.RouterGroup) {
	ready.POST("/agent/pair", s.agentPair)
	ready.POST("/agent/challenge", s.agentChallenge)
	ready.POST("/agent/auth", s.agentAuthenticate)
	a := ready.Group("/agent")
	a.Use(s.agentAuth)
	a.GET("/config", s.agentConfig)
	a.POST("/state", s.agentState)
	a.POST("/heartbeat", s.agentHeartbeat)
	a.POST("/meter", s.agentMeter)
	a.GET("/actions", s.agentActions)
	a.POST("/actions/:id", s.agentActionResult)
	a.GET("/kernels/:kernel/:version", s.agentKernel)
	a.GET("/release/:version", s.agentRelease)
	s.gateAdminRoutes(admin)
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
			failure(c, 503, "config_unavailable")
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
	if !bind(c, &st) {
		return
	}
	g, ctx := gateFrom(c), c.Request.Context()
	upd := g.Update().SetLastError(st.Error)
	advanced := st.AppliedRevision > g.AppliedRevision
	if advanced {
		upd.SetAppliedRevision(st.AppliedRevision)
	}
	if st.FailedRevision > 0 {
		upd.SetFailedRevision(st.FailedRevision)
	}
	g, err := upd.Save(ctx)
	if err != nil {
		failure(c, 500, "save_failed")
		return
	}
	if st.ConflictPort > 0 {
		if err = s.resolvePortConflict(ctx, g, st.ConflictPort); err != nil {
			_ = g.Update().SetLastError("端口冲突无法自动解决：" + err.Error()).Exec(ctx)
		}
		s.waiters.notify(g.ID)
	}
	if advanced {
		users, _ := s.confirmBudgets(ctx, g.ID, st.AppliedRevision)
		for _, uid := range users {
			_, _ = s.rebalanceUser(ctx, uid)
		}
	}
	c.JSON(200, gin.H{"ok": true})
}

func publicAddress(ip string) string {
	addr := net.ParseIP(ip)
	if addr == nil || addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() || addr.IsUnspecified() {
		return ""
	}
	return ip
}

// agentHeartbeat 记录版本、实际监听端口、运行中的内核与主机观测；统计端口被其他程序占用时换端口。
func (s *Server) agentHeartbeat(c *gin.Context) {
	var hb contracts.Heartbeat
	if !bind(c, &hb) {
		return
	}
	g, ctx := gateFrom(c), c.Request.Context()
	ports := make([]map[string]any, 0, len(hb.ListeningPorts))
	for _, p := range hb.ListeningPorts {
		ports = append(ports, map[string]any{"port": p.Port, "proto": p.Proto, "process": p.Process})
	}
	arch := hb.Arch
	if arch != "amd64" && arch != "arm64" {
		arch = g.Arch
	}
	upd := g.Update().SetVersion(hb.Version).SetHostname(hb.Hostname).SetArch(arch).SetListeningPorts(ports).SetKernelsRunning(hb.Kernels).SetLastSeenAt(time.Now())
	if hb.Host != nil {
		var host map[string]any
		if b, err := json.Marshal(hb.Host); err == nil && json.Unmarshal(b, &host) == nil {
			upd.SetHost(host)
		}
	}
	if g.PublicHost == "" {
		if ip := publicAddress(c.ClientIP()); ip != "" {
			upd.SetPublicHost(ip)
		}
	}
	g, err := upd.Save(ctx)
	if err != nil {
		failure(c, 500, "save_failed")
		return
	}
	before := g.ConfigRevision
	if g, err = s.ensureStatsPorts(ctx, g); err != nil {
		failure(c, 500, "port_allocation_failed")
		return
	}
	if g.ConfigRevision != before {
		s.waiters.notify(g.ID)
	}
	c.JSON(200, gin.H{"config_revision": g.ConfigRevision})
}

// agentActions 长轮询待执行动作；过期的动作标记为已过期，不再下发。
func (s *Server) agentActions(c *gin.Context) {
	g, ctx := gateFrom(c), c.Request.Context()
	timeout := time.NewTimer(25 * time.Second)
	defer timeout.Stop()
	for {
		wait := s.actionWaiters.wait(g.ID)
		_, _ = s.Store.Ent.GateAction.Update().Where(gateaction.GateIDEQ(g.ID), gateaction.StatusEQ(gateaction.StatusPending), gateaction.ExpiresAtLT(time.Now())).SetStatus(gateaction.StatusExpired).Save(ctx)
		items, err := s.Store.Ent.GateAction.Query().Where(gateaction.GateIDEQ(g.ID), gateaction.StatusEQ(gateaction.StatusPending)).Order(ent.Asc(gateaction.FieldID)).Limit(10).All(ctx)
		if err != nil {
			failure(c, 503, "actions_unavailable")
			return
		}
		if len(items) > 0 {
			out := make([]contracts.Action, 0, len(items))
			for _, a := range items {
				if _, err = a.Update().SetStatus(gateaction.StatusRunning).Save(ctx); err != nil {
					failure(c, 500, "save_failed")
					return
				}
				out = append(out, contracts.Action{ID: int64(a.ID), Kind: a.Kind, Payload: a.Payload, ExpiresAt: a.ExpiresAt})
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
	if a.Status == gateaction.StatusDone || a.Status == gateaction.StatusFailed {
		c.JSON(200, gin.H{"ok": true})
		return
	}
	status := gateaction.StatusDone
	if !res.OK {
		status = gateaction.StatusFailed
	}
	if _, err = a.Update().SetStatus(status).SetResult(res.Result).SetError(res.Error).SetFinishedAt(time.Now()).Save(ctx); err != nil {
		failure(c, 500, "save_failed")
		return
	}
	s.actionDone.notify(a.ID)
	c.JSON(200, gin.H{"ok": true})
}

// enqueueAction 下发一次性动作并唤醒 Gate 的动作长轮询。
func (s *Server) enqueueAction(ctx context.Context, gid int, kind string, payload map[string]any, ttl time.Duration) (*ent.GateAction, error) {
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
	if kernel != "xray" && kernel != "sing-box" {
		failure(c, 404, "kernel_not_found")
		return
	}
	path, sum, err := s.kernelCache().Get(kernel, c.Param("version"), g.Arch)
	if err != nil {
		failure(c, 404, "kernel_not_found")
		return
	}
	c.Header("X-Kernel-SHA256", sum)
	c.File(path)
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

// agentRelease 把中心自身程序提供给同架构的 Linux Gate 自更新；版本不符或平台不同时不提供。
func (s *Server) agentRelease(c *gin.Context) {
	g := gateFrom(c)
	if c.Param("version") != buildinfo.Version || runtime.GOOS != "linux" || runtime.GOARCH != g.Arch {
		failure(c, 404, "release_unavailable")
		return
	}
	exe, sum, err := selfSHA256()
	if err != nil {
		failure(c, 503, "release_unavailable")
		return
	}
	c.Header("X-Release-SHA256", sum)
	c.File(exe)
}
