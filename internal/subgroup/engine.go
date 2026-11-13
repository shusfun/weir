package subgroup

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/shusfun/weir/internal/sub"
	js "github.com/shusfun/weir/internal/sub/jscompat"
	"github.com/shusfun/weir/packages/contracts"
)

// Source 是机场订阅源（url 以 http 开头的条目）。
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
	ConfigVersion string
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
	Members          []contracts.SubscriptionMember
	Nested           map[int]string
}

// Cache 保存订阅源的上次结果（保护性缓存）。
type Cache interface {
	Load(ctx context.Context, sourceID int) ([]string, bool)
	Store(ctx context.Context, sourceID int, nodes []string)
}

// SourceResult 是单个订阅源本次的结果，供调用方更新节点数与用量。
type SourceResult struct {
	SourceID        int
	Nodes           int
	FromCache       bool
	UserInfo        string
	Title           string
	Error           string
	Location        string
	ConfigVersion   string `json:"-"`
	processingError error
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
	if b, err := decodeB64(clean); err == nil && utf8.Valid(b) {
		return string(b)
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

func runOperators(nodes []string, ops []any, ua string) ([]string, error) {
	if len(ops) == 0 {
		return nodes, nil
	}
	arr, _ := toJS(ops).(*js.Array)
	if arr == nil {
		return nil, errors.New("invalid_operators")
	}
	out, err := sub.RunOperatorChainChecked(nodes, arr, sub.OperatorContext{UserAgent: ua})
	if err != nil {
		return nil, err
	}
	return strs(out), nil
}

func filterRules(nodes []string, include, exclude string) []string {
	if strings.TrimSpace(include) == "" && strings.TrimSpace(exclude) == "" {
		return nodes
	}
	return sub.ApplyDirectionalFilters(nodes, include, exclude)
}

// manualLines 处理手动节点：修正编码 → 自定义名称 → 分组名 / 手动前缀 → 本节点的算子与筛选。
func manualLines(nodes []ManualNode, o Options) ([]string, error) {
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
		processed, err := runOperators([]string{u}, n.Operators, o.UserAgent)
		if err != nil {
			return nil, fmt.Errorf("node_rules_invalid[%d]: %w", n.ID, err)
		}
		lines := filterRules(processed, n.FilterInclude, n.FilterExclude)
		out = append(out, lines...)
	}
	return out, nil
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
	out, results, _ := CombineBounded(ctx, f, cache, sources, manual, o, 16<<20)
	return out, results
}

// CombineBounded enforces an aggregate limit while sources finish and members
// are expanded, before allocating a joined subscription body.
func CombineBounded(ctx context.Context, f Fetcher, cache Cache, sources []Source, manual []ManualNode, o Options, maxBytes int) (string, []SourceResult, error) {
	if maxBytes < 1 || maxBytes > 64<<20 {
		return "", nil, errors.New("profile_output_limit_invalid")
	}
	if err := validateCombination(sources, manual, o); err != nil {
		return "", nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	finish := beginStage(ctx, "subscription.combine", 0)
	combinedError := ""
	defer func() { finish(combinedError, false) }()
	fail := func(results []SourceResult) (string, []SourceResult, error) {
		combinedError = "profile_output_too_large"
		return "", results, errors.New(combinedError)
	}
	if o.ManualPrefix == "" {
		o.ManualPrefix = "手动节点"
	}
	lines, err := manualLines(manual, o)
	if err != nil {
		combinedError = "manual_rules_invalid"
		return "", nil, err
	}
	countBytes := func(lines []string) int {
		n := 0
		for _, line := range lines {
			if len(line) >= maxBytes-n {
				return maxBytes + 1
			}
			n += len(line) + 1
		}
		return n
	}
	allocated := countBytes(lines)
	if allocated > maxBytes {
		return fail(nil)
	}
	parts := make([][]string, len(sources))
	results := make([]SourceResult, len(sources))
	sem := make(chan struct{}, 3)
	var wg sync.WaitGroup
	var budgetMu sync.Mutex
	overflow := false
	for i, s := range sources {
		wg.Add(1)
		go func(i int, s Source) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()
			nodes, result := fetchSource(ctx, f, cache, s, o)
			budgetMu.Lock()
			defer budgetMu.Unlock()
			results[i] = result
			if size := countBytes(nodes); size > maxBytes-allocated {
				overflow = true
				cancel()
				return
			} else {
				allocated += size
			}
			parts[i] = nodes
		}(i, s)
	}
	wg.Wait()
	if overflow {
		return fail(results)
	}
	if err := ctx.Err(); err != nil {
		combinedError = "cancelled"
		return "", results, err
	}
	for _, result := range results {
		if result.processingError != nil {
			combinedError = "source_rules_invalid"
			return "", results, fmt.Errorf("source_rules_invalid[%d]: %w", result.SourceID, result.processingError)
		}
		if result.Error != "" {
			combinedError = "source_failed"
		}
	}
	assembled := 0
	appendLines := func(next []string) bool {
		n := countBytes(next)
		if n > maxBytes-assembled {
			return false
		}
		assembled += n
		lines = append(lines, next...)
		return true
	}
	if o.Members != nil {
		lines = nil
		sourceParts := map[int][]string{}
		for i, source := range sources {
			sourceParts[source.ID] = parts[i]
		}
		manualParts := map[int][]string{}
		for _, node := range manual {
			part, err := manualLines([]ManualNode{node}, o)
			if err != nil {
				combinedError = "manual_rules_invalid"
				return "", results, err
			}
			manualParts[node.ID] = part
		}
		for _, member := range o.Members {
			switch member.Kind {
			case "source":
				if !appendLines(sourceParts[member.ID]) {
					return fail(results)
				}
			case "node":
				if !appendLines(manualParts[member.ID]) {
					return fail(results)
				}
			case "profile":
				body := strings.TrimSpace(o.Nested[member.ID])
				if len(body) > maxBytes-assembled {
					return fail(results)
				}
				if !appendLines(strings.Split(body, "\n")) {
					return fail(results)
				}
			}
		}
	} else {
		assembled = countBytes(lines)
		for _, p := range parts {
			if !appendLines(p) {
				return fail(results)
			}
		}
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
	current, err = runOperators(current, ops, o.UserAgent)
	if err != nil {
		combinedError = "group_rules_invalid"
		return "", results, fmt.Errorf("group_rules_invalid: %w", err)
	}
	current = filterRules(current, o.FilterInclude, o.FilterExclude)
	outputBytes := countBytes(o.Prepended)
	for i, l := range current {
		current[i] = sub.SanitizeNodeForYaml(l)
		if o.FlagEmoji {
			current[i] = sub.AddFlagEmoji(current[i])
		}
		if len(current[i])+1 > maxBytes-outputBytes {
			return fail(results)
		}
		outputBytes += len(current[i]) + 1
	}
	out := strings.Join(current, "\n")
	if out != "" {
		out += "\n"
	}
	if len(o.Prepended) > 0 {
		out = strings.Join(o.Prepended, "\n") + "\n" + out
	}
	return out, results, nil
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
	out, _ := RunOperatorsChecked(nodes, ops, userAgent)
	return out
}

func RunOperatorsChecked(nodes []string, ops []any, userAgent string) ([]string, error) {
	if err := sub.ValidateOperatorPatterns(ops); err != nil {
		return nil, err
	}
	return runOperators(nodes, ops, userAgent)
}
