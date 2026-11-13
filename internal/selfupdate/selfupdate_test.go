package selfupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/ghrelease"
)

func pkg(t *testing.T, files map[string]string) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		_ = tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg})
		_, _ = tw.Write([]byte(body))
	}
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

type env struct {
	ran       [][]string
	dirs      []string
	body      []byte
	downloads int
	host      string
}

// setup 准备临时目录、假 GitHub 与记录安装参数的安装器。tamper 为真时下载内容与摘要不一致。
func setup(t *testing.T, roleName string, tamper bool) *env {
	root := t.TempDir()
	Dir, WorkDir, Base = filepath.Join(root, "update"), filepath.Join(root, "work"), filepath.Join(root, "opt")
	RoleFile, CenterCfg = filepath.Join(root, "role"), filepath.Join(root, "config.json")
	for _, d := range []string{Dir, Base} {
		_ = os.MkdirAll(d, 0o755)
	}
	if roleName != "" {
		_ = os.WriteFile(RoleFile, []byte(roleName+"\n"), 0o644)
	}
	_ = os.WriteFile(CenterCfg, []byte(`{"listen":"127.0.0.1:20000"}`), 0o600)
	name := "weir-0.1.14-linux-" + runtime.GOARCH + ".tar.gz"
	body := pkg(t, map[string]string{"weir": "new", "install.sh": "#!/bin/sh\n", "SHA256SUMS": "x", "VERSION": "0.1.14\n"})
	sum := sha256.Sum256(body)
	e := &env{body: body}
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/shusfun/weir/releases/tags/v0.1.14":
			_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": "v0.1.14", "assets": []map[string]any{{"name": name, "browser_download_url": srv.URL + "/dl/" + name, "digest": "sha256:" + hex.EncodeToString(sum[:])}}})
		case "/dl/" + name:
			e.downloads++
			if tamper {
				_, _ = w.Write(append([]byte("x"), body...))
				return
			}
			http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(body))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	e.host = u.Host
	GitHub = func() ghrelease.Client { return ghrelease.Client{HTTP: srv.Client(), API: srv.URL, Host: u.Host} }
	runInstaller = func(_ context.Context, dir string, args []string, log func(string)) error {
		e.ran = append(e.ran, args)
		e.dirs = append(e.dirs, dir)
		b, _ := os.ReadFile(filepath.Join(dir, "weir"))
		log("installer saw weir=" + string(b))
		return nil
	}
	return e
}

func request(t *testing.T, r Request) {
	t.Helper()
	if err := WriteRequest(r); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateDownloadsVerifiesAndRunsInstaller(t *testing.T) {
	e := setup(t, "standalone", false)
	request(t, Request{ID: "req-00000001", Action: "update", Version: "0.1.14"})
	if err := Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(e.ran) != 1 || strings.Join(e.ran[0], " ") != "standalone 127.0.0.1:20000" {
		t.Fatalf("installer args: %v", e.ran)
	}
	st, ok := ReadStatus()
	if !ok || st.State != "done" || st.ID != "req-00000001" || !strings.Contains(strings.Join(st.Logs, "\n"), "installer saw weir=new") {
		t.Fatalf("status: %+v", st)
	}
	if _, err := os.Stat(filepath.Join(Dir, "request.json")); err == nil {
		t.Fatal("request must be consumed")
	}
}

func TestDigestMismatchNeverInstalls(t *testing.T) {
	e := setup(t, "center", true)
	request(t, Request{ID: "req-00000002", Action: "update", Version: "0.1.14"})
	if err := Apply(context.Background()); err == nil {
		t.Fatal("tampered package must fail")
	}
	if len(e.ran) != 0 {
		t.Fatal("installer must not run")
	}
	if st, _ := ReadStatus(); st.State != "failed" || !strings.Contains(st.Error, "摘要") {
		t.Fatalf("status: %+v", st)
	}
}

func TestRejectsBadRequests(t *testing.T) {
	e := setup(t, "standalone", false)
	for _, r := range []Request{
		{ID: "req-00000003", Action: "update", Version: "0.1.0"},         // 改名前的版本
		{ID: "req-00000004", Action: "update", Version: "1.0.0; rm -rf"}, // 不是版本号
		{ID: "req-00000005", Action: "shell"},
		{ID: "x", Action: "rollback"},
	} {
		if err := WriteRequest(r); err == nil {
			t.Fatalf("WriteRequest accepted %+v", r)
		}
		// 中心被攻破时可能绕过 WriteRequest 直接写文件：root 一侧同样拒绝。
		b, _ := json.Marshal(r)
		_ = os.WriteFile(filepath.Join(Dir, "request.json"), b, 0o640)
		if err := Apply(context.Background()); err == nil {
			t.Fatalf("Apply accepted %+v", r)
		}
	}
	// 请求文件是符号链接：拒绝，不读取目标。
	secret := filepath.Join(t.TempDir(), "secret")
	_ = os.WriteFile(secret, []byte(`{"id":"req-00000006","action":"rollback"}`), 0o600)
	_ = os.Symlink(secret, filepath.Join(Dir, "request.json"))
	if err := Apply(context.Background()); err == nil {
		t.Fatal("symlinked request must be refused")
	}
	if len(e.ran) != 0 {
		t.Fatal("installer must not run for invalid requests")
	}
}

func TestFixedHostActionsAcceptNoCallerControlledArguments(t *testing.T) {
	for _, action := range []string{"enable-local-gate", "prepare-laya"} {
		r := Request{ID: "fixed-op-0001", Action: action, TraceID: strings.Repeat("a", 32)}
		if err := r.Valid(); err != nil {
			t.Fatalf("%s rejected: %v", action, err)
		}
		r.Version = "0.1.14"
		if err := r.Valid(); err == nil {
			t.Fatalf("%s accepted a version", action)
		}
		r.Version = ""
		r.Sources = []ghrelease.Source{{Kind: "direct"}}
		if err := r.Valid(); err == nil {
			t.Fatalf("%s accepted sources", action)
		}
	}
	if err := (Request{ID: "fixed-op-0001", Action: "prepare-laya", TraceID: "not-a-trace"}).Valid(); err == nil {
		t.Fatal("accepted malformed trace id")
	}
}

func TestApplyConsumesMalformedRequest(t *testing.T) {
	setup(t, "center", false)
	path := filepath.Join(Dir, "request.json")
	if err := os.WriteFile(path, []byte("{"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := Apply(context.Background()); err == nil {
		t.Fatal("malformed request must fail")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("malformed request was not consumed: %v", err)
	}
}

func TestPendingHostOperationSurvivesPageReload(t *testing.T) {
	setup(t, "center", false)
	previous := &writer{st: Status{ID: "previous-operation", Action: "prepare-laya", State: "done"}}
	previous.finish()
	request := Request{ID: "pending-operation", Action: "enable-local-gate", TraceID: strings.Repeat("b", 32)}
	if err := WriteRequest(request); err != nil {
		t.Fatal(err)
	}
	state, ok := ReadStatus()
	if !ok || state.ID != request.ID || state.State != "queued" || state.TraceID != request.TraceID {
		t.Fatal(state, ok)
	}
	started := &writer{st: Status{ID: request.ID, Action: request.Action, State: "installing"}}
	started.log("started")
	if state, ok := ReadStatus(); !ok || state.State != "installing" {
		t.Fatal(state, ok)
	}
}

func TestOperationReceiptSurvivesNewStatus(t *testing.T) {
	setup(t, "center", false)
	first := &writer{st: Status{ID: "local-operation-0001", Action: "enable-local-gate", State: "installing"}}
	first.log("starting")
	if got, ok := ReadOperation(first.st.ID); !ok || got.State != "installing" {
		t.Fatalf("first operation receipt: %+v, %v", got, ok)
	}
	second := &writer{st: Status{ID: "laya-operation-0001", Action: "prepare-laya", State: "installing"}}
	second.log("starting")
	if got, ok := ReadOperation(first.st.ID); !ok || got.State != "installing" {
		t.Fatalf("receipt overwritten by later status: %+v, %v", got, ok)
	}
}

func TestGateOnlyMachineCannotSelfUpdate(t *testing.T) {
	e := setup(t, "gate", false)
	request(t, Request{ID: "req-00000007", Action: "update", Version: "0.1.14"})
	if err := Apply(context.Background()); err == nil || len(e.ran) != 0 {
		t.Fatalf("gate role: err=%v ran=%v", err, e.ran)
	}
}

func TestRollbackReinstallsPreviousRelease(t *testing.T) {
	e := setup(t, "standalone", false)
	if err := WriteRequest(Request{ID: "req-00000008", Action: "rollback"}); err != nil {
		t.Fatal(err)
	}
	if err := Apply(context.Background()); err == nil {
		t.Fatal("no previous release: must fail")
	}
	prev := filepath.Join(Base, "releases", "0.1.13")
	_ = os.MkdirAll(prev, 0o755)
	_ = os.WriteFile(filepath.Join(prev, "weir"), []byte("old"), 0o755)
	_ = os.WriteFile(filepath.Join(prev, "install.sh"), []byte("#!/bin/sh\n"), 0o755)
	_ = os.Symlink("releases/0.1.13", filepath.Join(Base, "previous"))
	if Previous() != "0.1.13" {
		t.Fatalf("Previous() = %q", Previous())
	}
	request(t, Request{ID: "req-00000009", Action: "rollback"})
	if err := Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(e.ran) != 1 || e.dirs[0] == prev {
		t.Fatalf("rollback must reinstall from a copy, ran=%v dirs=%v", e.ran, e.dirs)
	}
	if st, _ := ReadStatus(); st.State != "done" || st.Version != "0.1.13" || !strings.Contains(strings.Join(st.Logs, "\n"), "weir=old") {
		t.Fatalf("status: %+v", st)
	}
}

// 中心预下载的文件按官方摘要校验后直接使用，不再下载；被替换过的文件丢弃，按来源顺序重新下载。
func TestPrefetchedPackageIsVerifiedBeforeUse(t *testing.T) {
	e := setup(t, "standalone", false)
	name := PrefetchName("0.1.14", runtime.GOARCH)
	_ = os.WriteFile(filepath.Join(Dir, name), e.body, 0o640)
	request(t, Request{ID: "req-00000010", Action: "update", Version: "0.1.14"})
	if err := Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	st, _ := ReadStatus()
	if e.downloads != 0 || len(e.ran) != 1 || !strings.Contains(strings.Join(st.Logs, "\n"), "预下载") {
		t.Fatalf("downloads=%d ran=%v logs=%v", e.downloads, e.ran, st.Logs)
	}
	if _, err := os.Stat(filepath.Join(Dir, name)); err == nil {
		t.Fatal("prefetched file must be removed")
	}

	e = setup(t, "standalone", false)
	_ = os.WriteFile(filepath.Join(Dir, name), append([]byte("evil"), e.body...), 0o640)
	request(t, Request{ID: "req-00000011", Action: "update", Version: "0.1.14"})
	if err := Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	st, _ = ReadStatus()
	if e.downloads != 1 || !strings.Contains(strings.Join(st.Logs, "\n"), "与官方摘要不一致") {
		t.Fatalf("downloads=%d logs=%v", e.downloads, st.Logs)
	}
}

// 请求里的来源顺序被采用；只接受直连与 https 镜像前缀，gate 来源与重复项拒绝。
func TestRequestSources(t *testing.T) {
	e := setup(t, "standalone", false)
	bad := []Request{
		{ID: "req-00000020", Action: "update", Version: "0.1.14", Sources: []ghrelease.Source{{Kind: "gate", GateID: 1}}},
		{ID: "req-00000021", Action: "update", Version: "0.1.14", Sources: []ghrelease.Source{{Kind: "mirror", Prefix: "http://x.test/"}}},
		{ID: "req-00000022", Action: "update", Version: "0.1.14", Sources: []ghrelease.Source{{Kind: "direct"}, {Kind: "direct"}}},
		{ID: "req-00000023", Action: "rollback", Sources: []ghrelease.Source{{Kind: "direct"}}},
	}
	for _, r := range bad {
		if err := r.Valid(); err == nil {
			t.Fatalf("accepted %+v", r)
		}
	}
	// 第一个镜像连不上，回到直连（假 GitHub）下载
	request(t, Request{ID: "req-00000024", Action: "update", Version: "0.1.14", Sources: []ghrelease.Source{{Kind: "mirror", Prefix: "https://127.0.0.1:1/"}, {Kind: "direct"}}})
	if err := Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	st, _ := ReadStatus()
	if logs := strings.Join(st.Logs, "\n"); e.downloads != 1 || !strings.Contains(logs, "127.0.0.1:1") || !strings.Contains(logs, "改用 "+e.host) {
		t.Fatalf("downloads=%d logs=%s", e.downloads, logs)
	}
}
