package center

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"runtime/debug"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shusfun/weir/internal/buildinfo"
	"github.com/shusfun/weir/internal/diagnostic"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/audit"
	"github.com/shusfun/weir/internal/store/ent/resource"
	"github.com/shusfun/weir/packages/contracts"
)

const diagnosticTTL = 10 * time.Minute

type diagnosticPreview struct {
	Actor     int               `json:"actor"`
	ExpiresAt time.Time         `json:"expires_at"`
	Bundle    diagnostic.Bundle `json:"bundle"`
}

func (s *Server) diagnosticRoutes(admin *gin.RouterGroup) {
	admin.POST("/workers/diagnostics/preview", s.previewDiagnostic)
	admin.POST("/workers/diagnostics/:id/export", s.exportDiagnostic)
	s.observationAnalysisRoutes(admin)
}

func (s *Server) previewDiagnostic(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var req struct {
		Items []diagnostic.Selection `json:"items"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8192)
	if err := decodeDiagnostic(c, &req); err != nil || len(req.Items) < 1 || len(req.Items) > 20 {
		failure(c, 400, "diagnostic_selection_invalid")
		return
	}
	seen := map[string]bool{}
	for _, item := range req.Items {
		if !validDiagnosticSelection(item) || seen[item.Key()] {
			failure(c, 400, "diagnostic_selection_invalid")
			return
		}
		seen[item.Key()] = true
	}
	ctx := c.Request.Context()
	n, err := s.Store.KV.Incr(ctx, "diagnostic-rate:"+strconv.Itoa(c.GetInt("user_id")), time.Minute)
	if err != nil {
		failInternal(c, 503, "diagnostic_unavailable", err)
		return
	}
	if n > 5 {
		failure(c, 429, "diagnostic_rate_limited")
		return
	}
	b := diagnostic.Bundle{FormatVersion: diagnostic.FormatVersion, ID: randomHex(24), TraceID: c.GetString("trace_id"), GeneratedAt: time.Now().UTC(), Environment: diagnosticEnvironment(), Entries: []diagnostic.Entry{}, Limitations: []string{
		"只含所选资源的已取得证据；未查询第三方内部日志，也未自动拉取离线 Gate 明细。",
		"配置快照只含明确允许的非机密字段；不导出节点原文、订阅正文、令牌、私钥或 dsh 会话。",
		"外部文本已脱敏且作为不可信数据单独存放，HTML 不执行。未实际复现的故障不会标为已复现。",
	}}
	for _, item := range req.Items {
		entry, e := s.diagnosticEntry(ctx, item)
		if e != nil {
			if ent.IsNotFound(e) || errors.Is(e, sql.ErrNoRows) {
				failure(c, 404, "diagnostic_item_not_found")
			} else {
				failInternal(c, 503, "diagnostic_unavailable", e)
			}
			return
		}
		b.Entries = append(b.Entries, entry)
	}
	preview := diagnosticPreview{Actor: c.GetInt("user_id"), ExpiresAt: b.GeneratedAt.Add(diagnosticTTL), Bundle: b}
	raw, err := json.Marshal(preview)
	if err != nil || len(raw) > diagnostic.MaxSnapshotBytes/2 {
		failure(c, 413, "diagnostic_too_large")
		return
	}
	sealed, err := s.Crypto.Encrypt(string(raw))
	if err == nil {
		err = s.Store.KV.Set(ctx, "diagnostic-preview:"+b.ID, sealed, diagnosticTTL)
	}
	if err != nil {
		failInternal(c, 503, "diagnostic_unavailable", err)
		return
	}
	c.Set("observation_id", b.ID)
	c.JSON(200, gin.H{"id": b.ID, "expires_at": preview.ExpiresAt, "bundle": b, "summary": b.Summary()})
}

func validDiagnosticSelection(item diagnostic.Selection) bool {
	switch item.Kind {
	case "protection-action":
		return validTraceHex(item.ID, 24)
	case "subscription-run":
		return len(item.ID) == 32 && validTraceHex(item.ID, 32)
	case "task", "laya-operation":
		return len(item.ID) == 48 && validTraceHex(item.ID, 48)
	case "analysis", "observation":
		return len(item.ID) == 64 && validTraceHex(item.ID, 64)
	case "gate", "event", "source", "audit":
		n, err := strconv.Atoi(item.ID)
		return err == nil && n > 0 && strconv.Itoa(n) == item.ID
	case "profile", "center", "account-policy", "protection-policy", "worker-policy", "laya", "dsh":
		return item.ID == "current"
	}
	return false
}

func decodeDiagnostic(c *gin.Context, out any) error {
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return err
	}
	return contracts.DecodeStrict(raw, out, 8192)
}

func diagnosticEnvironment() map[string]any {
	revision, dirty := buildinfo.Revision, "unknown"
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, value := range info.Settings {
			switch value.Key {
			case "vcs.revision":
				if revision == "unrecorded" || revision == "" {
					revision = value.Value
				}
			case "vcs.modified":
				dirty = value.Value
			}
		}
	}
	return map[string]any{"weir_version": buildinfo.Version, "commit": revision, "workspace_modified": dirty, "os": runtime.GOOS, "arch": runtime.GOARCH, "go": runtime.Version()}
}

func regression(command, scope string) []diagnostic.Reproduction {
	return []diagnostic.Reproduction{{Command: command, Scope: scope, Status: "not_reproduced"}}
}

func (s *Server) diagnosticEntry(ctx context.Context, item diagnostic.Selection) (diagnostic.Entry, error) {
	e := diagnostic.Entry{Selection: item, Evidence: map[string]any{}, ExternalData: map[string]string{}, Limitations: []string{}, Reproduction: []diagnostic.Reproduction{}}
	id, _ := strconv.Atoi(item.ID)
	switch item.Kind {
	case "dsh":
		health, err := s.dshHealth(ctx, time.Now().UTC())
		if err != nil {
			return e, err
		}
		e.Title, e.Expected, e.Actual = "dsh 运行状态", "官方运行时达到期望状态，后台分析遵守兼容范围", "observed"
		e.Evidence = map[string]any{"status": health, "source": diagnosticSources["center.dshHealth"]}
		if component, ok := health["health"].(componentHealth); ok {
			health["health"] = diagnosticComponent(component, "dsh", e.ExternalData)
		}
		e.Limitations = append(e.Limitations, "只含管理进程的有时效状态，不含启动令牌、运行日志、会话或任务原文。运行中不等于后台分析已执行。")
		e.Reproduction = regression("go test ./internal/center -run '^TestSystemStatus' -count=1", "系统状态的脱敏与时效合成回归，不启动 dsh")
	case "laya":
		e.Title, e.Expected, e.Actual = "Laya 模型状态", "运行时模型可访问，摘要符合锁定档案", "observed"
		e.Evidence = map[string]any{"status": s.centerLayaHealth(ctx, time.Now().UTC()), "source": diagnosticSources["center.probeCenterLaya"]}
		status := e.Evidence["status"].(gin.H)
		e.ExternalData["configured_model"] = diagnostic.Text(jsonText(status["configured_model"]))
		delete(status, "configured_model")
		if health, ok := status["health"].(componentHealth); ok {
			status["health"] = diagnosticComponent(health, "center", e.ExternalData)
		}
		configured, _, err := s.readLayaSettings(ctx)
		if err != nil {
			return e, err
		}
		if configured.Mode == "gate" && configured.GateID > 0 {
			observed, err := s.workerResources(ctx, configured.GateID)
			if err != nil {
				return e, err
			}
			if observed != nil {
				if model, ok := observed.Resources.Components["laya"]; ok {
					e.Evidence["gate_model"] = diagnosticComponent(observedComponent(model, time.Now().UTC()), "gate", e.ExternalData)
				}
			}
		}
		e.Limitations = append(e.Limitations, "模型可访问只证明官方模型接口返回摘要；这次健康采样未执行推理。缺失或过期采样不证明模型正常。")
		e.Reproduction = regression("go test ./internal/center -run '^TestSystemStatus' -count=1", "模型状态、摘要与时效合成回归，不宣称复现真实推理故障")
	case "laya-operation":
		row, err := s.Store.Ent.Resource.Query().Where(resource.KindEQ(layaOperationKind), resource.KeyEQ(s.layaHostKey(item.ID))).Only(ctx)
		if err != nil {
			return e, err
		}
		var op layaHostOperation
		if err := decodeStrict(row.Data, &op); err != nil {
			return e, err
		}
		if op.Status == "running" && time.Now().After(op.Deadline) {
			op.Status, op.Code = "unknown", "laya_operation_interrupted"
		}
		e.ExternalData["approved_version"] = diagnostic.Text(op.Version)
		op.Version = ""
		e.Title, e.Expected, e.Actual = "中心 Laya 托管活动", "只执行 root 批准的官方部署，模型摘要及推理分别检查", op.Status
		e.Evidence = map[string]any{"operation": op, "source": diagnosticSources["center.runLayaHosting"]}
		locations := map[string]diagnosticSource{}
		for _, stage := range op.Stages {
			if location, ok := diagnosticSources[stage.Origin]; ok {
				locations[stage.Origin] = location
			}
		}
		e.Evidence["stage_sources"] = locations
		e.Limitations = append(e.Limitations, "只含中心已保存环节。Docker 守护进程、镜像仓库及模型运行时内部不可观测；取消或中断后须核对容器，不能断言未产生副作用。")
		e.Reproduction = regression("go test ./internal/center -run '^TestLayaHosting' -count=1", "批准、身份、幂等及取消的合成回归，不重新执行原部署")
	case "protection-action":
		row, err := s.Store.Ent.Resource.Query().Where(resource.KindIn(protectionActionKind, "protection-permanent"), resource.KeyEQ(item.ID)).Only(ctx)
		if err != nil {
			return e, err
		}
		var action protectionAction
		if err = decodeStrict(row.Data, &action); err != nil {
			return e, err
		}
		e.Title, e.Expected, e.Actual = "安全处置", "宿主约束临时收紧，支持到期与撤销", "recorded"
		if action.RevokedAt != nil {
			e.Actual = "revoked"
		} else if !action.Permanent && !action.Until.After(time.Now()) {
			e.Actual = "expired"
		}
		e.Evidence = map[string]any{"id": action.ID, "trace_id": action.TraceID, "event_id": action.EventID, "run_id": action.RunID, "kind": action.Kind, "automatic": action.Automatic, "automatic_origin": action.automaticOrigin(), "permanent": action.Permanent, "policy_revision": action.PolicyRevision, "created_at": action.CreatedAt, "until": action.Until, "revoked_at": action.RevokedAt, "notification": action.Notification, "notification_attempts": action.NotificationAttempts, "source": diagnosticSources["center.tryAutomaticProtection"]}
		// Only registered policy fields and source IPs are copied. The model's
		// reason is external text and must not share the trusted evidence object.
		if action.Kind == "block" {
			e.Evidence["ip"] = action.IP
		}
		if action.Kind == "tighten" {
			e.Evidence["field"] = action.Field
			e.Evidence["value"] = action.Value
		}
		e.ExternalData["reason"] = diagnostic.Text(action.Reason)
		rows, err := s.Store.Ent.Audit.Query().Where(audit.TargetEQ(item.ID), audit.ActionIn("defense.auto.apply", "defense.auto.revoke", "defense.promote", "defense.tighten.confirm")).Order(ent.Asc(audit.FieldID)).Limit(100).All(ctx)
		if err != nil {
			return e, err
		}
		timeline := []map[string]any{}
		for _, row := range rows {
			timeline = append(timeline, map[string]any{"at": row.CreatedAt, "action": row.Action, "result": row.Result, "trace_id": row.Detail["trace_id"], "actor": row.Actor})
		}
		e.Evidence["timeline"] = timeline
		e.Limitations = append(e.Limitations, "处置记录不等于当前所有规则的最终结果；总开关、保护名单和后续策略可能暂停其效力。仅含已取得审计记录，不重放原事件。")
		e.Reproduction = regression("go test ./internal/center -run '^TestAutomaticProtection' -count=1", "绑定任务、自动护栏、撤销及幂等的合成回归，不重放真实封禁")
	case "protection-policy":
		view, _, err := s.protectionPolicy(ctx)
		if err != nil {
			return e, err
		}
		e.Title, e.Expected, e.Actual = "自动安全护栏", "保护名单、次数及最长时限由宿主执行", "configured"
		e.Evidence = map[string]any{"revision": view.Revision, "document": view.Document, "effective": view.Effective, "sources": view.Sources, "source": diagnosticSources["center.protectionPolicy"]}
		e.Reproduction = regression("go test ./internal/protection ./internal/center -run '^Test(AutomaticProtection|Protection)' -count=1", "保护名单、临时收紧与管理员审批的合成回归")
	case "worker-policy":
		view, _, err := s.workerPolicy(ctx)
		if err != nil {
			return e, err
		}
		e.Title, e.Expected, e.Actual = "VPS 后台任务预算", "并发与正文带宽有界，预算变更需确认", "configured"
		e.Evidence = map[string]any{"revision": view.Revision, "document": view.Document, "effective": view.Effective, "sources": view.Sources, "source": diagnosticSources["center.workerPolicy"]}
		e.Reproduction = regression("go test ./internal/center ./internal/taskqueue -run '^TestWorker' -count=1", "任务调度、预算和资源筛选的合成回归，不代表远端 CPU 限额已检测")
	case "subscription-run":
		run, err := s.loadSubscriptionRun(ctx, item.ID)
		if err != nil {
			return e, err
		}
		e.Title, e.Expected, e.Actual = "订阅处理时间线", "按成员顺序解析并组合；失败明确记录", run.Outcome
		e.Evidence = toMap(run)
		stages := []map[string]any{}
		for _, stage := range run.Stages {
			v := toMap(stage)
			v["source"] = diagnosticSources[stage.Origin]
			if stage.ErrorCode == "source_content_invalid" || stage.ErrorCode == "source_no_supported_nodes" {
				v["source"] = diagnosticSources["subgroup.parseSourceContent"]
			}
			stages = append(stages, v)
		}
		e.Evidence["stages"] = stages
		e.Limitations = append(e.Limitations, "不含订阅正文或节点凭据；第三方内部不可观测，阶段超过上限时 complete=false。", "响应阶段仅证明服务端写入正文，不证明客户端接收完成；异步内核检查通过同一 trace 的独立检查记录关联。")
		e.Reproduction = regression("go test ./internal/center -run '^TestSubscription(Nested|RawIdentity|SourceUpdate|Timeline)' -count=1", "缓存、嵌套、并发更新及时间线的合成回归，不代表上游瞬态故障已复现")
	case "task":
		return s.diagnosticTask(ctx, e)
	case "observation":
		row, err := s.Store.Ent.Resource.Query().Where(resource.KindIn(observationKind, observationPendingKind), resource.KeyEQ(item.ID)).Only(ctx)
		if err != nil {
			return e, err
		}
		var r observationRecord
		if err = decodeStrict(row.Data, &r); err != nil {
			return e, err
		}
		r = observedObservation(r, time.Now())
		e.Title, e.Expected, e.Actual = "Laya 观察记录", "按生效配置判断，必要时交给登记的 skill；修改走草稿", "not_checked"
		if r.Verdict != nil {
			e.Actual = r.Verdict.Outcome
		}
		e.Evidence = map[string]any{"observation_id": r.ID, "point": r.Point, "resource": r.Resource, "trace_id": r.TraceID, "created_at": r.CreatedAt, "finished_at": r.FinishedAt, "profile_version": r.ProfileVersion, "timezone": r.Timezone, "day": r.Day, "dsh_status": r.DSHStatus, "dsh_code": r.DSHCode}
		if r.Verdict != nil {
			e.Evidence["answer"] = map[string]any{"category": r.Verdict.Category, "confidence": r.Verdict.Confidence, "threshold": r.Verdict.Threshold, "code": r.Verdict.Code, "attempts": r.Verdict.Attempts, "execution": r.Verdict.Execution, "gate_id": r.Verdict.GateID}
			e.ExternalData["question"] = diagnostic.Text(r.Verdict.Question)
			e.ExternalData["model_metadata"] = diagnostic.Text(jsonText(map[string]string{"configured_model": r.Verdict.Model, "model_version": r.Verdict.ModelVersion, "runtime_version": r.Verdict.RuntimeVersion}))
		}
		if location, ok := diagnosticSources[r.Origin]; ok {
			e.Evidence["source_location"] = location
		} else {
			e.Limitations = append(e.Limitations, "事件来源位置未核实")
		}
		e.ExternalData["diagnostic_text"] = diagnostic.Text(r.ExternalData.Text)
		e.ExternalData["conclusion"] = diagnostic.Text(r.ExternalData.Conclusion)
		e.ExternalData["tool_calls"] = diagnostic.Text(jsonText(r.ExternalData.ToolCalls))
		e.Limitations = append(e.Limitations, "内容垃圾判断只提供审核建议，不调用 dsh、不自动永久删除；模型内部不可观测。")
		e.Reproduction = regression("go test ./internal/center -run '^TestObservation' -count=1", "模拟模型的三类判定、每日汇总与人工分析回归，不重放外部瞬态故障")
	case "analysis":
		row, err := s.Store.Ent.Resource.Query().Where(resource.KindEQ(observationAnalysisKind), resource.KeyEQ(item.ID)).Only(ctx)
		if err != nil {
			return e, err
		}
		var a observationAnalysis
		if err = decodeObservationAnalysis(row, &a); err != nil {
			return e, err
		}
		a = analysisObservedState(a, time.Now())
		e.Title, e.Expected, e.Actual = "人工分析活动", "分析完成并记录证据与审批边界", a.Status
		e.Evidence = map[string]any{"analysis_id": a.ID, "trace_id": a.TraceID, "snapshot_id": a.SnapshotID, "snapshot_trace_id": a.SnapshotTraceID, "actor": a.Actor, "items": a.Items, "profile_version": a.ProfileVersion, "created_at": a.CreatedAt, "finished_at": a.FinishedAt, "deadline": a.Deadline, "code": a.Code}
		e.ExternalData["dsh_version"] = diagnostic.Text(a.DSHVersion)
		e.ExternalData["conclusion"] = diagnostic.Text(a.ExternalData.Conclusion)
		e.ExternalData["tool_calls"] = diagnostic.Text(jsonText(a.ToolCalls))
		e.Limitations = append(e.Limitations, "只有宿主取得的分析结果；模型与第三方内部不可观测。分析成功不代表其建议已经生效。")
		e.Reproduction = regression("go test ./internal/center -run '^TestObservationAnalysis' -count=1", "模拟 dsh 的脱敏、幂等、取消与活动回归，不重放模型输出")
	case "gate":
		g, err := s.Store.Ent.Gate.Get(ctx, id)
		if err != nil {
			return e, err
		}
		e.Title, e.Expected, e.Actual = "VPS 状态", "Gate 在线并应用期望配置", "unknown"
		if g.LastSeenAt != nil {
			e.Actual = "offline"
			if gateOnline(g) {
				e.Actual = "online"
			}
		}
		e.Evidence = map[string]any{"gate_id": g.ID, "last_seen_at": g.LastSeenAt, "config_revision": g.ConfigRevision, "applied_revision": g.AppliedRevision, "failed_revision": g.FailedRevision, "environment": map[string]string{"os": "unknown", "arch": diagnostic.Text(g.Arch)}}
		e.ExternalData = map[string]string{"name": diagnostic.Text(g.Name), "hostname": diagnostic.Text(g.Hostname), "last_error": diagnostic.Text(g.LastError), "reported_version": diagnostic.Text(g.Version), "kernels_running": diagnostic.Text(jsonText(g.KernelsRunning))}
		caps, err := s.Store.Tasks.Capabilities(ctx, id)
		if err == nil {
			e.Evidence["protocol"] = caps.Protocol
			// Component identifiers and model versions are reported data too.
			e.ExternalData["reported_components"] = diagnostic.Text(jsonText(caps.Components))
		} else if !errors.Is(err, sql.ErrNoRows) {
			return e, err
		} else {
			e.Limitations = append(e.Limitations, "能力文档未知。")
		}
		e.Limitations = append(e.Limitations, "心跳是最后一次报告，不能据此证明当前内核健康；Gate 系统版本未上报。")
		e.Reproduction = regression("go test ./internal/center -run '^TestHeartbeatV2' -count=1", "合成心跳回归；不会连接或重启这台 VPS")
	case "event":
		v, err := s.loadEvent(ctx, id)
		if err != nil {
			return e, err
		}
		e.Title, e.Expected, e.Actual = "安全事件", "记录判断并按权限处理", diagnostic.Text(v.Status)
		e.Evidence = map[string]any{"event_id": id, "trace_id": v.TraceID, "created_at": v.CreatedAt, "failed_logins_10m": v.Failures, "source_ip": v.SourceIP}
		if v.Laya != nil {
			e.Evidence["laya"] = map[string]any{"outcome": v.Laya.Outcome, "confidence": v.Laya.Confidence, "threshold": v.Laya.Threshold, "profile_version": v.Laya.ProfileVersion, "attempts": v.Laya.Attempts}
			e.ExternalData["question"] = diagnostic.Text(v.Laya.Question)
		}
		e.ExternalData["event_kind"], e.ExternalData["scope"] = diagnostic.Text(v.Kind), diagnostic.Text(v.Scope)
		e.ExternalData["laya_error"], e.ExternalData["analysis"] = diagnostic.Text(v.LayaError), diagnostic.Text(v.Analysis)
		e.Limitations = append(e.Limitations, "包含保存的判断，不生成模型解释；未获取此事件全部第三方内部记录。")
		e.Reproduction = regression("go test ./internal/center -run '^TestDefenseLayaThenDraftThenConfirmedBlock$' -count=1", "使用模拟模型的审批流程回归，不代表重放真实攻击")
	case "source":
		v, err := s.Store.Ent.SubSource.Get(ctx, id)
		if err != nil {
			return e, err
		}
		e.Title, e.Expected, e.Actual = "订阅源状态", "成功拉取并解析订阅", "not_checked"
		if v.FetchedAt != nil {
			e.Actual = "last_fetch_recorded"
		}
		if v.LastError != "" {
			e.Actual = "failed"
		}
		e.Evidence = map[string]any{"source_id": id, "fetched_at": v.FetchedAt, "node_count": v.NodeCount, "known_nodes": v.KnownNodes, "configuration": map[string]any{"enabled": v.Enabled, "node_cache": v.NodeCache, "plus_as_space": v.PlusAsSpace, "exclude_traffic": v.ExcludeTraffic}}
		e.ExternalData = map[string]string{"name": diagnostic.Text(v.Name), "upstream_error": diagnostic.Text(v.LastError)}
		if v.LastError == "source_content_invalid" || v.LastError == "source_no_supported_nodes" {
			e.Evidence["error_code"] = v.LastError
			e.Evidence["source"] = diagnosticSources["subgroup.parseSourceContent"]
		}
		e.Limitations = append(e.Limitations, "不包含订阅地址、正文或节点凭据；缺少原始输入时不能重放此上游故障。")
		e.Reproduction = regression("go test ./internal/subgroup -count=1", "脱敏合成订阅夹具的处理链回归，不访问真实订阅源")
	case "audit":
		v, err := s.Store.Ent.Audit.Get(ctx, id)
		if err != nil {
			return e, err
		}
		e.Title, e.Expected, e.Actual = "审计记录", "按权限处理并记录结果", diagnostic.Text(v.Result)
		e.Evidence = map[string]any{"audit_id": id, "actor": v.Actor, "created_at": v.CreatedAt, "trace_id": v.Detail["trace_id"], "request_id": v.Detail["request_id"]}
		e.ExternalData = map[string]string{"action": diagnostic.Text(v.Action), "target": diagnostic.Text(v.Target)}
		e.Limitations = append(e.Limitations, "未导出任意审计 detail；只保留关联 ID，避免业务原文泄露。")
		e.Reproduction = regression("go test ./internal/center -run '^TestDiagnosticPreviewExport' -count=1", "脱敏与权限回归，不能重放原业务操作")
		if origin := accountAuditOrigin(v.Action); origin != "" {
			e.Evidence["source"] = diagnosticSources[origin]
			e.Reproduction = regression("go test ./internal/center -run '^Test(AccountPlan|AccountGrant|AccessCode)' -count=1", "套餐、并发用码、事务回滚和授权恢复的合成夹具回归，不能重新消费原码")
			if v.Action == "account.grant.sync" && v.Result == "pending" {
				e.Evidence["error_code"] = "grant_sync_failed"
			}
		}
	case "profile":
		v, err := s.aiProfile(ctx)
		if err != nil {
			return e, err
		}
		e.Title, e.Expected, e.Actual = "AI 生效配置", "已确认版本及观察点配置可核对", "snapshot_recorded"
		observations := map[string]any{}
		for name, cfg := range v.Effective.Observations {
			observations[name] = map[string]any{"enabled": cfg.Enabled, "confidence": cfg.Confidence, "source": v.Sources["observations/"+name]}
			e.ExternalData[name+".question"] = diagnostic.Text(cfg.Question)
			e.ExternalData[name+".skill"] = diagnostic.Text(cfg.Skill)
		}
		l, d := s.loadLaya(ctx), s.dshSettings(ctx)
		e.Evidence = map[string]any{"revision": v.Revision, "observations": observations, "laya": map[string]any{"enabled": l.Enabled, "mode": l.Mode, "gate_id": l.GateID, "observed_model_version": "unknown"}, "dsh": map[string]any{"enabled": d.Enabled, "auto_analyze": d.AutoAnalyze, "observed_version": "unknown"}}
		e.ExternalData["configured_model"] = diagnostic.Text(l.Model)
		e.ExternalData["configured_dsh_version"] = diagnostic.Text(d.Version)
		e.Limitations = append(e.Limitations, "仅导出判断设置；指令、skills 原文、插件文件和 Key 不导出。期望版本不代表实际运行版本。")
		e.Reproduction = regression("go test ./internal/center -run '^TestAIProfile' -count=1", "配置解析、预演和审批回归")
	case "account-policy":
		v, err := s.accountPolicy(ctx)
		if err != nil {
			return e, err
		}
		e.Title, e.Expected, e.Actual = "账户验证码与发信策略", "注册与发码执行生效策略，修改经管理员确认", "configured"
		e.Evidence = map[string]any{"revision": v.Revision, "sources": v.Sources, "document": v.Document, "effective": v.Effective, "source": diagnosticSources["center.accountPolicy"]}
		e.Limitations = append(e.Limitations, "仅包含无密钥的数值策略；不包含账户标识、验证码和计数键。历史影响未检测。")
		e.Reproduction = regression("go test ./internal/center -run '^TestAccountPolicy' -count=1", "账户策略边界、审批和版本恢复回归")
	case "center":
		e.Title, e.Expected, e.Actual = "中心运行环境", "中心运行且环境信息可核对", "request_served"
		e.Evidence = diagnosticEnvironment()
		e.Evidence["store_driver"] = s.Config.Store.Driver
		s.resources.RLock()
		e.Evidence["host"] = snapshotHost(s.resources.host, time.Now().UTC())
		s.resources.RUnlock()
		e.Limitations = append(e.Limitations, "主机资源只含最近两分钟的独立采样；缺失显示未知。服务能够响应不证明所有依赖健康。")
		e.Reproduction = regression("go test ./internal/center -run '^TestDiagnosticPreviewExport' -count=1", "本地诊断导出闭环回归")
	}
	return e, nil
}

func accountAuditOrigin(action string) string {
	switch action {
	case "account.plan":
		return "center.putAccountPlan"
	case "account.code.issue":
		return "center.issueAccessCodes"
	case "account.code.revoke":
		return "center.revokeAccessCode"
	case "account.code.redeem":
		return "center.redeemAccessCode"
	case "account.grant.sync":
		return "center.reconcileAccountGrant"
	case "security.access-code-guessing":
		return "center.checkAccessCode"
	case "auth.register":
		return "center.register"
	default:
		return ""
	}
}

func diagnosticVersions(in map[string]string) map[string]string {
	out := map[string]string{}
	for _, name := range []string{"xray", "sing-box", "mihomo"} {
		if v, ok := in[name]; ok {
			out[name] = diagnostic.Text(v)
		}
	}
	return out
}

func (s *Server) diagnosticTask(ctx context.Context, e diagnostic.Entry) (diagnostic.Entry, error) {
	r, err := s.Store.Tasks.Get(ctx, e.Selection.ID)
	if err != nil {
		return e, err
	}
	e.Title, e.Expected, e.Actual = "Gate 任务", "任务完成，结果通过中心校验", r.Status
	e.Evidence = map[string]any{"task_id": r.Task.ID, "trace_id": r.Task.TraceID, "kind": r.Task.Kind, "version": r.Task.Version, "gate_id": r.GateID, "attempt": r.Task.Attempt, "created_at": r.CreatedAt, "deadline": r.Task.Deadline, "candidates": r.Candidates}
	activity, err := s.Store.Tasks.Activity(ctx, r.Task.ID)
	if err != nil {
		return e, err
	}
	e.Evidence["scheduling"] = activity
	if r.Error != nil {
		e.ExternalData["reported_error"] = diagnostic.Text(jsonText(map[string]string{"code": r.Error.Code, "origin": r.Error.Origin}))
		e.Evidence["error_location_verified"] = false
		if location, ok := diagnosticSources[r.Error.Origin]; ok {
			e.Evidence["error_source_in_center_build"] = location
		}
		e.Limitations = append(e.Limitations, "错误位置由执行端报告，需对照对应版本源码核实。")
	}
	var resultID string
	err = s.Store.DB.QueryRowContext(ctx, `SELECT id FROM worker_tasks WHERE kind=$1 AND trace_id=$2 AND gate_id=$3 AND status='succeeded' ORDER BY created_at DESC,id DESC LIMIT 1`, contracts.TaskTraceRead, r.Task.TraceID, r.GateID).Scan(&resultID)
	if errors.Is(err, sql.ErrNoRows) {
		e.Limitations = append(e.Limitations, "Gate 明细未取得：先在系统状态读取时间线再预览；离线或过期时无法补全。")
	} else if err != nil {
		return e, err
	} else {
		trace, err := s.Store.Tasks.Get(ctx, resultID)
		if err != nil {
			return e, err
		}
		body, err := s.workerResult(trace.Result)
		if err != nil {
			return e, err
		}
		var result contracts.TraceReadResult
		if err = contracts.DecodeStrict([]byte(body), &result, contracts.MaxTaskResult); err != nil {
			return e, err
		}
		stages := []map[string]any{}
		for i, record := range result.Records {
			stage := map[string]any{"task_id": record.TaskID, "attempt": record.Attempt, "started": record.Started, "finished": record.Finished, "duration_ms": record.Finished.Sub(record.Started).Milliseconds(), "outcome": record.Outcome}
			if location, ok := diagnosticSources[record.Origin]; ok {
				stage["source_in_center_build"] = location
			}
			stages = append(stages, stage)
			// Gate strings are external data even when their protocol shape passed.
			e.ExternalData[fmt.Sprintf("stage.%d", i)] = diagnostic.Text(jsonText(map[string]string{"stage": record.Stage, "error_code": record.ErrorCode, "origin": record.Origin}))
		}
		e.Evidence["gate_timeline"] = map[string]any{"collected_at": result.CollectedAt, "complete": result.Complete, "stages": stages, "read_task_id": resultID}
		e.ExternalData["timeline_missing_reason"] = diagnostic.Text(result.MissingReason)
		e.Limitations = append(e.Limitations, "Gate 明细来自此前按需读取的快照；完整标记指该次返回，不代表包含全部历史。")
	}
	if r.GateID > 0 {
		if g, err := s.Store.Ent.Gate.Get(ctx, r.GateID); err == nil {
			e.Evidence["gate_snapshot"] = map[string]any{"version": diagnostic.Text(g.Version), "arch": diagnostic.Text(g.Arch), "config_revision": g.ConfigRevision, "applied_revision": g.AppliedRevision, "kernels": diagnosticVersions(g.KernelsRunning)}
		} else if !ent.IsNotFound(err) {
			return e, err
		}
	}
	e.Limitations = append(e.Limitations, "任务输入与结果正文不导出；不能用合成回归宣称真实网络故障已复现。")
	e.Reproduction = regression("go test ./internal/center -run '^TestWorkerTaskTimelineReadsOnlyFromOriginalGate$' -count=1", "模拟 Gate 的关联时间线回归，不执行所选任务的副作用")
	return e, nil
}

func (s *Server) selectedDiagnostic(c *gin.Context) (diagnostic.Bundle, []string, bool) {
	var empty diagnostic.Bundle
	c.Header("Cache-Control", "no-store")
	id := c.Param("id")
	if len(id) != 48 || !validTraceHex(id, 48) {
		failure(c, 404, "diagnostic_preview_expired")
		return empty, nil, false
	}
	var req struct {
		Keep []string `json:"keep"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8192)
	if err := decodeDiagnostic(c, &req); err != nil || len(req.Keep) < 1 || len(req.Keep) > 20 {
		failure(c, 400, "diagnostic_selection_invalid")
		return empty, nil, false
	}
	ctx := c.Request.Context()
	sealed, ok, err := s.Store.KV.Get(ctx, "diagnostic-preview:"+id)
	if err != nil {
		failInternal(c, 503, "diagnostic_unavailable", err)
		return empty, nil, false
	}
	if !ok {
		failure(c, 404, "diagnostic_preview_expired")
		return empty, nil, false
	}
	raw, err := s.Crypto.Decrypt(sealed)
	var preview diagnosticPreview
	if err == nil {
		err = json.Unmarshal([]byte(raw), &preview)
	}
	if err != nil {
		failInternal(c, 503, "diagnostic_unavailable", err)
		return empty, nil, false
	}
	if preview.Actor != c.GetInt("user_id") || !time.Now().Before(preview.ExpiresAt) {
		failure(c, 404, "diagnostic_preview_expired")
		return empty, nil, false
	}
	keep := map[string]bool{}
	for _, key := range req.Keep {
		if keep[key] {
			failure(c, 400, "diagnostic_selection_invalid")
			return empty, nil, false
		}
		keep[key] = true
	}
	b := preview.Bundle
	b.Entries = nil
	for _, e := range preview.Bundle.Entries {
		if keep[e.Selection.Key()] {
			b.Entries = append(b.Entries, e)
			delete(keep, e.Selection.Key())
		}
	}
	if len(keep) != 0 {
		failure(c, 400, "diagnostic_selection_invalid")
		return empty, nil, false
	}
	return b, req.Keep, true
}

func (s *Server) exportDiagnostic(c *gin.Context) {
	b, keep, ok := s.selectedDiagnostic(c)
	if !ok {
		return
	}
	id, ctx := b.ID, c.Request.Context()
	path, cleanup, err := diagnostic.WriteTemp(b)
	if err != nil {
		failInternal(c, 503, "diagnostic_unavailable", err)
		return
	}
	defer cleanup()
	data, err := json.Marshal(b)
	if err != nil {
		failInternal(c, 503, "diagnostic_unavailable", err)
		return
	}
	sum := sha256.Sum256(data)
	detail := traceDetail(c)
	detail["snapshot_trace_id"], detail["sha256"], detail["items"] = b.TraceID, hex.EncodeToString(sum[:]), keep
	// Fail closed: exporting without a durable audit is not an export.
	_, err = s.Store.Ent.Audit.Create().SetActor(c.GetInt("user_id")).SetAction("diagnostic.export").SetTarget(id).SetResult("prepared").SetIP(c.ClientIP()).SetDetail(detail).Save(ctx)
	if err != nil {
		failInternal(c, 503, "diagnostic_unavailable", err)
		return
	}
	c.Set("observation_id", id)
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Type", "application/zip")
	c.FileAttachment(path, "weir-diagnostic-"+id[:12]+".zip")
}
