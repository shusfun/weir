package center

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v3"

	"github.com/imzyb/MiGate/internal/dsh"
	"github.com/imzyb/MiGate/internal/store/ent/setting"
)

// dshPluginSettingKey 保存管理员确认过的适配插件；没有时使用内置插件。
const dshPluginSettingKey = "dsh-plugin"

// MiGate 维护的 dsh 插件：随程序发布（dshplugin/），由中心写到 runtime/plugins/（dsh 只读）。
// 插件只用 dsh 的声明式组合（预设、人设、工具行），不含任何脚本；dsh 升级后不兼容时，
// 管理员可以让 dsh 的创造模式改工作区里的副本，再由 MiGate 校验、试启动、确认后启用（适配草稿）。

//go:embed dshplugin/*
var dshPluginFS embed.FS

const dshPluginName = "@migate/dsh-plugin"

// pluginMeta 是插件 package.json 中 MiGate 关心的字段。
type pluginMeta struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	MiGate  struct {
		DSH string `json:"dsh"` // 兼容的 dsh 版本范围
	} `json:"migate"`
}

// builtinPluginFiles 读取随程序发布的插件文件。
func builtinPluginFiles() map[string]string {
	out := map[string]string{}
	for _, f := range dsh.PluginFiles {
		if b, err := dshPluginFS.ReadFile("dshplugin/" + f); err == nil {
			out[f] = string(b)
		}
	}
	return out
}

// pluginHash 是插件全部文件的摘要（按文件名排序）。
func pluginHash(files map[string]string) string {
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	h := sha256.New()
	for _, n := range names {
		fmt.Fprintf(h, "%s\x00%d\x00%s", n, len(files[n]), files[n])
	}
	return hex.EncodeToString(h.Sum(nil))
}

// forbiddenRows 是插件不能使用的组合包：能执行命令、联网、安装插件、运行脚本或派生不受控任务。
var forbiddenRows = []string{"@deepseek-ai/dsh-plugin-manager", "@deepseek-ai/dsh-tool-bash", "@deepseek-ai/dsh-tool-pwsh",
	"@deepseek-ai/dsh-terminal", "@deepseek-ai/dsh-tool-web", "@deepseek-ai/dsh-tool-jobs", "@deepseek-ai/dsh-subagent",
	"@deepseek-ai/dsh-tool-subagent", "@deepseek-ai/dsh-tool-workflow", "@deepseek-ai/dsh-ptc", "@deepseek-ai/dsh-tool-cordis", "@deepseek-ai/dsh-cordis"}

func forbiddenRow(name string) bool {
	for _, f := range forbiddenRows {
		if name == f || strings.HasPrefix(name, f+"-") || strings.HasPrefix(name, f+"/") {
			return true
		}
	}
	return false
}

// validatePlugin 校验插件：只能是规定的几个文件；package.json 名称固定、版本与兼容范围合法；
// cordis.patch.yml 不含 !!js 等标签，只新增行与改默认预设，只用 dsh 官方组合包且不含危险工具，
// MiGate 工具行原样指向占位符。返回插件元数据。
func validatePlugin(files map[string]string) (pluginMeta, error) {
	var meta pluginMeta
	allowed := map[string]bool{}
	for _, f := range dsh.PluginFiles {
		allowed[f] = true
	}
	for name, body := range files {
		if !allowed[name] {
			return meta, fmt.Errorf("插件不能包含文件 %s", name)
		}
		if len(body) > 64<<10 {
			return meta, fmt.Errorf("%s 超过 64 KB", name)
		}
	}
	if err := json.Unmarshal([]byte(files["package.json"]), &meta); err != nil {
		return meta, errors.New("package.json 不是有效的 JSON")
	}
	if meta.Name != dshPluginName || !dsh.ValidVersion(meta.Version) || !dsh.ValidRange(meta.MiGate.DSH) {
		return meta, errors.New("package.json 的 name、version 或 migate.dsh 兼容范围无效")
	}
	patch := files["cordis.patch.yml"]
	if strings.Count(patch, dsh.PlaceholderMCPURL) > 1 || strings.Count(patch, dsh.PlaceholderMCPToken) > 1 {
		return meta, errors.New("MiGate 工具的地址与令牌占位符只能出现在 MiGate 工具行里")
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(patch), &doc); err != nil {
		return meta, errors.New("cordis.patch.yml 不是有效的 YAML：" + err.Error())
	}
	if len(doc.Content) != 1 || doc.Content[0].Kind != yaml.SequenceNode {
		return meta, errors.New("cordis.patch.yml 必须是一个补丁列表")
	}
	if err := checkYAMLTags(&doc); err != nil {
		return meta, err
	}
	for _, row := range doc.Content[0].Content {
		if err := checkPatchRow(row); err != nil {
			return meta, err
		}
	}
	return meta, nil
}

// checkYAMLTags 拒绝 !!js 等自定义标签（dsh 会把它们当成代码执行）。
func checkYAMLTags(n *yaml.Node) error {
	if strings.HasPrefix(n.Tag, "!") && !strings.HasPrefix(n.Tag, "!!") || strings.HasPrefix(n.Tag, "!!") && !map[string]bool{"!!str": true, "!!int": true, "!!float": true, "!!bool": true, "!!null": true, "!!map": true, "!!seq": true}[n.Tag] {
		return fmt.Errorf("cordis.patch.yml 第 %d 行使用了不允许的标签 %s", n.Line, n.Tag)
	}
	for _, c := range n.Content {
		if err := checkYAMLTags(c); err != nil {
			return err
		}
	}
	return nil
}

// mapGet 取映射节点里某个键的值。
func mapGet(n *yaml.Node, key string) *yaml.Node {
	if n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

// checkPatchRow：insert 行里的组合包逐个检查；其他行只能是把 agent-preset-registry 的默认预设改掉。
func checkPatchRow(row *yaml.Node) error {
	if row.Kind != yaml.MappingNode {
		return fmt.Errorf("cordis.patch.yml 第 %d 行不是补丁项", row.Line)
	}
	if ins := mapGet(row, "insert"); ins != nil {
		if len(row.Content) != 2 || ins.Kind != yaml.SequenceNode {
			return fmt.Errorf("cordis.patch.yml 第 %d 行：insert 必须单独成项且是列表", row.Line)
		}
		return checkPluginNode(ins)
	}
	id := mapGet(row, "id")
	if id == nil || id.Value != "agent-preset-registry" {
		return fmt.Errorf("cordis.patch.yml 第 %d 行：只能新增行或修改 agent-preset-registry，不能改写 dsh 已有的行", row.Line)
	}
	for i := 0; i+1 < len(row.Content); i += 2 {
		if k := row.Content[i].Value; k != "id" && k != "config" {
			return fmt.Errorf("cordis.patch.yml 第 %d 行：agent-preset-registry 只能改 config.default", row.Line)
		}
	}
	cfg := mapGet(row, "config")
	if cfg == nil || cfg.Kind != yaml.MappingNode || len(cfg.Content) != 2 || cfg.Content[0].Value != "default" || cfg.Content[1].Kind != yaml.ScalarNode {
		return fmt.Errorf("cordis.patch.yml 第 %d 行：agent-preset-registry 只能改 config.default", row.Line)
	}
	return nil
}

// checkPluginNode 遍历补丁内容：每个组合包（name 字段）只能是 dsh 官方包或 cordis:group，
// 不能是危险工具；MiGate 工具行必须原样指向占位符，不能把令牌发给别的地址。
func checkPluginNode(n *yaml.Node) error {
	if n.Kind == yaml.MappingNode {
		if name := mapGet(n, "name"); name != nil {
			if name.Kind != yaml.ScalarNode {
				return fmt.Errorf("cordis.patch.yml 第 %d 行：组合包名称无效", name.Line)
			}
			v := name.Value
			switch {
			case v == "cordis:group":
			case !strings.HasPrefix(v, "@deepseek-ai/dsh-"):
				return fmt.Errorf("cordis.patch.yml 第 %d 行：只能使用 dsh 官方组合包，不能使用 %s", name.Line, v)
			case forbiddenRow(v):
				return fmt.Errorf("cordis.patch.yml 第 %d 行：MiGate 插件不能使用 %s（会执行命令、联网或安装插件）", name.Line, v)
			case v == "@deepseek-ai/dsh-mcp-client":
				if err := checkMCPRow(n); err != nil {
					return err
				}
			}
		}
	}
	for _, c := range n.Content {
		if err := checkPluginNode(c); err != nil {
			return err
		}
	}
	return nil
}

// checkMCPRow：只允许连接 MiGate 自己的工具，地址与令牌必须是占位符。
func checkMCPRow(n *yaml.Node) error {
	var row struct {
		Config struct {
			ServerName string            `yaml:"serverName"`
			Transport  string            `yaml:"transport"`
			URL        string            `yaml:"url"`
			Headers    map[string]string `yaml:"headers"`
			Command    string            `yaml:"command"`
		} `yaml:"config"`
	}
	if err := n.Decode(&row); err != nil {
		return fmt.Errorf("cordis.patch.yml 第 %d 行：MiGate 工具行无效", n.Line)
	}
	c := row.Config
	if c.ServerName != "migate" || c.Transport != "streamable-http" || c.URL != dsh.PlaceholderMCPURL || c.Command != "" ||
		len(c.Headers) != 1 || c.Headers["Authorization"] != "Bearer "+dsh.PlaceholderMCPToken {
		return fmt.Errorf("cordis.patch.yml 第 %d 行：MCP 只能连接 MiGate 自己的工具，地址与令牌必须保持占位符", n.Line)
	}
	return nil
}

// materializePlugin 把插件写到 runtime/plugins/<来源>-<摘要前 16 位>/（已存在就直接用），返回目录。
func (s *Server) materializePlugin(source string, files map[string]string) (string, error) {
	root := filepath.Join(s.dshLayout().Runtime(), "plugins")
	dir := filepath.Join(root, source+"-"+pluginHash(files)[:16])
	if _, err := os.Stat(filepath.Join(dir, ".complete")); err == nil {
		return dir, nil
	}
	tmp := dir + ".new"
	_ = os.RemoveAll(tmp)
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return "", err
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(tmp, name), []byte(body), 0o644); err != nil {
			return "", err
		}
	}
	if err := os.WriteFile(filepath.Join(tmp, ".complete"), nil, 0o644); err != nil {
		return "", err
	}
	_ = os.RemoveAll(dir)
	if err := os.Rename(tmp, dir); err != nil {
		return "", err
	}
	dsh.Publish(root)
	return dir, nil
}

// adaptedPlugin 是管理员确认过的适配插件（设置键 dsh-plugin）；没有时使用内置插件。
type adaptedPlugin struct {
	Dir     string `json:"dir"`
	Version string `json:"version"`
	Compat  string `json:"compat"`
	Hash    string `json:"hash"`
}

// activePlugin 为当前 dsh 版本选插件：兼容的适配插件优先，其次内置插件；都不兼容时不加载，返回原因。
func (s *Server) activePlugin(ctx context.Context, dshVersion string) (dir, label, note string) {
	var a adaptedPlugin
	if row, err := s.Store.Ent.Setting.Query().Where(setting.KeyEQ(dshPluginSettingKey)).Only(ctx); err == nil {
		b, _ := json.Marshal(row.Value)
		_ = json.Unmarshal(b, &a)
	}
	var notes []string
	if a.Dir != "" {
		inside := strings.HasPrefix(filepath.Clean(a.Dir), filepath.Join(s.dshLayout().Runtime(), "plugins")+string(filepath.Separator))
		if ok, _ := dsh.InRange(dshVersion, a.Compat); ok && inside {
			if _, err := os.Stat(filepath.Join(a.Dir, "cordis.patch.yml")); err == nil {
				return a.Dir, "适配插件 " + a.Version, ""
			}
		}
		notes = append(notes, fmt.Sprintf("适配插件 %s 不支持 dsh %s（兼容范围 %s）", a.Version, dshVersion, a.Compat))
	}
	files := builtinPluginFiles()
	meta, err := validatePlugin(files)
	if ok, _ := dsh.InRange(dshVersion, meta.MiGate.DSH); err == nil && ok {
		if dir, err = s.materializePlugin("builtin", files); err == nil {
			return dir, "内置插件 " + meta.Version, strings.Join(notes, "；")
		}
	}
	notes = append(notes, fmt.Sprintf("内置插件 %s 不支持 dsh %s（兼容范围 %s），只启用基础预设；可以用创造模式适配", meta.Version, dshVersion, meta.MiGate.DSH))
	return "", "", strings.Join(notes, "；")
}

// fileDiff 是适配草稿与当前插件的一个文件的差异（逐行，未变的长段落折叠）。
type fileDiff struct {
	Name  string     `json:"name"`
	State string     `json:"state"` // added / changed / removed / same
	Lines []diffLine `json:"lines,omitempty"`
}

type diffLine struct {
	Op   string `json:"op"` // " " 未变 / "+" 新增 / "-" 删除 / "…" 折叠
	Text string `json:"text"`
}

// diffPlugin 比较两份插件文件。
func diffPlugin(before, after map[string]string) []fileDiff {
	var out []fileDiff
	for _, name := range dsh.PluginFiles {
		b, hasB := before[name]
		a, hasA := after[name]
		d := fileDiff{Name: name}
		switch {
		case !hasA && !hasB:
			continue
		case !hasB:
			d.State = "added"
		case !hasA:
			d.State = "removed"
		case a == b:
			d.State = "same"
			out = append(out, d)
			continue
		default:
			d.State = "changed"
		}
		d.Lines = diffLines(b, a)
		out = append(out, d)
	}
	return out
}

// diffLines 逐行比较（最长公共子序列）；文件很大时退化为整段删除加整段新增。
func diffLines(before, after string) []diffLine {
	split := func(s string) []string {
		if s == "" {
			return nil
		}
		return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
	}
	a, b := split(before), split(after)
	var out []diffLine
	if len(a)*len(b) > 4_000_000 {
		for _, l := range a {
			out = append(out, diffLine{"-", l})
		}
		for _, l := range b {
			out = append(out, diffLine{"+", l})
		}
		return out
	}
	// dp[i][j] 是 a[i:] 与 b[j:] 的最长公共子序列长度。
	dp := make([][]int32, len(a)+1)
	for i := range dp {
		dp[i] = make([]int32, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else {
				dp[i][j] = max(dp[i+1][j], dp[i][j+1])
			}
		}
	}
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		switch {
		case i < len(a) && j < len(b) && a[i] == b[j]:
			out = append(out, diffLine{" ", a[i]})
			i, j = i+1, j+1
		case i < len(a) && (j == len(b) || dp[i+1][j] >= dp[i][j+1]):
			out = append(out, diffLine{"-", a[i]})
			i++
		default:
			out = append(out, diffLine{"+", b[j]})
			j++
		}
	}
	return foldUnchanged(out)
}

// foldUnchanged 只保留改动前后各 3 行，中间未变的部分折叠成一行。
func foldUnchanged(lines []diffLine) []diffLine {
	keep := make([]bool, len(lines))
	for i, l := range lines {
		if l.Op != " " {
			for k := max(0, i-3); k <= min(len(lines)-1, i+3); k++ {
				keep[k] = true
			}
		}
	}
	var out []diffLine
	for i := 0; i < len(lines); i++ {
		if keep[i] {
			out = append(out, lines[i])
			continue
		}
		n := 0
		for i < len(lines) && !keep[i] {
			n, i = n+1, i+1
		}
		i--
		out = append(out, diffLine{"…", fmt.Sprintf("（%d 行未变）", n)})
	}
	return out
}

// pluginDraft 是适配草稿的插件部分；文件快照在 runtime/plugin-drafts/<id>/（中心所有，dsh 改不了）。
type pluginDraft struct {
	Version string     `json:"version"`
	Compat  string     `json:"compat"`
	Hash    string     `json:"hash"`
	Dir     string     `json:"dir"`
	DSH     string     `json:"dsh_version"`
	Warn    []string   `json:"warnings,omitempty"`
	Files   []fileDiff `json:"files"`
}

// adaptPlugin 收集工作区里创造模式改好的插件副本 → 校验 → 存快照 → 带着它试启动 dsh → 生成待确认的草稿。
func (s *Server) adaptPlugin(c *gin.Context) {
	// 服务的写超时是 3 分钟：收集与试启动一共最多等 150 秒。
	ctx, cancel := context.WithTimeout(c.Request.Context(), 150*time.Second)
	defer cancel()
	set := s.dshSettings(ctx)
	if _, _, ok := s.dshEndpoint(); !ok || set.Version == "" {
		failure(c, 409, "dsh_not_running")
		return
	}
	got, err := s.dshJob(ctx, dsh.JobRequest{Kind: "plugin-collect"})
	if err == nil && got.Error != "" {
		err = errors.New(got.Error)
	}
	if err != nil {
		failDetail(c, 409, "plugin_draft_invalid", jobErr(err), nil)
		return
	}
	meta, err := validatePlugin(got.Files)
	if err == nil {
		if ok, _ := dsh.InRange(set.Version, meta.MiGate.DSH); !ok {
			err = fmt.Errorf("兼容范围 %s 不包含当前 dsh %s", meta.MiGate.DSH, set.Version)
		}
	}
	if err != nil {
		failDetail(c, 400, "plugin_draft_invalid", err.Error(), nil)
		return
	}
	cur, _, _ := s.activePlugin(ctx, set.Version)
	before := readPluginDir(cur)
	if pluginHash(got.Files) == pluginHash(before) {
		failDetail(c, 409, "plugin_draft_invalid", "工作区里的插件与当前使用的插件相同，没有可提交的修改", nil)
		return
	}
	s.smokePluginDraft(c, ctx, set.Version, meta, before, got.Files)
}

// smokePluginDraft 把草稿存为快照，让管理进程带着它试启动 dsh；通过后生成待确认的草稿。
func (s *Server) smokePluginDraft(c *gin.Context, ctx context.Context, version string, meta pluginMeta, before, files map[string]string) {
	id := randomToken(12)
	dir := filepath.Join(s.dshLayout().Runtime(), "plugin-drafts", id)
	err := os.MkdirAll(dir, 0o755)
	for name, body := range files {
		if err == nil {
			err = os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644)
		}
	}
	if err != nil {
		_ = os.RemoveAll(dir)
		failure(c, 500, "save_failed")
		return
	}
	dsh.Publish(filepath.Dir(dir))
	res, err := s.dshJob(ctx, dsh.JobRequest{Kind: "plugin-smoke", PluginDir: dir})
	if err == nil && res.Error != "" {
		err = errors.New(res.Error)
	}
	if err != nil {
		_ = os.RemoveAll(dir)
		failDetail(c, 422, "plugin_smoke_failed", jobErr(err), gin.H{"log": lastLines(res.Log, 30)})
		return
	}
	var warn []string
	for _, l := range res.Log {
		if low := strings.ToLower(l); (strings.Contains(low, "error") || strings.Contains(low, "warn") || strings.Contains(l, "失败")) && len(warn) < 20 {
			warn = append(warn, l)
		}
	}
	d := &Draft{ID: id, Kind: "plugin", Source: "dsh", Status: "passed", CreatedAt: time.Now().UTC(),
		Explain: fmt.Sprintf("dsh 创造模式适配的 MiGate 插件 %s（兼容 dsh %s），已用 dsh %s 试启动通过。", meta.Version, meta.MiGate.DSH, version),
		Plugin:  &pluginDraft{Version: meta.Version, Compat: meta.MiGate.DSH, Hash: pluginHash(files), Dir: dir, DSH: version, Warn: warn, Files: diffPlugin(before, files)}}
	if err = s.saveDraft(ctx, d); err != nil {
		_ = os.RemoveAll(dir)
		failure(c, 503, "draft_unavailable")
		return
	}
	s.audit(c, "dsh.plugin.draft", meta.Version, "passed")
	c.JSON(200, d)
}

// applyPluginDraft 启用确认过的适配插件：快照必须与起草时一致，写到 runtime/plugins/ 后 dsh 按新插件重启。
func (s *Server) applyPluginDraft(c *gin.Context, d *Draft) {
	ctx := c.Request.Context()
	p := d.Plugin
	drafts := filepath.Join(s.dshLayout().Runtime(), "plugin-drafts") + string(filepath.Separator)
	if p == nil || !strings.HasPrefix(filepath.Clean(p.Dir), drafts) {
		failure(c, 409, "draft_invalid")
		return
	}
	files := readPluginDir(p.Dir)
	if pluginHash(files) != p.Hash {
		failure(c, 409, "draft_changed")
		return
	}
	meta, err := validatePlugin(files)
	if err != nil {
		failDetail(c, 409, "draft_invalid", err.Error(), nil)
		return
	}
	dir, err := s.materializePlugin("adapted", files)
	if err != nil {
		failure(c, 500, "save_failed")
		return
	}
	value := map[string]any{"dir": dir, "version": meta.Version, "compat": meta.MiGate.DSH, "hash": p.Hash}
	if err = s.Store.Ent.Setting.Create().SetKey(dshPluginSettingKey).SetValue(value).OnConflictColumns(setting.FieldKey).UpdateNewValues().Exec(ctx); err != nil {
		failure(c, 500, "save_failed")
		return
	}
	_ = os.RemoveAll(p.Dir)
	_ = s.Store.KV.Del(ctx, draftPrefix+d.ID)
	s.wakeDSH()
	s.audit(c, "dsh.plugin.apply", meta.Version, "ok")
	c.JSON(200, gin.H{"plugin": value})
}

// restorePlugin 改回内置插件。
func (s *Server) restorePlugin(c *gin.Context) {
	_, _ = s.Store.Ent.Setting.Delete().Where(setting.KeyEQ(dshPluginSettingKey)).Exec(c.Request.Context())
	s.wakeDSH()
	s.audit(c, "dsh.plugin.restore", "", "ok")
	c.Status(204)
}

// readPluginDir 读取一个插件目录；目录为空时返回内置插件。
func readPluginDir(dir string) map[string]string {
	if dir == "" {
		return builtinPluginFiles()
	}
	out := map[string]string{}
	for _, f := range dsh.PluginFiles {
		if b, err := os.ReadFile(filepath.Join(dir, f)); err == nil {
			out[f] = string(b)
		}
	}
	return out
}

func jobErr(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "dsh 正忙于其他任务或没有响应，请稍后重试"
	}
	return err.Error()
}

func lastLines(lines []string, n int) []string {
	if len(lines) > n {
		return lines[len(lines)-n:]
	}
	return lines
}
