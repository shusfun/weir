package center

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/buildinfo"
	"github.com/shusfun/weir/internal/fetch"
	"github.com/shusfun/weir/internal/ghrelease"
	"github.com/shusfun/weir/internal/store/ent/resource"
	"github.com/shusfun/weir/packages/contracts"
)

// rawCall 以 Gate 身份发送原始请求体（上传分块用）。
func (g *fakeGate) rawCall(method, path string, body []byte) (int, []byte) {
	req := httptest.NewRequest(method, "https://example.com"+path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Authorization", "Bearer "+g.bearer)
	w := httptest.NewRecorder()
	g.c.router.ServeHTTP(w, req)
	return w.Code, w.Body.Bytes()
}

func (g *fakeGate) heartbeat(version string) contracts.HeartbeatReply {
	g.t.Helper()
	code, body := g.call("POST", "/api/v1/agent/heartbeat", contracts.Heartbeat{Version: version, Hostname: "hk", Arch: "amd64"})
	if code != 200 {
		g.t.Fatalf("heartbeat = %d %s", code, body)
	}
	var r contracts.HeartbeatReply
	_ = json.Unmarshal(body, &r)
	return r
}

// nextAction 等 Gate 收到某种动作。
func (g *fakeGate) nextAction(kind string) contracts.Action {
	g.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		code, body := g.call("GET", "/api/v1/agent/actions", nil)
		if code != 200 {
			g.t.Fatalf("actions = %d %s", code, body)
		}
		var list []contracts.Action
		_ = json.Unmarshal(body, &list)
		for _, a := range list {
			if a.Kind == kind {
				return a
			}
		}
	}
	g.t.Fatalf("no %s action", kind)
	return contracts.Action{}
}

func (g *fakeGate) finish(a contracts.Action, ok bool, msg string, result map[string]any) {
	g.t.Helper()
	if code, body := g.call("POST", "/api/v1/agent/actions/"+itoa(int(a.ID)), contracts.ActionResult{ID: a.ID, OK: ok, Error: msg, Result: result}); code != 200 {
		g.t.Fatalf("action result = %d %s", code, body)
	}
}

func sourcesDoc(t *testing.T, admin *testClient) map[string]any {
	return mustJSON(t, admin, "GET", "/api/v1/admin/github-sources", nil, 200)
}

func TestGitHubSourcesSettings(t *testing.T) {
	t.Setenv("WEIR_GITHUB_MIRRORS", "a.example,b.example")
	s, admin, g := setupGate(t)
	doc := sourcesDoc(t, admin)
	want := `[{"kind":"direct"},{"kind":"mirror","prefix":"https://a.example/"},{"kind":"mirror","prefix":"https://b.example/"}]`
	if got, _ := json.Marshal(doc["sources"]); string(got) != want || doc["default"] != true {
		t.Fatalf("default doc = %s %v", got, doc["default"])
	}
	if got, _ := json.Marshal(doc["defaults"]); string(got) != want {
		t.Fatalf("defaults = %s", got)
	}

	// 旧设置「安装包加速镜像」迁移为直连 + 这些前缀
	ctx := context.Background()
	if err := s.Store.Ent.Resource.Create().SetKind("settings").SetKey("client-mirrors").SetData(map[string]any{"mirrors": []any{"https://old.example/gh/", "bad"}}).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	doc = sourcesDoc(t, admin)
	if got, _ := json.Marshal(doc["sources"]); string(got) != `[{"kind":"direct"},{"kind":"mirror","prefix":"https://old.example/gh/"}]` || doc["default"] != true {
		t.Fatalf("legacy doc = %s", got)
	}
	rev := doc["revision"].(string)

	put := func(sources []map[string]any, revision string) *httptest.ResponseRecorder {
		return admin.do("PUT", "/api/v1/admin/github-sources", map[string]any{"sources": sources, "revision": revision}, nil)
	}
	gid := g.id
	for code, list := range map[string][]map[string]any{
		"invalid_mirror":       {{"kind": "mirror", "prefix": "http://x.example/"}},
		"invalid_source":       {{"kind": "direct"}, {"kind": "direct"}},
		"invalid_source_gate":  {{"kind": "direct"}, {"kind": "gate", "gate_id": 999}},
		"http_source_required": {{"kind": "gate", "gate_id": gid}},
		"source_limit":         {},
	} {
		if w := put(list, rev); w.Code != 400 || !strings.Contains(w.Body.String(), code) {
			t.Fatalf("%s: %d %s", code, w.Code, w.Body.String())
		}
	}
	dup := []map[string]any{{"kind": "direct"}, {"kind": "mirror", "prefix": "https://m.example/"}, {"kind": "mirror", "prefix": "https://m.example/"}}
	if w := put(dup, rev); w.Code != 400 {
		t.Fatalf("duplicate mirror = %d", w.Code)
	}
	good := []map[string]any{{"kind": "gate", "gate_id": gid}, {"kind": "mirror", "prefix": "https://m.example/"}, {"kind": "mirror", "prefix": "https://p.example/path/"}, {"kind": "direct"}}
	if w := put(good, "stale"); w.Code != 409 {
		t.Fatalf("stale revision = %d", w.Code)
	}
	saved := mustJSON(t, admin, "PUT", "/api/v1/admin/github-sources", map[string]any{"sources": good, "revision": rev}, 200)
	if saved["default"] != false || saved["sources"].([]any)[0].(map[string]any)["gate_name"] != "HK-1" {
		t.Fatalf("saved = %v", saved)
	}
	if w := put(good, rev); w.Code != 409 {
		t.Fatalf("old revision after save = %d", w.Code)
	}
	if n, _ := s.Store.Ent.Audit.Query().Count(ctx); n == 0 {
		t.Fatal("audit missing")
	}
	// 浏览器用的列表去掉 VPS，顺序不变
	pub := mustJSON(t, admin, "GET", "/api/v1/github-sources", nil, 200)
	if got, _ := json.Marshal(pub["sources"]); string(got) != `[{"kind":"mirror","prefix":"https://m.example/"},{"kind":"mirror","prefix":"https://p.example/path/"},{"kind":"direct"}]` {
		t.Fatalf("public = %s", got)
	}
	// 旧接口已移除
	if w := admin.do("GET", "/api/v1/client-mirrors", nil, nil); w.Code != 404 {
		t.Fatalf("old route = %d", w.Code)
	}
	// 安装命令只带主机名形式的镜像，按顺序；gate.sh 里直连排在镜像之后
	inv := mustJSON(t, admin, "POST", "/api/v1/admin/gates/invite", map[string]string{"name": "x"}, 201)
	if cmd := inv["command"].(string); !strings.Contains(cmd, "WEIR_GITHUB_MIRRORS=m.example sh -s") {
		t.Fatalf("command = %s", cmd)
	}
	if w := admin.do("GET", "/install/gate.sh", nil, nil); w.Code != 200 || !strings.Contains(w.Body.String(), "direct='last'") {
		t.Fatalf("gate.sh = %d %s", w.Code, w.Body.String())
	}
	if _, err := s.Store.Ent.Resource.Delete().Where(resource.KeyEQ("github-sources")).Exec(ctx); err != nil {
		t.Fatal(err)
	}
}

// 经 VPS 中转：只有发起的那台 Gate 能上传，offset 必须接上已收到的字节数，完成后内容一致。
func TestRelayUploadOwnershipAndResume(t *testing.T) {
	s, admin, g := setupGate(t)
	other := pairWithToken(t, admin, decode(t, admin.do("POST", "/api/v1/admin/gates/invite", map[string]string{"name": "JP"}, nil))["token"].(string))
	other.heartbeat("test")
	content := bytes.Repeat([]byte("relay"), (contracts.UploadChunk+1000)/5)
	sum := sha256.Sum256(content)
	dest := filepath.Join(t.TempDir(), "asset.part")
	var progress atomic.Int64
	done := make(chan error, 1)
	go func() {
		done <- s.relayFetch(context.Background(), int(g.id), contracts.AssetFetch{URLs: []string{"https://github.com/a/b/releases/download/v1/x"}, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(content)), MaxSize: 1 << 20}, dest, fetch.Reporter{Progress: func(d, _, _ int64) { progress.Store(d) }})
	}()
	a := g.nextAction(contracts.ActionAssetFetch)
	id, _ := a.Payload["upload_id"].(string)
	if id == "" || a.Payload["sha256"] != hex.EncodeToString(sum[:]) {
		t.Fatalf("payload = %v", a.Payload)
	}
	path := "/api/v1/agent/uploads/" + id
	// 别的 Gate 看不到也写不了
	if code, _ := other.rawCall("PUT", path+"?offset=0", content[:10]); code != 404 {
		t.Fatalf("other gate write = %d", code)
	}
	if code, _ := other.call("GET", path, nil); code != 404 {
		t.Fatalf("other gate read = %d", code)
	}
	if code, _ := g.rawCall("PUT", path+"?offset=0", make([]byte, contracts.UploadChunk+1)); code != 413 {
		t.Fatalf("oversized chunk = %d", code)
	}
	first := content[:contracts.UploadChunk]
	if code, body := g.rawCall("PUT", path+"?offset=0", first); code != 200 || !strings.Contains(string(body), `"received":524288`) {
		t.Fatalf("chunk 1 = %d %s", code, body)
	}
	// 位置不符：409 并回已收到的字节数
	code, body := g.rawCall("PUT", path+"?offset=0", first)
	var st contracts.UploadState
	_ = json.Unmarshal(body, &st)
	if code != 409 || st.Received != contracts.UploadChunk {
		t.Fatalf("mismatch = %d %s", code, body)
	}
	if code, body := g.call("GET", path, nil); code != 200 || !strings.Contains(string(body), `"received":524288`) {
		t.Fatalf("state = %d %s", code, body)
	}
	if code, body := g.rawCall("PUT", path+"?offset="+itoa(contracts.UploadChunk), content[contracts.UploadChunk:]); code != 200 {
		t.Fatalf("chunk 2 = %d %s", code, body)
	}
	g.finish(a, true, "", map[string]any{"size": len(content)})
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(dest); !bytes.Equal(got, content) || progress.Load() != int64(len(content)) {
		t.Fatalf("content mismatch, progress %d", progress.Load())
	}
	// 用完作废
	if code, _ := g.call("GET", path, nil); code != 404 {
		t.Fatalf("closed upload = %d", code)
	}

	// Gate 报失败：立刻返回它的原因
	go func() {
		done <- s.relayFetch(context.Background(), int(g.id), contracts.AssetFetch{URLs: []string{"https://github.com/a/b/x"}, SHA256: hex.EncodeToString(sum[:])}, dest, fetch.Reporter{})
	}()
	g.finish(g.nextAction(contracts.ActionAssetFetch), false, "下载失败：github.com 返回 HTTP 403", nil)
	if err := <-done; err == nil || !strings.Contains(err.Error(), "HTTP 403") {
		t.Fatalf("failure reason = %v", err)
	}
	// Gate 离线：不下发，直接报错
	old := time.Now().Add(-time.Hour)
	_ = s.Store.Ent.Gate.UpdateOneID(int(g.id)).SetLastSeenAt(old).Exec(context.Background())
	if err := s.relayFetch(context.Background(), int(g.id), contracts.AssetFetch{SHA256: hex.EncodeToString(sum[:])}, dest, fetch.Reporter{}); err == nil || !strings.Contains(err.Error(), "离线") {
		t.Fatalf("offline = %v", err)
	}
}

// fakeWeirRelease 扮演 GitHub 上的 Weir 发布：version 的两个架构发布包。direct 为假时下载地址返回 502（只能经 VPS 中转）。
func fakeWeirRelease(t *testing.T, s *Server, version string, direct bool) (map[string][]byte, *ghrelease.Client) {
	t.Helper()
	assets := map[string][]byte{}
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/shusfun/weir/releases/tags/v"+version || r.URL.Path == "/repos/shusfun/weir/releases":
			var as []map[string]any
			for _, arch := range []string{"amd64", "arm64"} {
				name := releaseAssetName(version, arch)
				sum := sha256.Sum256(assets[name])
				as = append(as, map[string]any{"name": name, "browser_download_url": srv.URL + "/dl/" + name, "digest": "sha256:" + hex.EncodeToString(sum[:]), "size": len(assets[name])})
			}
			rel := map[string]any{"tag_name": "v" + version, "assets": as}
			if strings.HasSuffix(r.URL.Path, "/releases") {
				_ = json.NewEncoder(w).Encode([]any{rel})
				return
			}
			_ = json.NewEncoder(w).Encode(rel)
		case strings.HasPrefix(r.URL.Path, "/dl/") && direct:
			_, _ = w.Write(assets[strings.TrimPrefix(r.URL.Path, "/dl/")])
		default:
			w.WriteHeader(502)
		}
	}))
	t.Cleanup(srv.Close)
	for _, arch := range []string{"amd64", "arm64"} {
		assets[releaseAssetName(version, arch)] = []byte("weir package " + arch)
	}
	u, _ := url.Parse(srv.URL)
	s.GitHub = &ghrelease.Client{HTTP: srv.Client(), API: srv.URL, Host: u.Host}
	return assets, s.GitHub
}

// 发布包直连失败时经 VPS 中转下载（内核、Gate 安装包、中心自更新的预下载都走这条管线），按官方摘要校验。
func TestWeirPackageViaGateRelay(t *testing.T) {
	s, _, g := setupGate(t)
	assets, gh := fakeWeirRelease(t, s, "0.1.20", false)
	gh.Sources = []ghrelease.Source{{Kind: ghrelease.SourceDirect}, {Kind: ghrelease.SourceGate, GateID: g.id}}
	dest := filepath.Join(t.TempDir(), "weir.tar.gz")
	job := s.jobs.Start("test-relay", "test", "test", func(ctx context.Context, run JobRun) (map[string]any, error) {
		return nil, s.fetchWeirPackage(ctx, run, "0.1.20", "arm64", dest)
	})
	a := g.nextAction(contracts.ActionAssetFetch)
	urls, _ := a.Payload["urls"].([]any)
	if len(urls) != 1 || !strings.HasSuffix(urls[0].(string), releaseAssetName("0.1.20", "arm64")) {
		t.Fatalf("relay urls = %v", a.Payload["urls"])
	}
	body := assets[releaseAssetName("0.1.20", "arm64")]
	if code, b := g.rawCall("PUT", "/api/v1/agent/uploads/"+a.Payload["upload_id"].(string)+"?offset=0", body); code != 200 {
		t.Fatalf("upload = %d %s", code, b)
	}
	g.finish(a, true, "", nil)
	j, _ := s.jobs.Wait(context.Background(), job.ID)
	if j.Status != jobDone {
		t.Fatalf("job = %+v", j)
	}
	if got, _ := os.ReadFile(dest); !bytes.Equal(got, body) {
		t.Fatal("relayed content mismatch")
	}
}

// 心跳：Gate 版本与中心不同时带 Release；正式构建带摘要与按来源顺序的地址，单机的本机 Gate 不带。
// 手动更新下发 release.replace。
func TestHeartbeatReleaseAndManualUpdate(t *testing.T) {
	s, admin, g := setupGate(t)
	// 开发构建：只给版本号
	r := g.heartbeat("0.0.1")
	if r.Release == nil || r.Release.Version != buildinfo.Version || len(r.Release.URLs) != 0 {
		t.Fatalf("dev release = %+v", r.Release)
	}
	if r = g.heartbeat(buildinfo.Version); r.Release != nil {
		t.Fatalf("same version = %+v", r.Release)
	}
	if w := admin.do("POST", "/api/v1/admin/gates/"+itoa(int(g.id))+"/update", nil, nil); w.Code != 409 || !strings.Contains(w.Body.String(), "gate_up_to_date") {
		t.Fatalf("up to date = %d %s", w.Code, w.Body.String())
	}

	old := buildinfo.Version
	buildinfo.Version = "0.1.20"
	t.Cleanup(func() { buildinfo.Version = old })
	assets, gh := fakeWeirRelease(t, s, "0.1.20", true)
	gh.Sources = []ghrelease.Source{{Kind: ghrelease.SourceMirror, Prefix: "https://m.example/"}, {Kind: ghrelease.SourceGate, GateID: g.id}, {Kind: ghrelease.SourceDirect}}
	var rel *contracts.ReleaseTarget
	for i := 0; i < 100 && (rel == nil || len(rel.URLs) == 0); i++ {
		rel = g.heartbeat("0.1.19").Release
		time.Sleep(20 * time.Millisecond)
	}
	sum := sha256.Sum256(assets[releaseAssetName("0.1.20", "amd64")])
	if rel == nil || rel.SHA256 != hex.EncodeToString(sum[:]) || len(rel.URLs) != 2 || !strings.HasPrefix(rel.URLs[0], "https://m.example/https://") {
		t.Fatalf("release = %+v", rel)
	}
	// 存下的 self_update 带上进度字段
	code, _ := g.call("POST", "/api/v1/agent/heartbeat", contracts.Heartbeat{Version: "0.1.19", Arch: "amd64", SelfUpdate: &contracts.SelfUpdateStatus{Version: "0.1.20", State: "downloading", Done: 10, Total: 100, Source: "m.example", At: time.Now()}})
	gv := mustJSON(t, admin, "GET", "/api/v1/admin/gates/"+itoa(int(g.id)), nil, 200)
	su, _ := gv["self_update"].(map[string]any)
	if code != 200 || su["state"] != "downloading" || su["source"] != "m.example" || su["total"] != float64(100) {
		t.Fatalf("self_update = %v", gv["self_update"])
	}

	res := mustJSON(t, admin, "POST", "/api/v1/admin/gates/"+itoa(int(g.id))+"/update", nil, 202)
	a := g.nextAction(contracts.ActionReleaseReplace)
	if a.Payload["version"] != "0.1.20" || a.Payload["sha256"] != hex.EncodeToString(sum[:]) || len(a.Payload["urls"].([]any)) != 2 || res["action_id"] == nil {
		t.Fatalf("payload = %v res = %v", a.Payload, res)
	}

	// 单机的本机 Gate（经回环连中心）随中心一起更新
	_ = s.Store.Ent.Gate.UpdateOneID(int(g.id)).SetCenterURL("http://127.0.0.1:19810").Exec(context.Background())
	code, body := g.call("POST", "/api/v1/agent/heartbeat", contracts.Heartbeat{Version: "0.1.19", Arch: "amd64", Center: "http://127.0.0.1:19810"})
	var hr contracts.HeartbeatReply
	_ = json.Unmarshal(body, &hr)
	if code != 200 || hr.Release != nil {
		t.Fatalf("colocated release = %+v", hr.Release)
	}
	_ = s.Store.Ent.Gate.UpdateOneID(int(g.id)).SetCenterURL("").SetLastSeenAt(time.Now().Add(-time.Hour)).Exec(context.Background())
	if w := admin.do("POST", "/api/v1/admin/gates/"+itoa(int(g.id))+"/update", nil, nil); w.Code != 409 || !strings.Contains(w.Body.String(), "gate_offline") {
		t.Fatalf("offline = %d %s", w.Code, w.Body.String())
	}
}

// 测速：中心测直连与镜像，VPS 测它自己（在线但未加入的 VPS 排在最后）。
func TestGitHubSourcesProbe(t *testing.T) {
	t.Setenv("WEIR_GITHUB_MIRRORS", "off")
	actionInline = 10 * time.Second
	t.Cleanup(func() { actionInline = 2 * time.Second })
	s, admin, g := setupGate(t)
	fakeWeirRelease(t, s, "0.1.20", true)
	go func() {
		a := g.nextAction(contracts.ActionSourceProbe)
		g.finish(a, true, "", map[string]any{"ttfb_ms": 12, "bytes": 1000, "speed": 5000})
	}()
	res := mustJSON(t, admin, "POST", "/api/v1/admin/github-sources/test", map[string]any{}, 200)
	items, _ := res["results"].([]any)
	if len(items) < 2 {
		t.Fatalf("results = %v", res)
	}
	first := items[0].(map[string]any)
	if first["kind"] != "direct" || first["configured"] != true || first["error"] != nil || first["bytes"].(float64) <= 0 {
		t.Fatalf("direct = %v", first)
	}
	last := items[len(items)-1].(map[string]any)
	if last["kind"] != "gate" || last["configured"] != false || last["gate_name"] != "HK-1" || last["speed"] != float64(5000) {
		t.Fatalf("gate = %v", last)
	}
}

// 只测一个来源：已保存的直连只返回这一项；不在列表里的镜像拒绝；在线但未加入的 VPS 可以单测。
func TestGitHubSourceProbeOne(t *testing.T) {
	t.Setenv("WEIR_GITHUB_MIRRORS", "off")
	actionInline = 10 * time.Second
	t.Cleanup(func() { actionInline = 2 * time.Second })
	s, admin, g := setupGate(t)
	fakeWeirRelease(t, s, "0.1.20", true)
	res := mustJSON(t, admin, "POST", "/api/v1/admin/github-sources/test", map[string]any{"source": map[string]any{"kind": "direct"}}, 200)
	items, _ := res["results"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["kind"] != "direct" || items[0].(map[string]any)["configured"] != true {
		t.Fatalf("one = %v", res)
	}
	if w := admin.do("POST", "/api/v1/admin/github-sources/test", map[string]any{"source": map[string]any{"kind": "mirror", "prefix": "https://no.example/"}}, nil); w.Code != 400 || !strings.Contains(w.Body.String(), "probe_source_invalid") {
		t.Fatalf("unknown mirror = %d %s", w.Code, w.Body.String())
	}
	go func() {
		a := g.nextAction(contracts.ActionSourceProbe)
		g.finish(a, true, "", map[string]any{"ttfb_ms": 8, "bytes": 1000, "speed": 4000})
	}()
	res = mustJSON(t, admin, "POST", "/api/v1/admin/github-sources/test", map[string]any{"source": map[string]any{"kind": "gate", "gate_id": g.id}}, 200)
	items, _ = res["results"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["kind"] != "gate" || items[0].(map[string]any)["configured"] != false || items[0].(map[string]any)["speed"] != float64(4000) {
		t.Fatalf("one gate = %v", res)
	}
}

// 全部测速转成任务后，直连的结果在 VPS 回复之前就已经写进任务。
func TestGitHubSourcesProbeStreams(t *testing.T) {
	t.Setenv("WEIR_GITHUB_MIRRORS", "off")
	actionInline = 40 * time.Millisecond
	t.Cleanup(func() { actionInline = 2 * time.Second })
	s, admin, g := setupGate(t)
	fakeWeirRelease(t, s, "0.1.20", true)
	release := make(chan struct{})
	go func() {
		a := g.nextAction(contracts.ActionSourceProbe)
		<-release
		g.finish(a, true, "", map[string]any{"ttfb_ms": 12, "bytes": 1000, "speed": 5000})
	}()
	res := mustJSON(t, admin, "POST", "/api/v1/admin/github-sources/test", map[string]any{}, 202)
	id, _ := res["job"].(map[string]any)["id"].(string)
	if id == "" {
		t.Fatalf("job = %v", res["job"])
	}
	deadline := time.Now().Add(5 * time.Second)
	var partial map[string]any
	for time.Now().Before(deadline) {
		j := mustJSON(t, admin, "GET", "/api/v1/admin/jobs/"+url.PathEscape(id), nil, 200)["job"].(map[string]any)
		if j["status"] != "running" {
			t.Fatalf("job ended before the gate probe: %v", j)
		}
		out, _ := j["output"].(map[string]any)
		items, _ := out["results"].([]any)
		if len(items) >= 1 && items[0].(map[string]any)["kind"] == "direct" && items[0].(map[string]any)["bytes"].(float64) > 0 {
			partial = j
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if partial == nil {
		t.Fatal("direct result was not published while the gate probe was still running")
	}
	close(release)
	done := waitJob(t, admin, id)
	items, _ := done["output"].(map[string]any)["results"].([]any)
	if done["status"] != "done" || len(items) < 2 || items[len(items)-1].(map[string]any)["kind"] != "gate" {
		t.Fatalf("done = %v", done)
	}
}
