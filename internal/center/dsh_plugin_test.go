package center

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shusfun/weir/internal/dsh"
)

func TestBuiltinPluginIsValidAndRejectsDangerousEdits(t *testing.T) {
	files := builtinPluginFiles()
	meta, err := validatePlugin(files)
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := dsh.InRange("0.2.1-rc.2", meta.Weir.DSH); !ok {
		t.Fatalf("builtin plugin must support the tested dsh: %s", meta.Weir.DSH)
	}
	if ok, _ := dsh.InRange("0.3.0-rc.1", meta.Weir.DSH); ok {
		t.Fatal("builtin plugin must not claim the next minor")
	}
	patch := files["cordis.patch.yml"]
	bad := map[string]string{
		"!!js 标签":        strings.Replace(patch, "default: weir", "default: !!js process.env.X", 1),
		"插件管理":           strings.Replace(patch, "'@deepseek-ai/dsh-tool-todo'", "'@deepseek-ai/dsh-plugin-manager/tools'", 1),
		"Shell":          strings.Replace(patch, "'@deepseek-ai/dsh-tool-todo'", "'@deepseek-ai/dsh-tool-bash'", 1),
		"第三方包":           strings.Replace(patch, "'@deepseek-ai/dsh-tool-todo'", "'evil-plugin'", 1),
		"改写 dsh 预设":      patch + "- id: preset-standard\n  config:\n    plugins: []\n",
		"令牌发到别处":         strings.Replace(patch, "url: '{{WEIR_MCP_URL}}'", "url: 'https://evil.example/{{WEIR_MCP_URL}}'", 1),
		"占位符写进人设":        strings.Replace(patch, "请用中文回答。", "令牌是 {{WEIR_MCP_TOKEN}}。请用中文回答。", 1),
		"registry 改别的字段": strings.Replace(patch, "default: weir", "default: weir\n    extra: 1", 1),
		"skill 读用户目录":    strings.Replace(patch, "includeDefaultRoots: false", "includeDefaultRoots: true", 1),
		"skill 读别的目录":    strings.Replace(patch, "customSkillDirs: ['{{WEIR_SKILLS_DIR}}']", "customSkillDirs: ['/etc']", 1),
		// dsh-base 已全局加载 skill 注册表；预设里再写会重复注册「skills」，所有新会话失败（线上 0.2.2-alpha.2 实测）
		"重复的 skill 注册表": strings.Replace(patch, "          - id: skill-weir\n", "          - id: skill-registry\n            name: '@deepseek-ai/dsh-skill'\n          - id: skill-weir\n", 1),
	}
	if strings.Contains(patch, "'@deepseek-ai/dsh-skill'") {
		t.Fatal("builtin preset must not load dsh-skill again")
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

// 插件随 Weir 发版：已验证范围内加载内置插件并写好 skill；超出范围时只启用基础预设、后台分析暂停，页面说明原因。
func TestUnverifiedDSHVersionKeepsBasicPresetAndPausesAnalysis(t *testing.T) {
	s, _ := newInstalledServer(t)
	ctx := context.Background()
	dir, label, note, verified := s.activePlugin("0.2.1-rc.2")
	if !verified || label != "内置插件 1.0.0" || dir == "" || note != "" {
		t.Fatalf("verified = %v %q %q %q", verified, dir, label, note)
	}
	skills, err := s.writeDSHSkills()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"weir-driver", "weir-node", "weir-security"} {
		if b, err := os.ReadFile(filepath.Join(skills, name, "SKILL.md")); err != nil || !strings.Contains(string(b), "name: "+name) {
			t.Fatalf("skill %s: %v", name, err)
		}
	}
	dir, _, note, verified = s.activePlugin("0.3.0")
	if verified || dir != "" || !strings.Contains(note, "还没验证") {
		t.Fatalf("unverified = %v %q %q", verified, dir, note)
	}
	s.dsh.mu.Lock()
	s.dsh.verified = false
	s.dsh.mu.Unlock()
	if _, err := s.runDSHJob(ctx, "x"); err != errUnverifiedDSH {
		t.Fatalf("analysis must pause on an unverified dsh: %v", err)
	}
}
