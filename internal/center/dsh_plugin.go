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

	"gopkg.in/yaml.v3"

	"github.com/shusfun/weir/internal/dsh"
)

// dshPluginSettingKey 保存管理员确认过的适配插件；没有时使用内置插件。
// Weir 维护的 dsh 插件：随程序发布（dshplugin/），由中心写到 runtime/plugins/（dsh 只读）。
// 插件只用 dsh 的声明式组合（预设、人设、工具行、skill 加载），不含任何脚本。dsh 出新版本后由 Weir 测试，
// 在下一个版本里放宽 package.json 的已验证范围；超出范围时只启用基础预设，后台安全分析暂停。

//go:embed dshplugin/*
var dshPluginFS embed.FS

const dshPluginName = "@weir/dsh-plugin"

// pluginMeta 是插件 package.json 中 Weir 关心的字段。
type pluginMeta struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Weir    struct {
		DSH string `json:"dsh"` // 兼容的 dsh 版本范围
	} `json:"weir"`
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
// Weir 工具行原样指向占位符。返回插件元数据。
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
	if meta.Name != dshPluginName || !dsh.ValidVersion(meta.Version) || !dsh.ValidRange(meta.Weir.DSH) {
		return meta, errors.New("package.json 的 name、version 或 weir.dsh 兼容范围无效")
	}
	patch := files["cordis.patch.yml"]
	if strings.Count(patch, dsh.PlaceholderMCPURL) > 1 || strings.Count(patch, dsh.PlaceholderMCPToken) > 1 {
		return meta, errors.New("Weir 工具的地址与令牌占位符只能出现在 Weir 工具行里")
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
// 不能是危险工具；Weir 工具行必须原样指向占位符，不能把令牌发给别的地址。
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
				return fmt.Errorf("cordis.patch.yml 第 %d 行：Weir 插件不能使用 %s（会执行命令、联网或安装插件）", name.Line, v)
			case v == "@deepseek-ai/dsh-mcp-client":
				if err := checkMCPRow(n); err != nil {
					return err
				}
			case v == "@deepseek-ai/dsh-skill-filesystem":
				if err := checkSkillRow(n); err != nil {
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

// checkSkillRow：skill 只能从 Weir 生成的目录加载（占位符），不读项目与用户目录。
func checkSkillRow(n *yaml.Node) error {
	var row struct {
		Config map[string]any `yaml:"config"`
	}
	if err := n.Decode(&row); err != nil {
		return fmt.Errorf("cordis.patch.yml 第 %d 行：skill 行无效", n.Line)
	}
	dirs, _ := row.Config["customSkillDirs"].([]any)
	for k := range row.Config {
		if k != "providerName" && k != "includeDefaultRoots" && k != "customSkillDirs" {
			return fmt.Errorf("cordis.patch.yml 第 %d 行：skill 行不能设置 %s", n.Line, k)
		}
	}
	if row.Config["includeDefaultRoots"] != false || len(dirs) != 1 || dirs[0] != dsh.PlaceholderSkills {
		return fmt.Errorf("cordis.patch.yml 第 %d 行：skill 只能从 Weir 的目录加载（includeDefaultRoots: false，customSkillDirs 只写占位符）", n.Line)
	}
	return nil
}

// checkMCPRow：只允许连接 Weir 自己的工具，地址与令牌必须是占位符。
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
		return fmt.Errorf("cordis.patch.yml 第 %d 行：Weir 工具行无效", n.Line)
	}
	c := row.Config
	if c.ServerName != "weir" || c.Transport != "streamable-http" || c.URL != dsh.PlaceholderMCPURL || c.Command != "" ||
		len(c.Headers) != 1 || c.Headers["Authorization"] != "Bearer "+dsh.PlaceholderMCPToken {
		return fmt.Errorf("cordis.patch.yml 第 %d 行：MCP 只能连接 Weir 自己的工具，地址与令牌必须保持占位符", n.Line)
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

// pluginVerified 判断某个 dsh 版本是否在内置插件已验证的范围内（只比较版本，不写文件）。
func pluginVerified(dshVersion string) bool {
	meta, err := validatePlugin(builtinPluginFiles())
	if err != nil {
		return false
	}
	ok, _ := dsh.InRange(dshVersion, meta.Weir.DSH)
	return ok
}

// activePlugin 为当前 dsh 版本选插件：在已验证范围内用内置插件；超出范围时不加载，返回原因。
// verified 为 false 时后台安全分析暂停（它按工具 id 禁用工具，新版本改了 id 时禁用会悄悄失效）。
func (s *Server) activePlugin(dshVersion string) (dir, label, note string, verified bool) {
	files := builtinPluginFiles()
	meta, err := validatePlugin(files)
	if err != nil {
		return "", "", "内置插件无效：" + err.Error(), false
	}
	if ok, _ := dsh.InRange(dshVersion, meta.Weir.DSH); ok {
		if dir, err = s.materializePlugin("builtin", files); err == nil {
			return dir, "内置插件 " + meta.Version, "", true
		}
		return "", "", "写入插件失败：" + err.Error(), false
	}
	return "", "", fmt.Sprintf("这个 dsh 版本（%s）还没验证（已验证 %s），Weir 助手预设未启用、后台安全分析暂停，等 Weir 发版适配", dshVersion, meta.Weir.DSH), false
}

// fileDiff 是两份文本的差异（逐行，未变的长段落折叠）；驱动草稿与驱动历史用它显示改前改后。
type fileDiff struct {
	Name  string     `json:"name"`
	State string     `json:"state"` // added / changed / removed / same
	Lines []diffLine `json:"lines,omitempty"`
}

type diffLine struct {
	Op   string `json:"op"` // " " 未变 / "+" 新增 / "-" 删除 / "…" 折叠
	Text string `json:"text"`
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
