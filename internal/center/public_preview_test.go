package center

import (
	"strings"
	"testing"
)

func TestPublicPreviewAllowsOnlySameOriginPublicDocuments(t *testing.T) {
	_, admin := newInstalledServer(t)
	for _, path := range []string{"/p/demo", "/pages", "/users", "/api/v1/pages/demo", "/api/v1/admin/public-pages"} {
		w := admin.do("GET", path, nil, nil)
		frame, ancestors := "DENY", "frame-ancestors 'none';"
		if path == "/p/demo" {
			frame, ancestors = "SAMEORIGIN", "frame-ancestors 'self';"
		}
		if w.Header().Get("X-Frame-Options") != frame || !strings.Contains(w.Header().Get("Content-Security-Policy"), ancestors) {
			t.Fatalf("%s framing headers: %v", path, w.Header())
		}
	}
}
