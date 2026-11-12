package dsh

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestCallerCompatibilityAndDegradation(t *testing.T) {
	for _, tc := range []struct {
		version, rng     string
		policy           DegradationPolicy
		base, background bool
	}{
		{"0.2.1", ">=0.2.0 <0.3.0", DegradeReject, true, true},
		{"0.3.0", ">=0.2.0 <0.3.0", DegradeReject, false, false},
		{"0.3.0", ">=0.2.0 <0.3.0", DegradeBaseOnly, true, false},
		{"0.3.0", ">=0.3.0 <0.4.0", DegradeBaseOnly, true, true},
	} {
		d, err := CheckCompatibility(Compatibility{InstalledVersion: tc.version, VerifiedRange: tc.rng, OutOfRange: tc.policy})
		if err != nil || d.EnableBase != tc.base || d.EnableBackground != tc.background {
			t.Fatalf("%+v => %+v %v", tc, d, err)
		}
	}
}

func TestAgentEnforcesCallerDegradationBeforeLaunchingBackground(t *testing.T) {
	root := t.TempDir()
	if err := SharedDir(filepath.Join(root, "control")); err != nil {
		t.Fatal(err)
	}
	d := Desired{Version: "0.3.0", Generation: 1, AnalystPatch: "background", MCPURL: "http://127.0.0.1:1/mcp", Compatibility: &Compatibility{VerifiedRange: ">=0.2.0 <0.3.0", OutOfRange: DegradeBaseOnly}}
	if err := WriteShared(filepath.Join(root, "control", "desired.json"), d); err != nil {
		t.Fatal(err)
	}
	a := agent{l: Layout{Root: root}}
	a.step(context.Background())
	if a.desired.AnalystPatch != "" || a.desired.PluginDir != "" {
		t.Fatal("unverified features enabled")
	}
	res := a.runJob(context.Background(), a.desired, JobRequest{Task: "test"})
	if !strings.Contains(res.Error, "未启用") {
		t.Fatalf("background not paused: %+v", res)
	}
}
