package control

import "testing"

func TestGateRolloutStateKeepsOldAgentPendingAndFailureUnapplied(t *testing.T) {
	if got := GateRolloutState("0.2.0", "0.2.1", false, "failed"); got != "pending" {
		t.Fatalf("old agent = %s", got)
	}
	if got := GateRolloutState("0.2.0", "0.2.1", true, "failed"); got != "failed" {
		t.Fatalf("failed receipt = %s", got)
	}
	if got := GateRolloutState("0.2.0", "0.2.1", true, "applied"); got != "running" {
		t.Fatalf("receipt before heartbeat = %s", got)
	}
	if got := GateRolloutState("0.2.1", "0.2.1", true, "applied"); got != "applied" {
		t.Fatalf("matching heartbeat = %s", got)
	}
	if got := GateRolloutState("0.2.0", "", true, ""); got != "unknown" {
		t.Fatalf("missing target = %s", got)
	}
}
