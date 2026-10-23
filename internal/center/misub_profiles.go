package center

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/imzyb/MiGate/internal/misub"
	"github.com/imzyb/MiGate/internal/store/ent"
	"github.com/imzyb/MiGate/internal/store/ent/predicate"
	"github.com/imzyb/MiGate/internal/store/ent/subgrant"
	"github.com/imzyb/MiGate/internal/store/ent/subnode"
	"github.com/imzyb/MiGate/internal/store/ent/subprofile"
	"github.com/imzyb/MiGate/internal/store/ent/subsource"
	"github.com/imzyb/MiGate/internal/store/ent/user"
)

// profileSettings 是订阅组可覆盖的设置；缺省字段沿用全局默认。
type profileSettings struct {
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
}

func decodeProfileSettings(m map[string]any) (profileSettings, error) {
	var ps profileSettings
	if len(m) == 0 {
		return ps, nil
	}
	b, _ := json.Marshal(m)
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	return ps, dec.Decode(&ps)
}

func (s *Server) profileView(ctx context.Context, p *ent.SubProfile) gin.H {
	grants, _ := s.Store.Ent.SubGrant.Query().Where(subgrant.ProfileIDEQ(p.ID)).All(ctx)
	users := make([]int, 0, len(grants))
	for _, g := range grants {
		users = append(users, g.UserID)
	}
	v := gin.H{"id": p.ID, "name": p.Name, "enabled": p.Enabled, "all_sources": p.AllSources, "source_ids": p.SourceIds, "node_ids": p.NodeIds,
		"operators": p.Operators, "settings": p.Settings, "expires_at": p.ExpiresAt, "downloads": p.Downloads, "position": p.Position,
		"user_ids": users, "shared": p.ShareToken != "", "share_expires_at": p.ShareExpiresAt}
	return v
}

func (s *Server) listSubProfiles(c *gin.Context) {
	ctx := c.Request.Context()
	items, err := s.Store.Ent.SubProfile.Query().Order(ent.Asc(subprofile.FieldPosition), ent.Asc(subprofile.FieldID)).All(ctx)
	if err != nil {
		failure(c, 503, "query_failed")
		return
	}
	out := make([]gin.H, 0, len(items))
	for _, it := range items {
		out = append(out, s.profileView(ctx, it))
	}
	c.JSON(200, gin.H{"items": out})
}

func (s *Server) saveProfile(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	var req struct {
		Name       *string         `json:"name"`
		Enabled    *bool           `json:"enabled"`
		AllSources *bool           `json:"all_sources"`
		SourceIDs  *[]int          `json:"source_ids"`
		NodeIDs    *[]int          `json:"node_ids"`
		Operators  *[]any          `json:"operators"`
		Settings   *map[string]any `json:"settings"`
		ExpiresAt  json.RawMessage `json:"expires_at"`
	}
	if !bind(c, &req) {
		return
	}
	ctx := c.Request.Context()
	var m *ent.SubProfileMutation
	var create *ent.SubProfileCreate
	var update *ent.SubProfileUpdateOne
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
			failDetail(c, 400, "invalid_profile_settings", err.Error(), nil)
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
	s.audit(c, "misub.profile.save", "profile:"+itoa(p.ID), "ok")
	c.JSON(200, s.profileView(ctx, p))
}

func (s *Server) deleteProfile(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	if _, err := s.Store.Ent.SubGrant.Delete().Where(subgrant.ProfileIDEQ(id)).Exec(ctx); err != nil {
		failure(c, 500, "delete_failed")
		return
	}
	if err := s.Store.Ent.SubProfile.DeleteOneID(id).Exec(ctx); err != nil {
		failure(c, 404, "profile_not_found")
		return
	}
	s.invalidateGroups(ctx)
	s.audit(c, "misub.profile.delete", "profile:"+itoa(id), "ok")
	c.Status(204)
}

// groupInput 汇集订阅组的源、手动节点与合并后的设置。
func (s *Server) groupInput(ctx context.Context, p *ent.SubProfile, ua string) ([]misub.Source, []misub.ManualNode, misub.Options, misub.Template, MisubSettings, error) {
	g, err := s.misubSettings(ctx)
	if err != nil {
		return nil, nil, misub.Options{}, misub.Template{}, g, err
	}
	ps, _ := decodeProfileSettings(p.Settings)
	o := misub.Options{UserAgent: ua, PrependManual: g.PrependManual, ManualPrefix: g.ManualPrefix, PrependSources: g.PrependSources, PrependGroupName: g.PrependGroupName,
		FlagEmoji: g.FlagEmoji, Operators: p.Operators, DefaultOperators: g.DefaultOperators, LegacyTransform: g.LegacyTransform,
		FilterInclude: strings.TrimSpace(g.FilterInclude + "\n" + deref(ps.FilterInclude)), FilterExclude: strings.TrimSpace(g.FilterExclude + "\n" + deref(ps.FilterExclude))}
	for v, dst := range map[*bool]*bool{ps.PrependManual: &o.PrependManual, ps.PrependSources: &o.PrependSources, ps.PrependGroupName: &o.PrependGroupName, ps.FlagEmoji: &o.FlagEmoji, ps.TrafficNodes: &g.TrafficNodes} {
		if v != nil {
			*dst = *v
		}
	}
	if ps.ManualPrefix != nil && strings.TrimSpace(*ps.ManualPrefix) != "" {
		o.ManualPrefix = *ps.ManualPrefix
	}
	if ps.RuleLevel != nil && *ps.RuleLevel != "" {
		g.RuleLevel = *ps.RuleLevel
	}
	ref := g.Template
	if ps.Template != nil {
		ref = *ps.Template
	}
	tpl, err := s.resolveTemplate(ctx, ref)
	if err != nil {
		return nil, nil, o, tpl, g, err
	}
	sq := s.Store.Ent.SubSource.Query().Where(subsource.EnabledEQ(true))
	nq := s.Store.Ent.SubNode.Query().Where(subnode.EnabledEQ(true))
	if !p.AllSources {
		sq = sq.Where(subsource.IDIn(p.SourceIds...))
		nq = nq.Where(subnode.IDIn(p.NodeIds...))
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
	sources := make([]misub.Source, 0, len(srcRows))
	for _, v := range srcRows {
		sources = append(sources, toSource(v))
	}
	manual := make([]misub.ManualNode, 0, len(nodeRows))
	for _, v := range nodeRows {
		manual = append(manual, misub.ManualNode{ID: v.ID, Name: v.Name, URL: v.URL, Group: v.Group, FilterInclude: v.FilterInclude, FilterExclude: v.FilterExclude, Operators: v.Operators, PlusAsSpace: v.PlusAsSpace})
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
		if ui["total"] > 0 {
			known = true
			if r := ui["total"] - ui["upload"] - ui["download"]; r > 0 {
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
		out = append(out, misub.NoticeNode("流量剩余 ≫ "+misub.FormatBytes(remaining)))
	}
	if expire > 0 {
		out = append(out, misub.NoticeNode("到期时间 ≫ "+time.Unix(expire, 0).Format("2006-01-02")))
	}
	return out
}

// groupNodes 返回订阅组节点列表：命中缓存直接返回；否则拉取并按全局设置缓存。
func (s *Server) groupNodes(ctx context.Context, p *ent.SubProfile, ua string, refresh bool) (string, []misub.SourceResult, misub.Template, MisubSettings, error) {
	sources, manual, o, tpl, g, err := s.groupInput(ctx, p, ua)
	if err != nil {
		return "", nil, tpl, g, err
	}
	key := fmt.Sprintf("misub-group:%d", p.ID)
	if !refresh && g.CacheMinutes > 0 {
		if raw, ok, _ := s.Store.KV.Get(ctx, key); ok {
			return raw, nil, tpl, g, nil
		}
	}
	list, results := misub.Combine(ctx, s.subFetcher(), kvCache{s}, sources, manual, o)
	s.recordResults(ctx, results)
	if g.CacheMinutes > 0 && strings.TrimSpace(list) != "" {
		_ = s.Store.KV.Set(ctx, key, list, time.Duration(g.CacheMinutes)*time.Minute)
	}
	return list, results, tpl, g, nil
}

// invalidateGroups 清除全部订阅组的输出缓存（源、节点、设置、模板变化后）。
func (s *Server) invalidateGroups(ctx context.Context) {
	keys, _ := s.Store.KV.Keys(ctx, "misub-group:", 5000)
	for _, k := range keys {
		_ = s.Store.KV.Del(ctx, k)
	}
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
	list, results, tpl, g, err := s.groupNodes(ctx, p, "clash-meta/2.5.0", true)
	if err != nil {
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
		body, ct, err := misub.Render(list, req.Target, s.renderOptions(p, g, "clash-meta/2.5.0", "", ""), tpl)
		if err != nil {
			out["output_error"] = err.Error()
		} else {
			out["output"], out["content_type"] = string(body), ct
		}
	}
	c.JSON(200, out)
}

func (s *Server) renderOptions(p *ent.SubProfile, g MisubSettings, ua, query, managed string) misub.RenderOptions {
	level := g.RuleLevel
	return misub.RenderOptions{FileName: p.Name, RuleLevel: level, Interval: g.UpdateInterval * 3600, SkipCertVerify: g.SkipCertVerify,
		EnableUDP: g.EnableUDP, UserAgent: ua, Query: query, ManagedURL: managed}
}

// shareProfile 生成（或轮换）匿名分享令牌；令牌只在本次响应中返回，有效期 1–365 天。
func (s *Server) shareProfile(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	var req struct {
		Days     int    `json:"days"`
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if !bind(c, &req) || !s.reauth(c, req.Password, req.Code) {
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
	s.audit(c, "misub.profile.share", "profile:"+itoa(id), "ok")
	c.JSON(200, gin.H{"url": requestOrigin(c.Request) + "/sub/g/" + token, "expires_at": exp})
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
	s.audit(c, "misub.profile.unshare", "profile:"+itoa(id), "ok")
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
		failure(c, 503, "query_failed")
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
				failure(c, 500, "save_failed")
				return
			}
		}
	}
	s.audit(c, "misub.profile.grants", "profile:"+itoa(id), fmt.Sprintf("ok users=%d", len(want)))
	p, _ := s.Store.Ent.SubProfile.Get(ctx, id)
	c.JSON(200, s.profileView(ctx, p))
}

// myGroups 列出分配给当前用户的订阅组与各自的订阅地址。
func (s *Server) myGroups(c *gin.Context) {
	u := c.MustGet("account").(*ent.User)
	ctx := c.Request.Context()
	grants, err := s.Store.Ent.SubGrant.Query().Where(subgrant.UserIDEQ(u.ID)).All(ctx)
	if err != nil {
		failure(c, 503, "query_failed")
		return
	}
	out := []gin.H{}
	for _, g := range grants {
		p, err := s.Store.Ent.SubProfile.Get(ctx, g.ProfileID)
		if err != nil || !p.Enabled {
			continue
		}
		out = append(out, gin.H{"id": p.ID, "name": p.Name, "url": requestOrigin(c.Request) + "/sub/g/" + g.Token, "expires_at": p.ExpiresAt, "last_used_at": g.LastUsedAt})
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
	if !p.Enabled {
		failure(c, 404, "subscription_not_found")
		return
	}
	ua := c.GetHeader("User-Agent")
	target := misub.Target(ua, c.Request.URL.RawQuery)
	var list string
	var g MisubSettings
	var tpl misub.Template
	if p.ExpiresAt != nil && time.Now().After(*p.ExpiresAt) {
		list = misub.NoticeNode("您的订阅已到期") + "\n"
		g, _ = s.misubSettings(ctx)
	} else {
		var err error
		if list, _, tpl, g, err = s.groupNodes(ctx, p, ua, c.Query("refresh") != ""); err != nil {
			failure(c, 503, "subscription_unavailable")
			return
		}
	}
	managed := requestOrigin(c.Request) + c.Request.URL.Path
	body, ct, err := misub.Render(list, target, s.renderOptions(p, g, ua, c.Request.URL.RawQuery, managed), tpl)
	if err != nil {
		failure(c, 422, "format_conversion_failed")
		return
	}
	_ = p.Update().AddDownloads(1).Exec(ctx)
	c.Header("Profile-Update-Interval", itoa(g.UpdateInterval))
	c.Header("Content-Disposition", "attachment; filename*=UTF-8''"+urlPathEscape(p.Name))
	c.Data(200, ct, body)
}

func userIDIn(ids []int) predicate.User { return user.IDIn(ids...) }

func urlPathEscape(s string) string { return url.PathEscape(s) }
