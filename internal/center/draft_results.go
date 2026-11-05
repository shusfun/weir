package center

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
)

// 草稿的去向：dsh 收不到确认窗的回调，每轮读 get_console_context 时从这里知道结果，不用问管理员。
// 只记标题、状态与原因，不记参数，所以不含机密；只给 dsh 窗口里的对话。

const (
	draftResultPrefix = "draft-result:"
	draftResultTTL    = 24 * time.Hour

	draftPending   = "待确认"
	draftApplied   = "已生效"
	draftFailed    = "确认时失败"
	draftDiscarded = "管理员放弃"
	draftWithdrawn = "已撤回"
	draftExpired   = "已过期"
)

type draftResult struct {
	ID      string    `json:"id"`
	Drawer  string    `json:"drawer,omitempty"`
	Titles  []string  `json:"titles"`
	Status  string    `json:"status"`
	Reason  string    `json:"reason,omitempty"`
	At      time.Time `json:"at"`
	NodeIDs []int     `json:"node_ids,omitempty"`
	GateIDs []int     `json:"gate_ids,omitempty"`
}

func (s *Server) recordDraft(ctx context.Context, d *Draft, status, reason string, nodes, gates []int) {
	if d.Source != "dsh" {
		return
	}
	r := draftResult{ID: d.ID, Drawer: d.Drawer, Status: status, Reason: redact(reason), At: time.Now().UTC(), NodeIDs: nodes, GateIDs: gates}
	for _, st := range d.Steps {
		r.Titles = append(r.Titles, st.Title)
	}
	b, err := json.Marshal(r)
	if err != nil {
		return
	}
	_ = s.Store.KV.Set(ctx, draftResultPrefix+d.ID, string(b), draftResultTTL)
}

// draftsForModel 是这个 dsh 会话最近的草稿（新到旧，最多 10 份）；已生效的附带节点与 VPS 此刻的状态。
func (s *Server) draftsForModel(ctx context.Context, drawer string) []map[string]any {
	keys, err := s.Store.KV.Keys(ctx, draftResultPrefix, 200)
	if err != nil {
		return nil
	}
	var list []draftResult
	for _, k := range keys {
		raw, ok, err := s.Store.KV.Get(ctx, k)
		if err != nil || !ok {
			continue
		}
		var r draftResult
		if json.Unmarshal([]byte(raw), &r) != nil || r.Drawer != drawer {
			continue
		}
		if r.Status == draftPending {
			if _, err := s.loadDraft(ctx, r.ID); err != nil {
				r.Status = draftExpired
			}
		}
		list = append(list, r)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].At.After(list[j].At) })
	if len(list) > 10 {
		list = list[:10]
	}
	out := make([]map[string]any, 0, len(list))
	for _, r := range list {
		item := map[string]any{"draft": r.ID, "steps": strings.Join(r.Titles, "；"), "status": r.Status, "at": r.At}
		if r.Reason != "" {
			item["reason"] = r.Reason
		}
		if r.Status == draftApplied {
			item["nodes"], item["gates"] = s.draftRuntime(ctx, r)
		}
		out = append(out, item)
	}
	return out
}

// draftRuntime 读节点与 VPS 此刻的状态：确认只代表配置写入，内核是否起来、端口是否监听要看这里。
func (s *Server) draftRuntime(ctx context.Context, r draftResult) ([]map[string]any, []map[string]any) {
	nodes := []map[string]any{}
	gates := append([]int{}, r.GateIDs...)
	for _, id := range r.NodeIDs {
		n, err := s.Store.Ent.Node.Get(ctx, id)
		if err != nil {
			nodes = append(nodes, map[string]any{"id": id, "state": "已不存在"})
			continue
		}
		v := map[string]any{"id": id, "name": n.Name, "state": "unknown"}
		if g, err := s.Store.Ent.Gate.Get(ctx, n.GateID); err == nil {
			state, why := s.nodeReadiness(ctx, n, g)
			v["state"], v["why"] = state, why
			if !containsInt(gates, g.ID) {
				gates = append(gates, g.ID)
			}
		}
		nodes = append(nodes, v)
	}
	out := []map[string]any{}
	for _, id := range gates {
		g, err := s.Store.Ent.Gate.Get(ctx, id)
		if err != nil {
			continue
		}
		v := gateView(g)
		out = append(out, map[string]any{"id": id, "name": g.Name, "online": v["online"], "state": v["state"], "last_error": v["last_error"]})
	}
	return nodes, out
}

// withdrawDraft 由 dsh 撤回自己这个会话提交、还没确认的草稿。
func (s *Server) withdrawDraft(ctx context.Context, id string) error {
	d, err := s.loadDraft(ctx, id)
	if err != nil {
		return errors.New("没有这份草稿，或它已经确认、放弃或过期")
	}
	_, drawer, _ := s.activeContext(ctx)
	if d.Source != "dsh" || d.Drawer != drawer {
		return errors.New("这份草稿不是这个会话提交的")
	}
	if _, applying, _ := s.Store.KV.Get(ctx, draftApplying+d.ID); applying {
		return errors.New("管理员已经确认，正在执行")
	}
	if err := s.Store.KV.Del(ctx, draftPrefix+d.ID); err != nil {
		return err
	}
	s.notifyConsole("drafts", "")
	s.recordDraft(ctx, d, draftWithdrawn, "", nil, nil)
	_, _ = s.Store.Ent.Audit.Create().SetAction("ai.withdraw").SetTarget("dsh " + d.ID).SetResult("ok").Save(ctx)
	return nil
}
