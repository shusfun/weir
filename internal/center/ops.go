package center

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/audit"
	"github.com/shusfun/weir/internal/store/ent/predicate"
	"github.com/shusfun/weir/packages/contracts"
)

func (s *Server) opsRoutes(admin *gin.RouterGroup) {
	admin.GET("/audit", s.listAudit)
	admin.GET("/ops/records", s.listOpsRecords)
	admin.POST("/gates/:id/host/repair", s.repairGateHost)
	admin.POST("/gates/:id/kernels/:kernel/diagnostics", s.gateKernelAction(contracts.ActionDiagnostics))
	admin.POST("/gates/:id/kernels/:kernel/restart", s.gateKernelAction(contracts.ActionCoreRestart))
}

// listAudit 分页查询操作审计；q 匹配操作名或目标，result 按结果筛选。审计只记录动作与目标，不含任何凭据。
func (s *Server) listAudit(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}
	q := s.Store.Ent.Audit.Query()
	if scope := c.Query("scope"); scope != "" {
		if scope != "accounts" {
			failure(c, 400, "invalid_query")
			return
		}
		q.Where(audit.Or(audit.ActionHasPrefix("auth."), audit.ActionHasPrefix("account."), audit.ActionIn("security.access-code-guessing", "security.disposable-email", "security.email-send-limit")))
	}
	if v := strings.TrimSpace(c.Query("q")); v != "" {
		if len(v) > 100 {
			failure(c, 400, "invalid_query")
			return
		}
		// 页面显示中文操作名，按中文搜也要能找到。
		cond := []predicate.Audit{audit.ActionContains(v), audit.TargetContains(v)}
		if codes := auditActionsMatching(v); len(codes) > 0 {
			cond = append(cond, audit.ActionIn(codes...))
		}
		q = q.Where(audit.Or(cond...))
	}
	if v := c.Query("result"); v != "" {
		// 结果可能带附加信息（如「ok chains=0」）：按第一个词筛选。
		q = q.Where(audit.Or(audit.ResultEQ(v), audit.ResultHasPrefix(v+" ")))
	}
	// id：只取这一条（对象链接打开某条审计时用）
	if v, err := strconv.Atoi(c.Query("id")); err == nil && v > 0 {
		q = q.Where(audit.IDEQ(v))
	}
	ctx := c.Request.Context()
	total, err := q.Clone().Count(ctx)
	if err != nil {
		failInternal(c, 503, "query_failed", err)
		return
	}
	items, err := q.Order(ent.Desc(audit.FieldID)).Offset((page - 1) * size).Limit(size).All(ctx)
	if err != nil {
		failInternal(c, 503, "query_failed", err)
		return
	}
	actors := map[int]string{}
	out := make([]gin.H, 0, len(items))
	for _, a := range items {
		trace, _ := a.Detail["trace_id"].(string)
		if !validTraceHex(trace, 32) {
			trace = ""
		}
		out = append(out, gin.H{"action_label": auditActionLabel(a.Action), "actor_name": s.auditActorName(ctx, a.Actor, actors), "target_name": s.auditTargetName(ctx, a.Target), "id": a.ID, "actor": a.Actor, "action": a.Action, "target": a.Target, "result": a.Result, "ip": a.IP, "created_at": a.CreatedAt, "trace_id": trace})
	}
	c.JSON(200, gin.H{"items": out, "total": total, "page": page, "page_size": size})
}

func hostRepairAllowed(key, value string) bool {
	switch key {
	case "net.ipv4.tcp_congestion_control":
		return value == "bbr" || value == "cubic"
	case "net.core.default_qdisc":
		return value == "fq" || value == "fq_codel"
	case "net.core.rmem_max", "net.core.wmem_max":
		return value == "16777216" || value == "8388608"
	case "dns.resolvers":
		return value == "public"
	case "dns.recheck":
		return value == "read"
	default:
		return false
	}
}

func (s *Server) applyHostRepair(ctx context.Context, gateID int, key, value string) (string, error) {
	if !hostRepairAllowed(key, value) {
		return "", fmt.Errorf("不允许修改 %s", key)
	}
	g, err := s.Store.Ent.Gate.Get(ctx, gateID)
	if err != nil {
		return "", err
	}
	res, err := s.gateProbe(ctx, g, contracts.ActionHostRepair, map[string]any{"key": key, "value": value}, 20*time.Second)
	if err != nil {
		return "", err
	}
	prev, _ := res["previous"].(string)
	return prev, nil
}

func (s *Server) repairGateHost(c *gin.Context) {
	g, ok := s.gateParam(c)
	if !ok {
		return
	}
	var req contracts.HostRepairTask
	if !bind(c, &req) {
		return
	}
	if !hostRepairAllowed(req.Key, req.Value) {
		failure(c, 400, "invalid_request")
		return
	}
	d := &Draft{
		ID: randomToken(12), Kind: "actions", Source: "analysis", Status: "passed",
		Explain: "管理员请求修复 " + g.Name + " 的 " + req.Key, CreatedAt: time.Now().UTC(),
		OpsStatus: "pending", GateID: g.ID, TraceID: c.GetString("request_id"),
		// 参数和模型提交的一样按 JSON 数字（float64）存：planDraft 只认这种编号
		Steps: []DraftStep{{Action: "host.repair", Title: "宿主修复", Args: map[string]any{"gate_id": float64(g.ID), "key": req.Key, "value": req.Value}}},
	}
	if err := s.planDraft(c.Request.Context(), d, false); err != nil {
		failDetail(c, 422, "draft_invalid", err.Error(), nil)
		return
	}
	if err := s.saveDraft(c.Request.Context(), d); err != nil {
		failInternal(c, 503, "save_failed", err)
		return
	}
	c.JSON(202, gin.H{"draft_id": d.ID, "ops_status": d.OpsStatus})
}

// gateKernelAction 让 VPS 执行白名单动作：读取内核诊断（分类计数与遮盖 IP 后的最近日志）或重启内核。
func (s *Server) gateKernelAction(kind string) gin.HandlerFunc {
	return func(c *gin.Context) {
		g, ok := s.gateParam(c)
		if !ok {
			return
		}
		kernel := c.Param("kernel")
		if kernel != "xray" && kernel != "sing-box" {
			failure(c, 404, "kernel_not_found")
			return
		}
		if kind == contracts.ActionCoreRestart && dryRun(c, dryResult{Preview: []string{"重启 " + g.Name + " 上的 " + kernel + "：这个内核上的节点会断开几秒，客户端自动重连"}}) {
			return
		}
		res, err := s.gateProbe(c.Request.Context(), g, kind, map[string]any{"kernel": kernel}, 30*time.Second)
		if err != nil {
			failErr(c, 422, err)
			return
		}
		if kind == contracts.ActionCoreRestart {
			s.audit(c, "gate.kernel.restart", "gate:"+itoa(g.ID)+":"+kernel, "ok")
		}
		if kind == contracts.ActionDiagnostics {
			text, err := kernelDiagnosticText(res)
			if err != nil {
				failInternal(c, 502, "kernel_diagnostic_invalid", err)
				return
			}
			// An assistant reading diagnostics is already investigating. Feeding
			// its own read back into the classifier would create an analysis loop.
			if c.Request.Context().Value(modelReadKey{}) != true && text != "" {
				_, err = s.enqueueObservation(c.Request.Context(), observationInput{Point: "gate.kernel", Resource: "gate:" + itoa(g.ID) + ":" + kernel, Text: text, Origin: "center.gateKernelAction"})
				s.tasks.note("observation-producer", err)
			}
		}
		c.JSON(200, gin.H{"result": res})
	}
}

func kernelDiagnosticText(result map[string]any) (string, error) {
	if result["tail"] == nil {
		return "", nil
	}
	raw, err := json.Marshal(result["tail"])
	if err != nil {
		return "", errors.New("invalid kernel diagnostic log")
	}
	var lines []string
	if err = contracts.DecodeStrict(raw, &lines, 64<<10); err != nil || len(lines) > 50 {
		return "", errors.New("invalid kernel diagnostic log")
	}
	for _, line := range lines {
		if len(line) > 8192 {
			return "", errors.New("kernel diagnostic line exceeds limit")
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n")), nil
}
