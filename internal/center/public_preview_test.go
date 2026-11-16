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

func TestConsolePolicyAllowsHTTPSBrowserTestsWithoutOpeningScripts(t *testing.T) {
	_, admin := newInstalledServer(t)
	policy := admin.do("GET", "/gates", nil, nil).Header().Get("Content-Security-Policy")
	directives := map[string]string{}
	for _, directive := range strings.Split(policy, ";") {
		parts := strings.Fields(directive)
		if len(parts) > 0 {
			directives[parts[0]] = strings.Join(parts[1:], " ")
		}
	}
	if directives["connect-src"] != "'self' https:" {
		t.Fatalf("Gate HTTPS endpoints blocked or insecure connections allowed: %s", policy)
	}
	if directives["script-src"] != "'self' https://challenges.cloudflare.com" || directives["default-src"] != "'self'" || directives["font-src"] != "'self' data:" {
		t.Fatalf("unrelated content restrictions changed: %s", policy)
	}
}
