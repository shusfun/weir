package dsh

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func tarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		tw.Write([]byte(body))
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func TestEnsureNodeAcceptsOnlyOfficialDigest(t *testing.T) {
	arch := nodeArch()
	if arch == "" {
		t.Skip("unsupported arch")
	}
	good := tarGz(t, map[string]string{"node-v22.30.1/bin/node": "real-node"})
	sum := sha256.Sum256(good)
	name := fmt.Sprintf("node-v22.30.1-linux-%s.tar.gz", arch)
	official := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/index.json":
			w.Write([]byte(`[{"version":"v24.1.0"},{"version":"v22.30.1"},{"version":"v22.18.0"}]`))
		case "/v22.30.1/SHASUMS256.txt":
			fmt.Fprintf(w, "%s  %s\n", hex.EncodeToString(sum[:]), name)
		case "/v22.30.1/" + name:
			w.Write(good)
		default:
			w.WriteHeader(404)
		}
	}))
	defer official.Close()
	tampered := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(tarGz(t, map[string]string{"node-v22.30.1/bin/node": "evil"}))
	}))
	defer tampered.Close()

	in := Installer{Dir: t.TempDir(), NodeOfficial: official.URL, NodeMirrors: []string{tampered.URL}}
	bin, err := in.EnsureNode(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(bin); string(b) != "real-node" || !strings.Contains(bin, "v22.30.1") {
		t.Fatalf("installed %s = %q", bin, b)
	}
	if in.NodeBin() != bin {
		t.Fatal("NodeBin must find the installed runtime")
	}
	// 所有来源都与官方摘要不一致时拒绝安装。
	bad := Installer{Dir: t.TempDir(), NodeOfficial: official.URL, NodeMirrors: []string{tampered.URL}}
	official.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/index.json":
			w.Write([]byte(`[{"version":"v22.30.1"}]`))
		case "/v22.30.1/SHASUMS256.txt":
			fmt.Fprintf(w, "%s  %s\n", strings.Repeat("0", 64), name)
		default:
			w.Write(good)
		}
	})
	if _, err := bad.EnsureNode(t.Context()); err == nil {
		t.Fatal("digest mismatch must be rejected")
	}
}

func TestExtractRejectsTraversal(t *testing.T) {
	archive := tarGz(t, map[string]string{"root/../../escape": "x"})
	if err := extractTarGz(archive, filepath.Join(t.TempDir(), "out"), 1); err == nil {
		t.Fatal("path traversal must be rejected")
	}
}

func TestReleasesAndOfficialIntegrity(t *testing.T) {
	reg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.EscapedPath() {
		case "/@deepseek-ai%2Fdsh":
			w.Write([]byte(`{"dist-tags":{"latest":"0.2.1-rc.2","next":"0.2.1-rc.3"},"time":{"0.2.1-rc.2":"2026-10-01T00:00:00Z","0.2.1-rc.3":"2026-10-10T00:00:00Z","bad/../x":"2026-10-11T00:00:00Z"},"versions":{"0.2.1-rc.2":{},"0.2.1-rc.3":{},"bad/../x":{}}}`))
		case "/@deepseek-ai%2Fdsh/0.2.1-rc.2":
			w.Write([]byte(`{"name":"@deepseek-ai/dsh","dist":{"integrity":"sha512-abc"}}`))
		case "/@deepseek-ai%2Fdsh/9.9.9":
			w.Write([]byte(`{"name":"evil-dsh","dist":{"integrity":"sha512-abc"}}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer reg.Close()
	in := Installer{Dir: t.TempDir(), Registries: []string{reg.URL}}
	list, err := in.Releases(t.Context())
	if err != nil || len(list) != 2 || list[0].Version != "0.2.1-rc.3" || list[0].Tags[0] != "next" {
		t.Fatalf("releases = %+v %v", list, err)
	}
	if v, err := in.officialIntegrity(t.Context(), "0.2.1-rc.2"); err != nil || v != "sha512-abc" {
		t.Fatalf("integrity = %q %v", v, err)
	}
	if _, err := in.officialIntegrity(t.Context(), "9.9.9"); err == nil {
		t.Fatal("a package with another name must be rejected")
	}
	lock := filepath.Join(t.TempDir(), "package-lock.json")
	os.WriteFile(lock, []byte(`{"packages":{"node_modules/@deepseek-ai/dsh":{"version":"0.2.1-rc.2","integrity":"sha512-abc"}}}`), 0o644)
	if got, _ := lockIntegrity(lock); got != "sha512-abc" {
		t.Fatalf("lock integrity = %q", got)
	}
	for _, v := range []string{"../x", "1.2", "1.2.3; rm", ""} {
		if ValidVersion(v) {
			t.Fatalf("accepted version %q", v)
		}
	}
}

func TestParseEventsUsesStreamNotExitCode(t *testing.T) {
	stream := `{"type":"session","sessionId":"s"}
{"type":"tool_call","callId":"1","tool":"mcp__migate__submit_defense_draft","input":{}}
{"type":"status","phase":"turn_end","turn":1,"reason":{"kind":"completed"}}
{"type":"final","text":"已提交"}
`
	r := parseEvents(strings.NewReader(stream))
	if !r.Completed || r.Final != "已提交" || len(r.ToolCalls) != 1 {
		t.Fatalf("result = %+v", r)
	}
	if r := parseEvents(strings.NewReader(`{"type":"error","message":"boom"}`)); r.Completed || r.Error != "boom" {
		t.Fatalf("error result = %+v", r)
	}
}

// 工作区：写 AGENTS.md（插件副本路径已填入）、工作区补丁；插件补丁填入 MCP 地址与令牌；
// 工作区里正在改的插件副本在插件来源不变时保留；试启动只接受 plugin-drafts 下的目录。
func TestPrepareWorkspaceAndPluginCopy(t *testing.T) {
	l := Layout{Root: t.TempDir()}
	src := filepath.Join(l.Runtime(), "plugins", "builtin-x")
	os.MkdirAll(src, 0o755)
	os.WriteFile(filepath.Join(src, "cordis.patch.yml"), []byte("url: '"+PlaceholderMCPURL+"'\nauth: 'Bearer "+PlaceholderMCPToken+"'\n"), 0o644)
	os.WriteFile(filepath.Join(src, "package.json"), []byte(`{"name":"x"}`), 0o644)
	a := &agent{l: l}
	d := Desired{Version: "0.2.1-rc.2", WebPatch: "# base\n", Instructions: "副本在 " + placeholderCopy, PluginDir: src, MCPURL: "http://127.0.0.1:9/mcp", MCPToken: "tok"}
	base, plugin, perr, err := a.prepareWorkspace(d)
	if err != nil || perr != "" || len(base) != 2 || plugin == "" {
		t.Fatalf("prepare = %v %q %q %v", base, plugin, perr, err)
	}
	notes, _ := os.ReadFile(filepath.Join(l.State(), "home", "AGENTS.md"))
	if !strings.Contains(string(notes), l.PluginCopy()) {
		t.Fatalf("AGENTS.md = %s", notes)
	}
	if b, _ := os.ReadFile(base[0]); !strings.Contains(string(b), "documentsDirectory") || !strings.Contains(string(b), "workspace") {
		t.Fatalf("workspace patch = %s", b)
	}
	if b, _ := os.ReadFile(plugin); !strings.Contains(string(b), "http://127.0.0.1:9/mcp") || !strings.Contains(string(b), "Bearer tok") || strings.Contains(string(b), "{{") {
		t.Fatalf("plugin patch = %s", b)
	}
	copied := filepath.Join(l.PluginCopy(), "package.json")
	os.WriteFile(copied, []byte(`{"name":"editing"}`), 0o600)
	a.prepareWorkspace(d)
	if b, _ := os.ReadFile(copied); string(b) != `{"name":"editing"}` {
		t.Fatal("the copy being edited must be kept while the plugin source is unchanged")
	}
	if res := a.collectPlugin(); res.Error != "" || res.Files["package.json"] != `{"name":"editing"}` {
		t.Fatalf("collect = %+v", res)
	}
	if res := a.smokePlugin(t.Context(), d, src); !strings.Contains(res.Error, "插件目录无效") {
		t.Fatalf("smoke outside plugin-drafts = %+v", res)
	}
}

// 管理进程的文件协议：没有版本时报告 stopped 且不带令牌；任务编号非法的请求被忽略；
// dsh 未启用时任务返回原因；中心删除请求后结果一并清理。
func TestAgentFileProtocol(t *testing.T) {
	l := Layout{Root: t.TempDir()}
	for _, d := range []string{filepath.Join(l.Control(), "jobs"), filepath.Join(l.Status(), "jobs"), l.State()} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := WriteShared(filepath.Join(l.Control(), "desired.json"), Desired{Generation: 7}); err != nil {
		t.Fatal(err)
	}
	id := strings.Repeat("ab", 16)
	req := filepath.Join(l.Control(), "jobs", id+".json")
	WriteShared(req, JobRequest{ID: id, Task: "分析安全事件 1"})
	WriteShared(filepath.Join(l.Control(), "jobs", "..evil.json"), JobRequest{ID: "..evil", Task: "x"})
	a := &agent{l: l}
	a.step(t.Context())
	var st AgentStatus
	if err := ReadJSON(filepath.Join(l.Status(), "status.json"), &st); err != nil || st.State != "stopped" || st.Generation != 7 || st.Token != "" {
		t.Fatalf("status = %+v %v", st, err)
	}
	out := filepath.Join(l.Status(), "jobs", id+".json")
	var res JobResult
	for i := 0; i < 200 && ReadJSON(out, &res) != nil; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if res.ID != id || res.Error != "dsh 未启用" {
		t.Fatalf("job result = %+v", res)
	}
	if _, err := os.Stat(filepath.Join(l.Status(), "jobs", "..evil.json")); err == nil {
		t.Fatal("invalid job ids must be ignored")
	}
	for a.busy.Load() {
		time.Sleep(10 * time.Millisecond)
	}
	os.Remove(req)
	a.step(t.Context())
	if _, err := os.Stat(out); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the result must be removed once the center deletes the request")
	}
}
