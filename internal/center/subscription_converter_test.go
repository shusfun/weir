package center

import (
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/subgroup"
	"testing"
)

func TestExternalConversionRemovedWithoutSilentFallback(t *testing.T) {
	s, admin := newInstalledServer(t)
	for _, method := range []string{"GET", "PUT"} {
		w := admin.do(method, "/api/v1/admin/subscriptions/converter", map[string]any{"enabled": true}, nil)
		if w.Code < 400 {
			t.Fatalf("removed endpoint accepted %s: %d", method, w.Code)
		}
	}
	_, _, err := s.renderGroup(t.Context(), &ent.SubProfile{ID: 1}, "trojan://pw@node.example:443", "clash", subgroup.RenderOptions{Engine: "external"}, subgroup.Template{})
	if err == nil || err.Error() != "external_conversion_removed" {
		t.Fatalf("legacy config silently converted: %v", err)
	}
	ps, err := decodeProfileSettings(map[string]any{"engine": "external"})
	if err != nil || ps.Engine == nil || *ps.Engine != "external" {
		t.Fatalf("legacy configuration lost: %v", err)
	}
	mustJSON(t, admin, "PUT", "/api/v1/admin/subscriptions/settings", map[string]any{"engine": "external"}, 400)
}
