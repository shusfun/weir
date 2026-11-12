package contracts

import (
	"encoding/json"
	"testing"
)

func TestTaskSchemaRejectsUnknownAndTrailingInput(t *testing.T) {
	for _, raw := range []string{`{"kernel":"xray","shell":"id"}`, `{"kernel":"xray"} {}`, `null`} {
		if err := ValidateTaskPayload(ActionCoreRestart, 1, json.RawMessage(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	if err := ValidateTaskPayload(ActionCoreRestart, 1, json.RawMessage(`{"kernel":"xray"}`)); err != nil {
		t.Fatal(err)
	}
	if err := ValidateTaskPayload("shell", 1, json.RawMessage(`{}`)); err == nil {
		t.Fatal("unknown task accepted")
	}
	if err := ValidateTaskPayload(ActionCoreRestart, 2, json.RawMessage(`{}`)); err == nil {
		t.Fatal("unknown version accepted")
	}
}
