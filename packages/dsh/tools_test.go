package dsh

import (
	"context"
	"encoding/json"
	"testing"
)

func TestRegistrySourcesSecretsAndObservation(t *testing.T) {
	calls, events, failures := 0, 0, 0
	r, err := NewRegistry(func(_ context.Context, e Event) {
		events++
		if e.Status == "failed" {
			failures++
		}
	}, Tool{Name: "read", Sources: []string{"console"}, Secret: []string{"token"}, InputSchema: map[string]any{"type": "object", "properties": map[string]any{"name": map[string]any{"type": "string"}, "token": map[string]any{"type": "string"}}, "required": []string{"name", "token"}}, Handle: func(_ context.Context, _ string, b json.RawMessage) (ToolResult, error) {
		calls++
		return ToolResult{Text: string(b)}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	listed := r.Tools("console")
	if len(listed) != 1 || len(listed[0].InputSchema["properties"].(map[string]any)) != 1 {
		t.Fatalf("tools=%+v", listed)
	}
	if _, err = r.Call(context.Background(), "analysis", "read", nil); err == nil {
		t.Fatal("source restriction ignored")
	}
	if _, err = r.Call(context.Background(), "console", "read", json.RawMessage(`{"token":"x"}`)); err == nil {
		t.Fatal("secret accepted")
	}
	res, err := r.Call(context.Background(), "console", "read", json.RawMessage(`{"name":"safe"}`))
	if err != nil || res.IsError || calls != 1 || events != 3 || failures != 2 {
		t.Fatalf("res=%+v err=%v calls=%d events=%d failures=%d", res, err, calls, events, failures)
	}
}
