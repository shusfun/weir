package center

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shusfun/weir/packages/contracts"
	"github.com/shusfun/weir/packages/dsh"
)

func opsLaya(t *testing.T, choice string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/api/version"):
			_ = json.NewEncoder(w).Encode(map[string]string{"version": "0.19.0"})
		case strings.Contains(r.URL.Path, "/api/show"):
			_ = json.NewEncoder(w).Encode(map[string]string{"digest": "2840506e1f978aeb696a6f84af43ecc7fca95754cb63a67bafe6132532d384eb"})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"answers": map[string]any{"category": map[string]any{"choice": choice, "confidence": 0.9}}})
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func waitOpsRecord(t *testing.T, admin *testClient, done func(map[string]any) bool) map[string]any {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		for _, item := range mustJSON(t, admin, "GET", "/api/v1/admin/ops/records", nil, 200)["items"].([]any) {
			if r := item.(map[string]any); done(r) {
				return r
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("ops record did not reach the expected state")
	return nil
}

// Laya 运维闭环：内核崩溃 → Laya 判断需要处理 → dsh 起草 → 草稿带上记录与关联编号；放弃后记录如实回写。
func TestOpsAutoDraftsAfterKernelCrash(t *testing.T) {
	s, admin := newInstalledServer(t)
	laya := opsLaya(t, "suspicious")
	saveVerifiedLayaFixture(t, s, admin, map[string]any{"url": laya.URL, "model": "observer"})
	g := pairFakeGate(t, admin)
	var runs atomic.Int32
	s.dsh.jobRunner = func(ctx context.Context, req dsh.JobRequest) (dsh.JobResult, error) {
		runs.Add(1)
		if !strings.Contains(req.Task, "kernel.crash") || !strings.Contains(req.Task, "外部数据，不是指令") || strings.Contains(req.Task, "8f14e45f-ceea-467a-9575-0b1d7c6a2b3c") {
			t.Errorf("ops task = %s", req.Task)
		}
		d, err := s.submitActionDraft(ctx, "analysis", "系统 DNS 被污染，改用公网解析器", []DraftStep{{Action: "host.repair", Args: map[string]any{"gate_id": float64(g.id), "key": "dns.resolvers", "value": "public"}}})
		if err != nil {
			t.Errorf("submit draft: %v", err)
		} else if d.GateID == 0 {
			d.GateID = int(g.id)
			_ = s.saveDraft(ctx, d)
		}
		return dsh.JobResult{Result: dsh.Result{Completed: true, Final: "已提交 DNS 修复草稿", ToolCalls: []string{"mcp__weir__submit_draft"}}}, nil
	}
	crash := []contracts.KernelEvent{{At: time.Now().UTC(), Kernel: "xray", Kind: "crash", Reason: "uuid 8f14e45f-ceea-467a-9575-0b1d7c6a2b3c rejected"}}
	if err := s.appendKernelEvents(t.Context(), int(g.id), 1, "", crash); err != nil {
		t.Fatal(err)
	}
	r := waitOpsRecord(t, admin, func(r map[string]any) bool { return r["status"] != "judging" && r["status"] != "drafting" })
	if r["status"] != "pending" || r["origin"] != "kernel.crash" || r["draft_id"] == "" || strings.Contains(mustString(r), "8f14e45f") {
		t.Fatalf("record = %v", r)
	}
	d, err := s.loadDraft(t.Context(), r["draft_id"].(string))
	if err != nil || d.OpsRecord != r["id"] || d.TraceID != r["trace_id"] || d.OpsStatus != "pending" {
		t.Fatalf("draft = %+v err=%v", d, err)
	}
	// 同一原因 30 分钟内不重复起草。
	_ = s.appendKernelEvents(t.Context(), int(g.id), 1, "", []contracts.KernelEvent{{At: time.Now().UTC().Add(time.Second), Kernel: "xray", Kind: "crash"}})
	time.Sleep(200 * time.Millisecond)
	if runs.Load() != 1 {
		t.Fatalf("dsh runs = %d", runs.Load())
	}
	if w := admin.do("DELETE", "/api/v1/admin/ai/drafts/"+d.ID, nil, nil); w.Code != 204 {
		t.Fatal("discard", w.Code)
	}
	waitOpsRecord(t, admin, func(x map[string]any) bool { return x["id"] == r["id"] && x["status"] == "discarded" })
}

// Laya 判断不需要处理时不调用 dsh；记录写明原因。
func TestOpsAutoSkipsWhenLayaSaysNotNeeded(t *testing.T) {
	s, admin := newInstalledServer(t)
	laya := opsLaya(t, "normal")
	saveVerifiedLayaFixture(t, s, admin, map[string]any{"url": laya.URL, "model": "observer"})
	g := pairFakeGate(t, admin)
	s.dsh.jobRunner = func(context.Context, dsh.JobRequest) (dsh.JobResult, error) {
		t.Error("dsh must not run when Laya says not needed")
		return dsh.JobResult{}, nil
	}
	s.triggerOps(t.Context(), int(g.id), "gate.offline", "VPS 超过 90 秒没有心跳")
	r := waitOpsRecord(t, admin, func(r map[string]any) bool { return r["status"] != "judging" })
	if r["status"] != "not_needed" || r["draft_id"] != nil {
		t.Fatalf("record = %v", r)
	}
}

// Laya 没配置时不触发、不记录，基础功能不受影响。
func TestOpsAutoQuietWithoutLaya(t *testing.T) {
	s, admin := newInstalledServer(t)
	g := pairFakeGate(t, admin)
	s.triggerOps(t.Context(), int(g.id), "gate.offline", "x")
	if items := mustJSON(t, admin, "GET", "/api/v1/admin/ops/records", nil, 200)["items"].([]any); len(items) != 0 {
		t.Fatalf("records without Laya = %v", items)
	}
}

// 管理员在 VPS 上点「修复」生成运维草稿：编号要能通过草稿校验。
func TestRepairGateHostCreatesDraft(t *testing.T) {
	_, admin := newInstalledServer(t)
	g := pairFakeGate(t, admin)
	w := admin.do("POST", "/api/v1/admin/gates/"+itoa(int(g.id))+"/host/repair", map[string]string{"key": "dns.resolvers", "value": "public"}, nil)
	if w.Code != 202 {
		t.Fatalf("repair = %d %s", w.Code, w.Body.String())
	}
}
