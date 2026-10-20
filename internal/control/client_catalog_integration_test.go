package control

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/imzyb/MiGate/ent/resource"
	"image"
	"image/png"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testClientCatalogIntegration(t *testing.T, s *Server, request func(string, string, any, string, string) *httptest.ResponseRecorder, admin, ordinary string) {
	t.Helper()
	ctx := context.Background()
	defer s.Store.Resource.Delete().Where(resource.KindEQ("settings"), resource.KeyEQ("client-catalog")).Exec(ctx)
	read := func(path, cookie string) catalogDocument {
		t.Helper()
		w := request("GET", path, nil, "", cookie)
		if w.Code != 200 {
			t.Fatal("catalog read failed", w.Code)
		}
		var d catalogDocument
		if json.Unmarshal(w.Body.Bytes(), &d) != nil {
			t.Fatal("catalog decode failed")
		}
		return d
	}
	if request("GET", "/api/v1/clients", nil, "", "").Code != 401 || request("GET", "/api/v1/admin/clients", nil, "", ordinary).Code != 403 {
		t.Fatal("catalog access boundary")
	}
	d := read("/api/v1/admin/clients", admin)
	if len(d.Clients) != len(defaultClientCatalog()) {
		t.Fatal("compatible defaults missing")
	}
	body := map[string]any{"clients": d.Clients, "revision": d.Revision, "password": "local-testing-password"}
	if request("PUT", "/api/v1/admin/clients", body, "", ordinary).Code != 403 {
		t.Fatal("ordinary catalog write")
	}
	for _, invalid := range []map[string]any{
		{"revision": d.Revision, "password": "local-testing-password"},
		{"clients": nil, "revision": d.Revision, "password": "local-testing-password"},
		{"clients": d.Clients, "revision": d.Revision, "password": "local-testing-password", "unknown": true},
	} {
		if request("PUT", "/api/v1/admin/clients", invalid, "", admin).Code != 400 {
			t.Fatal("ambiguous or unknown catalog request accepted")
		}
	}
	body["password"] = "wrong"
	if request("PUT", "/api/v1/admin/clients", body, "", admin).Code != 401 {
		t.Fatal("reauth bypass")
	}
	body["password"] = "local-testing-password"
	items := append([]catalogClient{}, d.Clients...)
	items[0], items[1] = items[1], items[0]
	items[0].Enabled = false
	var icon bytes.Buffer
	png.Encode(&icon, image.NewNRGBA(image.Rect(0, 0, 8, 8)))
	icon.WriteString("private-exif-fixture")
	items[1].IconData = "data:image/png;base64," + base64.StdEncoding.EncodeToString(icon.Bytes())
	body["clients"] = items
	if w := request("PUT", "/api/v1/admin/clients", body, "", admin); w.Code != 200 {
		t.Fatal("catalog save failed", w.Code)
	}
	saved := read("/api/v1/admin/clients", admin)
	if saved.Clients[0].Enabled || saved.Clients[0].ID != items[0].ID {
		t.Fatal("order/state not persisted")
	}
	user := read("/api/v1/clients", ordinary)
	if len(user.Clients) == 0 || user.Clients[0].IconData == "" {
		t.Fatal("enabled raster unavailable to user")
	}
	decoded, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(user.Clients[0].IconData, "data:image/png;base64,"))
	if bytes.Contains(decoded, []byte("private-exif")) {
		t.Fatal("icon source metadata leaked")
	}
	if len(user.Clients) != len(items)-1 || user.Revision != "" {
		t.Fatal("private/disabled client leaked")
	}

	// Real Redis cache and catalog ownership, with no dependency on GitHub in integration.
	if request("GET", "/api/v1/clients/"+items[1].ID+"/release", nil, "", "").Code != 401 {
		t.Fatal("anonymous release lookup")
	}
	if request("GET", "/api/v1/clients/"+items[0].ID+"/release", nil, "", ordinary).Code != 404 {
		t.Fatal("disabled client release leaked")
	}
	if request("GET", "/api/v1/clients/nonexistent/release", nil, "", ordinary).Code != 404 {
		t.Fatal("unknown client release")
	}
	repo := items[1].Repo
	if repo != "" {
		key := releaseCacheKey(repo)
		observed := time.Now().UTC().Format(time.RFC3339)
		cached, _ := json.Marshal(clientRelease{Status: "current", Source: "github-releases", ObservedAt: observed, Version: "v-owned-integration", URL: "https://github.com/" + repo + "/releases/tag/v-owned-integration"})
		if s.Redis.Set(ctx, key, cached, time.Minute).Err() != nil {
			t.Fatal("release cache fixture")
		}
		defer s.Redis.Del(ctx, key)
		w := request("GET", "/api/v1/clients/"+items[1].ID+"/release", nil, "", ordinary)
		var r clientRelease
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &r) != nil || r.Version != "v-owned-integration" || r.ObservedAt != observed {
			t.Fatal("cached release projection")
		}
		if s.Redis.Del(ctx, key).Err() != nil || s.Redis.Set(ctx, key+":fetch", "fixture", time.Minute).Err() != nil {
			t.Fatal("lease fixture")
		}
		defer s.Redis.Del(ctx, key+":fetch")
		w = request("GET", "/api/v1/clients/"+items[1].ID+"/release", nil, "", ordinary)
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &r) != nil || r.Status != "pending" {
			t.Fatal("shared fetch lease")
		}
	}
	if request("PUT", "/api/v1/admin/clients", body, "", admin).Code != 409 {
		t.Fatal("stale catalog overwrote")
	}
	body["revision"] = saved.Revision
	bad := append([]catalogClient{}, items...)
	bad[0].URL = "https://evil.test/?token={subscription}"
	body["clients"] = bad
	if request("PUT", "/api/v1/admin/clients", body, "", admin).Code != 400 {
		t.Fatal("token template accepted")
	}
	if read("/api/v1/admin/clients", admin).Revision != saved.Revision {
		t.Fatal("rejected write changed catalog")
	}
	body["clients"] = []catalogClient{}
	if request("PUT", "/api/v1/admin/clients", body, "", admin).Code != 200 {
		t.Fatal("clear failed")
	}
	empty := read("/api/v1/admin/clients", admin)
	body["revision"] = empty.Revision
	body["reset"] = true
	if request("PUT", "/api/v1/admin/clients", body, "", admin).Code != 200 || len(read("/api/v1/admin/clients", admin).Clients) != len(d.Clients) {
		t.Fatal("reset failed")
	}
}
