package center

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/text/width"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/packages/contracts"
)

// 读取时转换保持无写入；首次保存连同旧规则集快照一起持久化。
func migrateCustomPolicy(p contracts.ProxyPolicy) contracts.ProxyPolicy {
	c := p.Custom
	if c.Version >= 2 {
		return p
	}
	rules := map[string]string{}
	for k, v := range p.Rules {
		rules[k] = v
	}
	c.Legacy = &contracts.CustomLegacy{Mode: c.Mode, SourceMode: c.SourceMode, Source: c.Source, Lines: slices.Clone(c.Lines), Rules: rules, RulesFile: customRuleFile(c)}
	c.Version = 2
	c.Entries = []contracts.CustomEntry{}
	convert := func(lines []string, mode string) []contracts.CustomEntry {
		out := []contracts.CustomEntry{}
		for _, r := range customPlanRules(lines) {
			for _, value := range r.Values {
				target := "direct"
				if mode == contracts.CustomOnly {
					target = "select"
				}
				out = append(out, contracts.CustomEntry{ID: "legacy-" + digest(r.Kind + ":" + value)[:20], Kind: r.Kind, Value: value, Target: target, Enabled: mode != "", LastProxyTarget: "select"})
			}
		}
		return out
	}
	lines := c.Lines
	if c.Mode == "" && (c.Check == nil || c.Check.Status != "failed") {
		lines = customLines(optimizeCustomRules(c.Source).Rules)
	}
	c.Entries = convert(lines, c.Mode)
	if c.Check != nil && c.Check.Status == "failed" {
		mode := c.SourceMode
		if mode == "off" {
			mode = ""
		}
		c.Pending = &contracts.CustomCandidate{Entries: convert(customLines(optimizeCustomRules(c.Source).Rules), mode), Source: c.Source}
	}
	if c.Mode == contracts.CustomOnly {
		p.Rules = map[string]string{}
		for _, cat := range ruleCategories {
			p.Rules[cat.Key] = "direct"
		}
	}
	p.Custom = c
	return p
}

// 新清单只去重，不精简覆盖关系：重排、启停或不同走向可能让原本被遮住的规则生效。
func parseCustomEntries(source string) customResult {
	r := customResult{Rules: []customRule{}, Counts: map[string]int{}, Unused: []customUnused{}, Warnings: []string{}}
	seen := map[customRule]bool{}
	for i, raw := range strings.Split(strings.TrimPrefix(source, "\xef\xbb\xbf"), "\n") {
		line := strings.TrimSpace(width.Fold.String(raw))
		if customCommentLine(line) {
			continue
		}
		r.Total++
		items, why := parseCustomLine(line)
		if why != "" {
			r.Unused = append(r.Unused, customUnused{Line: i + 1, Text: strings.TrimSpace(raw), Reason: why})
		}
		for _, item := range items {
			if seen[item] {
				r.Reduced++
				continue
			}
			seen[item] = true
			r.Rules = append(r.Rules, item)
			r.Counts[item.Kind]++
			r.Warnings = append(r.Warnings, optimizeCustomRules(item.line()).Warnings...)
		}
	}
	return r
}

type customEntryRequest struct {
	Revision *int    `json:"revision"`
	Source   string  `json:"source"`
	Target   *string `json:"target"`
	Kind     *string `json:"kind"`
	Value    *string `json:"value"`
	Enabled  *bool   `json:"enabled"`
	Position *int    `json:"position"`
}

func (s *Server) parseCustomList(c *gin.Context, admin bool) {
	if _, ok := s.subject(c, admin); !ok {
		return
	}
	var req customEntryRequest
	if !bind(c, &req) {
		return
	}
	if len(req.Source) > customMaxSource {
		failure(c, 400, "custom_rules_too_large")
		return
	}
	r := parseCustomEntries(req.Source)
	if len(r.Rules) > customMaxRules {
		failure(c, 400, "custom_rules_too_large")
		return
	}
	c.JSON(200, r)
}

func (s *Server) appendCustomEntries(c *gin.Context, admin bool) {
	u, ok := s.subject(c, admin)
	if !ok {
		return
	}
	var req customEntryRequest
	if !bind(c, &req) {
		return
	}
	if len(req.Source) > customMaxSource {
		failure(c, 400, "custom_rules_too_large")
		return
	}
	r := parseCustomEntries(req.Source)
	entries := slices.Clone(loadPolicy(u).Custom.Entries)
	seen := map[customRule]bool{}
	for _, e := range entries {
		seen[customRule{e.Kind, e.Value}] = true
	}
	target := "select"
	if req.Target != nil {
		target = *req.Target
	}
	for _, item := range r.Rules {
		if seen[item] {
			r.Reduced++
			continue
		}
		last := target
		if last == "direct" {
			last = "select"
		}
		entries = append(entries, contracts.CustomEntry{ID: randomHex(12), Kind: item.Kind, Value: item.Value, Target: target, Enabled: true, LastProxyTarget: last})
		seen[item] = true
	}
	s.writeCustomEntries(c, u, req.Revision, entries, req.Source, &r)
}

func (s *Server) changeCustomEntry(c *gin.Context, admin bool) {
	u, ok := s.subject(c, admin)
	if !ok {
		return
	}
	var req customEntryRequest
	if !bind(c, &req) {
		return
	}
	entries := slices.Clone(loadPolicy(u).Custom.Entries)
	i := slices.IndexFunc(entries, func(e contracts.CustomEntry) bool { return e.ID == c.Param("entry") })
	if i < 0 {
		failure(c, 404, "custom_entry_not_found")
		return
	}
	if c.Request.Method == "DELETE" {
		entries = slices.Delete(entries, i, i+1)
	} else {
		e := &entries[i]
		if req.Kind != nil {
			e.Kind = *req.Kind
		}
		if req.Value != nil {
			e.Value = *req.Value
		}
		if req.Target != nil {
			if e.Target != "direct" {
				e.LastProxyTarget = e.Target
			}
			e.Target = *req.Target
			if e.Target != "direct" {
				e.LastProxyTarget = e.Target
			}
		}
		if req.Enabled != nil {
			e.Enabled = *req.Enabled
		}
		if req.Position != nil {
			if *req.Position < 0 || *req.Position >= len(entries) {
				failure(c, 400, "invalid_custom_position")
				return
			}
			value := entries[i]
			entries = slices.Delete(entries, i, i+1)
			entries = slices.Insert(entries, *req.Position, value)
		}
	}
	s.writeCustomEntries(c, u, req.Revision, entries, "", nil)
}

func policyFingerprint(p contracts.ProxyPolicy) string {
	b, _ := json.Marshal(p)
	return digest(string(b))
}

func (s *Server) validateCustomEntries(c *gin.Context, u *ent.User, base contracts.ProxyPolicy, entries []contracts.CustomEntry) bool {
	ctx := c.Request.Context()
	if len(entries) > customMaxRules {
		failure(c, 400, "custom_rules_too_large")
		return false
	}
	types, err := s.userTypes(ctx, u)
	if err != nil {
		failInternal(c, 503, "query_failed", err)
		return false
	}
	targets := map[string]bool{"direct": true, "auto": true, "select": true}
	for _, typ := range types {
		targets[typ.Key] = true
	}
	old := map[string]contracts.CustomEntry{}
	for _, e := range base.Custom.Entries {
		old[e.ID] = e
	}
	ids, rules := map[string]bool{}, map[customRule]bool{}
	total := 0
	for i := range entries {
		e := &entries[i]
		if e.ID == "" {
			e.ID = randomHex(12)
		}
		if len(e.ID) > 80 || ids[e.ID] {
			failure(c, 400, "invalid_custom_entry")
			return false
		}
		ids[e.ID] = true
		if _, exists := customCanonical[e.Kind]; !exists {
			failure(c, 400, "invalid_custom_entry")
			return false
		}
		if strings.ContainsAny(e.Value, ",\r\n") {
			failure(c, 400, "invalid_custom_entry")
			return false
		}
		rule, why := customTyped(e.Kind, strings.TrimSpace(e.Value))
		if why != "" {
			failDetail(c, 400, "invalid_custom_entry", why, nil)
			return false
		}
		e.Value = rule.Value
		total += len(e.Value)
		if total > customMaxSource {
			failure(c, 400, "custom_rules_too_large")
			return false
		}
		if rules[rule] {
			failure(c, 400, "duplicate_custom_entry")
			return false
		}
		rules[rule] = true
		if !targets[e.Target] && old[e.ID].Target != e.Target {
			failure(c, 400, "invalid_rule_target")
			return false
		}
		if e.Target == "" {
			failure(c, 400, "invalid_rule_target")
			return false
		}
		if e.LastProxyTarget == "" || e.LastProxyTarget == "direct" {
			e.LastProxyTarget = "select"
		}
		if !targets[e.LastProxyTarget] && old[e.ID].LastProxyTarget != e.LastProxyTarget {
			failure(c, 400, "invalid_rule_target")
			return false
		}
	}
	return true
}

// 内核检查在锁外执行；提交时验证同一份基础设置，锁内重读避免与分类设置相互覆盖。
func (s *Server) writeCustomEntries(c *gin.Context, u *ent.User, revision *int, entries []contracts.CustomEntry, source string, parsed *customResult) {
	ctx := c.Request.Context()
	base := loadPolicy(u)
	if revision == nil || *revision != base.Custom.Revision {
		failure(c, 409, "custom_revision_conflict")
		return
	}
	if len(entries) > customMaxRules || len(source) > customMaxSource {
		failure(c, 400, "custom_rules_too_large")
		return
	}
	if !s.validateCustomEntries(c, u, base, entries) {
		return
	}
	if dryRun(c, dryResult{Preview: []string{fmt.Sprintf("清单共 %d 条；按顺序优先匹配，保存后客户端下次更新订阅时生效", len(entries))}}) {
		return
	}
	candidate := base
	candidate.Custom.Entries = entries
	candidate.Custom.Pending = nil
	jobStep(ctx, "检查完整候选订阅", 60*time.Second)
	check := s.checkCustomRules(ctx, u, candidate)
	if ctx.Err() != nil {
		failDetail(c, 409, "job_cancelled", "已取消，没有保存任何改动", nil)
		return
	}
	if !jobCommit(c) {
		return
	}
	jobStep(ctx, "保存清单", 0)
	conflict := errors.New("custom settings changed during check")
	err := s.Store.Lock.Do(ctx, "proxy-policy:"+itoa(u.ID), func(ctx context.Context) error {
		latest, err := s.Store.Ent.User.Get(ctx, u.ID)
		if err != nil {
			return err
		}
		pol := loadPolicy(latest)
		if policyFingerprint(pol) != policyFingerprint(base) {
			return conflict
		}
		pol.Custom.Check = &check
		if source != "" {
			pol.Custom.Source = source
		}
		pol.Custom.Revision++
		if check.Status == "failed" {
			pol.Custom.Pending = &contracts.CustomCandidate{Entries: entries, Source: source}
		} else {
			pol.Custom.Entries = entries
			pol.Custom.Pending = nil
		}
		u, err = latest.Update().SetProxyPolicy(policyMap(pol)).Save(ctx)
		return err
	})
	if errors.Is(err, conflict) {
		failure(c, 409, "custom_revision_conflict")
		return
	}
	if err != nil {
		failInternal(c, 500, "update_failed", err)
		return
	}
	if check.Status == "failed" {
		s.audit(c, "proxy.custom_rules", "user:"+itoa(u.ID), "failed")
		failDetail(c, 422, "custom_check_failed", check.Detail, nil)
		return
	}
	s.audit(c, "proxy.custom_rules", "user:"+itoa(u.ID), "ok")
	v, err := s.proxyView(ctx, u)
	if err != nil {
		failInternal(c, 503, "query_failed", err)
		return
	}
	if parsed != nil {
		v["parsed"] = parsed
	}
	c.JSON(200, v)
}

func (s *Server) testCustomEntry(c *gin.Context, admin bool) {
	u, ok := s.subject(c, admin)
	if !ok {
		return
	}
	var req struct {
		Target   string                 `json:"target"`
		Revision *int                   `json:"revision"`
		Entry    *contracts.CustomEntry `json:"entry"`
	}
	if !bind(c, &req) {
		return
	}
	pol := loadPolicy(u)
	if req.Revision == nil || *req.Revision != pol.Custom.Revision {
		failure(c, 409, "custom_revision_conflict")
		return
	}
	i := slices.IndexFunc(pol.Custom.Entries, func(e contracts.CustomEntry) bool { return e.ID == c.Param("entry") })
	if i < 0 {
		failure(c, 404, "custom_entry_not_found")
		return
	}
	pol.Custom.Entries = slices.Clone(pol.Custom.Entries)
	if req.Entry != nil {
		e := *req.Entry
		e.ID = pol.Custom.Entries[i].ID
		pol.Custom.Entries[i] = e
		if !s.validateCustomEntries(c, u, loadPolicy(u), pol.Custom.Entries) {
			return
		}
	}
	target := customTestTarget(req.Target)
	if target == "" {
		failure(c, 400, "invalid_test_target")
		return
	}
	e := pol.Custom.Entries[i]
	addr, _ := netip.ParseAddr(target)
	// 内网保护规则不参与“本条是否匹配”的判断。
	matched := false
	switch e.Kind {
	case "domain":
		matched = !addr.IsValid() && target == e.Value
	case "domain_suffix":
		matched = !addr.IsValid() && (target == e.Value || strings.HasSuffix(target, "."+e.Value))
	case "domain_keyword":
		matched = !addr.IsValid() && strings.Contains(target, e.Value)
	case "ip_cidr":
		pre, err := netip.ParsePrefix(e.Value)
		matched = err == nil && addr.IsValid() && pre.Contains(addr.Unmap())
	}
	s.respondCustomMatch(c, u, pol, target, gin.H{"entry_id": e.ID, "entry_matches": matched, "entry_enabled": e.Enabled, "revision": pol.Custom.Revision})
}

func (s *Server) respondCustomMatch(c *gin.Context, u *ent.User, pol contracts.ProxyPolicy, target string, out gin.H) {
	p, err := s.customPlanned(c.Request.Context(), u, pol)
	if err != nil {
		failInternal(c, 503, "query_failed", err)
		return
	}
	plan, items := s.subPlan(c.Request.Context(), p)
	names := make([]string, len(p.Entries))
	for i, e := range p.Entries {
		names[i] = e.Name
	}
	out["target"], out["match"] = target, plan.Match(target, names, items)
	c.JSON(200, out)
}
