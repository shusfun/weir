package center

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/shusfun/weir/internal/store/ent/audit"
	"github.com/shusfun/weir/packages/laya"
)

func TestLayaDiscoveryRequiresConfirmationAndKeepsSecretsEncrypted(t *testing.T) {
	s, admin := newInstalledServer(t)
	var inference atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-secret" {
			t.Error("missing fixture authentication")
		}
		switch r.URL.Path {
		case "/api/version":
			_, _ = w.Write([]byte(`{"version":"0.18.0"}`))
		case "/api/show":
			_ = json.NewEncoder(w).Encode(map[string]string{"digest": laya.ModelManifestSHA256})
		default:
			inference.Add(1)
			w.WriteHeader(500)
		}
	}))
	defer upstream.Close()
	before, revision, _ := s.readLayaSettings(t.Context())
	w := admin.do("POST", "/api/v1/admin/defense/laya/discover", map[string]any{"url": upstream.URL, "key": "fixture-secret"}, nil)
	var result struct {
		Items []layaDiscoveredInstance `json:"items"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || len(result.Items) != 1 || result.Items[0].PreviewID == "" || strings.Contains(w.Body.String(), "fixture-secret") {
		t.Fatalf("discovery: %d %s", w.Code, w.Body.String())
	}
	_, after, _ := s.readLayaSettings(t.Context())
	if revision != after || before.Enabled {
		t.Fatal("read-only discovery enabled or changed configuration")
	}
	id := result.Items[0].PreviewID
	sealed, ok, err := s.Store.KV.Get(t.Context(), "laya-attach-preview:"+id)
	if err != nil || !ok || strings.Contains(sealed, "fixture-secret") {
		t.Fatal("preview retained plaintext key")
	}
	w = admin.do("POST", "/api/v1/admin/defense/laya/attach/"+id, nil, nil)
	if w.Code != 200 || strings.Contains(w.Body.String(), "fixture-secret") {
		t.Fatalf("attach: %d %s", w.Code, w.Body.String())
	}
	configured, _, err := s.readLayaSettings(t.Context())
	if err != nil || configured.Enabled || configured.URL != upstream.URL || configured.Key == "fixture-secret" {
		t.Fatalf("attachment: %+v err=%v", configured, err)
	}
	if key, err := s.reveal(configured.Key); err != nil || key != "fixture-secret" {
		t.Fatal("encrypted key not usable")
	}
	if inference.Load() != 0 {
		t.Fatal("discovery/attachment ran inference or installation")
	}
	if count, err := s.Store.Ent.Audit.Query().Where(audit.ActionEQ("defense.laya.attach"), audit.TargetEQ(id)).Count(t.Context()); err != nil || count != 1 {
		t.Fatal("missing attachment audit")
	}
}

func TestLayaDiscoveryRejectsChangedInstancesAndNonAdministrators(t *testing.T) {
	s, admin := newInstalledServer(t)
	var changed atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/version" {
			version := "0.18.0"
			if changed.Load() {
				version = "0.19.0"
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"version": version})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"digest": laya.ModelManifestSHA256})
	}))
	defer upstream.Close()
	w := admin.do("POST", "/api/v1/admin/defense/laya/discover", map[string]any{"url": upstream.URL}, nil)
	var result struct {
		Items []layaDiscoveredInstance `json:"items"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || len(result.Items) != 1 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	id := result.Items[0].PreviewID
	changed.Store(true)
	w = admin.do("POST", "/api/v1/admin/defense/laya/attach/"+id, nil, nil)
	if w.Code != 409 || !strings.Contains(w.Body.String(), "laya_instance_changed") {
		t.Fatalf("changed instance: %d %s", w.Code, w.Body.String())
	}
	configured, _, _ := s.readLayaSettings(t.Context())
	if configured.Enabled {
		t.Fatal("unconfirmed instance enabled")
	}
	mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "laya-discover@example.test", "password": "Passw0rd"}, 200)
	u := login(t, s, "laya-discover@example.test", "Passw0rd")
	anon := &testClient{t: t, router: s.Router()}
	for _, path := range []string{"/api/v1/admin/defense/laya/discover", "/api/v1/admin/defense/laya/attach/" + id} {
		if w := u.do("POST", path, map[string]any{}, nil); w.Code != 403 {
			t.Fatalf("ordinary user: %d", w.Code)
		}
		if w := anon.do("POST", path, map[string]any{}, nil); w.Code != 401 {
			t.Fatalf("anonymous: %d", w.Code)
		}
	}
}
