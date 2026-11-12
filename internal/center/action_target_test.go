package center

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/shusfun/weir/packages/contracts"
)

// SEC-01：动作只属于目标设备；另一台 VPS 既领不到，也不能替它回报结果。
func TestGateActionBoundToTargetDevice(t *testing.T) {
	s, admin := newInstalledServer(t)
	a := pairFakeGate(t, admin)
	b := pairFakeGate(t, admin)
	action, err := s.enqueueAction(t.Context(), int(a.id), contracts.ActionDiagnostics, map[string]any{"kernel": "xray"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	// B 自己也有一个动作，长轮询立刻返回，看它拿到的列表里有没有 A 的。
	if _, err = s.enqueueAction(t.Context(), int(b.id), contracts.ActionDiagnostics, map[string]any{"kernel": "xray"}, time.Minute); err != nil {
		t.Fatal(err)
	}
	code, body := b.call("GET", "/api/v1/agent/actions", nil)
	var list []contracts.Action
	if json.Unmarshal(body, &list) != nil || len(list) == 0 {
		t.Fatalf("own action not delivered: %d %s", code, body)
	}
	for _, item := range list {
		if item.ID == int64(action.ID) {
			t.Fatalf("other gate received the action: %d %s", code, body)
		}
	}
	if code, _ := b.call("POST", "/api/v1/agent/actions/"+itoa(action.ID), contracts.ActionResult{OK: true}); code != 404 {
		t.Fatalf("other gate reported the result: %d", code)
	}
	if got, _ := s.Store.Ent.GateAction.Get(t.Context(), action.ID); got.Status != "pending" {
		t.Fatalf("action status changed by wrong gate: %s", got.Status)
	}
}
