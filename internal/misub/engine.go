package misub

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/shusfun/weir/internal/sub"
	js "github.com/shusfun/weir/internal/sub/jscompat"
)

// Source 是机场订阅源（对应上游 misub 中 url 以 http 开头的条目）。
type Source struct {
	ID            int
	Name          string
	URL           string
	UserAgent     string // 自定义 UA；为空时按客户端 UA 推断
	FilterInclude string
	FilterExclude string
	Operators     []any // 本源的算子链（上游 operators，JSON 结构原样保留）
	PlusAsSpace   bool
	NodeCache     bool // 保护性缓存：拉取失败或节点骤减时回退到上次结果
	KnownNodes    int  // 上次成功的节点数（骤减保护依据）
}

// ManualNode 是手动添加的节点链接。
type ManualNode struct {
	ID            int
	Name          string
	URL           string
	Group         string
	FilterInclude string
	FilterExclude string
	Operators     []any
	PlusAsSpace   bool
}

// Options 是一次生成的参数：订阅组设置与全局默认值已由调用方合并。
type Options struct {
	UserAgent        string
	PrependManual    bool   // 手动节点加前缀
	ManualPrefix     string // 默认「手动节点」
	PrependSources   bool   // 订阅源节点加源名称前缀
	PrependGroupName bool   // 手动节点加分组名
	FlagEmoji        bool   // 地区旗帜
	Operators        []any  // 订阅组算子链；为空时用全局默认
	DefaultOperators []any
	LegacyTransform  map[string]any // 旧版 nodeTransform（无算子链时转换使用）
	FilterInclude    string
	FilterExclude    string
	Prepended        []string // 置顶的提示节点（流量 / 到期）
}

// Cache 保存订阅源的上次结果（保护性缓存）。
type Cache interface {
	Load(ctx context.Context, sourceID int) ([]string, bool)
	Store(ctx context.Context, sourceID int, nodes []string)
}

// SourceResult 是单个订阅源本次的结果，供调用方更新节点数与用量。
type SourceResult struct {
	SourceID  int
	Nodes     int
	FromCache bool
	UserInfo  string
	Title     string
	Error     string
}

var realProtocols = []string{"ss://", "ssr://", "vmess://", "vless://", "trojan://", "hysteria://", "hysteria2://", "hy2://", "tuic://", "anytls://", "snell://", "wireguard://", "socks5://", "socks://", "http://", "https://", "naive+https://"}

var localEndpoint = regexp.MustCompile(`@(127\.0\.0\.1|localhost|0\.0\.0\.0)(:\d+)?([/?#]|$)`)

// isRealProxyNode 排除流量 / 到期提示等指向本机的虚拟节点。
func isRealProxyNode(node string) bool {
	t := strings.ToLower(strings.TrimSpace(node))
	if t == "" || localEndpoint.MatchString(t) {
		return false
	}
	for _, p := range realProtocols {
		if strings.HasPrefix(t, p) {
			return true
		}
	}
	return false
}

// suspiciousDrop：之前至少 10 个节点、这次不足 30%，视为上游异常，保留旧结果。
func suspiciousDrop(previous, next int) bool {
	return previous >= 10 && next < previous && float64(next)/float64(previous) < 0.3
}

func decodeB64(s string) ([]byte, error) {
	s = strings.NewReplacer("-", "+", "_", "/").Replace(s)
	if pad := len(s) % 4; pad != 0 {
		s += strings.Repeat("=", 4-pad)
	}
	return base64.StdEncoding.DecodeString(s)
}

var spaces = regexp.MustCompile(`\s`)

// decodeContent：整体是 Base64 时解码，否则原样返回。
func decodeContent(text string) string {
	clean := spaces.ReplaceAllString(text, "")
	if sub.IsValidBase64(clean) {
		if b, err := decodeB64(clean); err == nil {
			return string(b)
		}
	}
	return text
}

// toJS 把 Go 的 JSON 结构转换为引擎使用的 JS 值（保持键顺序由 JSON 文本决定）。
func toJS(v any) any {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	out, err := js.JSONParse(string(b))
	if err != nil {
		return nil
	}
	return out
}

func strs(list []any) []string {
	out := make([]string, 0, len(list))
	for _, v := range list {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func runOperators(nodes []string, ops []any, ua string) []string {
	if len(ops) == 0 || len(nodes) == 0 {
		return nodes
	}
	arr, _ := toJS(ops).(*js.Array)
	if arr == nil {
		return nodes
	}
	return strs(sub.RunOperatorChain(nodes, arr, sub.OperatorContext{UserAgent: ua}))
}

func filterRules(nodes []string, include, exclude string) []string {
	if strings.TrimSpace(include) == "" && strings.TrimSpace(exclude) == "" {
		return nodes
	}
	return sub.ApplyFilterRules(nodes, js.Obj("filterInclude", include, "filterExclude", exclude))
}

// manualLines 处理手动节点：修正编码 → 自定义名称 → 分组名 / 手动前缀 → 本节点的算子与筛选。
func manualLines(nodes []ManualNode, o Options) []string {
	var out []string
	for _, n := range nodes {
		raw := strings.TrimSpace(n.URL)
		if raw == "" {
			continue
		}
		u := sub.FixNodeURLEncoding(raw, sub.FixOptions{PlusAsSpace: n.PlusAsSpace})
		if name := strings.TrimSpace(n.Name); name != "" {
			u = sub.ApplyManualNodeName(u, name)
		}
		if o.PrependGroupName && strings.TrimSpace(n.Group) != "" {
			u = sub.PrependNodeName(u, strings.TrimSpace(n.Group))
		}
		if o.PrependManual {
			u = sub.PrependNodeName(u, o.ManualPrefix)
		}
		lines := filterRules(runOperators([]string{u}, n.Operators, o.UserAgent), n.FilterInclude, n.FilterExclude)
		out = append(out, lines...)
	}
	return out
}

// fetchSource 拉取并处理一个订阅源；失败、空结果或节点骤减时按保护性缓存回退。
func fetchSource(ctx context.Context, f Fetcher, cache Cache, s Source, o Options) ([]string, SourceResult) {
	res := SourceResult{SourceID: s.ID}
	cached := func() []string {
		if !s.NodeCache || cache == nil {
			return nil
		}
		nodes, ok := cache.Load(ctx, s.ID)
		if ok {
			res.FromCache = true
		}
		return nodes
	}
	ua := strings.TrimSpace(s.UserAgent)
	if ua == "" {
		ua = sub.GetProcessedUserAgent(o.UserAgent, s.URL)
	}
	resp, err := f.Get(ctx, s.URL, ua)
	if err != nil {
		res.Error = err.Error()
		return cached(), res
	}
	res.UserInfo, res.Title = resp.UserInfo, resp.Title
	text := decodeContent(string(resp.Body))
	parsed := sub.ParseNodeList(text, sub.FixOptions{PlusAsSpace: s.PlusAsSpace})
	if len(parsed) == 0 {
		if alt := sub.ParseNodeList(decodeContent(base64.StdEncoding.EncodeToString(resp.Body)), sub.FixOptions{PlusAsSpace: s.PlusAsSpace}); len(alt) > 0 {
			parsed = alt
		}
	}
	nodes := make([]string, 0, len(parsed))
	for _, p := range parsed {
		nodes = append(nodes, js.ToString(p.Get("url")))
	}
	nodes = filterRules(runOperators(nodes, s.Operators, o.UserAgent), s.FilterInclude, s.FilterExclude)
	real := 0
	for _, n := range nodes {
		if isRealProxyNode(n) {
			real++
		}
	}
	if suspiciousDrop(s.KnownNodes, real) {
		res.Error = "node_count_drop_rejected"
		return cached(), res
	}
	if s.NodeCache && cache != nil {
		if real == 0 {
			res.Error = "empty_source"
			return cached(), res
		}
		if old, ok := cache.Load(ctx, s.ID); ok && suspiciousDrop(len(old), real) {
			res.Error, res.FromCache = "node_count_drop_rejected", true
			return old, res
		}
		cache.Store(ctx, s.ID, nodes)
	}
	res.Nodes = real
	if o.PrependSources && s.Name != "" {
		for i, n := range nodes {
			nodes[i] = sub.PrependNodeName(n, s.Name)
		}
	}
	return nodes, res
}

// legacyOperators 把旧版 nodeTransform 配置转换为算子链（对应上游 adaptLegacyTransform）。
func legacyOperators(cfg map[string]any) []any {
	if cfg == nil || cfg["enabled"] != true {
		return nil
	}
	get := func(m any, k string) map[string]any {
		mm, _ := m.(map[string]any)
		v, _ := mm[k].(map[string]any)
		return v
	}
	on := func(m map[string]any) bool { return m != nil && m["enabled"] == true }
	var ops []any
	if f, _ := cfg["filter"].(map[string]any); f != nil {
		for _, k := range []string{"include", "exclude", "protocols", "regions", "script", "useless"} {
			if on(get(f, k)) {
				ops = append(ops, map[string]any{"id": "legacy-filter", "type": "filter", "enabled": true, "params": f})
				break
			}
		}
	}
	rename, _ := cfg["rename"].(map[string]any)
	if r := get(rename, "regex"); on(r) {
		if rules, _ := r["rules"].([]any); len(rules) > 0 {
			ops = append(ops, map[string]any{"id": "legacy-rename-regex", "type": "rename", "enabled": true, "params": map[string]any{"regex": r}})
		}
	}
	if t := get(rename, "template"); on(t) {
		tpl, _ := t["template"].(string)
		if tpl == "" {
			tpl = "{emoji}{region}-{protocol}-{index}"
		}
		offset := t["indexStart"]
		if offset == nil {
			offset = 1.0
		}
		scope := t["indexScope"]
		if scope == nil {
			scope = "region"
		}
		ops = append(ops, map[string]any{"id": "legacy-rename-template", "type": "rename", "enabled": true,
			"params": map[string]any{"template": map[string]any{"enabled": true, "template": tpl, "offset": offset, "indexScope": scope}}})
	}
	if d, _ := cfg["dedup"].(map[string]any); on(d) {
		ops = append(ops, map[string]any{"id": "legacy-dedup", "type": "dedup", "enabled": true, "params": d})
	}
	if s, _ := cfg["sort"].(map[string]any); on(s) {
		if keys, _ := s["keys"].([]any); len(keys) > 0 {
			ops = append(ops, map[string]any{"id": "legacy-sort", "type": "sort", "enabled": true, "params": s})
		}
	}
	return ops
}

// Combine 生成订阅组的节点列表：手动节点在前，订阅源按顺序并发拉取（最多 3 个同时），
// 合并去重后依次执行：旗帜处理 → 算子链（组 → 全局默认 → 旧版转换）→ 组与全局筛选 → YAML 安全化。
func Combine(ctx context.Context, f Fetcher, cache Cache, sources []Source, manual []ManualNode, o Options) (string, []SourceResult) {
	if o.ManualPrefix == "" {
		o.ManualPrefix = "手动节点"
	}
	lines := manualLines(manual, o)
	parts := make([][]string, len(sources))
	results := make([]SourceResult, len(sources))
	sem := make(chan struct{}, 3)
	var wg sync.WaitGroup
	for i, s := range sources {
		wg.Add(1)
		go func(i int, s Source) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			parts[i], results[i] = fetchSource(ctx, f, cache, s, o)
		}(i, s)
	}
	wg.Wait()
	for _, p := range parts {
		lines = append(lines, p...)
	}
	seen := map[string]bool{}
	var current []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" && !seen[l] {
			seen[l] = true
			current = append(current, l)
		}
	}
	if !o.FlagEmoji {
		for i, l := range current {
			current[i] = sub.RemoveFlagEmoji(l)
		}
	}
	ops := o.Operators
	if len(ops) == 0 {
		ops = o.DefaultOperators
	}
	if len(ops) == 0 {
		ops = legacyOperators(o.LegacyTransform)
	}
	current = runOperators(current, ops, o.UserAgent)
	current = filterRules(current, o.FilterInclude, o.FilterExclude)
	for i, l := range current {
		current[i] = sub.SanitizeNodeForYaml(l)
		if o.FlagEmoji {
			current[i] = sub.AddFlagEmoji(current[i])
		}
	}
	out := strings.Join(current, "\n")
	if out != "" {
		out += "\n"
	}
	if len(o.Prepended) > 0 {
		out = strings.Join(o.Prepended, "\n") + "\n" + out
	}
	return out, results
}

// NoticeNode 生成置顶的提示节点（指向本机、不可连接），客户端里显示为一个节点名称。
func NoticeNode(text string) string {
	return "trojan://00000000-0000-0000-0000-000000000000@127.0.0.1:443#" + strings.ReplaceAll(js.EncodeURIComponent(text), "+", "%20")
}

// FormatBytes 与上游 formatBytes 一致：1024 进制，保留两位小数。
func FormatBytes(n int64) string {
	units := []string{"B", "KB", "MB", "GB", "TB", "PB"}
	v, i := float64(n), 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	s := strconv.FormatFloat(v, 'f', 2, 64)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	return s + " " + units[i]
}

// RunOperators 对一组节点链接执行算子链（管理员预览处理效果用）。
func RunOperators(nodes []string, ops []any, userAgent string) []string {
	return runOperators(nodes, ops, userAgent)
}
