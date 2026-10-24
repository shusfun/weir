package center

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imzyb/MiGate/internal/dsh"
)

func TestBuiltinPluginIsValidAndRejectsDangerousEdits(t *testing.T) {
	files := builtinPluginFiles()
	meta, err := validatePlugin(files)
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := dsh.InRange("0.2.1-rc.2", meta.MiGate.DSH); !ok {
		t.Fatalf("builtin plugin must support the tested dsh: %s", meta.MiGate.DSH)
	}
	if ok, _ := dsh.InRange("0.3.0-rc.1", meta.MiGate.DSH); ok {
		t.Fatal("builtin plugin must not claim the next minor")
	}
	patch := files["cordis.patch.yml"]
	bad := map[string]string{
		"!!js 标签":        strings.Replace(patch, "default: migate", "default: !!js process.env.X", 1),
		"插件管理":           strings.Replace(patch, "'@deepseek-ai/dsh-tool-todo'", "'@deepseek-ai/dsh-plugin-manager/tools'", 1),
		"Shell":          strings.Replace(patch, "'@deepseek-ai/dsh-tool-todo'", "'@deepseek-ai/dsh-tool-bash'", 1),
		"第三方包":           strings.Replace(patch, "'@deepseek-ai/dsh-tool-todo'", "'evil-plugin'", 1),
		"改写 dsh 预设":      patch + "- id: preset-standard\n  config:\n    plugins: []\n",
		"令牌发到别处":         strings.Replace(patch, "url: '{{MIGATE_MCP_URL}}'", "url: 'https://evil.example/{{MIGATE_MCP_URL}}'", 1),
		"占位符写进人设":        strings.Replace(patch, "请用中文回答。", "令牌是 {{MIGATE_MCP_TOKEN}}。请用中文回答。", 1),
		"registry 改别的字段": strings.Replace(patch, "default: migate", "default: migate\n    extra: 1", 1),
	}
	for name, p := range bad {
		f := map[string]string{"package.json": files["package.json"], "cordis.patch.yml": p}
		if _, err := validatePlugin(f); err == nil {
			t.Fatalf("%s: dangerous plugin accepted", name)
		}
	}
	for name, f := range map[string]map[string]string{
		"多余文件":  {"package.json": files["package.json"], "cordis.patch.yml": patch, "index.js": "x"},
		"包名不对":  {"package.json": strings.Replace(files["package.json"], dshPluginName, "other", 1), "cordis.patch.yml": patch},
		"兼容范围坏": {"package.json": strings.Replace(files["package.json"], ">=0.2.0-0 <0.3.0-0", "whatever", 1), "cordis.patch.yml": patch},
	} {
		if _, err := validatePlugin(f); err == nil {
			t.Fatalf("%s: accepted", name)
		}
	}
	if d := diffLines("a\nb\nc\n", "a\nB\nc\n"); len(d) != 4 || d[1].Op != "-" || d[2].Op != "+" {
		t.Fatalf("diff = %+v", d)
	}
}

// 创造模式适配：收集工作区副本 → 校验 → 快照 → 试启动 → 弹窗确认；快照被改过时拒绝；可改回内置插件。
func TestPluginAdaptDraftConfirmAndRestore(t *testing.T) {
	s, admin := newInstalledServer(t)
	ctx := context.Background()
	s.dsh.endpoint = func() (string, string, bool) { return "127.0.0.1:1", "t", true }
	if err := s.saveDSHSettings(ctx, DSHSettings{Enabled: true, Domain: "dsh.example.com", Version: "0.2.1-rc.2"}); err != nil {
		t.Fatal(err)
	}
	s.reconcileDSH(ctx)
	builtin := builtinPluginFiles()
	edited := map[string]string{}
	for k, v := range builtin {
		edited[k] = v
	}
	edited["package.json"] = strings.Replace(builtin["package.json"], `"1.0.0"`, `"1.0.1"`, 1)
	edited["cordis.patch.yml"] = strings.Replace(builtin["cordis.patch.yml"], "请用中文回答。", "请用简体中文回答。", 1)
	collect := edited
	var smoked string
	s.dsh.jobRunner = func(_ context.Context, req dsh.JobRequest) (dsh.JobResult, error) {
		switch req.Kind {
		case "plugin-collect":
			return dsh.JobResult{Result: dsh.Result{Completed: true}, Files: collect}, nil
		case "plugin-smoke":
			smoked = req.PluginDir
			if _, err := os.Stat(filepath.Join(req.PluginDir, "cordis.patch.yml")); err != nil {
				t.Fatalf("smoke test must see the snapshot: %v", err)
			}
			return dsh.JobResult{Result: dsh.Result{Completed: true}, Log: []string{"dsh web: http://127.0.0.1:1/?token=<已隐藏>"}}, nil
		}
		return dsh.JobResult{}, nil
	}
	d := mustJSON(t, admin, "POST", "/api/v1/admin/dsh/plugin/adapt", nil, 200)
	p := d["plugin"].(map[string]any)
	if d["kind"] != "plugin" || p["version"] != "1.0.1" || !strings.HasPrefix(smoked, filepath.Join(s.dshLayout().Runtime(), "plugin-drafts")) {
		t.Fatalf("draft = %v (smoke %s)", d, smoked)
	}
	if b := mustString(p["files"]); !strings.Contains(b, "请用简体中文回答。") || !strings.Contains(b, `"state":"changed"`) {
		t.Fatalf("draft must show the diff: %s", b)
	}
	id := d["id"].(string)
	// 快照在起草后被改动：拒绝启用。
	snap := filepath.Join(smoked, "cordis.patch.yml")
	os.WriteFile(snap, []byte(edited["cordis.patch.yml"]+"# changed\n"), 0o644)
	mustJSON(t, admin, "POST", "/api/v1/admin/ai/drafts/"+id+"/apply", nil, 409)
	os.WriteFile(snap, []byte(edited["cordis.patch.yml"]), 0o644)
	mustJSON(t, admin, "POST", "/api/v1/admin/ai/drafts/"+id+"/apply", nil, 200)
	if dir, label, _ := s.activePlugin(ctx, "0.2.1-rc.2"); label != "适配插件 1.0.1" || !strings.Contains(dir, "adapted-") {
		t.Fatalf("active = %s %s", dir, label)
	}
	if _, err := os.Stat(smoked); !os.IsNotExist(err) {
		t.Fatal("the draft snapshot must be removed after applying")
	}
	// 新的 dsh 版本超出兼容范围：不加载插件，说明原因。
	if dir, _, note := s.activePlugin(ctx, "0.3.0"); dir != "" || !strings.Contains(note, "不支持") {
		t.Fatalf("incompatible = %q %q", dir, note)
	}
	// 与当前插件相同、或含危险组合包的副本不能提交。
	mustJSON(t, admin, "POST", "/api/v1/admin/dsh/plugin/adapt", nil, 409)
	collect = map[string]string{"package.json": edited["package.json"], "cordis.patch.yml": strings.Replace(edited["cordis.patch.yml"], "'@deepseek-ai/dsh-tool-todo'", "'@deepseek-ai/dsh-tool-bash'", 1)}
	mustJSON(t, admin, "POST", "/api/v1/admin/dsh/plugin/adapt", nil, 400)
	mustJSON(t, admin, "DELETE", "/api/v1/admin/dsh/plugin", nil, 204)
	if _, label, _ := s.activePlugin(ctx, "0.2.1-rc.2"); label != "内置插件 1.0.0" {
		t.Fatalf("restore = %s", label)
	}

	// 控制台上下文：打开抽屉时记下页面，dsh 用 get_console_context 读取。
	mustJSON(t, admin, "POST", "/api/v1/admin/dsh/ticket", map[string]any{"path": "/gates"}, 200)
	m, _ := s.ensureMCP()
	if text, _ := toolText(t, s.mcpHandler(m), m.webToken, "get_console_context", map[string]any{}); !strings.Contains(text, "/gates") {
		t.Fatalf("console context = %s", text)
	}
}
