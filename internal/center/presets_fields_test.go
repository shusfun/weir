package center

import "testing"

func TestPresetFieldsAlwaysAnArray(t *testing.T) {
	_, admin, _ := setupGate(t)
	list := mustJSON(t, admin, "GET", "/api/v1/admin/presets", nil, 200)["items"].([]any)
	if len(list) != 11 {
		t.Fatalf("expected 11 presets, got %d", len(list))
	}
	for _, v := range list {
		p := v.(map[string]any)
		if _, ok := p["fields"].([]any); !ok {
			t.Errorf("%s fields is not an array", p["id"])
		}
	}
}
