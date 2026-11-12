package center

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/predicate"
	"github.com/shusfun/weir/internal/store/ent/subgrant"
	"github.com/shusfun/weir/internal/store/ent/subnode"
	"github.com/shusfun/weir/internal/store/ent/subprofile"
	"github.com/shusfun/weir/internal/store/ent/subsource"
	"github.com/shusfun/weir/internal/store/ent/user"
	"github.com/shusfun/weir/internal/sub"
	"github.com/shusfun/weir/internal/subgroup"
	"github.com/shusfun/weir/packages/contracts"
)

// profileSettings 是订阅组可覆盖的设置；缺省字段沿用全局默认。
const maxSubscriptionOutputBytes = 16 << 20

type profileSettings struct {
	Engine           *string `json:"engine,omitempty"`
	Template         *string `json:"template,omitempty"`
	RuleLevel        *string `json:"rule_level,omitempty"`
	FlagEmoji        *bool   `json:"flag_emoji,omitempty"`
	PrependManual    *bool   `json:"prepend_manual,omitempty"`
	ManualPrefix     *string `json:"manual_prefix,omitempty"`
	PrependSources   *bool   `json:"prepend_sources,omitempty"`
	PrependGroupName *bool   `json:"prepend_group_name,omitempty"`
	FilterInclude    *string `json:"filter_include,omitempty"`
	FilterExclude    *string `json:"filter_exclude,omitempty"`
	TrafficNodes     *bool   `json:"traffic_nodes,omitempty"`
	EnableUDP        *bool   `json:"enable_udp,omitempty"`
	EnableTFO        *bool   `json:"enable_tfo,omitempty"`
	SkipCertVerify   *bool   `json:"skip_cert_verify,omitempty"`
	DNSMode          *string `json:"dns_mode,omitempty"`
	CustomDNS        *string `json:"custom_dns,omitempty"`
	RegionOverrides  *[]any  `json:"region_overrides,omitempty"`
}

func decodeProfileSettings(m map[string]any) (profileSettings, error) {
	var ps profileSettings
	if len(m) == 0 {
		return ps, nil
	}
	b, _ := json.Marshal(m)
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&ps); err != nil {
		return ps, err
	}
	if ps.Engine != nil && !validSubscriptionEngine(*ps.Engine) {
		return ps, errors.New("invalid_engine")
	}
	var regions []any
	for _, field := range []struct {
		name string
		text *string
	}{{"filter_include", ps.FilterInclude}, {"filter_exclude", ps.FilterExclude}} {
		if field.text != nil {
			if err := sub.ValidateFilterRules(*field.text); err != nil {
				return ps, fmt.Errorf("invalid_%s: %w", field.name, err)
			}
		}
	}
	if ps.RegionOverrides != nil {
		regions = *ps.RegionOverrides
	}
	return ps, checkSubscriptionRendering(deref(ps.DNSMode), deref(ps.CustomDNS), regions)
}

func (s *Server) profileView(ctx context.Context, p *ent.SubProfile) gin.H {
	grants, _ := s.Store.Ent.SubGrant.Query().Where(subgrant.ProfileIDEQ(p.ID)).All(ctx)
	users := make([]int, 0, len(grants))
	for _, g := range grants {
		users = append(users, g.UserID)
	}
	v := gin.H{"id": p.ID, "name": p.Name, "enabled": p.Enabled, "all_sources": p.AllSources, "source_ids": p.SourceIds, "node_ids": p.NodeIds,
		"operators": p.Operators, "settings": p.Settings, "expires_at": p.ExpiresAt, "downloads": p.Downloads, "position": p.Position,
		"members": p.Members, "user_ids": users, "shared": p.ShareToken != "", "share_expires_at": p.ShareExpiresAt}
	return v
}

func (s *Server) listSubProfiles(c *gin.Context) {
	ctx := c.Request.Context()
	items, err := s.Store.Ent.SubProfile.Query().Order(ent.Asc(subprofile.FieldPosition), ent.Asc(subprofile.FieldID)).All(ctx)
	if err != nil {
		failInternal(c, 503, "query_failed", err)
		return
	}
	out := make([]gin.H, 0, len(items))
	for _, it := range items {
		out = append(out, s.profileView(ctx, it))
	}
	c.JSON(200, gin.H{"items": out})
}

func (s *Server) saveProfile(c *gin.Context) {
	if err := s.Store.Lock.Do(c.Request.Context(), "subscription-profiles", func(context.Context) error {
		s.saveProfileLocked(c)
		return nil
	}); err != nil {
		failInternal(c, 503, "save_failed", err)
	}
}

func (s *Server) saveProfileLocked(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	var req struct {
		Name       *string                         `json:"name"`
		Enabled    *bool                           `json:"enabled"`
		AllSources *bool                           `json:"all_sources"`
		SourceIDs  *[]int                          `json:"source_ids"`
		NodeIDs    *[]int                          `json:"node_ids"`
		Members    *[]contracts.SubscriptionMember `json:"members"`
		Operators  *[]any                          `json:"operators"`
		Settings   *map[string]any                 `json:"settings"`
		ExpiresAt  json.RawMessage                 `json:"expires_at"`
	}
	if !bind(c, &req) {
		return
	}
	ctx := c.Request.Context()
	var m *ent.SubProfileMutation
	var create *ent.SubProfileCreate
	var update *ent.SubProfileUpdateOne
	candidate := &ent.SubProfile{Enabled: true}
	if id == 0 {
		if req.Name == nil {
			failure(c, 400, "invalid_name")
			return
		}
		create = s.Store.Ent.SubProfile.Create()
		n, _ := s.Store.Ent.SubProfile.Query().Count(ctx)
		create.SetPosition(n)
		m = create.Mutation()
	} else {
		old, err := s.Store.Ent.SubProfile.Get(ctx, id)
		if err != nil {
			failure(c, 404, "profile_not_found")
			return
		}
		update = old.Update()
		m = update.Mutation()
		*candidate = *old
	}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" || len(name) > 100 {
			failure(c, 400, "invalid_name")
			return
		}
		m.SetName(name)
	}
	if req.Enabled != nil {
		m.SetEnabled(*req.Enabled)
	}
	if req.AllSources != nil {
		m.SetAllSources(*req.AllSources)
		candidate.AllSources = *req.AllSources
	}
	if req.Members != nil && (req.SourceIDs != nil || req.NodeIDs != nil) {
		failure(c, 400, "invalid_profile_members")
		return
	}
	// 选择的源与节点必须存在；保存顺序即输出顺序。
	if req.SourceIDs != nil {
		ids := dedupe(*req.SourceIDs)
		if n, _ := s.Store.Ent.SubSource.Query().Where(subsource.IDIn(ids...)).Count(ctx); n != len(ids) {
			failure(c, 400, "unknown_source")
			return
		}
		m.SetSourceIds(ids)
	}
	if req.NodeIDs != nil {
		ids := dedupe(*req.NodeIDs)
		if n, _ := s.Store.Ent.SubNode.Query().Where(subnode.IDIn(ids...)).Count(ctx); n != len(ids) {
			failure(c, 400, "unknown_node")
			return
		}
		m.SetNodeIds(ids)
	}
	if req.Members != nil {
		candidate.Members = append([]contracts.SubscriptionMember{}, (*req.Members)...)
		m.SetMembers(candidate.Members)
		var sources, nodes []int
		for _, member := range candidate.Members {
			switch member.Kind {
			case "source":
				sources = append(sources, member.ID)
			case "node":
				nodes = append(nodes, member.ID)
			}
		}
		m.SetSourceIds(sources)
		m.SetNodeIds(nodes)
	} else if req.SourceIDs != nil || req.NodeIDs != nil || candidate.AllSources {
		candidate.Members = nil
		m.ClearMembers()
	}
	if err := s.checkProfileGraph(ctx, candidate); err != nil {
		failDetail(c, 400, "invalid_profile_members", err.Error(), nil)
		return
	}
	if req.Operators != nil {
		if err := checkOperators(*req.Operators); err != nil {
			failDetail(c, 400, "invalid_operators", err.Error(), nil)
			return
		}
		m.SetOperators(*req.Operators)
	}
	if req.Settings != nil {
		ps, err := decodeProfileSettings(*req.Settings)
		if err != nil {
			code := "invalid_profile_settings"
			if strings.HasPrefix(err.Error(), "invalid_filter_include:") {
				code = "invalid_filter_include"
			} else if strings.HasPrefix(err.Error(), "invalid_filter_exclude:") {
				code = "invalid_filter_exclude"
			} else if strings.HasPrefix(err.Error(), "invalid_") || strings.HasPrefix(err.Error(), "custom_dns_") || strings.HasPrefix(err.Error(), "too_many_region_") {
				code = "invalid_" + subscriptionRenderingField(err)
			}
			failDetail(c, 400, code, err.Error(), nil)
			return
		}
		if ps.RuleLevel != nil && !validRuleLevel(*ps.RuleLevel) {
			failure(c, 400, "invalid_profile_settings")
			return
		}
		if ps.Template != nil {
			if _, err = s.resolveTemplate(ctx, *ps.Template); err != nil {
				failDetail(c, 400, "invalid_template", err.Error(), nil)
				return
			}
		}
		m.SetSettings(*req.Settings)
	}
	if len(req.ExpiresAt) > 0 {
		if string(req.ExpiresAt) == "null" {
			m.ClearExpiresAt()
		} else {
			var t time.Time
			if json.Unmarshal(req.ExpiresAt, &t) != nil {
				failure(c, 400, "invalid_expires_at")
				return
			}
			m.SetExpiresAt(t)
		}
	}
	if dryRun(c, dryResult{Preview: []string{"保存订阅组；分配了它的用户下次更新订阅时拿到新内容"}}) {
		return
	}
	var p *ent.SubProfile
	var err error
	if create != nil {
		p, err = create.Save(ctx)
	} else {
		p, err = update.Save(ctx)
	}
	if err != nil {
		failure(c, 400, "save_failed")
		return
	}
	s.invalidateGroups(ctx)
	s.audit(c, "subscription.profile.save", "profile:"+itoa(p.ID), "ok")
	c.JSON(200, s.profileView(ctx, p))
}

func (s *Server) deleteProfile(c *gin.Context) {
	if err := s.Store.Lock.Do(c.Request.Context(), "subscription-profiles", func(context.Context) error {
		s.deleteProfileLocked(c)
		return nil
	}); err != nil {
		failInternal(c, 503, "delete_failed", err)
	}
}

func (s *Server) deleteProfileLocked(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	parents, err := s.profileParents(ctx, id)
	if err != nil {
		failInternal(c, 503, "query_failed", err)
		return
	}
	if len(parents) > 0 {
		failDetail(c, 409, "profile_in_use", strings.Join(parents, "、"), nil)
		return
	}
	if !s.checkExitSourceDeletion(c, 0, id) {
		return
	}
	if c.GetBool(dryRunKey) {
		p, err := s.Store.Ent.SubProfile.Get(ctx, id)
		if err != nil {
			failure(c, 404, "profile_not_found")
			return
		}
		users, _ := s.Store.Ent.SubGrant.Query().Where(subgrant.ProfileIDEQ(id)).Count(ctx)
		dryRun(c, dryResult{Preview: []string{fmt.Sprintf("删除订阅组「%s」：分配给 %d 个用户的订阅地址随即失效", p.Name, users)}})
		return
	}
	if _, err := s.Store.Ent.SubGrant.Delete().Where(subgrant.ProfileIDEQ(id)).Exec(ctx); err != nil {
		failInternal(c, 500, "delete_failed", err)
		return
	}
	if err := s.Store.Ent.SubProfile.DeleteOneID(id).Exec(ctx); err != nil {
		failure(c, 404, "profile_not_found")
		return
	}
	s.invalidateGroups(ctx)
	s.audit(c, "subscription.profile.delete", "profile:"+itoa(id), "ok")
	c.Status(204)
}

// groupInput 汇集订阅组的源、手动节点与合并后的设置。
func (s *Server) groupInput(ctx context.Context, p *ent.SubProfile, ua string) ([]subgroup.Source, []subgroup.ManualNode, subgroup.Options, subgroup.Template, SubscriptionSettings, error) {
	g, err := s.subscriptionSettings(ctx)
	if err != nil {
		return nil, nil, subgroup.Options{}, subgroup.Template{}, g, err
	}
	ps, err := decodeProfileSettings(p.Settings)
	if err != nil {
		return nil, nil, subgroup.Options{}, subgroup.Template{}, g, err
	}
	if p.Members != nil {
		if err := validateMemberResources(ctx, s.Store.Ent, p); err != nil {
			return nil, nil, subgroup.Options{}, subgroup.Template{}, g, err
		}
	}
	o := subgroup.Options{UserAgent: ua, PrependManual: g.PrependManual, ManualPrefix: g.ManualPrefix, PrependSources: g.PrependSources, PrependGroupName: g.PrependGroupName,
		FlagEmoji: g.FlagEmoji, Operators: p.Operators, DefaultOperators: g.DefaultOperators, LegacyTransform: g.LegacyTransform,
		FilterInclude: strings.TrimSpace(g.FilterInclude + "\n" + deref(ps.FilterInclude)), FilterExclude: strings.TrimSpace(g.FilterExclude + "\n" + deref(ps.FilterExclude))}
	for v, dst := range map[*bool]*bool{ps.PrependManual: &o.PrependManual, ps.PrependSources: &o.PrependSources, ps.PrependGroupName: &o.PrependGroupName, ps.FlagEmoji: &o.FlagEmoji, ps.TrafficNodes: &g.TrafficNodes} {
		if v != nil {
			*dst = *v
		}
	}
	for v, dst := range map[*bool]*bool{ps.EnableUDP: &g.EnableUDP, ps.EnableTFO: &g.EnableTFO, ps.SkipCertVerify: &g.SkipCertVerify} {
		if v != nil {
			*dst = *v
		}
	}
	if ps.DNSMode != nil {
		g.DNSMode = *ps.DNSMode
	}
	if ps.CustomDNS != nil {
		g.CustomDNS = *ps.CustomDNS
	}
	if ps.RegionOverrides != nil {
		g.RegionOverrides = *ps.RegionOverrides
	}
	if ps.ManualPrefix != nil && strings.TrimSpace(*ps.ManualPrefix) != "" {
		o.ManualPrefix = *ps.ManualPrefix
	}
	if ps.RuleLevel != nil && *ps.RuleLevel != "" {
		g.RuleLevel = *ps.RuleLevel
	}
	ref := g.Template
	if ps.Engine != nil {
		g.Engine = *ps.Engine
	}
	if ps.Template != nil {
		ref = *ps.Template
	}
	g.Template = ref
	tpl, err := s.resolveTemplate(ctx, ref)
	if err != nil {
		return nil, nil, o, tpl, g, err
	}
	sq := s.Store.Ent.SubSource.Query().Where(subsource.EnabledEQ(true))
	nq := s.Store.Ent.SubNode.Query().Where(subnode.EnabledEQ(true))
	sourceIDs, nodeIDs := p.SourceIds, p.NodeIds
	if p.Members != nil {
		sourceIDs, nodeIDs = nil, nil
		for _, member := range p.Members {
			if member.Kind == "source" {
				sourceIDs = append(sourceIDs, member.ID)
			}
			if member.Kind == "node" {
				nodeIDs = append(nodeIDs, member.ID)
			}
		}
		o.Members = p.Members
	}
	if !p.AllSources {
		sq = sq.Where(subsource.IDIn(sourceIDs...))
		nq = nq.Where(subnode.IDIn(nodeIDs...))
	}
	srcRows, err := sq.Order(ent.Asc(subsource.FieldPosition), ent.Asc(subsource.FieldID)).All(ctx)
	if err != nil {
		return nil, nil, o, tpl, g, err
	}
	nodeRows, err := nq.Order(ent.Asc(subnode.FieldPosition), ent.Asc(subnode.FieldID)).All(ctx)
	if err != nil {
		return nil, nil, o, tpl, g, err
	}
	srcRows = orderBy(srcRows, p.SourceIds, p.AllSources, func(v *ent.SubSource) int { return v.ID })
	nodeRows = orderBy(nodeRows, p.NodeIds, p.AllSources, func(v *ent.SubNode) int { return v.ID })
	sources := make([]subgroup.Source, 0, len(srcRows))
	for _, v := range srcRows {
		sources = append(sources, toSource(v))
	}
	manual := make([]subgroup.ManualNode, 0, len(nodeRows))
	for _, v := range nodeRows {
		manual = append(manual, subgroup.ManualNode{ID: v.ID, Name: v.Name, URL: v.URL, Group: v.Group, FilterInclude: v.FilterInclude, FilterExclude: v.FilterExclude, Operators: v.Operators, PlusAsSpace: v.PlusAsSpace})
	}
	if g.TrafficNodes {
		o.Prepended = trafficNotices(srcRows, p.ExpiresAt)
	}
	return sources, manual, o, tpl, g, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// orderBy 按订阅组里保存的顺序排列（全部源时保持全局顺序）。
func orderBy[T any](rows []T, order []int, all bool, id func(T) int) []T {
	if all || len(order) == 0 {
		return rows
	}
	pos := map[int]int{}
	for i, v := range order {
		pos[v] = i
	}
	out := make([]T, len(rows))
	copy(out, rows)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && pos[id(out[j])] < pos[id(out[j-1])]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// trafficNotices 生成「流量剩余」「到期时间」提示节点：只用上游明确报告的数值，缺失时不生成（不当作 0）。
func trafficNotices(sources []*ent.SubSource, profileExpires *time.Time) []string {
	var remaining int64
	var expire int64
	known := false
	for _, s := range sources {
		ui := parseUserInfo(s.UserInfo)
		if ui == nil || s.ExcludeTraffic {
			continue
		}
		if used, total, ok := sourceTraffic(ui); ok {
			known = true
			if r := total - used; r > 0 {
				remaining += r
			}
		}
		if e := ui["expire"]; e > 0 && (expire == 0 || e < expire) {
			expire = e
		}
	}
	if profileExpires != nil {
		expire = profileExpires.Unix()
	}
	var out []string
	if known {
		out = append(out, subgroup.NoticeNode("流量剩余 ≫ "+subgroup.FormatBytes(remaining)))
	}
	if expire > 0 {
		out = append(out, subgroup.NoticeNode("到期时间 ≫ "+time.Unix(expire, 0).Format("2006-01-02")))
	}
	return out
}

// groupNodes 返回订阅组节点列表：命中缓存直接返回；否则拉取并按全局设置缓存。
func (s *Server) groupNodes(ctx context.Context, p *ent.SubProfile, ua string, refresh bool) (string, []subgroup.SourceResult, subgroup.Template, SubscriptionSettings, error) {
	ctx, finish := s.startSubscriptionRun(ctx, p.ID)
	remaining := maxSubscriptionMembers
	body, results, tpl, settings, err := s.groupNodesAt(ctx, p, ua, refresh, map[int]bool{}, 0, &remaining)
	if err != nil {
		now := time.Now().UTC()
		recordSubscriptionStage(ctx, p.ID, subgroup.Stage{Operation: "subscription.resolve", Started: now, Finished: now, Error: err.Error()}, "center.groupNodesAt")
	}
	finish(err)
	return body, results, tpl, settings, err
}

func (s *Server) groupNodesAt(ctx context.Context, p *ent.SubProfile, ua string, refresh bool, path map[int]bool, depth int, remaining *int) (string, []subgroup.SourceResult, subgroup.Template, SubscriptionSettings, error) {
	if err := ctx.Err(); err != nil {
		return "", nil, subgroup.Template{}, SubscriptionSettings{}, err
	}
	if depth > 3 || path[p.ID] {
		return "", nil, subgroup.Template{}, SubscriptionSettings{}, fmt.Errorf("profile_graph_invalid")
	}
	if depth > 0 && (!p.Enabled || p.ExpiresAt != nil && !time.Now().Before(*p.ExpiresAt)) {
		return "", nil, subgroup.Template{}, SubscriptionSettings{}, fmt.Errorf("profile_member_inactive")
	}
	path[p.ID] = true
	defer delete(path, p.ID)
	ctx = s.subscriptionContext(ctx, p.ID)
	generation, _, generationErr := s.Store.KV.Get(ctx, "sub-group-generation")
	sources, manual, o, tpl, g, err := s.groupInput(ctx, p, ua)
	if err != nil {
		return "", nil, tpl, g, err
	}
	*remaining -= len(sources) + len(manual)
	if *remaining < 0 {
		return "", nil, tpl, g, fmt.Errorf("profile_expansion_exceeded")
	}
	var nestedResults []subgroup.SourceResult
	if p.Members != nil {
		o.Nested = map[int]string{}
		nestedBytes := 0
		for _, member := range p.Members {
			if member.Kind != "profile" {
				continue
			}
			*remaining -= 1
			if *remaining < 0 {
				return "", nil, tpl, g, fmt.Errorf("profile_expansion_exceeded")
			}
			child, err := s.Store.Ent.SubProfile.Get(ctx, member.ID)
			if err != nil {
				return "", nil, tpl, g, fmt.Errorf("unknown_profile")
			}
			body, results, _, _, err := s.groupNodesAt(ctx, child, ua, refresh, path, depth+1, remaining)
			if err != nil {
				return "", nil, tpl, g, err
			}
			nestedBytes += len(body)
			if nestedBytes > maxSubscriptionOutputBytes {
				return "", nil, tpl, g, errors.New("profile_output_too_large")
			}
			o.Nested[child.ID] = body
			nestedResults = append(nestedResults, results...)
		}
	}
	dependencies := append([]subgroup.Source(nil), sources...)
	for i := range dependencies {
		dependencies[i].KnownNodes = 0
	}
	key := "sub-group:" + itoa(p.ID) + ":" + version(struct {
		Generation string
		Sources    []subgroup.Source
		Manual     []subgroup.ManualNode
		Options    subgroup.Options
		Template   subgroup.Template
		Settings   SubscriptionSettings
	}{generation, dependencies, manual, o, tpl, g})
	cacheable := g.CacheMinutes > 0 && generationErr == nil
	for _, result := range nestedResults {
		if result.Error != "" {
			cacheable = false
		}
	}
	if !refresh && cacheable {
		if raw, ok, _ := s.Store.KV.Get(ctx, key); ok {
			if len(raw) > maxSubscriptionOutputBytes {
				return "", nil, tpl, g, errors.New("profile_output_too_large")
			}
			now := time.Now().UTC()
			recordSubscriptionStage(ctx, p.ID, subgroup.Stage{Operation: "subscription.cache_hit", Started: now, Finished: now}, "center.groupNodesAt")
			slog.InfoContext(ctx, "subscription group cache", "trace", taskTrace(ctx), "profile", p.ID, "outcome", "hit")
			return raw, nil, tpl, g, nil
		}
	}
	list, results, combineErr := subgroup.CombineBounded(ctx, s.subFetcher(), kvCache{s}, sources, manual, o, maxSubscriptionOutputBytes)
	if combineErr != nil {
		return "", results, tpl, g, combineErr
	}
	if len(list) > maxSubscriptionOutputBytes {
		return "", results, tpl, g, errors.New("profile_output_too_large")
	}
	s.recordResults(ctx, results)
	results = append(results, nestedResults...)
	for _, result := range results {
		if result.Error != "" {
			cacheable = false
		}
	}
	if cacheable {
		_ = s.Store.KV.Set(ctx, key, list, time.Duration(g.CacheMinutes)*time.Minute)
	}
	return list, results, tpl, g, nil
}

// Generation invalidation also fences an in-flight fetch from repopulating a
// cache entry after its dependencies changed. Old entries expire by their TTL.
func (s *Server) invalidateGroups(ctx context.Context) {
	_, err := s.Store.KV.Incr(ctx, "sub-group-generation", 0)
	s.tasks.note("subscription-cache-invalidation", err)
}

// previewProfile 管理员预览：实时拉取（不使用输出缓存），返回节点、每个源的结果与所选格式的输出。
func (s *Server) previewProfile(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	var req struct {
		Target string `json:"target"`
	}
	_ = c.ShouldBindJSON(&req)
	ctx := c.Request.Context()
	p, err := s.Store.Ent.SubProfile.Get(ctx, id)
	if err != nil {
		failure(c, 404, "profile_not_found")
		return
	}
	ctx, finish := s.startSubscriptionRun(ctx, p.ID)
	var runErr error
	defer func() { finish(runErr) }()
	list, results, tpl, g, err := s.groupNodes(ctx, p, "clash-meta/2.5.0", true)
	if err != nil {
		runErr = err
		failDetail(c, 422, "preview_failed", err.Error(), nil)
		return
	}
	nodes := []string{}
	for _, l := range strings.Split(strings.TrimSpace(list), "\n") {
		if l != "" {
			nodes = append(nodes, l)
		}
	}
	out := gin.H{"nodes": nodes, "sources": results}
	if req.Target != "" {
		body, ct, err := s.renderGroup(ctx, p, list, req.Target, s.renderOptions(p, g, "clash-meta/2.5.0", "", ""), tpl)
		if err != nil {
			runErr = err
			out["output_error"] = err.Error()
		} else {
			out["output"], out["content_type"] = string(body), ct
			if checkKernel(req.Target) != "" {
				out["check"] = s.checkSubscription(ctx, req.Target, body)
			}
		}
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		runErr = err
		failure(c, 500, "response_encoding_failed")
		return
	}
	if err := writeSubscriptionBody(c, ctx, p.ID, "application/json; charset=utf-8", encoded); err != nil {
		runErr = err
	}
}

func (s *Server) renderGroup(ctx context.Context, p *ent.SubProfile, list, target string, options subgroup.RenderOptions, tpl subgroup.Template) ([]byte, string, error) {
	started := time.Now().UTC()
	var body []byte
	var contentType string
	var err error
	if options.Engine == "external" {
		body, contentType, err = s.convertGroup(ctx, p.ID, list, target, options)
	} else {
		body, contentType, err = subgroup.Render(list, target, options, tpl)
	}
	if err == nil && len(body) > maxSubscriptionOutputBytes {
		body, err = nil, errors.New("profile_output_too_large")
	}
	stageErr := err
	if err != nil && subscriptionStageCode(err.Error()) == "source_fetch_failed" {
		stageErr = errors.New("format_conversion_failed")
	}
	subscriptionStageResult(ctx, p.ID, "subscription.render", "center.renderGroup", started, stageErr)
	return body, contentType, err
}

func (s *Server) renderOptions(p *ent.SubProfile, g SubscriptionSettings, ua, query, managed string) subgroup.RenderOptions {
	level := g.RuleLevel
	return subgroup.RenderOptions{Engine: g.Engine, ExternalTemplate: g.Template, FileName: p.Name, RuleLevel: level, Interval: g.UpdateInterval * 3600, SkipCertVerify: g.SkipCertVerify,
		EnableUDP: g.EnableUDP, EnableTFO: g.EnableTFO, DNSMode: g.DNSMode, CustomDNS: g.CustomDNS, RegionOverrides: g.RegionOverrides, UserAgent: ua, Query: query, ManagedURL: managed}
}

// shareProfile 生成（或轮换）匿名分享令牌；令牌只在本次响应中返回，有效期 1–365 天。
func (s *Server) shareProfile(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	var req struct {
		Days int `json:"days"`
	}
	if !bind(c, &req) {
		return
	}
	if req.Days < 1 || req.Days > 365 {
		failure(c, 400, "invalid_share_days")
		return
	}
	ctx := c.Request.Context()
	token := randomToken(24)
	exp := time.Now().AddDate(0, 0, req.Days)
	if err := s.Store.Ent.SubProfile.UpdateOneID(id).SetShareToken(digest(token)).SetShareExpiresAt(exp).Exec(ctx); err != nil {
		failure(c, 404, "profile_not_found")
		return
	}
	s.audit(c, "subscription.profile.share", "profile:"+itoa(id), "ok")
	c.JSON(200, gin.H{"url": s.siteOrigin(c) + "/sub/g/" + token, "expires_at": exp})
}

func (s *Server) unshareProfile(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	if err := s.Store.Ent.SubProfile.UpdateOneID(id).SetShareToken("").ClearShareExpiresAt().Exec(c.Request.Context()); err != nil {
		failure(c, 404, "profile_not_found")
		return
	}
	s.audit(c, "subscription.profile.unshare", "profile:"+itoa(id), "ok")
	c.Status(204)
}

// setGrants 设定可使用该订阅组的用户（完整列表）。新增的用户各得一个独立令牌，移除的用户令牌立即失效。
func (s *Server) setGrants(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	var req struct {
		UserIDs []int `json:"user_ids"`
	}
	if !bind(c, &req) {
		return
	}
	ctx := c.Request.Context()
	if _, err := s.Store.Ent.SubProfile.Get(ctx, id); err != nil {
		failure(c, 404, "profile_not_found")
		return
	}
	want := dedupe(req.UserIDs)
	if len(want) > 0 {
		if n, _ := s.Store.Ent.User.Query().Where(userIDIn(want)).Count(ctx); n != len(want) {
			failure(c, 400, "unknown_user")
			return
		}
	}
	existing, err := s.Store.Ent.SubGrant.Query().Where(subgrant.ProfileIDEQ(id)).All(ctx)
	if err != nil {
		failInternal(c, 503, "query_failed", err)
		return
	}
	if c.GetBool(dryRunKey) {
		removed := 0
		for _, g := range existing {
			if !contains(want, g.UserID) {
				removed++
			}
		}
		preview := []string{fmt.Sprintf("分配给 %d 个用户；新分配的用户各得到一个订阅地址", len(want))}
		if removed > 0 {
			preview = append(preview, fmt.Sprintf("取消 %d 个用户的分配，他们的这个订阅地址随即失效", removed))
		}
		dryRun(c, dryResult{Preview: preview})
		return
	}
	have := map[int]bool{}
	for _, g := range existing {
		if !contains(want, g.UserID) {
			_ = s.Store.Ent.SubGrant.DeleteOne(g).Exec(ctx)
			continue
		}
		have[g.UserID] = true
	}
	for _, uid := range want {
		if !have[uid] {
			if _, err = s.Store.Ent.SubGrant.Create().SetProfileID(id).SetUserID(uid).SetToken(randomToken(24)).Save(ctx); err != nil {
				failInternal(c, 500, "save_failed", err)
				return
			}
		}
	}
	s.audit(c, "subscription.profile.grants", "profile:"+itoa(id), fmt.Sprintf("ok users=%d", len(want)))
	p, _ := s.Store.Ent.SubProfile.Get(ctx, id)
	c.JSON(200, s.profileView(ctx, p))
}

// myGroups 列出分配给当前用户的订阅组与各自的订阅地址。
func (s *Server) myGroups(c *gin.Context) {
	u := c.MustGet("account").(*ent.User)
	ctx := c.Request.Context()
	grants, err := s.Store.Ent.SubGrant.Query().Where(subgrant.UserIDEQ(u.ID)).All(ctx)
	if err != nil {
		failInternal(c, 503, "query_failed", err)
		return
	}
	out := []gin.H{}
	for _, g := range grants {
		p, err := s.Store.Ent.SubProfile.Get(ctx, g.ProfileID)
		if err != nil || !p.Enabled {
			continue
		}
		out = append(out, gin.H{"id": p.ID, "name": p.Name, "url": s.siteOrigin(c) + "/sub/g/" + g.Token, "expires_at": p.ExpiresAt, "last_used_at": g.LastUsedAt})
	}
	c.JSON(200, gin.H{"items": out})
}

// serveGroup 输出订阅组：令牌是用户的分配令牌或匿名分享令牌。用户停用 / 到期、分享撤下 / 过期都拒绝；
// 订阅组本身到期时只返回一个「订阅已到期」提示节点。
func (s *Server) serveGroup(c *gin.Context) {
	if !s.Config.Installed || s.Store == nil {
		failure(c, 503, "setup_required")
		return
	}
	ctx := c.Request.Context()
	token := c.Param("token")
	if len(token) < 16 || len(token) > 128 {
		failure(c, 404, "subscription_not_found")
		return
	}
	if wantsBrowserPage(c) && s.disguised(ctx) {
		disguisePage(c)
		return
	}
	var p *ent.SubProfile
	if g, err := s.Store.Ent.SubGrant.Query().Where(subgrant.TokenEQ(token)).Only(ctx); err == nil {
		u, err := s.Store.Ent.User.Get(ctx, g.UserID)
		if err != nil || !u.Enabled || u.ExpiresAt != nil && time.Now().After(*u.ExpiresAt) {
			failure(c, 403, "subscription_inactive")
			return
		}
		if p, err = s.Store.Ent.SubProfile.Get(ctx, g.ProfileID); err != nil {
			failure(c, 404, "subscription_not_found")
			return
		}
		_ = g.Update().SetLastUsedAt(time.Now()).Exec(ctx)
	} else if sp, err := s.Store.Ent.SubProfile.Query().Where(subprofile.ShareTokenEQ(digest(token))).Only(ctx); err == nil {
		if sp.ShareExpiresAt == nil || time.Now().After(*sp.ShareExpiresAt) {
			failure(c, 404, "subscription_not_found")
			return
		}
		p = sp
	} else {
		failure(c, 404, "subscription_not_found")
		return
	}
	s.writeGroup(c, p, "")
}

// writeGroup 输出订阅组内容；target 为空时按 UA 与查询参数判断格式。
func (s *Server) writeGroup(c *gin.Context, p *ent.SubProfile, forced string) {
	ctx := c.Request.Context()
	if !p.Enabled {
		failure(c, 404, "subscription_not_found")
		return
	}
	ctx, finish := s.startSubscriptionRun(ctx, p.ID)
	var runErr error
	defer func() { finish(runErr) }()
	ua := c.GetHeader("User-Agent")
	target := forced
	if target == "" {
		target = subgroup.Target(ua, c.Request.URL.RawQuery)
	}
	var list string
	var g SubscriptionSettings
	var tpl subgroup.Template
	if p.ExpiresAt != nil && time.Now().After(*p.ExpiresAt) {
		list = subgroup.NoticeNode("您的订阅已到期") + "\n"
		g, _ = s.subscriptionSettings(ctx)
	} else {
		var err error
		if list, _, tpl, g, err = s.groupNodes(ctx, p, ua, c.Query("refresh") != ""); err != nil {
			runErr = err
			failInternal(c, 503, "subscription_unavailable", err)
			return
		}
	}
	managed := s.siteOrigin(c) + c.Request.URL.Path
	body, ct, err := s.renderGroup(ctx, p, list, target, s.renderOptions(p, g, ua, c.Request.URL.RawQuery, managed), tpl)
	if err != nil {
		runErr = err
		failure(c, 422, "format_conversion_failed")
		return
	}
	_ = p.Update().AddDownloads(1).Exec(ctx)
	s.recordSubCheck(withTrace(ctx, c.GetString("request_id")), "profile-"+itoa(p.ID), "订阅组 · "+p.Name, target, body)
	c.Header("Profile-Update-Interval", itoa(g.UpdateInterval))
	c.Header("Content-Disposition", "attachment; filename*=UTF-8''"+urlPathEscape(p.Name))
	runErr = writeSubscriptionBody(c, ctx, p.ID, ct, body)
}

func userIDIn(ids []int) predicate.User { return user.IDIn(ids...) }

func urlPathEscape(s string) string { return url.PathEscape(s) }
