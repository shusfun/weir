package center

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/store/ent/exit"
	"github.com/shusfun/weir/internal/store/ent/resource"
	"github.com/shusfun/weir/packages/contracts"
)

// The coordinator is exercised through the same leased task channel as a Gate.
// The fake worker supplies only the protocol probe result; kernel traffic has a separate fixture.
func serveUoTProbeTasks(t *testing.T, g *fakeGate) {
	t.Helper()
	stop, done := make(chan struct{}), make(chan struct{})
	t.Cleanup(func() { close(stop); <-done })
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			case <-time.After(20 * time.Millisecond):
			}
			code, body := g.call("POST", "/api/v1/agent/poll", contracts.AgentPoll{Protocol: contracts.AgentProtocol, ConfigRevision: -1, Resources: contracts.WorkerResources{Concurrency: 1}})
			var reply contracts.AgentPollReply
			if code != 200 || json.Unmarshal(body, &reply) != nil {
				t.Errorf("worker poll: %d %s", code, body)
				return
			}
			for _, task := range reply.Tasks {
				if task.Kind != contracts.ActionChainProbe {
					t.Errorf("unexpected task kind: %s", task.Kind)
					return
				}
				update := contracts.TaskUpdate{Attempt: task.Attempt, LeaseToken: task.LeaseToken, Result: json.RawMessage(`{"latency_ms":1}`)}
				if code, body := g.call("POST", "/api/v1/agent/tasks/"+task.ID+"/result", update); code != 204 {
					t.Errorf("worker result: %d %s", code, body)
					return
				}
			}
		}
	}()
}

func TestUoTStableIdentityAndReceiverRoutes(t *testing.T) {
	s, admin, fg := setupGate(t)
	ctx := t.Context()
	other := pairFakeGate(t, admin)
	for _, gid := range []int{int(fg.id), int(other.id)} {
		if err := s.Store.Tasks.SetCapabilities(ctx, gid, contracts.CapabilityDocument{Protocol: contracts.AgentProtocol, Version: "test", Arch: "amd64", Tasks: map[string]int{contracts.TaskCoreInstall: contracts.TaskVersion}, ConfigFeatures: map[string]int{contracts.ConfigUoT: 1, contracts.ConfigOutboundBridges: 1}}); err != nil {
			t.Fatal(err)
		}
	}
	direct, err := s.directExit(ctx, s.Store.Ent, int(other.id))
	if err != nil {
		t.Fatal(err)
	}
	n := s.Store.Ent.Node.Create().SetGateID(int(other.id)).SetExitID(direct.ID).SetName("receiver").SetKernel("xray").SetProtocol("vless").SetPort(31051).SaveX(ctx)
	e := s.Store.Ent.Exit.Create().SetGateID(int(fg.id)).SetRemark("internal").SetTag("internal-exit").SetKind(exit.KindNode).SetExitNodeID(n.ID).SaveX(ctx)
	if _, err = s.syncInternalLocked(ctx); err != nil {
		t.Fatal(err)
	}
	first, err := readUoT(ctx, s.Store.Ent, e.ID)
	if err != nil || first == nil {
		t.Fatal(err)
	}
	if _, err = s.syncInternalLocked(ctx); err != nil {
		t.Fatal(err)
	}
	second, err := readUoT(ctx, s.Store.Ent, e.ID)
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatal("transport identity changed", err)
	}
	if _, err := s.uotOutbound(ctx, e); err == nil {
		t.Fatal("entry enabled before receiver acknowledged")
	}
	if err := s.acknowledgeUoT(ctx, first.GateID, first.ReceiverRevision-1); err != nil {
		t.Fatal(err)
	}
	if p, _ := readUoT(ctx, s.Store.Ent, e.ID); p.Ready {
		t.Fatal("stale receiver acknowledgement unlocked entry")
	}
	if err := s.acknowledgeUoT(ctx, first.GateID, first.ReceiverRevision); err != nil {
		t.Fatal(err)
	}
	readyEntry := s.Store.Ent.Gate.GetX(ctx, first.EntryID)
	if err := s.acknowledgeUoT(ctx, first.GateID, first.ReceiverRevision); err != nil {
		t.Fatal(err)
	}
	if again := s.Store.Ent.Gate.GetX(ctx, first.EntryID); again.ConfigRevision != readyEntry.ConfigRevision {
		t.Fatal("duplicate acknowledgement advanced entry")
	}
	server, user, err := s.uotKeys(first)
	if err != nil {
		t.Fatal(err)
	}
	row := s.Store.Ent.Resource.Query().Where(resource.KindEQ(uotResourceKind)).OnlyX(ctx)
	if strings.Contains(row.String(), server) || strings.Contains(row.String(), user) {
		t.Fatal("unsealed keys")
	}
	g := s.Store.Ent.Gate.GetX(ctx, int(other.id))
	if err = s.checkPort(ctx, g, first.Port, 0); err == nil {
		t.Fatal("node can steal UoT port")
	}
	cfg := contracts.GateConfig{Inbounds: []contracts.Inbound{{ID: int64(n.ID), Tag: nodeTag(n.ID), Kernel: "xray", Protocol: "vless", Port: n.Port}}, Routes: []contracts.Route{{ID: 1, InboundTag: nodeTag(n.ID), OutboundTag: "selected", Domain: "example.test", Priority: 1001}}}
	need := map[string]bool{"xray": true}
	if err = s.compileUoTReceivers(ctx, g, &cfg, need); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Inbounds) != 2 || !need["sing-box"] || len(cfg.Credentials) != 1 || !cfg.Credentials[0].Internal {
		t.Fatal("receiver or internal accounting absent")
	}
	if len(cfg.Routes) != 2 || cfg.Routes[1].Domain != "example.test" || cfg.Routes[1].OutboundTag != "selected" || cfg.Routes[1].Priority != 1001 {
		t.Fatal("receiver bypassed selected routing", cfg.Routes)
	}
	http := s.Store.Ent.Exit.Create().SetGateID(n.GateID).SetTag("receiver-tcp").SetKind(exit.KindHTTP).SetAddress("203.0.113.20").SetPort(8080).SaveX(ctx)
	s.Store.Ent.Route.Create().SetGateID(n.GateID).SetNodeID(n.ID).SetExitID(http.ID).SetDomain("tcp.example.test").SaveX(ctx)
	if _, known := s.exitUDP(ctx, e.ID, map[int]bool{}); known {
		t.Fatal("UoT capability ignored the receiving node's selective routes")
	}
	e.Update().SetEnabled(false).ExecX(ctx)
	if _, err = s.syncInternalLocked(ctx); err != nil {
		t.Fatal(err)
	}
	if p, err := readUoT(ctx, s.Store.Ent, e.ID); err != nil || p != nil {
		t.Fatal("disabled exit retained transport", err)
	}
}
