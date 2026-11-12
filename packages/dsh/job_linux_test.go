//go:build linux

package dsh

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A process fixture exercises the lifecycle boundary without an API key or
// a provider call. It is not a real model/harness acceptance test.
func TestJobProcessExitCompletionAndCancellation(t *testing.T) {
	for _, tc := range []struct {
		name, script, want string
		failure            bool
	}{
		{"complete", `printf '%s\n' '{"type":"tool_call","tool":"status.read"}' '{"type":"final","text":"ready"}' '{"type":"status","phase":"turn_end","reason":{"kind":"completed"}}'`, "succeeded", false},
		{"unknown", `printf '%s\n' '{"type":"final","text":"partial"}'`, "unknown", true},
		{"exit", `exit 7`, "failed", true},
		{"cancel", `sleep 30`, "timeout", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			node := filepath.Join(dir, "node")
			if err := os.WriteFile(node, []byte("#!/bin/sh\n"+tc.script+"\n"), 0700); err != nil {
				t.Fatal(err)
			}
			profile := filepath.Join(dir, "home", "profiles", "example-analyst")
			if err := os.MkdirAll(profile, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(profile, "cordis.yml"), []byte("[]"), 0600); err != nil {
				t.Fatal(err)
			}
			var events []Event
			job := Job{Paths: Paths{Node: node, Bin: "fixture.js", Dir: dir, Observe: func(_ context.Context, e Event) { events = append(events, e) }}, Namespace: "example", Patch: []byte("[]"), Compatibility: Compatibility{InstalledVersion: "0.2.1", VerifiedRange: ">=0.2.0 <0.3.0", OutOfRange: DegradeReject}}
			ctx := t.Context()
			if tc.name == "cancel" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 100*time.Millisecond)
				defer cancel()
			}
			got, err := job.Run(ctx, "PRIVATE-TASK")
			if (err != nil) != tc.failure || got.State != tc.want {
				t.Fatalf("result=%+v err=%v", got, err)
			}
			if tc.name == "cancel" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("timeout=%v", err)
			}
			if len(events) != 1 || events[0].Status != map[string]string{"complete": "succeeded", "unknown": "failed", "exit": "failed", "cancel": "timeout"}[tc.name] {
				t.Fatalf("events=%+v", events)
			}
			if strings.Contains(events[0].Code, "PRIVATE-TASK") {
				t.Fatal("prompt leaked")
			}
			if _, err := os.Stat(filepath.Join(profile, "cordis.patch.yml")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("ephemeral patch retained: %v", err)
			}
		})
	}
}
