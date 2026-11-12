//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/shusfun/weir/packages/contracts"
)

func TestResourceUsageWithSupervisedKernel(t *testing.T) {
	e := setup(t)
	e.seedSingbox()
	e.s.StartBackground(e.ctx)
	e.startGate("resources")
	if err := e.s.Store.Ent.Gate.UpdateOneID(e.gateID).SetPublicHost("127.0.0.1").Exec(e.ctx); err != nil {
		t.Fatal(err)
	}
	n := e.post("/api/v1/admin/nodes", map[string]any{"gate_id": e.gateID, "preset": "ss2022", "name": "resource-node", "skip_probe": true}, nil)
	e.applied("resource-node")
	kernel := n["kernel"].(string)
	read := func() contracts.HostSample {
		b, _ := e.get(fmt.Sprintf("/api/v1/admin/gates/%d", e.gateID), nil)
		var v struct{ Host contracts.HostSample }
		if err := json.Unmarshal(b, &v); err != nil {
			t.Fatal(err)
		}
		return v.Host
	}
	eventually(t, 35*time.Second, "supervised CPU and RSS heartbeat", func() bool {
		h := read()
		p := h.Processes[kernel]
		return p.State == "running" && p.CPUPercent != nil && p.MemoryRSS != nil && *p.MemoryRSS > 0 && h.ObservationID != ""
	})
	b, _ := e.get("/api/v1/admin/center/resources", nil)
	var center struct{ Host contracts.HostSample }
	json.Unmarshal(b, &center)
	if p := center.Host.Processes["weir-center"]; p.State != "running" || p.CPUPercent == nil || p.MemoryRSS == nil {
		t.Fatalf("center=%+v", center.Host)
	}
	beforePID := helperRunning(t, e.paths.Socket)[kernel]
	restartedAt := time.Now()
	e.post(fmt.Sprintf("/api/v1/admin/gates/%d/kernels/%s/restart", e.gateID, kernel), map[string]any{}, nil)
	eventually(t, 15*time.Second, "new supervised process", func() bool { pid := helperRunning(t, e.paths.Socket)[kernel]; return pid > 0 && pid != beforePID })
	// 采样器的首帧未知由单元测试覆盖；心跳可能已经带上重启后的第二帧。
	eventually(t, 2*contracts.HostSampleEvery+5*time.Second, "fresh CPU after restart", func() bool {
		h := read()
		p := h.Processes[kernel]
		return h.ObservedAt.After(restartedAt) && p.State == "running" && p.CPUPercent != nil && p.MemoryRSS != nil
	})
	for _, path := range []string{"/api/v1/admin/center/resources/history", fmt.Sprintf("/api/v1/admin/gates/%d/history", e.gateID)} {
		b, _ := e.get(path, nil)
		var history struct {
			Points []struct {
				Processes map[string]contracts.ProcessSample
			}
		}
		if err := json.Unmarshal(b, &history); err != nil {
			t.Fatal(err)
		}
		if len(history.Points) != 60 || len(history.Points[59].Processes) == 0 {
			t.Fatalf("history %s: %s", path, b)
		}
	}
}
