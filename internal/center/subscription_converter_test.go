package center

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/subgroup"
)

func TestConverterSettingsSecretAndPermissions(t *testing.T) {
	s, admin := newInstalledServer(t)
	request := map[string]any{"enabled": true, "endpoint": "https://converter.example/sub", "authorization": "Bearer converter-secret", "max_bytes": 8 << 20}
	w := admin.do("PUT", "/api/v1/admin/subscriptions/converter", request, nil)
	if w.Code != 200 || strings.Contains(w.Body.String(), "converter-secret") {
		t.Fatalf("save: %d %s", w.Code, w.Body.String())
	}
	cfg, err := s.converterSettings(context.Background())
	if err != nil || cfg.Authorization == "" || strings.Contains(cfg.Authorization, "converter-secret") {
		t.Fatal("authorization not sealed", err)
	}
	plain, err := s.Crypto.Decrypt(cfg.Authorization)
	if err != nil || plain != "Bearer converter-secret" {
		t.Fatal("secret roundtrip", err)
	}
	delete(request, "authorization")
	w = admin.do("PUT", "/api/v1/admin/subscriptions/converter", request, nil)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	after, _ := s.converterSettings(context.Background())
	if after.Authorization != cfg.Authorization {
		t.Fatal("empty update cleared secret")
	}
	request["clear_authorization"] = true
	w = admin.do("PUT", "/api/v1/admin/subscriptions/converter", request, nil)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	after, _ = s.converterSettings(context.Background())
	if after.Authorization != "" {
		t.Fatal("explicit clearing failed")
	}
	guest := &testClient{t: t, router: admin.router}
	if w := guest.do("GET", "/api/v1/admin/subscriptions/converter", nil, nil); w.Code < 400 {
		t.Fatal("anonymous access accepted")
	}
}

func TestExternalRenderNeverFallsBack(t *testing.T) {
	s, _ := newInstalledServer(t)
	_, _, err := s.renderGroup(context.Background(), &ent.SubProfile{ID: 1}, "trojan://pw@node.example:443", "clash", subgroup.RenderOptions{Engine: "external", RuleLevel: "none"}, subgroup.Template{})
	if err == nil || err.Error() != "converter_not_configured" {
		t.Fatalf("external failure hidden: %v", err)
	}
	for _, body := range []string{"", "<html>error</html>", "No nodes were found", `{"error":"rejected"}`, "proxies: invalid"} {
		if validateConvertedOutput("clash", []byte(body)) == nil {
			t.Fatal("invalid YAML output accepted")
		}
	}
	if err := validateConvertedOutput("clash", []byte("proxies: []\n")); err != nil {
		t.Fatal(err)
	}
	ps, err := decodeProfileSettings(map[string]any{"engine": "external"})
	if err != nil || ps.Engine == nil || *ps.Engine != "external" {
		t.Fatal("engine setting did not decode", err)
	}
	if _, err = decodeProfileSettings(map[string]any{"engine": "fallback"}); err == nil {
		t.Fatal("unknown engine accepted")
	}
	encoded, _ := json.Marshal(ps)
	if !strings.Contains(string(encoded), "external") {
		t.Fatal("engine not preserved for backup")
	}
}
