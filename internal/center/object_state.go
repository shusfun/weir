package center

import (
	"context"
	"time"

	"github.com/shusfun/weir/internal/store/ent"
)

func gateObjectState(g *ent.Gate) objectState {
	v := objectState{Status: "unknown", Label: "未知", Reason: "VPS 尚未上报心跳", Next: []string{"connect"}}
	if g == nil {
		v.Status, v.Reason, v.Next = "unavailable", "VPS 已删除", []string{}
		return v
	}
	v.ObservedAt = g.LastSeenAt
	switch {
	case g.Status == "revoked":
		v.Status, v.Label, v.Reason, v.Next = "revoked", "已撤销", "设备身份已撤销，不能再获取配置", []string{"delete"}
	case g.LastSeenAt == nil:
		v.Next = []string{"connect", "delete"}
		return v
	case time.Since(*g.LastSeenAt) >= 90*time.Second:
		v.Status, v.Label, v.Reason = "stale", "离线", "超过 90 秒没有收到心跳，当前运行状态未知"
	case g.ConfigRevision > 0 && g.FailedRevision >= g.ConfigRevision:
		v.Status, v.Label, v.Reason, v.Next = "failed", "应用失败", "最新配置应用失败，请查看回执原因", []string{"details", "retry"}
	case g.AppliedRevision < g.ConfigRevision:
		v.Status, v.Label, v.Reason, v.Next = "pending", "待生效", "配置已更新，等待 VPS 应用回执", []string{"details"}
	default:
		v.Status, v.Label, v.Reason, v.Next = "ok", "在线", "已收到心跳且配置已生效；节点连通性以各路径实测为准", []string{"details"}
	}
	return v
}

func (s *Server) nodeObjectState(ctx context.Context, n *ent.Node, g *ent.Gate) objectState {
	if !n.Enabled {
		return objectState{Status: "disabled", Label: "已停用", Reason: "节点已停用，不在订阅里", Next: []string{"enable"}}
	}
	v := gateObjectState(g)
	if v.Status != "ok" {
		v.BlockedByGate = true
		return v
	}
	state, why := s.nodeReadiness(ctx, n, g)
	if state != "ready" {
		v.Status, v.Label, v.Reason, v.Next = "unknown", "未知", why, []string{"details"}
		if state == "credentials" {
			v.Status, v.Label, v.Next = "pending", "待分配", []string{"authorize"}
		}
		if state == "pending" {
			v.Status, v.Label = "pending", "待生效"
		}
		return v
	}
	for _, p := range nodePorts(n, g) {
		if p.Firewall == "blocked" {
			return objectState{Status: "failed", Label: "未放行", Reason: p.Why, ObservedAt: g.LastSeenAt, Next: []string{"details"}}
		}
	}
	if result := s.loadNodeTest(ctx, n); result != nil {
		return nodeTestObjectState(result)
	}
	v.Status, v.Label, v.Reason, v.Next = "unknown", "未知", "还没有当前配置的外部连接结果", []string{"test"}
	r, ok := s.loadReach(ctx, n.ID)
	if !ok {
		return v
	}
	v.ObservedAt = &r.At
	if r.At.IsZero() || time.Since(r.At) > 5*time.Minute {
		v.Status, v.Reason = "stale", "外部连接观测已过期，请重新实测"
		return v
	}
	switch r.Status {
	case "ok":
		v.Status, v.Label, v.Reason = "partial", "端口可达", "中心 TCP 端口检查通过；协议握手和 UDP 尚不能据此确认"
	case "failed":
		v.Status, v.Label, v.Reason = "failed", "连不上", "中心外部连接失败："+r.Why
	default:
		v.Reason = firstNonEmpty(r.Why, v.Reason)
	}
	return v
}

func nodeTestObjectState(result *nodeTestResult) objectState {
	v := objectState{Status: "unknown", Label: "未知", Reason: "当前配置尚无完整实测结果", ObservedAt: &result.At, Next: []string{"test", "details"}}
	switch {
	case result.Status == "running" || result.Status == "pending":
		v.Status, v.Label, v.Reason = "pending", "实测中", "等待协议连接与路径实测结果"
	case result.Status == "unknown":
		v.Reason = firstNonEmpty(result.Why, v.Reason)
	case result.At.IsZero() || time.Since(result.At) > 5*time.Minute:
		v.Status, v.Label, v.Reason = "stale", "观测已过期", "路径实测已超过五分钟，请重新实测"
	case result.Status == "failed" || result.TCP.Status == "failed" || result.UDP.Status == "failed":
		v.Status, v.Label, v.Reason = "failed", "实测失败", "协议或路径实测未通过，请查看实测详情"
	case result.Status == "passed" && result.External == "passed" && result.TCP.Status == "passed" && result.UDP.Status == "passed":
		v.Status, v.Label, v.Reason = "ok", "实测通过", "当前配置的外部 TCP 协议连接与 UDP 往返均通过"
	case result.Status == "passed" && result.TCP.Status == "passed":
		v.Status, v.Label, v.Reason = "partial", "部分通过", "协议 TCP 连接通过；UDP 或公网独立观测尚未完整覆盖"
		if result.External == "passed" && result.UDP.Status == "not_applicable" {
			v.Label, v.Reason = "仅 TCP", "当前配置的外部 TCP 协议连接通过；此路径不提供 UDP"
		}
	}
	return v
}

func userObjectState(u *ent.User) objectState {
	v := objectState{Status: "ok", Label: "可使用", Reason: "账户已启用且授权期限有效", Next: []string{"details"}}
	switch {
	case !u.Enabled:
		v.Status, v.Label, v.Reason, v.Next = "disabled", "已停用", "账户已停用，订阅和节点均不可使用", []string{"enable"}
	case u.ExpiresAt != nil && !time.Now().Before(*u.ExpiresAt):
		v.Status, v.Label, v.Reason, v.Next = "failed", "已到期", "账户已到期，续期后恢复", []string{"renew"}
	case !u.Unlimited && u.Used >= u.Quota:
		v.Status, v.Label, v.Reason, v.Next = "failed", "额度已用完", "没有可用流量额度", []string{"renew"}
	case !u.Unlimited && u.Quota > 0 && float64(u.Used)/float64(u.Quota) >= .9:
		v.Status, v.Label, v.Reason = "warning", "临近额度", "流量已使用至少 90%"
	case u.ExpiresAt != nil && time.Until(*u.ExpiresAt) < 7*24*time.Hour:
		v.Status, v.Label, v.Reason, v.Next = "warning", "即将到期", "账户将在七天内到期", []string{"renew"}
	}
	return v
}

func exitUsageState(e *ent.Exit, applied, kept, unknown int, centerOK bool, centerAt *time.Time) objectState {
	if e != nil && !e.Enabled {
		return objectState{Status: "disabled", Label: "已停用", Reason: "出口已停用", Next: []string{"enable", "details"}}
	}
	if applied+kept+unknown == 0 {
		if centerOK {
			return objectState{Status: "partial", Label: "仅中心测通", Reason: "中心实测通过，尚未在 VPS 上验证", ObservedAt: centerAt, Next: []string{"probe", "details"}}
		}
		return objectState{Status: "unknown", Label: "未知", Reason: "尚未在 VPS 上使用，也没有实测", Next: []string{"probe", "details"}}
	}
	switch {
	case kept > 0:
		return objectState{Status: "partial", Label: "部分未替换", Reason: "有 VPS 仍使用上一次生效配置，详情里写明原因", Next: []string{"probe", "details"}}
	case unknown > 0:
		return objectState{Status: "unknown", Label: "部分未知", Reason: "有 VPS 还没有验证结果", Next: []string{"probe", "details"}}
	case applied > 0:
		return objectState{Status: "ok", Label: "已生效", Reason: "引用它的 VPS 都在使用当前连接参数", Next: []string{"details"}}
	default:
		return objectState{Status: "unknown", Label: "未知", Reason: "还没有可确认的使用结果", Next: []string{"details"}}
	}
}

func exitObjectState(e *ent.Exit, g *ent.Gate, p exitProbe, hasProbe bool) objectState {
	if !e.Enabled {
		return objectState{Status: "disabled", Label: "已停用", Reason: "出口已停用", Next: []string{"enable", "details"}}
	}
	v := gateObjectState(g)
	if v.Status != "ok" {
		return v
	}
	if e.Kind == "freedom" || e.Kind == "blackhole" {
		v.Label, v.Reason = "配置已生效", "本地出口配置已应用；外部连接能力以路径实测为准"
		return v
	}
	v = objectState{Status: "unknown", Label: "未知", Reason: "还没有当前出口的协议实测结果", Next: []string{"probe", "details"}}
	if !hasProbe {
		return v
	}
	v.ObservedAt = &p.At
	if p.At.IsZero() || time.Since(p.At) > 5*time.Minute {
		v.Status, v.Label, v.Reason = "stale", "观测已过期", "出口实测已超过五分钟，请重新实测"
	} else if !p.OK {
		v.Status, v.Label, v.Reason = "failed", "实测失败", "协议连接未通过，请查看实测详情"
	} else if p.Mode != "proxy" {
		v.Status, v.Label, v.Reason = "partial", "端口可达", "仅完成端口检查，协议握手与 UDP 未确认"
	} else if p.UDP == nil {
		v.Status, v.Label, v.Reason = "partial", "TCP 通过", "协议请求通过，UDP 能力未知"
	} else if !*p.UDP {
		v.Status, v.Label, v.Reason = "partial", "仅 TCP", "协议请求通过，UDP 实测未通过"
	} else {
		v.Status, v.Label, v.Reason = "ok", "实测通过", "协议请求与 UDP 实测均通过"
	}
	return v
}

func sourceObjectState(v *ent.SubSource, refreshHours int) objectState {
	state := objectState{Status: "unknown", Label: "未知", Reason: "还没有成功拉取记录", ObservedAt: v.FetchedAt, Next: []string{"refresh", "details"}}
	switch {
	case !v.Enabled:
		state.Status, state.Label, state.Reason, state.Next = "disabled", "已停用", "订阅源已停用", []string{"enable", "details"}
	case v.LastError != "":
		state.Status, state.Label, state.Reason = "failed", "刷新失败", "本次刷新未成功，请查看刷新详情"
		if v.FetchedAt != nil {
			state.Status, state.Label, state.Reason = "partial", "保留旧缓存", "本次刷新失败，保留上次成功缓存和时间；当前节点可用性未知"
		}
	case v.FetchedAt == nil || v.FetchedAt.IsZero():
	case v.NodeCount == 0:
		state.Status, state.Label, state.Reason = "warning", "没有节点", "最近一次成功拉取没有节点"
	case refreshHours > 0 && time.Since(*v.FetchedAt) > time.Duration(refreshHours)*time.Hour:
		state.Status, state.Label, state.Reason = "stale", "缓存已过期", "成功拉取时间已超过设定刷新周期，请重新刷新"
	default:
		state.Status, state.Label, state.Reason = "partial", "已缓存", "已有订阅节点缓存；协议连通性和 UDP 需按节点实测"
	}
	return state
}
