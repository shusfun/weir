package center

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"github.com/shusfun/weir/internal/store/ent/audit"
)

func TestLogoSettings(t *testing.T) {
	s, admin := newInstalledServer(t)
	anon := &testClient{t: t, router: admin.router}
	if got := mustJSON(t, anon, "GET", "/api/v1/logo", nil, 200); got["data"] != "" {
		t.Fatalf("initial logo: %v", got)
	}
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	data := "data:image/png;base64," + base64.StdEncoding.EncodeToString(b.Bytes())
	parent := "00-0123456789abcdef0123456789abcdef-1111111111111111-01"
	w := admin.do("PUT", "/api/v1/admin/settings/logo", map[string]string{"data": data}, map[string]string{"traceparent": parent})
	if w.Code != 200 || w.Header().Get("X-Trace-Id") != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("save: %d %s", w.Code, w.Body)
	}
	record, err := s.Store.Ent.Audit.Query().Where(audit.ActionEQ("settings.logo")).Only(t.Context())
	if err != nil || record.Detail["trace_id"] != w.Header().Get("X-Trace-Id") || record.Detail["request_id"] != w.Header().Get("X-Request-Id") {
		t.Fatalf("audit: %v %v", record, err)
	}
	if got := mustJSON(t, anon, "GET", "/api/v1/logo", nil, 200); got["data"] != data {
		t.Fatalf("read: %v", got)
	}
	mustJSON(t, anon, "PUT", "/api/v1/admin/settings/logo", map[string]string{"data": ""}, 401)
	mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "logo-user@example.test", "password": "Passw0rd"}, 200)
	user := login(t, s, "logo-user@example.test", "Passw0rd")
	mustJSON(t, user, "PUT", "/api/v1/admin/settings/logo", map[string]string{"data": ""}, 403)
	mustJSON(t, admin, "PUT", "/api/v1/admin/settings/logo", map[string]string{"data": "data:image/png;base64,AAAA"}, 400)
	mustJSON(t, admin, "PUT", "/api/v1/admin/settings/logo", map[string]string{"data": "data:image/png;base64," + strings.Repeat("A", maxLogoBytes*2)}, 400)
	mustJSON(t, admin, "PUT", "/api/v1/admin/settings/logo", map[string]string{"data": ""}, 200)
	if got := mustJSON(t, anon, "GET", "/api/v1/logo", nil, 200); got["data"] != "" {
		t.Fatalf("deleted logo: %v", got)
	}
}
