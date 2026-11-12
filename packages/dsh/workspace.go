package dsh

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// dsh 的默认工作区放在托管目录中，避免无桌面系统缺少「文档」目录。
// 插件来源、身份和声明由宿主管理，连接信息在启动前填入。

// PluginFiles 是声明式适配插件的文件集合。
var PluginFiles = []string{"package.json", "cordis.patch.yml", "README.md"}

// PluginValidator enforces the caller's declarative plugin policy before loading.
// The callback must not log plugin contents, which may contain sensitive values.
type PluginValidator func(files map[string]string) error

// 插件补丁里的占位符：MCP 地址与令牌每次中心启动都会变，由管理进程在启动 dsh 前填入。
const (
	PlaceholderMCPURL   = "{{DSH_MCP_URL}}"
	PlaceholderMCPToken = "{{DSH_MCP_TOKEN}}"
	PlaceholderSkills   = "{{DSH_SKILLS_DIR}}"
)

func (l Layout) Workspace() string { return filepath.Join(l.State(), "workspace") }
func (l Layout) DefaultWorkspace() string {
	return filepath.Join(l.Workspace(), "deepseek-harness", "default-workspace")
}

// FillPlaceholders 填入 MCP 地址、令牌与 skill 目录。
func FillPlaceholders(b []byte, url, token, skills string) ([]byte, error) {
	var doc yaml.Node
	decoder := yaml.NewDecoder(bytes.NewReader(b))
	if err := decoder.Decode(&doc); err != nil {
		return nil, errors.New("invalid plugin YAML")
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, errors.New("plugin must contain one YAML document")
	}
	var fill func(*yaml.Node)
	fill = func(n *yaml.Node) {
		if n.Kind == yaml.ScalarNode && n.Tag == "!!str" {
			n.Value = string(bytes.ReplaceAll([]byte(n.Value), []byte(PlaceholderMCPURL), []byte(url)))
			n.Value = string(bytes.ReplaceAll([]byte(n.Value), []byte(PlaceholderSkills), []byte(skills)))
			n.Value = string(bytes.ReplaceAll([]byte(n.Value), []byte(PlaceholderMCPToken), []byte(token)))
		}
		for _, child := range n.Content {
			fill(child)
		}
	}
	fill(&doc)
	return yaml.Marshal(&doc)
}

func readPlugin(dir string) (map[string]string, error) {
	r, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	d, err := r.Open(".")
	if err != nil {
		return nil, err
	}
	defer d.Close()
	entries, err := d.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	allowed := map[string]bool{}
	for _, name := range PluginFiles {
		allowed[name] = true
	}
	files := map[string]string{}
	for _, entry := range entries {
		if !allowed[entry.Name()] || entry.Type()&os.ModeSymlink != 0 || entry.IsDir() {
			return nil, errors.New("unsupported plugin file")
		}
		f, err := r.Open(entry.Name())
		if err != nil {
			return nil, err
		}
		st, err := f.Stat()
		if err != nil || !st.Mode().IsRegular() || st.Size() > 64<<10 {
			f.Close()
			return nil, errors.New("invalid plugin file")
		}
		b, err := io.ReadAll(io.LimitReader(f, (64<<10)+1))
		f.Close()
		if err != nil || len(b) > 64<<10 {
			return nil, errors.New("plugin file exceeds limit")
		}
		files[entry.Name()] = string(b)
	}
	if files["cordis.patch.yml"] == "" {
		return nil, errors.New("missing plugin patch")
	}
	return files, nil
}

// workspacePatch 把 dsh 的「文档」目录指向 dir，dsh 首次使用时在其下建默认工作区。
// workspace-controller 行本身没有其他配置，整体替换它的 config 是安全的。
func workspacePatch(dir string) string {
	return fmt.Sprintf("- id: workspace-controller\n  config:\n    documentsDirectory: %q\n", dir)
}

// prepareWorkspace 写好 AGENTS.md、工作区补丁、基础预设与插件补丁；返回基础补丁层、插件补丁（可能为空）
// 与插件问题（插件读取失败时只用基础预设启动）。
func (a *agent) prepareWorkspace(d Desired) (base []string, plugin, pluginErr string, err error) {
	l := a.l
	home := filepath.Join(l.State(), "home")
	for _, dir := range []string{home, l.Workspace()} {
		if err = os.MkdirAll(dir, 0o700); err != nil {
			return nil, "", "", err
		}
	}
	if err = os.WriteFile(filepath.Join(home, "AGENTS.md"), []byte(d.Instructions), 0o600); err != nil {
		return nil, "", "", err
	}
	ws, web := filepath.Join(l.State(), namespace(d.Namespace)+"-workspace.patch.yml"), filepath.Join(l.State(), namespace(d.Namespace)+"-web.patch.yml")
	if err = os.WriteFile(ws, []byte(workspacePatch(l.Workspace())), 0o600); err != nil {
		return nil, "", "", err
	}
	if err = os.WriteFile(web, []byte(d.WebPatch), 0o600); err != nil {
		return nil, "", "", err
	}
	base = []string{ws, web}
	if d.PluginDir == "" {
		return base, "", "", nil
	}
	if a.validatePlugin == nil {
		return base, "", "plugin validator not configured", nil
	}
	files, rerr := readPlugin(d.PluginDir)
	if rerr != nil {
		return base, "", "plugin files rejected", nil
	}
	if rerr = a.validatePlugin(files); rerr != nil {
		return base, "", "plugin rejected by host policy", nil
	}
	if _, rerr = loopbackEndpoint(d.MCPURL); rerr != nil {
		return base, "", "invalid plugin MCP endpoint", nil
	}
	if d.SkillsDir == "" {
		return base, "", "skill 目录缺失", nil
	}
	plugin = filepath.Join(l.State(), namespace(d.Namespace)+"-plugin.patch.yml")
	filled, fillErr := FillPlaceholders([]byte(files["cordis.patch.yml"]), d.MCPURL, d.MCPToken, d.SkillsDir)
	if fillErr != nil {
		return base, "", "invalid plugin YAML", nil
	}
	if err = os.WriteFile(plugin, filled, 0o600); err != nil {
		return nil, "", "", err
	}
	return base, plugin, "", nil
}

// dispatch 按任务类型执行。
func (a *agent) dispatch(ctx context.Context, d Desired, req JobRequest) JobResult {
	ctx = WithObservation(ctx, req.TraceID, a.observe)
	var res JobResult
	switch req.Kind {
	case "", "analysis":
		res = JobResult{Result: a.runJob(ctx, d, req)}
	default:
		res = JobResult{Result: Result{Error: "未知任务"}}
	}
	res.ID = req.ID
	return res
}
