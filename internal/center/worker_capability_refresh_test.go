package center

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/shusfun/weir/packages/contracts"
)

func TestWorkerCapabilityRefreshRecoversNodeCreation(t *testing.T) {
	for _, raw := range []string{
		`{"protocol":2,"version":"test","arch":"amd64","tasks":{"subscription.convert":1}}`,
		`{"protocol":2`,
	} {
		t.Run(raw, func(t *testing.T) {
			s, admin, g := setupGate(t)
			ctx, gid := t.Context(), int(g.id)
			caps, err := s.Store.Tasks.Capabilities(ctx, gid)
			if err != nil {
				t.Fatal(err)
			}
			before := s.Store.Ent.Gate.GetX(ctx, gid)
			if _, err := s.Store.DB.ExecContext(ctx, `UPDATE worker_capabilities SET hash='invalid',document=$1 WHERE gate_id=$2`, raw, gid); err != nil {
				t.Fatal(err)
			}
			request := contracts.AgentPoll{Protocol: contracts.AgentProtocol, ConfigRevision: -1, Resources: contracts.WorkerResources{Concurrency: 1}}
			if code, body := g.call("POST", "/api/v1/agent/poll", request); code != 409 || !strings.Contains(string(body), "capabilities_required") {
				t.Fatalf("invalid cache used without fresh report: %d %s", code, body)
			}
			invalid := caps
			invalid.Tasks = map[string]int{"subscription.convert": 1}
			request.Capabilities = &invalid
			logs := captureLog(t)
			trace := "11223344556677889900aabbccddeeff"
			headers := g.requestHeaders("/api/v1/agent/poll")
			headers["traceparent"] = "00-" + trace + "-1111111111111111-01"
			w := g.c.do("POST", "/api/v1/agent/poll", request, headers)
			if w.Code != 400 {
				t.Fatalf("unsupported incoming capability accepted: %d %s", w.Code, w.Body.String())
			}
			var stored string
			if err := s.Store.DB.QueryRowContext(ctx, `SELECT document FROM worker_capabilities WHERE gate_id=$1`, gid).Scan(&stored); err != nil || stored != raw {
				t.Fatalf("rejected report changed cache: %v", err)
			}
			request.Capabilities = &caps
			w = g.c.do("POST", "/api/v1/agent/poll", request, headers)
			var reply contracts.AgentPollReply
			if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil || w.Code != 200 || reply.Config == nil {
				t.Fatalf("fresh capability could not recover poll: %d %s %v", w.Code, w.Body.String(), err)
			}
			after := s.Store.Ent.Gate.GetX(ctx, gid)
			if after.ConfigRevision != before.ConfigRevision+1 || after.PublicKey != before.PublicKey {
				t.Fatal("recovery did not refresh configuration while preserving device identity")
			}
			if got, err := s.Store.Tasks.Capabilities(ctx, gid); err != nil || got.Tasks[contracts.TaskCoreInstall] != contracts.TaskVersion {
				t.Fatalf("fresh report not persisted: %+v %v", got, err)
			}
			if code, body := g.call("POST", "/api/v1/agent/poll", request); code != 200 {
				t.Fatalf("repeat report: %d %s", code, body)
			}
			if s.Store.Ent.Gate.GetX(ctx, gid).ConfigRevision != after.ConfigRevision {
				t.Fatal("identical report repeatedly advanced configuration")
			}
			w = admin.do("POST", "/api/v1/admin/nodes", map[string]any{"gate_id": gid, "preset": "hysteria2", "skip_probe": true}, headersForTrace(trace))
			if w.Code != 200 || w.Header().Get("X-Trace-Id") != trace {
				t.Fatalf("Hysteria2 creation after refresh: %d %s", w.Code, w.Body.String())
			}
			for _, want := range []string{"operation=worker.capabilities.validate", "outcome=failed", "operation=worker.capabilities.refresh", "previous_invalid=true outcome=succeeded", "trace=" + trace, "route=/api/v1/admin/nodes status=200"} {
				if !strings.Contains(logs.String(), want) {
					t.Fatalf("missing trace evidence %q: %s", want, logs.String())
				}
			}
			for _, line := range strings.Split(logs.String(), "\n") {
				if strings.Contains(line, "trace="+trace) {
					t.Log(line)
				}
			}
		})
	}
}

func headersForTrace(trace string) map[string]string {
	return map[string]string{"traceparent": "00-" + trace + "-2222222222222222-01"}
}

func TestWorkerCapabilityRefreshPreservesStorageErrors(t *testing.T) {
	s, admin := newInstalledServer(t)
	g := pairFakeGate(t, admin)
	ctx, gid := t.Context(), int(g.id)
	caps, err := s.Store.Tasks.Capabilities(ctx, gid)
	if err != nil {
		t.Fatal(err)
	}
	before := s.Store.Ent.Gate.GetX(ctx, gid).ConfigRevision
	if _, err := s.Store.DB.ExecContext(ctx, `DROP TABLE worker_capabilities`); err != nil {
		t.Fatal(err)
	}
	code, body := g.call("POST", "/api/v1/agent/poll", contracts.AgentPoll{Protocol: contracts.AgentProtocol, Capabilities: &caps, Resources: contracts.WorkerResources{Concurrency: 1}})
	if code != 503 || !strings.Contains(string(body), "capabilities_update_failed") {
		t.Fatalf("storage failure hidden: %d %s", code, body)
	}
	if s.Store.Ent.Gate.GetX(ctx, gid).ConfigRevision != before {
		t.Fatal("storage read failure changed configuration")
	}
}
