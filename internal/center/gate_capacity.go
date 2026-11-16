package center

import (
	"context"
	"fmt"
	"time"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/gate"
)

func (s *Server) gateCapacityPriorities(ctx context.Context, gates map[int]*ent.Gate) (out map[int]bool, err error) {
	finish := operationStage(ctx, "subscription.capacity.plan", "gate_count", len(gates))
	defer func() { finish(err) }()
	out = map[int]bool{}
	now := time.Now()
	for id := range gates {
		settings, usage, e := readGateTraffic(ctx, s.Store.Ent, id)
		if e != nil {
			return nil, e
		}
		out[id] = settings.AutoDeprioritize && networkAlert(settings, usage, now) == "critical"
	}
	return out, nil
}

func (s *Server) gateTrafficNotices(ctx context.Context) (out []Notice) {
	finish := operationStage(ctx, "traffic.host.notices")
	var failure error
	defer func() { finish(failure) }()
	gates, err := s.Store.Ent.Gate.Query().Where(gate.StatusNEQ(gate.StatusRevoked)).Order(gate.ByID()).All(ctx)
	if err != nil {
		failure = err
		return []Notice{{ID: "gate-traffic:unknown", Level: "warn", Title: "供应商流量状态未知", Detail: "无法读取 VPS 列表，请稍后重试。", Action: &NoticeAction{Kind: "open", Label: "查看 VPS", To: "/gates"}}}
	}
	now := time.Now()
	for _, g := range gates {
		settings, usage, err := readGateTraffic(ctx, s.Store.Ent, g.ID)
		action := &NoticeAction{Kind: "open", Label: "查看额度", To: "/gates?expand=" + itoa(g.ID) + "&traffic=" + itoa(g.ID)}
		if err != nil {
			failure = err
			out = append(out, Notice{ID: "gate-traffic:" + itoa(g.ID) + ":unknown", Level: "warn", Title: g.Name + " 供应商流量状态未知", Detail: "无法读取累计用量或额度配置。", Action: action})
			continue
		}
		level := networkAlert(settings, usage, now)
		if level != "warning" && level != "critical" {
			continue
		}
		percent := 80
		if level == "critical" {
			percent = 95
		}
		detail := "已观测网卡用量达到供应商周期额度的 " + itoa(percent) + "%"
		if now.Sub(usage.ObservedAt) > 90*time.Second {
			detail += "；观测已过期，显示最后累计值"
		}
		if settings.AutoDeprioritize && level == "critical" {
			detail += "；该 VPS 的节点已在内置自动候选中后移，客户端实际选择仍由其探测结果决定"
		}
		out = append(out, Notice{ID: fmt.Sprintf("gate-traffic:%d:%s:%s", g.ID, usage.Start.Format("2006-01-02"), level), Level: "warn", Title: g.Name + " 供应商流量提醒", Detail: detail + "。网卡观测可能不完整，可按供应商账单校准。", Action: action,
			Extra: map[string]any{"gate_id": g.ID, "alert": level, "observed_at": usage.ObservedAt, "period_start": usage.Start}})
	}
	return out
}
