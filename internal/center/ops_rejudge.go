package center

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// rejudgeOpsDraft 在第一次系统写入前重新采集当前状态再判断。
// 不需要、低置信度、未知或 Laya 不可用都拦下。不用旧草稿说明代替当前观测。
func (s *Server) rejudgeOpsDraft(ctx context.Context, d *Draft) (string, bool) {
	text, err := s.currentOpsText(ctx, d)
	if err != nil || strings.TrimSpace(text) == "" {
		return "没有采集到当前状态，已拦下", true
	}
	d.OpsBefore = text
	v, reason, blocked := s.judgeOpsText(ctx, text)
	if blocked {
		return reason, true
	}
	if v != nil {
		d.Confidence = v.Confidence
	}
	return "", false
}

// retestOpsDraft 在执行后重新采集并比较。问题消失算改善；仍需要处理或结果未知不算通过。
func (s *Server) retestOpsDraft(ctx context.Context, d *Draft) (string, bool) {
	if defenseDraft(d) {
		return "", false
	}
	text, err := s.currentOpsText(ctx, d)
	if err != nil || strings.TrimSpace(text) == "" {
		return "执行后没有采集到当前状态，不能确认改善", true
	}
	if fresh, err := s.freshDNSStatus(ctx, d); err == nil && fresh != "" {
		text = text + "\nfresh_dns=" + fresh
	}
	_, reason, blocked := s.judgeOpsText(ctx, text)
	if blocked {
		if strings.Contains(reason, "不需要") || strings.Contains(reason, "正常") {
			return "", false
		}
		return reason, true
	}
	return "执行后仍然需要处理，没有改善", true
}

func defenseDraft(d *Draft) bool {
	for _, st := range d.Steps {
		if st.Action == "defense.block" {
			return true
		}
	}
	return false
}

func (s *Server) judgeOpsText(ctx context.Context, text string) (*Verdict, string, bool) {
	l, configured := s.layaConfig(ctx)
	if !configured {
		return nil, "Laya 不可用，已拦下，没有强制执行", true
	}
	profile, err := s.aiProfile(ctx)
	if err != nil {
		return nil, "无法读取判断配置，已拦下", true
	}
	cfg, ok := profile.Effective.Observations["gate.kernel"]
	if !ok {
		return nil, "没有运维判断配置，已拦下", true
	}
	compatibility := defaultLayaCompatibility()
	if profile.Effective.LayaCompatibility != nil {
		compatibility = *profile.Effective.LayaCompatibility
	}
	v, err := s.classifyConfigured(ctx, l, text, cfg, profile.Revision, compatibility)
	if err != nil || v == nil || v.Category == "unknown" || v.Outcome == "unknown" || v.Confidence < v.Threshold {
		code := "unknown"
		if v != nil && v.Code != "" {
			code = v.Code
		}
		return v, fmt.Sprintf("复判未达到处理条件（%s），已拦下", code), true
	}
	if v.Outcome != "needed" {
		return v, fmt.Sprintf("当前状态不需要处理（%s），已拦下", v.Outcome), true
	}
	return v, "", false
}

func (s *Server) freshDNSStatus(ctx context.Context, d *Draft) (string, error) {
	for _, st := range d.Steps {
		if st.Action != "host.repair" || st.Args["key"] != "dns.resolvers" {
			continue
		}
		gid := intArg(st.Args, "gate_id")
		if gid == 0 {
			gid = d.GateID
		}
		return s.applyHostRepair(ctx, gid, "dns.recheck", "read")
	}
	return "", nil
}

func (s *Server) currentOpsText(ctx context.Context, d *Draft) (string, error) {
	for _, st := range d.Steps {
		if st.Action != "defense.block" {
			continue
		}
		event, err := s.loadEvent(ctx, intArg(st.Args, "event_id"))
		if err != nil || event == nil {
			return "", fmt.Errorf("没有采集到当前安全事件")
		}
		body, _ := json.Marshal(map[string]any{"event_id": event.ID, "kind": event.Kind, "status": event.Status, "failed_logins_10m": event.Failures, "trace_id": d.TraceID})
		return string(body), nil
	}
	gid := d.GateID
	if gid == 0 {
		for _, st := range d.Steps {
			if n := intArg(st.Args, "gate_id"); n > 0 {
				gid = n
				break
			}
		}
	}
	if gid == 0 {
		return "", fmt.Errorf("草稿没有关联可复测的资源")
	}
	g, err := s.Store.Ent.Gate.Get(ctx, gid)
	if err != nil {
		return "", err
	}
	health := map[string]any{}
	if g.Host != nil {
		if raw, ok := g.Host["health"]; ok {
			health, _ = raw.(map[string]any)
		}
	}
	body, _ := json.Marshal(map[string]any{
		"gate": g.Name, "online": g.LastSeenAt != nil, "last_error": g.LastError,
		"health": health, "kernel_events": s.kernelEvents(ctx, gid), "trace_id": d.TraceID,
	})
	if len(strings.TrimSpace(string(body))) == 0 || string(body) == "null" {
		return "", fmt.Errorf("当前状态为空")
	}
	return string(body), nil
}
