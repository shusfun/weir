package kernelconf

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Driver 是某个内核某个版本区间的「驱动」：怎么调用、支持什么、建节点的方案，以及怎么生成配置。
// 驱动是数据文件，可单独更新，也可由 dsh 出草稿；格式说明见 .agents/skills/weir-kernel-driver。
// 新版本用 extends 继承上一版本、只写变化；继承在中心解析（Resolve），下发给 Gate 的是展开后的全文。
type Driver struct {
	Kernel     string   `yaml:"kernel"`
	Name       string   `yaml:"name"`
	MinVersion string   `yaml:"min_version"`
	MaxVersion string   `yaml:"max_version,omitempty"`
	Extends    string   `yaml:"extends,omitempty"`
	Notes      string   `yaml:"notes,omitempty"`
	Protocols  []string `yaml:"protocols,omitempty"`
	Networks   []string `yaml:"networks,omitempty"`
	Securities []string `yaml:"securities,omitempty"`
	// OutboundNetworks 是作客户端连出去（链式代理、分流出口）时支持的传输；没写时只有 tcp。
	// 出口协议看模板里有没有 outbound.<协议>。
	OutboundNetworks []string `yaml:"outbound_networks,omitempty"`
	// Features 是生成配置要用到的内核特性（如 Xray 的 balancer、observatory，sing-box 的 urltest）。
	// 没写或为空表示一项都不支持——和协议列表「为空即不限制」的含义相反。
	Features []string `yaml:"features,omitempty"`
	// Compat 是与其他内核版本的已知不兼容（推荐版本时据此避开）。
	Compat []Compat `yaml:"compat,omitempty"`
	// Run / Check 是运行与自检参数，{config} 换成配置文件路径。
	Run   []string `yaml:"run,omitempty"`
	Check []string `yaml:"check,omitempty"`
	// Presets 是建节点时的内置方案。
	Presets []Preset `yaml:"presets,omitempty"`
	// Templates 是配置模板（root 为入口）；Strip 是生成后要删除的字段（点分路径，* 匹配任意元素或键）。
	Templates map[string]any `yaml:"templates,omitempty"`
	Strip     []string       `yaml:"strip,omitempty"`
}

// Compat 记录一项不兼容：本驱动在 Security / Protocol 组合下，与某内核某版本范围的客户端连不上。
type Compat struct {
	Kernel   string `yaml:"kernel"`
	Range    string `yaml:"range"` // 如 ">=1.14.0 <1.15.0"
	Protocol string `yaml:"protocol,omitempty"`
	Security string `yaml:"security,omitempty"`
	Reason   string `yaml:"reason"`
}

// Preset 是一个建节点方案：管理员只选方案，伪装与传输参数按 Settings 固定生成。
type Preset struct {
	ID        string         `yaml:"id" json:"id"`
	Name      string         `yaml:"name" json:"name"`
	Summary   string         `yaml:"summary" json:"summary"`
	Protocol  string         `yaml:"protocol" json:"protocol"`
	Network   string         `yaml:"network" json:"network"`
	Security  string         `yaml:"security" json:"security"`
	Cert      string         `yaml:"cert" json:"cert"` // none / required / self_signed_ok
	ChainExit bool           `yaml:"chain_exit,omitempty" json:"chain_exit"`
	Reality   bool           `yaml:"reality,omitempty" json:"reality"` // 第 ④ 步由 VPS 实测 REALITY 目标
	Note      string         `yaml:"note,omitempty" json:"note,omitempty"`
	Order     int            `yaml:"order,omitempty" json:"order"`
	Settings  map[string]any `yaml:"settings,omitempty" json:"-"`
	Fields    []PresetField  `yaml:"fields,omitempty" json:"fields"`
}

// PresetField 是方案「高级」里可以改的字段；键名与节点 settings 一致。
type PresetField struct {
	Key     string      `yaml:"key" json:"key"`
	Label   string      `yaml:"label" json:"label"`
	Kind    string      `yaml:"kind" json:"kind"` // text / number / select / bool
	Unit    string      `yaml:"unit,omitempty" json:"unit,omitempty"`
	Hint    string      `yaml:"hint,omitempty" json:"hint,omitempty"`
	Options [][2]string `yaml:"options,omitempty" json:"options,omitempty"`
}

// Generators 是方案 Settings 里可以引用的生成器：值写成 "$gen:名称"（REALITY 密钥对用 "$gen:reality.private"
// 与 "$gen:reality.public"，同一次生成）。生成方法留在 Go 里，驱动只按名称引用。
var Generators = []string{"reality.private", "reality.public", "hex8", "hex12", "hex24", "ss2022", "wg.private"}

var (
	idPattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{0,47}$`)
	keyPattern  = regexp.MustCompile(`^[a-z][a-z0-9_]{0,47}$`)
	argPattern  = regexp.MustCompile(`^(-{1,2}[A-Za-z][A-Za-z0-9-]{0,31}|[a-z][a-z0-9-]{0,31}|\{config\})$`)
	safeVersion = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z._+-]{0,63}$`)
	// GenPattern 匹配方案设置里的生成器引用，如 "/$gen:hex12"。
	genPattern = regexp.MustCompile(`\$gen:([a-z0-9.]+)`)
)

// GenPattern 供中心替换生成器引用。
var GenPattern = genPattern

// Kernels 是驱动可以描述的内核。
var Kernels = []string{"xray", "sing-box"}

// ParseDriver 严格解析一份展开后的驱动（不处理 extends）。
func ParseDriver(text string) (Driver, error) {
	var d Driver
	dec := yaml.NewDecoder(strings.NewReader(text))
	dec.KnownFields(true)
	if err := dec.Decode(&d); err != nil {
		return d, fmt.Errorf("驱动格式：%w", err)
	}
	return d, d.validate()
}

func (d Driver) validate() error {
	known := false
	for _, k := range Kernels {
		known = known || k == d.Kernel
	}
	switch {
	case !known:
		return fmt.Errorf("驱动：未知内核 %q", d.Kernel)
	case !idPattern.MatchString(d.Name):
		return fmt.Errorf("驱动：名称 %q 只能用小写字母、数字、点与连字符", d.Name)
	case !safeVersion.MatchString(d.MinVersion):
		return errors.New("驱动：min_version 必填")
	case d.MaxVersion != "" && !safeVersion.MatchString(d.MaxVersion):
		return errors.New("驱动：max_version 格式无效")
	case d.MaxVersion != "" && CompareVersions(d.MaxVersion, d.MinVersion) < 0:
		return errors.New("驱动：max_version 小于 min_version")
	}
	for _, c := range d.Compat {
		if c.Kernel == "" || c.Range == "" || c.Reason == "" {
			return errors.New("驱动：compat 每项都要写 kernel、range、reason")
		}
		if _, err := InRange("0.0.0", c.Range); err != nil {
			return fmt.Errorf("驱动：compat 版本范围 %q 无效", c.Range)
		}
	}
	if len(d.Protocols) == 0 {
		return errors.New("驱动：protocols 必填")
	}
	if _, ok := d.Templates["root"]; !ok {
		return errors.New("驱动：templates 里必须有 root")
	}
	if err := stringKeys(d.Templates); err != nil {
		return err
	}
	if err := checkArgs("run", d.Run); err != nil {
		return err
	}
	if err := checkArgs("check", d.Check); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, p := range d.Presets {
		if !idPattern.MatchString(p.ID) || seen[p.ID] {
			return fmt.Errorf("驱动：方案 id %q 无效或重复", p.ID)
		}
		seen[p.ID] = true
		if p.Name == "" || p.Protocol == "" {
			return fmt.Errorf("驱动：方案 %s 缺少 name 或 protocol", p.ID)
		}
		if err := d.Allows(p.Protocol, p.Network, p.Security); err != nil {
			return fmt.Errorf("驱动：方案 %s：%w", p.ID, err)
		}
		switch p.Cert {
		case "none", "required", "self_signed_ok":
		default:
			return fmt.Errorf("驱动：方案 %s 的 cert 只能是 none / required / self_signed_ok", p.ID)
		}
		for k, v := range p.Settings {
			if !keyPattern.MatchString(k) {
				return fmt.Errorf("驱动：方案 %s 的设置键 %q 无效", p.ID, k)
			}
			if s, ok := v.(string); ok {
				for _, m := range genPattern.FindAllStringSubmatch(s, -1) {
					if !validGenerator(m[1]) {
						return fmt.Errorf("驱动：方案 %s 引用了未知生成器 %s", p.ID, m[0])
					}
				}
			}
		}
		for _, f := range p.Fields {
			if !keyPattern.MatchString(f.Key) || f.Label == "" {
				return fmt.Errorf("驱动：方案 %s 的字段 %q 无效", p.ID, f.Key)
			}
			switch f.Kind {
			case "text", "number", "select", "bool":
			default:
				return fmt.Errorf("驱动：方案 %s 字段 %s 的 kind 无效", p.ID, f.Key)
			}
		}
	}
	return nil
}

// stringKeys 确认模板里的对象键都是字符串（YAML 里写成数字的键要加引号）。
func stringKeys(node any) error {
	switch n := node.(type) {
	case map[string]any:
		for _, v := range n {
			if err := stringKeys(v); err != nil {
				return err
			}
		}
	case []any:
		for _, v := range n {
			if err := stringKeys(v); err != nil {
				return err
			}
		}
	case map[any]any:
		return errors.New("驱动：模板里的键要写成字符串（数字键加引号）")
	}
	return nil
}

func validGenerator(name string) bool {
	for _, g := range Generators {
		if g == name {
			return true
		}
	}
	return false
}

// checkArgs 限制运行参数：只能是选项名、子命令和 {config}，不能带路径或其他值（辅助程序以 root 启动内核）。
func checkArgs(what string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("驱动：%s 必填", what)
	}
	if len(args) > 8 {
		return fmt.Errorf("驱动：%s 参数过多", what)
	}
	n := 0
	for _, a := range args {
		if !argPattern.MatchString(a) {
			return fmt.Errorf("驱动：%s 参数 %q 只能是选项名、子命令或 {config}", what, a)
		}
		if a == "{config}" {
			n++
		}
	}
	if n != 1 {
		return fmt.Errorf("驱动：%s 必须包含一次 {config}", what)
	}
	return nil
}

// CheckArgs 校验一组运行参数（Gate 的辅助程序启动内核前再校验一遍）。
func CheckArgs(a KernelArgs) error {
	if err := checkArgs("run", a.Run); err != nil {
		return err
	}
	return checkArgs("check", a.Check)
}

// Args 把 {config} 换成配置文件路径。
func Args(args []string, config string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		if a == "{config}" {
			a = config
		}
		out[i] = a
	}
	return out
}

// Has 判断驱动是否声明了某项特性；没有声明就是不支持。
func (d Driver) Has(feature string) bool {
	for _, f := range d.Features {
		if strings.EqualFold(f, feature) {
			return true
		}
	}
	return false
}

func contains(list []string, v string) bool {
	if len(list) == 0 {
		return true
	}
	for _, item := range list {
		if strings.EqualFold(item, v) {
			return true
		}
	}
	return false
}

// Allows 判断入站组合是否被该版本支持；不支持时返回明确原因，不静默降级。
func (d Driver) Allows(protocol, network, security string) error {
	switch {
	case !contains(d.Protocols, protocol):
		return fmt.Errorf("%s %s 不支持协议 %s", d.Kernel, d.MinVersion, protocol)
	case network != "" && !contains(d.Networks, network):
		return fmt.Errorf("%s %s 不支持传输 %s", d.Kernel, d.MinVersion, network)
	case security != "" && security != "none" && !contains(d.Securities, security):
		return fmt.Errorf("%s %s 不支持安全层 %s", d.Kernel, d.MinVersion, security)
	}
	return nil
}

// OutboundAllows 判断这个内核能否作客户端连接某出口：模板里要有 outbound.<协议>，传输要在 outbound_networks 里。
func (d Driver) OutboundAllows(protocol, network string) error {
	if _, ok := d.Templates["outbound."+protocol]; !ok {
		return fmt.Errorf("%s %s 不能连接 %s 出口", d.Kernel, d.MinVersion, protocol)
	}
	if network == "" || network == "tcp" {
		return nil
	}
	for _, n := range d.OutboundNetworks {
		if strings.EqualFold(n, network) {
			return nil
		}
	}
	return fmt.Errorf("%s %s 作出口时不支持传输 %s", d.Kernel, d.MinVersion, network)
}

// Covers 判断某内核版本是否在驱动的版本区间里。
func (d Driver) Covers(version string) bool {
	return CompareVersions(version, d.MinVersion) >= 0 && (d.MaxVersion == "" || CompareVersions(version, d.MaxVersion) <= 0)
}

// Preset 按 id 找方案。
func (d Driver) Preset(id string) (Preset, bool) {
	for _, p := range d.Presets {
		if p.ID == id {
			return p, true
		}
	}
	return Preset{}, false
}

// Advanced 判断建节点时能否覆盖某个字段（方案的高级字段）。
func (p Preset) Advanced(key string) bool {
	for _, f := range p.Fields {
		if f.Key == key {
			return true
		}
	}
	return false
}

// Resolve 展开 extends：source 返回某名称驱动的原文。子驱动的 templates 按名称覆盖父驱动、presets 按 id 覆盖，
// 其他写了的字段整体替换；版本区间只取子驱动自己的。返回展开后的全文（下发给 Gate）。
func Resolve(text string, source func(name string) (string, bool)) (Driver, string, error) {
	merged, err := resolveMap(text, source, 0)
	if err != nil {
		return Driver{}, "", err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err = enc.Encode(merged); err != nil {
		return Driver{}, "", err
	}
	full := buf.String()
	d, err := ParseDriver(full)
	return d, full, err
}

func resolveMap(text string, source func(string) (string, bool), depth int) (map[string]any, error) {
	if depth > 8 {
		return nil, errors.New("驱动：extends 层数过多（可能循环继承）")
	}
	// 展开后的全文是重新编码的，行号对不上原文：未知字段与类型错误先在原文上严格解析一遍报出来
	var raw Driver
	dec := yaml.NewDecoder(strings.NewReader(text))
	dec.KnownFields(true)
	if err := dec.Decode(&raw); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("驱动格式：%w", err)
	}
	var m map[string]any
	if err := yaml.Unmarshal([]byte(text), &m); err != nil {
		return nil, fmt.Errorf("驱动格式：%w", err)
	}
	if m == nil {
		return nil, errors.New("驱动是空的")
	}
	parentName, _ := m["extends"].(string)
	delete(m, "extends")
	if parentName == "" {
		return m, nil
	}
	parentText, ok := source(parentName)
	if !ok {
		return nil, fmt.Errorf("驱动：找不到被继承的 %s", parentName)
	}
	parent, err := resolveMap(parentText, source, depth+1)
	if err != nil {
		return nil, err
	}
	if pk, ck := parent["kernel"], m["kernel"]; ck != nil && pk != ck {
		return nil, errors.New("驱动：只能继承同一内核的驱动")
	}
	out := map[string]any{}
	for k, v := range parent {
		if k == "min_version" || k == "max_version" || k == "name" {
			continue
		}
		out[k] = v
	}
	for k, v := range m {
		switch k {
		case "templates":
			t := map[string]any{}
			if pt, ok := parent["templates"].(map[string]any); ok {
				for n, x := range pt {
					t[n] = x
				}
			}
			if ct, ok := v.(map[string]any); ok {
				for n, x := range ct {
					t[n] = x
				}
			}
			out[k] = t
		case "presets":
			out[k] = mergePresets(parent["presets"], v)
		default:
			out[k] = v
		}
	}
	return out, nil
}

func mergePresets(parent, child any) []any {
	var out []any
	index := map[string]int{}
	add := func(list any) {
		items, _ := list.([]any)
		for _, it := range items {
			m, _ := it.(map[string]any)
			id, _ := m["id"].(string)
			if i, ok := index[id]; ok && id != "" {
				out[i] = it
				continue
			}
			index[id] = len(out)
			out = append(out, it)
		}
	}
	add(parent)
	add(child)
	return out
}

// RawField 读取驱动原文里的一个顶层字段（不展开 extends、不校验），如 kernel、extends。
func RawField(text, key string) string {
	var m map[string]any
	if yaml.Unmarshal([]byte(text), &m) != nil {
		return ""
	}
	s, _ := m[key].(string)
	return s
}

// CompareVersions 按数字段比较版本号（忽略前缀 v 与 "-" 之后的后缀）：a<b 返回负数。
func CompareVersions(a, b string) int {
	parse := func(v string) []int {
		v = strings.TrimPrefix(strings.TrimSpace(v), "v")
		if i := strings.IndexAny(v, "-+"); i >= 0 {
			v = v[:i]
		}
		var out []int
		for _, part := range strings.Split(v, ".") {
			n, _ := strconv.Atoi(part)
			out = append(out, n)
		}
		return out
	}
	x, y := parse(a), parse(b)
	for i := 0; i < len(x) || i < len(y); i++ {
		var p, q int
		if i < len(x) {
			p = x[i]
		}
		if i < len(y) {
			q = y[i]
		}
		if p != q {
			return p - q
		}
	}
	return 0
}

// InRange 判断版本是否满足空格分隔的比较式（>=、>、<=、<、=），全部满足才算。
func InRange(version, rng string) (bool, error) {
	parts := strings.Fields(rng)
	if len(parts) == 0 {
		return false, errors.New("空的版本范围")
	}
	for _, p := range parts {
		op := strings.TrimRight(p, "0123456789.v")
		v := strings.TrimPrefix(p, op)
		if v == "" || !safeVersion.MatchString(v) {
			return false, fmt.Errorf("版本范围 %q 无效", p)
		}
		c := CompareVersions(version, v)
		ok := false
		switch op {
		case ">=":
			ok = c >= 0
		case ">":
			ok = c > 0
		case "<=":
			ok = c <= 0
		case "<":
			ok = c < 0
		case "=", "":
			ok = c == 0
		default:
			return false, fmt.Errorf("版本范围 %q 的比较符无效", p)
		}
		if !ok {
			return false, nil
		}
	}
	return true, nil
}

// Incompatible 返回本驱动与某客户端内核版本的不兼容原因（兼容时为空）。
func (d Driver) Incompatible(kernel, version, protocol, security string) string {
	for _, c := range d.Compat {
		if c.Kernel != kernel || c.Protocol != "" && c.Protocol != protocol || c.Security != "" && c.Security != security {
			continue
		}
		if ok, _ := InRange(version, c.Range); ok {
			return c.Reason
		}
	}
	return ""
}

// SortedPresets 按 Order 排序（相同时保持原顺序）。
func (d Driver) SortedPresets() []Preset {
	out := append([]Preset(nil), d.Presets...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Order < out[j].Order })
	return out
}
