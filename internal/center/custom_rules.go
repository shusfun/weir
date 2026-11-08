package center

import (
	"context"
	"net/netip"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/net/idna"
	"golang.org/x/text/width"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/link"
	"github.com/shusfun/weir/internal/subout"
	"github.com/shusfun/weir/packages/contracts"
)

// 自定义清单解析与旧格式兼容：批量粘贴整理为规范规则，逐条有序设置见 custom_entries.go。
// 表达不了的行列出原因、不算失败；规则写进订阅，由客户端执行。只有内容过大才拒绝。

const (
	customMaxSource = 256 << 10
	customMaxRules  = 5000
	// 关键词覆盖要逐对比较，关键词很多时只做去重，免得一份粘贴占住 CPU。
	customMaxKeywordCover = 500
	// customEmptyRule 是清单为空时的占位：不会命中的 .invalid 域名。
	customEmptyRule = "DOMAIN,empty.weir.invalid"
)

type customRule struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

// customUnused 是表达不了的一行：行号、原文、原因。
type customUnused struct {
	Line   int    `json:"line"`
	Text   string `json:"text"`
	Reason string `json:"reason"`
}

// customResult 是优化结果：Total 是有内容的行数，Reduced 是精简掉的规则数。
type customResult struct {
	Rules    []customRule   `json:"rules"`
	Counts   map[string]int `json:"counts"`
	Total    int            `json:"total"`
	Reduced  int            `json:"reduced"`
	Unused   []customUnused `json:"unused"`
	Warnings []string       `json:"warnings"`
}

var (
	customLinePrefix = map[string]string{"DOMAIN": "domain", "HOST": "domain", "DOMAIN-SUFFIX": "domain_suffix", "HOST-SUFFIX": "domain_suffix",
		"DOMAIN-KEYWORD": "domain_keyword", "HOST-KEYWORD": "domain_keyword", "IP-CIDR": "ip_cidr", "IP-CIDR6": "ip_cidr", "IP6-CIDR": "ip_cidr"}
	customCanonical = map[string]string{"domain": "DOMAIN", "domain_suffix": "DOMAIN-SUFFIX", "domain_keyword": "DOMAIN-KEYWORD", "ip_cidr": "IP-CIDR"}
	customTypeWord  = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]*$`)
	customSeparator = strings.NewReplacer("、", ",", ";", ",")
)

func (r customRule) line() string { return customCanonical[r.Kind] + "," + r.Value }

// customCommentLine 判断一行是否没有内容（注释、Clash 规则集文件头）。
func customCommentLine(line string) bool {
	return line == "" || line == "payload:" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") || strings.HasPrefix(line, "!") || strings.HasPrefix(line, ";")
}

// parseCustomLine 把一行转成规则。一行里可以有多个域名（逗号、顿号或空格分隔），认不出的部分写进 reason。
func parseCustomLine(line string) (rules []customRule, reason string) {
	line = strings.TrimSpace(strings.TrimPrefix(line, "- "))
	line = strings.Trim(line, `'"`)
	if i := strings.Index(line, " #"); i >= 0 {
		line = strings.TrimSpace(line[:i])
	}
	parts := strings.Split(customSeparator.Replace(line), ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	word := strings.ToUpper(parts[0])
	if kind, ok := customLinePrefix[word]; ok && len(parts) >= 2 {
		// 行尾的策略名与 no-resolve 不要：走向由页面上选的方式决定
		r, why := customTyped(kind, parts[1])
		if why != "" {
			return nil, why
		}
		return []customRule{r}, ""
	}
	if len(parts) >= 2 && customTypeWord.MatchString(parts[0]) && !strings.Contains(parts[0], ".") {
		return nil, parts[0] + " 类型的规则写不进所有客户端的订阅"
	}
	var bad []string
	for _, p := range parts {
		fields := strings.Fields(p)
		// hosts 文件的写法：第一列是屏蔽用的回环地址
		if len(fields) >= 2 && (fields[0] == "0.0.0.0" || fields[0] == "127.0.0.1" || fields[0] == "::") {
			fields = fields[1:]
		}
		for _, tok := range fields {
			if r, ok := customToken(tok); ok {
				rules = append(rules, r)
			} else {
				bad = append(bad, tok)
			}
		}
	}
	if len(bad) > 0 {
		reason = "不像域名或 IP：" + strings.Join(bad, " ")
	}
	return rules, reason
}

// customTyped 处理写明了类型的规则；此时不要求域名带点（如 DOMAIN-SUFFIX,cn）。
func customTyped(kind, value string) (customRule, string) {
	switch kind {
	case "ip_cidr":
		if pre, ok := customPrefix(value); ok {
			return customRule{kind, pre.String()}, ""
		}
		return customRule{}, "不是有效的 IP 段：" + value
	case "domain_keyword":
		v := strings.ToLower(value)
		if v == "" || strings.ContainsAny(v, " \t") {
			return customRule{}, "关键词无效：" + value
		}
		return customRule{kind, v}, ""
	}
	ascii, err := idna.Lookup.ToASCII(strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(value, "*."), "+."), "."))
	if err != nil || ascii == "" || len(ascii) > 253 {
		return customRule{}, "不是有效的域名：" + value
	}
	return customRule{kind, ascii}, ""
}

// customToken 认没写类型的一项：裸域名、通配写法、网址、IP 或 IP 段；带前缀的 Xray 写法与广告规则的 ||x^ 也认。
func customToken(tok string) (customRule, bool) {
	switch {
	case strings.HasPrefix(tok, "full:"):
		r, why := customTyped("domain", tok[5:])
		return r, why == ""
	case strings.HasPrefix(tok, "domain:"):
		r, why := customTyped("domain_suffix", tok[7:])
		return r, why == ""
	case strings.HasPrefix(tok, "keyword:"):
		r, why := customTyped("domain_keyword", tok[8:])
		return r, why == ""
	}
	if pre, ok := customPrefix(tok); ok {
		return customRule{"ip_cidr", pre.String()}, true
	}
	tok = strings.TrimSuffix(strings.TrimPrefix(tok, "||"), "^")
	tok = strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(tok, "*."), "+."), ".")
	h := normalizePublicHost(tok)
	if h == "" {
		return customRule{}, false
	}
	if pre, ok := customPrefix(h); ok {
		return customRule{"ip_cidr", pre.String()}, true
	}
	return customRule{"domain_suffix", h}, true
}

func customPrefix(v string) (netip.Prefix, bool) {
	if pre, err := netip.ParsePrefix(v); err == nil {
		if a := pre.Addr(); a.Is4In6() {
			pre = netip.PrefixFrom(a.Unmap(), max(pre.Bits()-96, 0))
		}
		return pre.Masked(), true
	}
	if a, err := netip.ParseAddr(v); err == nil {
		a = a.Unmap()
		return netip.PrefixFrom(a, a.BitLen()), true
	}
	return netip.Prefix{}, false
}

// mergeCustomPrefixes 去掉被覆盖的 IP 段，并把相邻的两半合成上一级。
func mergeCustomPrefixes(in []netip.Prefix) []netip.Prefix {
	slices.SortFunc(in, func(a, b netip.Prefix) int {
		if c := a.Addr().Compare(b.Addr()); c != 0 {
			return c
		}
		return a.Bits() - b.Bits()
	})
	var out []netip.Prefix
	for _, p := range in {
		if n := len(out); n > 0 && out[n-1].Contains(p.Addr()) && out[n-1].Bits() <= p.Bits() {
			continue
		}
		out = append(out, p)
		for n := len(out); n >= 2; n = len(out) {
			a, b := out[n-2], out[n-1]
			if a.Bits() != b.Bits() || a.Bits() == 0 {
				break
			}
			parent := netip.PrefixFrom(a.Addr(), a.Bits()-1).Masked()
			if parent.Addr() != a.Addr() || !parent.Contains(b.Addr()) {
				break
			}
			out = append(out[:n-2], parent)
		}
	}
	return out
}

// optimizeCustomRules 把原文整理成规则：去重、去掉被上级后缀或关键词覆盖的域名、合并 IP 段。
func optimizeCustomRules(source string) customResult {
	res := customResult{Rules: []customRule{}, Counts: map[string]int{}, Unused: []customUnused{}, Warnings: []string{}}
	var parsed []customRule
	for i, raw := range strings.Split(strings.TrimPrefix(source, "\xef\xbb\xbf"), "\n") {
		line := strings.TrimSpace(width.Fold.String(raw))
		if customCommentLine(line) {
			continue
		}
		res.Total++
		rules, reason := parseCustomLine(line)
		parsed = append(parsed, rules...)
		if reason != "" {
			res.Unused = append(res.Unused, customUnused{Line: i + 1, Text: strings.TrimSpace(raw), Reason: reason})
		}
	}
	seen := map[customRule]bool{}
	suffixes := map[string]bool{}
	var keywords []string
	var prefixes []netip.Prefix
	var uniq []customRule
	for _, r := range parsed {
		if seen[r] {
			continue
		}
		seen[r] = true
		switch r.Kind {
		case "domain_suffix":
			suffixes[r.Value] = true
		case "domain_keyword":
			keywords = append(keywords, r.Value)
		case "ip_cidr":
			prefixes = append(prefixes, netip.MustParsePrefix(r.Value))
			continue
		}
		uniq = append(uniq, r)
	}
	if len(keywords) > customMaxKeywordCover {
		keywords = nil
	}
	covered := func(r customRule) bool {
		for _, k := range keywords {
			if k != r.Value && strings.Contains(r.Value, k) {
				return true
			}
		}
		if r.Kind == "domain_keyword" {
			return false
		}
		if r.Kind == "domain" && suffixes[r.Value] {
			return true
		}
		for v := r.Value; ; {
			i := strings.IndexByte(v, '.')
			if i < 0 {
				return false
			}
			v = v[i+1:]
			if suffixes[v] {
				return true
			}
		}
	}
	for _, r := range uniq {
		if !covered(r) {
			res.Rules = append(res.Rules, r)
		}
	}
	for _, p := range mergeCustomPrefixes(prefixes) {
		res.Rules = append(res.Rules, customRule{"ip_cidr", p.String()})
	}
	res.Reduced = len(parsed) - len(res.Rules)
	for _, r := range res.Rules {
		res.Counts[r.Kind]++
		switch r.Kind {
		case "domain_suffix":
			if !strings.Contains(r.Value, ".") {
				res.Warnings = append(res.Warnings, "后缀 "+r.Value+" 范围很大，会匹配这个顶级域名下的全部网站")
			}
		case "domain_keyword":
			if len(r.Value) < 3 {
				res.Warnings = append(res.Warnings, "关键词 "+r.Value+" 太短，可能匹配到不相关的网站")
			}
		case "ip_cidr":
			if p := netip.MustParsePrefix(r.Value); p.Addr().Is4() && p.Bits() < 8 || p.Addr().Is6() && p.Bits() < 16 {
				res.Warnings = append(res.Warnings, "IP 段 "+r.Value+" 范围很大")
			}
		}
	}
	return res
}

func customLines(rules []customRule) []string {
	out := make([]string, len(rules))
	for i, r := range rules {
		out[i] = r.line()
	}
	return out
}

// customPlanRules 把规范写法转成订阅规则，同一类型放在一起。
func customPlanRules(lines []string) []subout.Rule {
	byKind := map[string][]string{}
	for _, l := range lines {
		word, value, ok := strings.Cut(l, ",")
		if !ok {
			continue
		}
		if kind, ok := customLinePrefix[word]; ok {
			byKind[kind] = append(byKind[kind], value)
		}
	}
	var out []subout.Rule
	for _, kind := range []string{"domain", "domain_suffix", "domain_keyword", "ip_cidr"} {
		if len(byKind[kind]) > 0 {
			out = append(out, subout.Rule{Kind: kind, Values: byKind[kind]})
		}
	}
	return out
}

func customView(c contracts.CustomRules) gin.H {
	entries := c.Entries
	if entries == nil {
		entries = []contracts.CustomEntry{}
	}
	active := 0
	for _, e := range entries {
		if e.Enabled {
			active++
		}
	}
	return gin.H{"version": c.Version, "revision": c.Revision, "entries": entries, "pending": c.Pending, "active_count": active, "source": c.Source, "check": c.Check, "result": parseCustomEntries(c.Source)}
}

type customRequest struct {
	Revision *int                     `json:"revision"`
	Entries  *[]contracts.CustomEntry `json:"entries"`
	Source   string                   `json:"source"`
}

// customPlanned 用候选设置生成计划：节点取用户第一条在生效时间内、有节点的链接；没有时计划里没有节点。
func (s *Server) customPlanned(ctx context.Context, u *ent.User, pol contracts.ProxyPolicy) (planned, error) {
	links, err := s.Store.Ent.Link.Query().Where(link.UserIDEQ(u.ID)).Order(ent.Asc(link.FieldID)).All(ctx)
	if err != nil {
		return planned{}, err
	}
	for _, l := range links {
		if windowState(u, l, time.Now()) != "active" {
			continue
		}
		entries, err := s.linkEntries(ctx, u, l)
		if err != nil {
			return planned{}, err
		}
		p, err := s.planEntries(ctx, u, entries, false)
		if err != nil {
			return planned{}, err
		}
		if len(p.Entries) > 0 {
			p.Policy = pol
			return p, nil
		}
	}
	return planned{Policy: pol}, nil
}

// checkCustomRules 用真实内核加载候选设置生成的 Clash 与 sing-box 配置（清单内联）。只证明客户端能加载，不证明网站命中。
func (s *Server) checkCustomRules(ctx context.Context, u *ent.User, pol contracts.ProxyPolicy) contracts.CustomCheck {
	// 主动保存/重试不能被上次环境故障留下的失败缓存挡住。
	ctx = context.WithValue(ctx, freshSubCheckKey{}, true)
	res := contracts.CustomCheck{Status: "unknown", At: time.Now().UTC()}
	p, err := s.customPlanned(ctx, u, pol)
	if err != nil {
		res.Detail = "读取链接失败：" + err.Error()
		return res
	}
	if len(p.Entries) == 0 {
		res.Detail = "还没有可用的链接和节点，无法生成配置来检查"
		return res
	}
	plan, items := s.subPlan(ctx, p)
	var unknown []string
	passed := 0
	for _, target := range []string{"clash", "singbox"} {
		kernel := checkKernel(target)
		jobStep(ctx, "用 "+kernel+" 检查候选订阅", 30*time.Second)
		body, _, err := subout.RenderPlan(items, target, plan)
		if err != nil {
			res.Status, res.Detail = "failed", kernel+"：生成配置失败："+err.Error()
			return res
		}
		switch r := s.checkSubscription(ctx, target, body); r.Status {
		case "passed":
			passed++
		case "failed":
			res.Status, res.Detail = "failed", kernel+"："+r.Error
			return res
		default:
			unknown = append(unknown, kernel+"："+r.Error)
		}
	}
	if len(unknown) > 0 {
		res.Detail = strings.Join(unknown, "；")
		return res
	}
	res.Status = "passed"
	return res
}

// 整份替换与单条修改使用相同的版本校验、内核检查与提交逻辑。
func (s *Server) putCustomRules(c *gin.Context, admin bool) {
	u, ok := s.proxySubject(c, admin)
	if !ok {
		return
	}
	var req customRequest
	if !bind(c, &req) {
		return
	}
	if req.Entries != nil {
		s.writeCustomEntries(c, u, req.Revision, *req.Entries, req.Source, nil)
		return
	}
	// 旧客户端的整份替换没有版本，不能覆盖新版的逐条修改。
	failure(c, 400, "invalid_custom_entry")
}

// customTestTarget 把测试输入整理成小写域名或 IP；单段的名称（如 cn）不算域名。
func customTestTarget(raw string) string {
	h := normalizePublicHost(raw)
	if a, err := netip.ParseAddr(h); err == nil {
		return a.Unmap().String()
	}
	return h
}

// testCustomRules 算一个目标按粘贴框当前内容的走向：只计算，不发起连接，不写入。
func (s *Server) testCustomRules(c *gin.Context, admin bool) {
	u, ok := s.proxySubject(c, admin)
	if !ok {
		return
	}
	var req struct {
		customRequest
		Target string `json:"target"`
	}
	if !bind(c, &req) {
		return
	}
	target := customTestTarget(req.Target)
	if target == "" {
		failure(c, 400, "invalid_test_target")
		return
	}
	pol := loadPolicy(u)
	if req.Revision != nil && *req.Revision != pol.Revision {
		failure(c, 409, "custom_revision_conflict")
		return
	}
	if req.Entries != nil {
		pol.Custom.Entries = slices.Clone(*req.Entries)
		if !s.validateCustomEntries(c, u, loadPolicy(u), pol.Custom.Entries) {
			return
		}
	}
	s.respondCustomMatch(c, u, pol, target, gin.H{"revision": pol.Revision})
}
