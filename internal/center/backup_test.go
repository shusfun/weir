package center

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
)

const upstreamBackup = `{
  "version": 1, "app": "MiSub", "type": "misub-backup", "exportedAt": "2026-10-01T00:00:00Z",
  "data": {
    "subscriptions": [
      {"id": "a1", "name": "机场A", "url": "https://sub.example.com/link?token=x", "enabled": true, "customUserAgent": "clash-meta/2.5.0", "exclude": "过期", "enableNodeCache": true, "website": "https://example.com", "notes": "月付"},
      {"id": "a2", "name": "内网", "url": "http://192.168.1.2/sub", "enabled": true},
      {"id": "m1", "name": "家宽", "url": "trojan://p@203.0.113.9:443?sni=example.com#home", "group": "住宅", "enabled": true},
      {"id": "m2", "name": "坏节点", "url": "not a node", "enabled": true}
    ],
    "profiles": [
      {"id": "p1", "name": "日常", "enabled": true, "subscriptions": ["a1", "a2"], "manualNodes": ["m1"], "ruleLevel": "lite", "transformConfig": "clash_acl4ssr_lite", "expiresAt": "2027-01-01T00:00:00Z", "prefixSettings": {"enableManualNodes": false}}
    ],
    "ruleTemplates": [{"id": "t1", "name": "我的模板", "content": "[custom]\nruleset=🎯 全球直连,[]GEOIP,CN\ncustom_proxy_group=🚀 节点选择` + "`" + `select` + "`" + `[]DIRECT\n"}]
  }
}`

func TestMisubBackupImportExportAndWebdav(t *testing.T) {
	s, admin := newInstalledServer(t)
	mustJSON(t, admin, "POST", "/api/v1/admin/backup/import", map[string]any{"content": upstreamBackup, "password": "wrong"}, 401)
	st := mustJSON(t, admin, "POST", "/api/v1/admin/backup/import", map[string]any{"content": upstreamBackup, "password": "Passw0rd"}, 200)
	if st["sources"].(float64) != 1 || st["nodes"].(float64) != 1 || st["profiles"].(float64) != 1 || st["templates"].(float64) != 1 || len(st["skipped"].([]any)) != 2 {
		t.Fatalf("import stats = %v", st)
	}
	ctx := t.Context()
	src := s.Store.Ent.SubSource.Query().OnlyX(ctx)
	if src.FilterExclude != "过期" || !src.NodeCache || src.Remark != "月付" || src.UserAgent != "clash-meta/2.5.0" {
		t.Fatalf("source = %+v", src)
	}
	p := s.Store.Ent.SubProfile.Query().OnlyX(ctx)
	if len(p.SourceIds) != 1 || len(p.NodeIds) != 1 || p.ExpiresAt == nil || p.Settings["template"] != "builtin:clash_acl4ssr_lite" || p.Settings["prepend_manual"] != false || p.Settings["rule_level"] != "lite" {
		t.Fatalf("profile = %+v", p)
	}
	mustJSON(t, admin, "POST", "/api/v1/admin/backup/import", map[string]any{"content": "{}", "password": "Passw0rd"}, 400)

	// 导出后再导入：数量翻倍（导入只新增），订阅组引用指向新建的源与节点。
	w := admin.do("GET", "/api/v1/admin/backup/export", nil, nil)
	if w.Code != 200 || !strings.Contains(w.Header().Get("Content-Disposition"), "misub-backup-") {
		t.Fatalf("export = %d", w.Code)
	}
	again := mustJSON(t, admin, "POST", "/api/v1/admin/backup/import", map[string]any{"content": w.Body.String(), "password": "Passw0rd"}, 200)
	if again["sources"].(float64) != 1 || again["nodes"].(float64) != 1 || again["profiles"].(float64) != 1 || again["templates"].(float64) != 1 {
		t.Fatalf("re-import = %v", again)
	}
	if n, _ := s.Store.Ent.SubProfile.Query().Count(ctx); n != 2 {
		t.Fatalf("profiles = %d", n)
	}

	// WebDAV：上传、按保留份数清理、列出、恢复。
	var mu sync.Mutex
	files := map[string][]byte{}
	dav := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, p, ok := r.BasicAuth(); !ok || u != "bk" || p != "secret" {
			w.WriteHeader(401)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		name := strings.TrimPrefix(r.URL.Path, "/dav/")
		switch r.Method {
		case "PUT":
			b, _ := io.ReadAll(r.Body)
			files[name] = b
			w.WriteHeader(201)
		case "GET":
			b, ok := files[name]
			if !ok {
				w.WriteHeader(404)
				return
			}
			w.Write(b)
		case "DELETE":
			delete(files, name)
			w.WriteHeader(204)
		case "PROPFIND":
			names := make([]string, 0, len(files))
			for n := range files {
				names = append(names, n)
			}
			sort.Strings(names)
			w.WriteHeader(207)
			fmt.Fprint(w, `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:"><d:response><d:href>/dav/</d:href></d:response>`)
			for _, n := range names {
				fmt.Fprintf(w, `<d:response><d:href>/dav/%s</d:href></d:response>`, n)
			}
			fmt.Fprint(w, `</d:multistatus>`)
		}
	}))
	defer dav.Close()
	mustJSON(t, admin, "PUT", "/api/v1/admin/backup/webdav", map[string]any{"url": dav.URL + "/dav", "username": "bk", "password": "secret", "keep": 1, "hours": 24, "account_password": "Passw0rd"}, 200)
	if v := mustJSON(t, admin, "GET", "/api/v1/admin/backup/webdav", nil, 200); v["password_set"] != true || strings.Contains(mustString(v), "secret") {
		t.Fatalf("webdav view leaks password: %v", v)
	}
	mustJSON(t, admin, "POST", "/api/v1/admin/backup/webdav/run", nil, 200)
	mu.Lock()
	files["misub-backup-00000000-000000.json"] = []byte(upstreamBackup) // 更旧的一份
	mu.Unlock()
	mustJSON(t, admin, "POST", "/api/v1/admin/backup/webdav/run", nil, 200)
	list := mustJSON(t, admin, "GET", "/api/v1/admin/backup/webdav/files", nil, 200)["files"].([]any)
	if len(list) != 1 {
		t.Fatalf("keep=1 must prune old backups: %v", list)
	}
	mustJSON(t, admin, "POST", "/api/v1/admin/backup/webdav/restore", map[string]any{"file": "../etc/passwd", "password": "Passw0rd"}, 400)
	res := mustJSON(t, admin, "POST", "/api/v1/admin/backup/webdav/restore", map[string]any{"file": list[0], "password": "Passw0rd"}, 200)
	if res["profiles"].(float64) != 2 {
		t.Fatalf("restore = %v", res)
	}
	_ = json.Valid
}
