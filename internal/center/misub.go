package center

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/shusfun/weir/internal/misub"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/ruletemplate"
	"github.com/shusfun/weir/internal/store/ent/setting"
	"github.com/shusfun/weir/internal/store/ent/subnode"
	"github.com/shusfun/weir/internal/store/ent/subsource"
)

// MiSub 订阅管理：订阅源 + 手动节点 → 订阅组（处理链、规则模板）→ 分配给用户或分享。
// 订阅组的节点来自外部机场，流量不经本项目的 Gate，因此不计入用户额度；到期与停用仍然生效。

const misubSettingKey = "misub-settings"

// MisubSettings 是订阅管理的全局默认值（订阅组可逐项覆盖）。
type MisubSettings struct {
	FlagEmoji        bool           `json:"flag_emoji"`
	PrependManual    bool           `json:"prepend_manual"`
	ManualPrefix     string         `json:"manual_prefix"`
	PrependSources   bool           `json:"prepend_sources"`
	PrependGroupName bool           `json:"prepend_group_name"`
	DefaultOperators []any          `json:"default_operators"`
	LegacyTransform  map[string]any `json:"node_transform,omitempty"`
	RuleLevel        string         `json:"rule_level"`
	Template         string         `json:"template"` // builtin:<id> / custom:<id> / remote:<url> / 空
	TrafficNodes     bool           `json:"traffic_nodes"`
	CacheMinutes     int            `json:"cache_minutes"`
	SkipCertVerify   bool           `json:"skip_cert_verify"`
	EnableUDP        bool           `json:"enable_udp"`
	FilterInclude    string         `json:"filter_include"`
	FilterExclude    string         `json:"filter_exclude"`
	UpdateInterval   int            `json:"update_interval"`
}

func defaultMisubSettings() MisubSettings {
	return MisubSettings{FlagEmoji: true, PrependManual: true, ManualPrefix: "手动节点", PrependSources: true, RuleLevel: "std",
		TrafficNodes: true, CacheMinutes: 10, EnableUDP: true, UpdateInterval: 24}
}

func (s *Server) misubSettings(ctx context.Context) (MisubSettings, error) {
	out := defaultMisubSettings()
	row, err := s.Store.Ent.Setting.Query().Where(setting.KeyEQ(misubSettingKey)).Only(ctx)
	if ent.IsNotFound(err) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	b, _ := json.Marshal(row.Value)
	err = json.Unmarshal(b, &out)
	return out, err
}

func (s *Server) misubRoutes(r *gin.Engine, auth, admin *gin.RouterGroup) {
	admin.GET("/misub/settings", s.getMisubSettings)
	admin.PUT("/misub/settings", s.putMisubSettings)
	admin.GET("/misub/sources", s.listSources)
	admin.POST("/misub/sources", s.saveSource)
	admin.PATCH("/misub/sources/:id", s.saveSource)
	admin.DELETE("/misub/sources/:id", s.deleteSource)
	admin.POST("/misub/sources/:id/refresh", s.refreshSource)
	admin.POST("/misub/sources/order", s.orderSources)
	admin.GET("/misub/nodes", s.listManualNodes)
	admin.POST("/misub/nodes", s.saveManualNode)
	admin.POST("/misub/nodes/import", s.importManualNodes)
	admin.PATCH("/misub/nodes/:id", s.saveManualNode)
	admin.DELETE("/misub/nodes/:id", s.deleteManualNode)
	admin.POST("/misub/nodes/groups/rename", s.renameNodeGroup)
	admin.GET("/misub/profiles", s.listSubProfiles)
	admin.POST("/misub/profiles", s.saveProfile)
	admin.PATCH("/misub/profiles/:id", s.saveProfile)
	admin.DELETE("/misub/profiles/:id", s.deleteProfile)
	admin.POST("/misub/profiles/:id/preview", s.previewProfile)
	admin.POST("/misub/profiles/:id/share", s.shareProfile)
	admin.DELETE("/misub/profiles/:id/share", s.unshareProfile)
	admin.PUT("/misub/profiles/:id/grants", s.setGrants)
	admin.POST("/misub/operators/preview", s.previewOperators)
	admin.GET("/misub/templates", s.listTemplates)
	admin.POST("/misub/templates", s.saveTemplate)
	admin.PATCH("/misub/templates/:id", s.saveTemplate)
	admin.DELETE("/misub/templates/:id", s.deleteTemplate)
	auth.GET("/me/groups", s.myGroups)
	r.GET("/sub/g/:token", s.serveGroup)
}

func (s *Server) getMisubSettings(c *gin.Context) {
	st, err := s.misubSettings(c.Request.Context())
	if err != nil {
		failure(c, 503, "settings_unavailable")
		return
	}
	c.JSON(200, st)
}

func (s *Server) putMisubSettings(c *gin.Context) {
	var st MisubSettings
	if !bind(c, &st) {
		return
	}
	if err := checkOperators(st.DefaultOperators); err != nil {
		failDetail(c, 400, "invalid_operators", err.Error(), nil)
		return
	}
	if st.CacheMinutes < 0 || st.CacheMinutes > 1440 || st.UpdateInterval < 1 || st.UpdateInterval > 168 || !validRuleLevel(st.RuleLevel) {
		failure(c, 400, "invalid_settings")
		return
	}
	if _, err := s.resolveTemplate(c.Request.Context(), st.Template); err != nil {
		failDetail(c, 400, "invalid_template", err.Error(), nil)
		return
	}
	var value map[string]any
	b, _ := json.Marshal(st)
	_ = json.Unmarshal(b, &value)
	if err := s.Store.Ent.Setting.Create().SetKey(misubSettingKey).SetValue(value).OnConflictColumns(setting.FieldKey).UpdateNewValues().Exec(c.Request.Context()); err != nil {
		failure(c, 500, "save_failed")
		return
	}
	s.audit(c, "misub.settings", "", "ok")
	c.JSON(200, st)
}

func validRuleLevel(v string) bool {
	switch v {
	case "", "none", "lite", "std", "full":
		return true
	}
	return false
}

// checkOperators 只做结构检查：每一项必须是带 type 的对象；具体参数由引擎按上游语义执行（未知算子不生效）。
func checkOperators(ops []any) error {
	if len(ops) > 50 {
		return fmt.Errorf("最多 50 个处理步骤")
	}
	for i, op := range ops {
		m, ok := op.(map[string]any)
		if !ok {
			return fmt.Errorf("第 %d 步不是对象", i+1)
		}
		switch m["type"] {
		case "filter", "rename", "dedup", "sort", "script", "prefix", "regionFilter", "protocolFilter", "useless", "emoji", "flag", "template":
		default:
			return fmt.Errorf("第 %d 步类型 %v 不受支持", i+1, m["type"])
		}
	}
	b, _ := json.Marshal(ops)
	if len(b) > 256<<10 {
		return fmt.Errorf("处理链过大")
	}
	return nil
}

func idParam(c *gin.Context) (int, bool) {
	if c.Param("id") == "" {
		return 0, true
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		failure(c, 400, "invalid_id")
		return 0, false
	}
	return id, true
}

// ---- 订阅源 ----

func sourceView(v *ent.SubSource) gin.H {
	return gin.H{"id": v.ID, "name": v.Name, "url": v.URL, "enabled": v.Enabled, "user_agent": v.UserAgent, "filter_include": v.FilterInclude,
		"filter_exclude": v.FilterExclude, "operators": v.Operators, "plus_as_space": v.PlusAsSpace, "node_cache": v.NodeCache,
		"exclude_traffic": v.ExcludeTraffic, "website": v.Website, "remark": v.Remark, "position": v.Position, "node_count": v.NodeCount,
		"user_info": parseUserInfo(v.UserInfo), "title": v.Title, "last_error": v.LastError, "fetched_at": v.FetchedAt}
}

// parseUserInfo 解析 Subscription-Userinfo；缺失为 nil（页面显示「未知」，不当作 0）。
func parseUserInfo(raw string) map[string]int64 {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	out := map[string]int64{}
	for _, part := range strings.Split(raw, ";") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); ok && err == nil {
			out[strings.TrimSpace(k)] = n
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (s *Server) listSources(c *gin.Context) {
	items, err := s.Store.Ent.SubSource.Query().Order(ent.Asc(subsource.FieldPosition), ent.Asc(subsource.FieldID)).All(c.Request.Context())
	if err != nil {
		failure(c, 503, "query_failed")
		return
	}
	out := make([]gin.H, 0, len(items))
	var used, total int64
	known := 0
	for _, it := range items {
		out = append(out, sourceView(it))
		if ui := parseUserInfo(it.UserInfo); it.Enabled && !it.ExcludeTraffic && ui != nil && ui["total"] > 0 {
			used += ui["upload"] + ui["download"]
			total += ui["total"]
			known++
		}
	}
	c.JSON(200, gin.H{"items": out, "traffic": gin.H{"used": used, "total": total, "sources": known}})
}

func (s *Server) saveSource(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	var req struct {
		Name           *string `json:"name"`
		URL            *string `json:"url"`
		Enabled        *bool   `json:"enabled"`
		UserAgent      *string `json:"user_agent"`
		FilterInclude  *string `json:"filter_include"`
		FilterExclude  *string `json:"filter_exclude"`
		Operators      *[]any  `json:"operators"`
		PlusAsSpace    *bool   `json:"plus_as_space"`
		NodeCache      *bool   `json:"node_cache"`
		ExcludeTraffic *bool   `json:"exclude_traffic"`
		Website        *string `json:"website"`
		Remark         *string `json:"remark"`
	}
	if !bind(c, &req) {
		return
	}
	ctx := c.Request.Context()
	var m *ent.SubSourceMutation
	var create *ent.SubSourceCreate
	var update *ent.SubSourceUpdateOne
	var old *ent.SubSource
	if id == 0 {
		if req.URL == nil {
			failure(c, 400, "invalid_source_url")
			return
		}
		create = s.Store.Ent.SubSource.Create()
		n, _ := s.Store.Ent.SubSource.Query().Count(ctx)
		create.SetPosition(n)
		m = create.Mutation()
	} else {
		var err error
		if old, err = s.Store.Ent.SubSource.Get(ctx, id); err != nil {
			failure(c, 404, "source_not_found")
			return
		}
		update = old.Update()
		m = update.Mutation()
	}
	if req.URL != nil {
		if _, err := misub.CheckURL(*req.URL); err != nil && !(s.AllowPrivateSources && err.Error() == "source_url_private") {
			failure(c, 400, err.Error())
			return
		}
		m.SetURL(strings.TrimSpace(*req.URL))
	}
	if req.UserAgent != nil {
		for _, r := range *req.UserAgent {
			if r < 0x20 || r > 0x7e {
				failure(c, 400, "invalid_user_agent")
				return
			}
		}
		if len(*req.UserAgent) > 256 {
			failure(c, 400, "invalid_user_agent")
			return
		}
		m.SetUserAgent(strings.TrimSpace(*req.UserAgent))
	}
	if req.Operators != nil {
		if err := checkOperators(*req.Operators); err != nil {
			failDetail(c, 400, "invalid_operators", err.Error(), nil)
			return
		}
		m.SetOperators(*req.Operators)
	}
	setStr := func(v *string, max int, set func(string)) bool {
		if v == nil {
			return true
		}
		if len(*v) > max {
			failure(c, 400, "value_too_long")
			return false
		}
		set(strings.TrimSpace(*v))
		return true
	}
	if !setStr(req.Name, 100, m.SetName) || !setStr(req.FilterInclude, 64<<10, m.SetFilterInclude) || !setStr(req.FilterExclude, 64<<10, m.SetFilterExclude) ||
		!setStr(req.Website, 2048, m.SetWebsite) || !setStr(req.Remark, 500, m.SetRemark) {
		return
	}
	for v, set := range map[*bool]func(bool){req.Enabled: m.SetEnabled, req.PlusAsSpace: m.SetPlusAsSpace, req.NodeCache: m.SetNodeCache, req.ExcludeTraffic: m.SetExcludeTraffic} {
		if v != nil {
			set(*v)
		}
	}
	// 更改 URL、UA 或名称修正方式后旧缓存与用量不再对应这个源：清除。
	if old != nil && (req.URL != nil && *req.URL != old.URL || req.UserAgent != nil && *req.UserAgent != old.UserAgent || req.PlusAsSpace != nil && *req.PlusAsSpace != old.PlusAsSpace) {
		m.SetNodeCount(0)
		m.SetKnownNodes(0)
		m.SetUserInfo("")
		m.SetTitle("")
		m.ClearFetchedAt()
		_ = s.Store.KV.Del(ctx, sourceCacheKey(old.ID))
	}
	var saved *ent.SubSource
	var err error
	if create != nil {
		saved, err = create.Save(ctx)
	} else {
		saved, err = update.Save(ctx)
	}
	if err != nil {
		failure(c, 400, "save_failed")
		return
	}
	s.invalidateGroups(ctx)
	s.audit(c, "misub.source.save", "source:"+itoa(saved.ID), "ok")
	c.JSON(200, sourceView(saved))
}

func (s *Server) deleteSource(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	if err := s.Store.Ent.SubSource.DeleteOneID(id).Exec(ctx); err != nil {
		failure(c, 404, "source_not_found")
		return
	}
	_ = s.Store.KV.Del(ctx, sourceCacheKey(id))
	s.dropFromProfiles(ctx, "source_ids", id)
	s.invalidateGroups(ctx)
	s.audit(c, "misub.source.delete", "source:"+itoa(id), "ok")
	c.Status(204)
}

// orderSources 携带完整 ID 列表，只更新顺序；列表与现有集合不一致时拒绝。
func (s *Server) orderSources(c *gin.Context) {
	var req struct {
		IDs []int `json:"ids"`
	}
	if !bind(c, &req) {
		return
	}
	ctx := c.Request.Context()
	ids, err := s.Store.Ent.SubSource.Query().IDs(ctx)
	if err != nil || len(ids) != len(req.IDs) || len(dedupe(req.IDs)) != len(ids) {
		failure(c, 409, "order_changed")
		return
	}
	have := map[int]bool{}
	for _, id := range ids {
		have[id] = true
	}
	for _, id := range req.IDs {
		if !have[id] {
			failure(c, 409, "order_changed")
			return
		}
	}
	for i, id := range req.IDs {
		if err = s.Store.Ent.SubSource.UpdateOneID(id).SetPosition(i).Exec(ctx); err != nil {
			failure(c, 500, "save_failed")
			return
		}
	}
	s.invalidateGroups(ctx)
	c.Status(204)
}

func sourceCacheKey(id int) string { return "misub-source:" + itoa(id) }

// kvCache 是订阅源的保护性缓存（不过期，直到源被修改或删除）。
type kvCache struct{ s *Server }

func (k kvCache) Load(ctx context.Context, id int) ([]string, bool) {
	raw, ok, err := k.s.Store.KV.Get(ctx, sourceCacheKey(id))
	if err != nil || !ok {
		return nil, false
	}
	var nodes []string
	return nodes, json.Unmarshal([]byte(raw), &nodes) == nil
}

func (k kvCache) Store(ctx context.Context, id int, nodes []string) {
	b, _ := json.Marshal(nodes)
	_ = k.s.Store.KV.Set(ctx, sourceCacheKey(id), string(b), 0)
}

func toSource(v *ent.SubSource) misub.Source {
	return misub.Source{ID: v.ID, Name: v.Name, URL: v.URL, UserAgent: v.UserAgent, FilterInclude: v.FilterInclude, FilterExclude: v.FilterExclude,
		Operators: v.Operators, PlusAsSpace: v.PlusAsSpace, NodeCache: v.NodeCache, KnownNodes: v.KnownNodes}
}

// recordResults 保存订阅源本次的节点数、用量、标题与错误。
func (s *Server) recordResults(ctx context.Context, results []misub.SourceResult) {
	now := time.Now()
	for _, r := range results {
		upd := s.Store.Ent.SubSource.UpdateOneID(r.SourceID).SetLastError(r.Error)
		if r.Error == "" {
			upd.SetNodeCount(r.Nodes).SetFetchedAt(now)
			if r.Nodes >= 10 {
				upd.SetKnownNodes(r.Nodes)
			}
			if r.UserInfo != "" {
				upd.SetUserInfo(r.UserInfo)
			}
			if r.Title != "" {
				upd.SetTitle(r.Title)
			}
		}
		_ = upd.Exec(ctx)
	}
}

func (s *Server) subFetcher() misub.Fetcher {
	return misub.Fetcher{AllowPrivate: s.AllowPrivateSources}
}

func (s *Server) refreshSource(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	src, err := s.Store.Ent.SubSource.Get(ctx, id)
	if err != nil {
		failure(c, 404, "source_not_found")
		return
	}
	list, results := misub.Combine(ctx, s.subFetcher(), kvCache{s}, []misub.Source{toSource(src)}, nil, misub.Options{UserAgent: "clash-meta/2.5.0"})
	s.recordResults(ctx, results)
	s.invalidateGroups(ctx)
	src, _ = s.Store.Ent.SubSource.Get(ctx, id)
	nodes := []string{}
	for _, l := range strings.Split(strings.TrimSpace(list), "\n") {
		if l != "" {
			nodes = append(nodes, l)
		}
	}
	c.JSON(200, gin.H{"source": sourceView(src), "result": results[0], "nodes": nodes})
}

// ---- 手动节点 ----

func manualView(v *ent.SubNode) gin.H {
	return gin.H{"id": v.ID, "name": v.Name, "url": v.URL, "group": v.Group, "enabled": v.Enabled, "filter_include": v.FilterInclude,
		"filter_exclude": v.FilterExclude, "operators": v.Operators, "plus_as_space": v.PlusAsSpace, "position": v.Position}
}

func (s *Server) listManualNodes(c *gin.Context) {
	items, err := s.Store.Ent.SubNode.Query().Order(ent.Asc(subnode.FieldPosition), ent.Asc(subnode.FieldID)).All(c.Request.Context())
	if err != nil {
		failure(c, 503, "query_failed")
		return
	}
	out := make([]gin.H, 0, len(items))
	groups := map[string]int{}
	for _, it := range items {
		out = append(out, manualView(it))
		groups[it.Group]++
	}
	c.JSON(200, gin.H{"items": out, "groups": groups})
}

// validNodeURL 只接受可解析的节点链接（不接受 http 订阅地址，那是订阅源）。
func validNodeURL(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 64<<10 || strings.HasPrefix(strings.ToLower(raw), "http://") || strings.HasPrefix(strings.ToLower(raw), "https://") {
		return false
	}
	u, err := url.Parse(raw)
	return err == nil && u.Scheme != ""
}

func (s *Server) saveManualNode(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	var req struct {
		Name          *string `json:"name"`
		URL           *string `json:"url"`
		Group         *string `json:"group"`
		Enabled       *bool   `json:"enabled"`
		FilterInclude *string `json:"filter_include"`
		FilterExclude *string `json:"filter_exclude"`
		Operators     *[]any  `json:"operators"`
		PlusAsSpace   *bool   `json:"plus_as_space"`
	}
	if !bind(c, &req) {
		return
	}
	ctx := c.Request.Context()
	var m *ent.SubNodeMutation
	var create *ent.SubNodeCreate
	var update *ent.SubNodeUpdateOne
	if id == 0 {
		if req.URL == nil {
			failure(c, 400, "invalid_node_url")
			return
		}
		create = s.Store.Ent.SubNode.Create()
		n, _ := s.Store.Ent.SubNode.Query().Count(ctx)
		create.SetPosition(n)
		m = create.Mutation()
	} else {
		old, err := s.Store.Ent.SubNode.Get(ctx, id)
		if err != nil {
			failure(c, 404, "node_not_found")
			return
		}
		update = old.Update()
		m = update.Mutation()
	}
	if req.URL != nil {
		if !validNodeURL(*req.URL) {
			failure(c, 400, "invalid_node_url")
			return
		}
		m.SetURL(strings.TrimSpace(*req.URL))
	}
	if req.Operators != nil {
		if err := checkOperators(*req.Operators); err != nil {
			failDetail(c, 400, "invalid_operators", err.Error(), nil)
			return
		}
		m.SetOperators(*req.Operators)
	}
	for v, set := range map[*string]func(string){req.Name: m.SetName, req.Group: m.SetGroup, req.FilterInclude: m.SetFilterInclude, req.FilterExclude: m.SetFilterExclude} {
		if v != nil {
			if len(*v) > 64<<10 {
				failure(c, 400, "value_too_long")
				return
			}
			set(strings.TrimSpace(*v))
		}
	}
	if req.Enabled != nil {
		m.SetEnabled(*req.Enabled)
	}
	if req.PlusAsSpace != nil {
		m.SetPlusAsSpace(*req.PlusAsSpace)
	}
	var saved *ent.SubNode
	var err error
	if create != nil {
		saved, err = create.Save(ctx)
	} else {
		saved, err = update.Save(ctx)
	}
	if err != nil {
		failure(c, 400, "save_failed")
		return
	}
	s.invalidateGroups(ctx)
	c.JSON(200, manualView(saved))
}

// importManualNodes 批量导入：粘贴节点链接、Base64、Clash YAML 或 sing-box JSON，按引擎解析后逐条保存。
func (s *Server) importManualNodes(c *gin.Context) {
	var req struct {
		Content string `json:"content"`
		Group   string `json:"group"`
	}
	if !bind(c, &req) {
		return
	}
	ctx := c.Request.Context()
	links := misub.ParseContent(req.Content)
	if len(links) == 0 {
		failure(c, 400, "no_nodes_found")
		return
	}
	if len(links) > 2000 {
		failure(c, 400, "too_many_nodes")
		return
	}
	n, _ := s.Store.Ent.SubNode.Query().Count(ctx)
	bulk := make([]*ent.SubNodeCreate, 0, len(links))
	for i, l := range links {
		bulk = append(bulk, s.Store.Ent.SubNode.Create().SetURL(l).SetGroup(strings.TrimSpace(req.Group)).SetPosition(n+i))
	}
	if _, err := s.Store.Ent.SubNode.CreateBulk(bulk...).Save(ctx); err != nil {
		failure(c, 500, "save_failed")
		return
	}
	s.invalidateGroups(ctx)
	s.audit(c, "misub.nodes.import", "", fmt.Sprintf("ok count=%d", len(links)))
	c.JSON(200, gin.H{"imported": len(links)})
}

func (s *Server) deleteManualNode(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	if err := s.Store.Ent.SubNode.DeleteOneID(id).Exec(ctx); err != nil {
		failure(c, 404, "node_not_found")
		return
	}
	s.dropFromProfiles(ctx, "node_ids", id)
	s.invalidateGroups(ctx)
	c.Status(204)
}

// renameNodeGroup 重命名分组；to 为空表示解散（节点保留，移出分组）。目标名已存在时拒绝，不静默合并。
func (s *Server) renameNodeGroup(c *gin.Context) {
	var req struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if !bind(c, &req) {
		return
	}
	ctx := c.Request.Context()
	to := strings.TrimSpace(req.To)
	if to != "" && to != req.From {
		if n, _ := s.Store.Ent.SubNode.Query().Where(subnode.GroupEQ(to)).Count(ctx); n > 0 {
			failure(c, 409, "group_exists")
			return
		}
	}
	if _, err := s.Store.Ent.SubNode.Update().Where(subnode.GroupEQ(req.From)).SetGroup(to).Save(ctx); err != nil {
		failure(c, 500, "save_failed")
		return
	}
	s.invalidateGroups(ctx)
	c.Status(204)
}

// dropFromProfiles 删除源或节点时同步移出订阅组的引用。
func (s *Server) dropFromProfiles(ctx context.Context, field string, id int) {
	profiles, _ := s.Store.Ent.SubProfile.Query().All(ctx)
	for _, p := range profiles {
		list := p.SourceIds
		if field == "node_ids" {
			list = p.NodeIds
		}
		if !contains(list, id) {
			continue
		}
		kept := []int{}
		for _, v := range list {
			if v != id {
				kept = append(kept, v)
			}
		}
		if field == "node_ids" {
			_ = p.Update().SetNodeIds(kept).Exec(ctx)
		} else {
			_ = p.Update().SetSourceIds(kept).Exec(ctx)
		}
	}
}

// ---- 规则模板 ----

func (s *Server) listTemplates(c *gin.Context) {
	items, err := s.Store.Ent.RuleTemplate.Query().Order(ent.Asc(ruletemplate.FieldID)).All(c.Request.Context())
	if err != nil {
		failure(c, 503, "query_failed")
		return
	}
	custom := make([]gin.H, 0, len(items))
	for _, it := range items {
		custom = append(custom, gin.H{"id": it.ID, "name": it.Name, "format": it.Format, "content": it.Content, "updated_at": it.UpdatedAt})
	}
	c.JSON(200, gin.H{"custom": custom, "builtin": misub.BuiltinTemplates()})
}

func (s *Server) saveTemplate(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	var req struct {
		Name    string `json:"name"`
		Format  string `json:"format"`
		Content string `json:"content"`
	}
	if !bind(c, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len(req.Name) > 100 || len(req.Content) > 512<<10 || strings.TrimSpace(req.Content) == "" || req.Format != "ini" && req.Format != "yaml" {
		failure(c, 400, "invalid_template")
		return
	}
	if err := misub.CheckTemplate(req.Format, req.Content); err != nil {
		failDetail(c, 400, "invalid_template", err.Error(), nil)
		return
	}
	ctx := c.Request.Context()
	var err error
	if id == 0 {
		var t *ent.RuleTemplate
		t, err = s.Store.Ent.RuleTemplate.Create().SetName(req.Name).SetFormat(ruletemplate.Format(req.Format)).SetContent(req.Content).Save(ctx)
		if err == nil {
			id = t.ID
		}
	} else {
		err = s.Store.Ent.RuleTemplate.UpdateOneID(id).SetName(req.Name).SetFormat(ruletemplate.Format(req.Format)).SetContent(req.Content).Exec(ctx)
	}
	if err != nil {
		failure(c, 400, "save_failed")
		return
	}
	s.invalidateGroups(ctx)
	c.JSON(200, gin.H{"id": id})
}

func (s *Server) deleteTemplate(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	ref := "custom:" + itoa(id)
	if st, _ := s.misubSettings(ctx); st.Template == ref {
		failure(c, 409, "template_in_use")
		return
	}
	profiles, _ := s.Store.Ent.SubProfile.Query().All(ctx)
	for _, p := range profiles {
		if t, _ := p.Settings["template"].(string); t == ref {
			failure(c, 409, "template_in_use")
			return
		}
	}
	if err := s.Store.Ent.RuleTemplate.DeleteOneID(id).Exec(ctx); err != nil {
		failure(c, 404, "template_not_found")
		return
	}
	c.Status(204)
}

// resolveTemplate 把模板引用解析为内容：builtin:<id>、custom:<id>、remote:<https 地址>（拉取后缓存 1 小时）。
func (s *Server) resolveTemplate(ctx context.Context, ref string) (misub.Template, error) {
	kind, value, _ := strings.Cut(strings.TrimSpace(ref), ":")
	switch kind {
	case "":
		return misub.Template{}, nil
	case "builtin":
		t, ok := misub.BuiltinTemplate(value)
		if !ok {
			return t, fmt.Errorf("内置模板 %s 不存在", value)
		}
		return t, nil
	case "custom":
		id, _ := strconv.Atoi(value)
		t, err := s.Store.Ent.RuleTemplate.Get(ctx, id)
		if err != nil {
			return misub.Template{}, fmt.Errorf("自定义模板 %s 不存在", value)
		}
		return misub.Template{Kind: "custom", Text: t.Content, INI: t.Format == ruletemplate.FormatIni}, nil
	case "remote":
		u, err := misub.CheckURL(value)
		if err != nil || u.Scheme != "https" {
			return misub.Template{}, fmt.Errorf("远程模板须为公网 HTTPS 地址")
		}
		key := "misub-template:" + value
		if raw, ok, _ := s.Store.KV.Get(ctx, key); ok {
			return misub.Template{Kind: "remote", Text: raw, INI: misub.IsINIPath(value)}, nil
		}
		resp, err := s.subFetcher().Get(ctx, value, "Weir")
		if err != nil {
			return misub.Template{}, fmt.Errorf("远程模板拉取失败：%v", err)
		}
		_ = s.Store.KV.Set(ctx, key, string(resp.Body), time.Hour)
		return misub.Template{Kind: "remote", Text: string(resp.Body), INI: misub.IsINIPath(value)}, nil
	}
	return misub.Template{}, fmt.Errorf("模板引用 %q 无效", ref)
}

// previewOperators 对粘贴的节点链接试运行处理链，不保存任何内容。
func (s *Server) previewOperators(c *gin.Context) {
	var req struct {
		Links     []string `json:"links"`
		Operators []any    `json:"operators"`
	}
	if !bind(c, &req) {
		return
	}
	if len(req.Links) > 2000 {
		failure(c, 400, "too_many_nodes")
		return
	}
	if err := checkOperators(req.Operators); err != nil {
		failDetail(c, 400, "invalid_operators", err.Error(), nil)
		return
	}
	out := misub.RunOperators(req.Links, req.Operators, "clash-meta/2.5.0")
	c.JSON(200, gin.H{"output": strings.Join(out, "\n"), "count": len(out)})
}
