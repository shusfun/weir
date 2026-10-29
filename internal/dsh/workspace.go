package dsh

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// 工作区与 Weir 插件：dsh 的默认工作区建在 state/workspace/ 下（Linux 服务器通常没有「文档」目录，
// dsh 自己找不到默认位置，会一直要求手动选择）；$DSH_HOME/AGENTS.md 写入 Weir 的说明；
// Weir 插件随 Weir 发版维护，补丁里的 MCP 地址、令牌与 skill 目录在启动 dsh 前填入。

// PluginFiles 是 Weir 插件包含的全部文件。
var PluginFiles = []string{"package.json", "cordis.patch.yml", "README.md"}

// 插件补丁里的占位符：MCP 地址与令牌每次中心启动都会变，由管理进程在启动 dsh 前填入。
const (
	PlaceholderMCPURL   = "{{WEIR_MCP_URL}}"
	PlaceholderMCPToken = "{{WEIR_MCP_TOKEN}}"
	PlaceholderSkills   = "{{WEIR_SKILLS_DIR}}"
)

func (l Layout) Workspace() string { return filepath.Join(l.State(), "workspace") }
func (l Layout) DefaultWorkspace() string {
	return filepath.Join(l.Workspace(), "deepseek-harness", "default-workspace")
}

// FillPlaceholders 填入 MCP 地址、令牌与 skill 目录。
func FillPlaceholders(b []byte, url, token, skills string) []byte {
	b = bytes.ReplaceAll(b, []byte(PlaceholderMCPURL), []byte(url))
	b = bytes.ReplaceAll(b, []byte(PlaceholderSkills), []byte(skills))
	return bytes.ReplaceAll(b, []byte(PlaceholderMCPToken), []byte(token))
}

// workspacePatch 把 dsh 的「文档」目录指向 dir，dsh 首次使用时在其下建默认工作区。
// workspace-controller 行本身没有其他配置，整体替换它的 config 是安全的。
func workspacePatch(dir string) string {
	return fmt.Sprintf("# 由 Weir 生成。\n- id: workspace-controller\n  config:\n    documentsDirectory: %q\n", dir)
}

// prepareWorkspace 写好 AGENTS.md、工作区补丁、基础预设与插件补丁；返回基础补丁层、插件补丁（可能为空）
// 与插件问题（插件读取失败时只用基础预设启动，Weir 的工具照常可用）。
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
	ws, web := filepath.Join(l.State(), "weir-workspace.patch.yml"), filepath.Join(l.State(), "weir-web.patch.yml")
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
	src, rerr := os.ReadFile(filepath.Join(d.PluginDir, "cordis.patch.yml"))
	if rerr != nil {
		return base, "", "读取 Weir 插件失败：" + rerr.Error(), nil
	}
	if d.SkillsDir == "" {
		return base, "", "Weir skill 目录缺失", nil
	}
	plugin = filepath.Join(l.State(), "weir-plugin.patch.yml")
	if err = os.WriteFile(plugin, FillPlaceholders(src, d.MCPURL, d.MCPToken, d.SkillsDir), 0o600); err != nil {
		return nil, "", "", err
	}
	return base, plugin, "", nil
}

// dispatch 按任务类型执行。
func (a *agent) dispatch(ctx context.Context, d Desired, req JobRequest) JobResult {
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
