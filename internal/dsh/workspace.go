package dsh

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// 工作区与 MiGate 插件：dsh 的默认工作区建在 state/workspace/ 下（Linux 服务器通常没有「文档」目录，
// dsh 自己找不到默认位置，会一直要求手动选择）；$DSH_HOME/AGENTS.md 写入 MiGate 的说明；
// 工作区里放一份 MiGate 插件的副本，供 dsh 的创造模式适配新版本，改好后由 MiGate 校验、试启动、确认。

// PluginFiles 是 MiGate 插件包含的全部文件（适配草稿也只能改这几个）。
var PluginFiles = []string{"package.json", "cordis.patch.yml", "README.md", "ADAPT.md"}

// 插件补丁里的占位符：MCP 地址与令牌每次中心启动都会变，由管理进程在启动 dsh 前填入。
const (
	PlaceholderMCPURL   = "{{MIGATE_MCP_URL}}"
	PlaceholderMCPToken = "{{MIGATE_MCP_TOKEN}}"
	placeholderCopy     = "{{MIGATE_PLUGIN_COPY}}"
)

func (l Layout) Workspace() string { return filepath.Join(l.State(), "workspace") }
func (l Layout) DefaultWorkspace() string {
	return filepath.Join(l.Workspace(), "deepseek-harness", "default-workspace")
}
func (l Layout) PluginCopy() string { return filepath.Join(l.DefaultWorkspace(), "migate-plugin") }

// FillPlaceholders 填入 MCP 地址与令牌。
func FillPlaceholders(b []byte, url, token string) []byte {
	b = bytes.ReplaceAll(b, []byte(PlaceholderMCPURL), []byte(url))
	return bytes.ReplaceAll(b, []byte(PlaceholderMCPToken), []byte(token))
}

// workspacePatch 把 dsh 的「文档」目录指向 dir，dsh 首次使用时在其下建默认工作区。
// workspace-controller 行本身没有其他配置，整体替换它的 config 是安全的。
func workspacePatch(dir string) string {
	return fmt.Sprintf("# 由 MiGate 生成。\n- id: workspace-controller\n  config:\n    documentsDirectory: %q\n", dir)
}

// prepareWorkspace 写好 AGENTS.md、工作区补丁、基础预设与插件补丁；返回基础补丁层、插件补丁（可能为空）
// 与插件问题（插件读取失败时只用基础预设启动，MiGate 的工具照常可用）。
func (a *agent) prepareWorkspace(d Desired) (base []string, plugin, pluginErr string, err error) {
	l := a.l
	home := filepath.Join(l.State(), "home")
	for _, dir := range []string{home, l.Workspace()} {
		if err = os.MkdirAll(dir, 0o700); err != nil {
			return nil, "", "", err
		}
	}
	notes := strings.ReplaceAll(d.Instructions, placeholderCopy, l.PluginCopy())
	if err = os.WriteFile(filepath.Join(home, "AGENTS.md"), []byte(notes), 0o600); err != nil {
		return nil, "", "", err
	}
	ws, web := filepath.Join(l.State(), "migate-workspace.patch.yml"), filepath.Join(l.State(), "migate-web.patch.yml")
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
		return base, "", "读取 MiGate 插件失败：" + rerr.Error(), nil
	}
	plugin = filepath.Join(l.State(), "migate-plugin.patch.yml")
	if err = os.WriteFile(plugin, FillPlaceholders(src, d.MCPURL, d.MCPToken), 0o600); err != nil {
		return nil, "", "", err
	}
	copyPlugin(d.PluginDir, l.PluginCopy())
	return base, plugin, "", nil
}

// copyPlugin 把当前插件复制到工作区供创造模式修改；插件来源没变时保留工作区里正在改的副本。
func copyPlugin(src, dst string) {
	marker := filepath.Join(dst, ".migate-source")
	if b, err := os.ReadFile(marker); err == nil && strings.TrimSpace(string(b)) == src {
		return
	}
	if os.MkdirAll(dst, 0o700) != nil {
		return
	}
	for _, f := range PluginFiles {
		if b, err := os.ReadFile(filepath.Join(src, f)); err == nil {
			_ = os.WriteFile(filepath.Join(dst, f), b, 0o600)
		}
	}
	_ = os.WriteFile(marker, []byte(src), 0o600)
}

// dispatch 按任务类型执行。
func (a *agent) dispatch(ctx context.Context, d Desired, req JobRequest) JobResult {
	var res JobResult
	switch req.Kind {
	case "", "analysis":
		res = JobResult{Result: a.runJob(ctx, d, req)}
	case "plugin-collect":
		res = a.collectPlugin()
	case "plugin-smoke":
		res = a.smokePlugin(ctx, d, req.PluginDir)
	default:
		res = JobResult{Result: Result{Error: "未知任务"}}
	}
	res.ID = req.ID
	return res
}

// collectPlugin 读取工作区里创造模式改好的插件副本。
func (a *agent) collectPlugin() JobResult {
	files := map[string]string{}
	for _, f := range PluginFiles {
		b, err := os.ReadFile(filepath.Join(a.l.PluginCopy(), f))
		if err != nil {
			continue
		}
		if len(b) > 64<<10 {
			return JobResult{Result: Result{Error: f + " 超过 64 KB"}}
		}
		files[f] = string(b)
	}
	if len(files) == 0 {
		return JobResult{Result: Result{Error: "工作区里没有 MiGate 插件副本：" + a.l.PluginCopy()}}
	}
	return JobResult{Result: Result{Completed: true}, Files: files}
}

// smokePlugin 用全新的临时家目录带着插件试启动一次 dsh：插件写错或与当前 dsh 不兼容时启动会失败。
func (a *agent) smokePlugin(ctx context.Context, d Desired, dir string) JobResult {
	drafts := filepath.Clean(filepath.Join(a.l.Runtime(), "plugin-drafts"))
	if !strings.HasPrefix(filepath.Clean(dir), drafts+string(filepath.Separator)) {
		return JobResult{Result: Result{Error: "插件目录无效"}}
	}
	p := a.paths(d.Version)
	if d.Version == "" || p.Node == "" {
		return JobResult{Result: Result{Error: "dsh 未启用或运行环境缺失"}}
	}
	src, err := os.ReadFile(filepath.Join(dir, "cordis.patch.yml"))
	if err != nil {
		return JobResult{Result: Result{Error: "读取插件草稿失败"}}
	}
	tmp, err := os.MkdirTemp(a.l.State(), "smoke-")
	if err != nil {
		return JobResult{Result: Result{Error: err.Error()}}
	}
	defer os.RemoveAll(tmp)
	ws, web, plugin := filepath.Join(tmp, "ws.yml"), filepath.Join(tmp, "web.yml"), filepath.Join(tmp, "plugin.yml")
	_ = os.WriteFile(ws, []byte(workspacePatch(filepath.Join(tmp, "workspace"))), 0o600)
	_ = os.WriteFile(web, []byte(d.WebPatch), 0o600)
	_ = os.WriteFile(plugin, FillPlaceholders(src, d.MCPURL, d.MCPToken), 0o600)
	port, err := FreePort()
	if err != nil {
		return JobResult{Result: Result{Error: err.Error()}}
	}
	var r Runner
	run, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	err = r.Start(run, WebOptions{Paths: Paths{Node: p.Node, Bin: p.Bin, Dir: tmp}, Version: d.Version, Port: port, TrustedHost: "127.0.0.1", Patches: []string{ws, web, plugin}})
	logs := r.Status().Logs
	r.Stop()
	if err != nil {
		return JobResult{Result: Result{Error: "带着插件启动 dsh 失败：" + err.Error()}, Log: logs}
	}
	return JobResult{Result: Result{Completed: true}, Log: logs}
}
