package center

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/shusfun/weir/internal/store"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/ruletemplate"
	"github.com/shusfun/weir/internal/store/ent/setting"
	"github.com/shusfun/weir/internal/store/ent/subnode"
	"github.com/shusfun/weir/internal/store/ent/subsource"
	"github.com/shusfun/weir/internal/sub"
	"github.com/shusfun/weir/internal/subgroup"
	"github.com/shusfun/weir/packages/contracts"
)

// 订阅源页：订阅源 + 手动节点 → 订阅组（处理链、规则模板）→ 分配给用户或分享。
// 订阅组的节点来自外部机场，流量不经本项目的 Gate，因此不计入用户额度；到期与停用仍然生效。

const subscriptionSettingKey = "subscription-settings"

func sourceRefreshValue(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	default:
		return 0, false
	}
}

// SubscriptionSettings 是订阅源页的全局默认值（订阅组可逐项覆盖）。
type SubscriptionSettings struct {
	Engine           string         `json:"engine"`
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
	EnableTFO        bool           `json:"enable_tfo"`
	DNSMode          string         `json:"dns_mode"`
	CustomDNS        string         `json:"custom_dns"`
	RegionOverrides  []any          `json:"region_overrides"`
	FilterInclude    string         `json:"filter_include"`
	FilterExclude    string         `json:"filter_exclude"`
	UpdateInterval   int            `json:"update_interval"`
	SourceRefresh    int            `json:"source_refresh_hours"`
	// Disguise 是伪装页开关（默认关闭）：打开后浏览器访问订阅地址只看到伪装页，剩余流量与一键导入改为登录后
	// 在「我的订阅」查看；客户端拉取订阅不受影响。
	Disguise bool `json:"disguise"`
}

func defaultSubscriptionSettings() SubscriptionSettings {
	return SubscriptionSettings{FlagEmoji: true, PrependManual: true, ManualPrefix: "手动节点", PrependSources: true, RuleLevel: "std",
		TrafficNodes: true, CacheMinutes: 10, EnableUDP: true, DNSMode: "clean", UpdateInterval: 24, SourceRefresh: 0}
}

func (s *Server) subscriptionSettings(ctx context.Context) (SubscriptionSettings, error) {
	out := defaultSubscriptionSettings()
	row, err := s.Store.Ent.Setting.Query().Where(setting.KeyEQ(subscriptionSettingKey)).Only(ctx)
	if ent.IsNotFound(err) {
		if old, e := s.Store.Ent.Setting.Query().Where(setting.KeyEQ(notifyKey)).Only(ctx); e == nil {
			if raw, ok := old.Value["source_refresh_hours"]; ok {
				if n, ok := sourceRefreshValue(raw); ok {
					out.SourceRefresh = int(n)
				}
				value := map[string]any{}
				bb, _ := json.Marshal(out)
				_ = json.Unmarshal(bb, &value)
				_ = s.Store.Ent.Setting.Create().SetKey(subscriptionSettingKey).SetValue(value).OnConflictColumns(setting.FieldKey).UpdateNewValues().Exec(ctx)
				delete(old.Value, "source_refresh_hours")
				_, _ = s.Store.Ent.Setting.Update().Where(setting.KeyEQ(notifyKey)).SetValue(old.Value).Save(ctx)
			}
		}
		return out, nil
	}
	if err != nil {
		return out, err
	}
	b, _ := json.Marshal(row.Value)
	err = json.Unmarshal(b, &out)
	if err != nil {
		return out, err
	}
	if _, ok := row.Value["source_refresh_hours"]; !ok {
		if old, e := s.Store.Ent.Setting.Query().Where(setting.KeyEQ(notifyKey)).Only(ctx); e == nil {
			if raw, ok := old.Value["source_refresh_hours"]; ok {
				if n, ok := sourceRefreshValue(raw); ok {
					out.SourceRefresh = int(n)
				}
				value := map[string]any{}
				bb, _ := json.Marshal(out)
				_ = json.Unmarshal(bb, &value)
				_ = s.Store.Ent.Setting.Create().SetKey(subscriptionSettingKey).SetValue(value).OnConflictColumns(setting.FieldKey).UpdateNewValues().Exec(ctx)
				delete(old.Value, "source_refresh_hours")
				_, _ = s.Store.Ent.Setting.Update().Where(setting.KeyEQ(notifyKey)).SetValue(old.Value).Save(ctx)
			}
		}
	}
	return out, nil
}

func (s *Server) subscriptionRoutes(r *gin.Engine, auth, admin *gin.RouterGroup) {
	admin.GET("/subscriptions/converter", s.getConverterSettings)
	admin.PUT("/subscriptions/converter", s.putConverterSettings)
	admin.GET("/subscriptions/runs", s.listSubscriptionRuns)
	admin.GET("/subscriptions/runs/:id", s.getSubscriptionRun)
	admin.GET("/subscriptions/settings", s.getSubscriptionSettings)
	admin.PUT("/subscriptions/settings", s.putSubscriptionSettings)
	admin.GET("/subscriptions/sources", s.listSources)
	admin.POST("/subscriptions/sources", s.saveSource)
	admin.PATCH("/subscriptions/sources/:id", s.saveSource)
	admin.DELETE("/subscriptions/sources/:id", s.deleteSource)
	admin.POST("/subscriptions/sources/:id/refresh", s.refreshSource)
	admin.POST("/subscriptions/sources/order", s.orderSources)
	admin.GET("/subscriptions/nodes", s.listManualNodes)
	admin.POST("/subscriptions/nodes", s.saveManualNode)
	admin.POST("/subscriptions/nodes/import", s.importManualNodes)
	admin.PATCH("/subscriptions/nodes/:id", s.saveManualNode)
	admin.DELETE("/subscriptions/nodes/:id", s.deleteManualNode)
	admin.POST("/subscriptions/nodes/groups/rename", s.renameNodeGroup)
	admin.GET("/subscriptions/profiles", s.listSubProfiles)
	admin.POST("/subscriptions/profiles", s.saveProfile)
	admin.PATCH("/subscriptions/profiles/:id", s.saveProfile)
	admin.DELETE("/subscriptions/profiles/:id", s.deleteProfile)
	admin.POST("/subscriptions/profiles/:id/preview", s.previewProfile)
	admin.POST("/subscriptions/profiles/:id/test", s.asJob("实测订阅组节点", "已取消，保留已完成条目", s.testSubscriptionProfile))
	admin.GET("/subscriptions/profiles/:id/test", s.getSubscriptionProfileTest)
	admin.POST("/subscriptions/profiles/:id/share", s.shareProfile)
	admin.DELETE("/subscriptions/profiles/:id/share", s.unshareProfile)
	admin.PUT("/subscriptions/profiles/:id/grants", s.setGrants)
	admin.POST("/subscriptions/operators/preview", s.previewOperators)
	admin.GET("/subscriptions/templates", s.listTemplates)
	admin.POST("/subscriptions/templates", s.saveTemplate)
	admin.PATCH("/subscriptions/templates/:id", s.saveTemplate)
	admin.DELETE("/subscriptions/templates/:id", s.deleteTemplate)
	auth.GET("/me/groups", s.myGroups)
	r.GET("/sub/g/:token", s.serveGroup)
}

func (s *Server) getSubscriptionSettings(c *gin.Context) {
	st, err := s.subscriptionSettings(c.Request.Context())
	if err != nil {
		failInternal(c, 503, "settings_unavailable", err)
		return
	}
	c.JSON(200, st)
}

func (s *Server) putSubscriptionSettings(c *gin.Context) {
	var st SubscriptionSettings
	if !bind(c, &st) {
		return
	}
	if !validSubscriptionEngine(st.Engine) {
		failure(c, 400, "invalid_engine")
		return
	}
	if err := checkOperators(st.DefaultOperators); err != nil {
		failDetail(c, 400, "invalid_operators", err.Error(), nil)
		return
	}
	for _, text := range []string{st.FilterInclude, st.FilterExclude} {
		if err := sub.ValidateFilterRules(text); err != nil {
			failDetail(c, 400, "invalid_filter_rules", err.Error(), nil)
			return
		}
	}
	if err := checkSubscriptionRendering(st.DNSMode, st.CustomDNS, st.RegionOverrides); err != nil {
		failDetail(c, 400, "invalid_"+subscriptionRenderingField(err), err.Error(), nil)
		return
	}
	if st.CacheMinutes < 0 || st.CacheMinutes > 1440 || st.UpdateInterval < 1 || st.UpdateInterval > 168 || st.SourceRefresh < 0 || st.SourceRefresh > 168 || !validRuleLevel(st.RuleLevel) {
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
	if err := s.Store.Ent.Setting.Create().SetKey(subscriptionSettingKey).SetValue(value).OnConflictColumns(setting.FieldKey).UpdateNewValues().Exec(c.Request.Context()); err != nil {
		failInternal(c, 500, "save_failed", err)
		return
	}
	s.audit(c, "subscription.settings", "", "ok")
	s.invalidateGroups(c.Request.Context())
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
	return sub.ValidateOperatorPatterns(ops)
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
		key := strings.TrimSpace(k)
		if key != "upload" && key != "download" && key != "total" && key != "expire" {
			continue
		}
		if n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); ok && err == nil && n >= 0 {
			out[key] = n
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// 部分上游只报告额度或期限；缺失传输计数时不能推算剩余流量。
func sourceTraffic(ui map[string]int64) (int64, int64, bool) {
	upload, hasUpload := ui["upload"]
	download, hasDownload := ui["download"]
	total, hasTotal := ui["total"]
	if !hasUpload || !hasDownload || !hasTotal || total <= 0 || upload > math.MaxInt64-download {
		return 0, 0, false
	}
	return upload + download, total, true
}

func (s *Server) listSources(c *gin.Context) {
	items, err := s.Store.Ent.SubSource.Query().Order(ent.Asc(subsource.FieldPosition), ent.Asc(subsource.FieldID)).All(c.Request.Context())
	if err != nil {
		failInternal(c, 503, "query_failed", err)
		return
	}
	out := make([]gin.H, 0, len(items))
	var used, total int64
	known := 0
	for _, it := range items {
		out = append(out, sourceView(it))
		if u, t, ok := sourceTraffic(parseUserInfo(it.UserInfo)); it.Enabled && !it.ExcludeTraffic && ok && used <= math.MaxInt64-u && total <= math.MaxInt64-t {
			used += u
			total += t
			known++
		}
	}
	c.JSON(200, gin.H{"items": out, "traffic": gin.H{"used": used, "total": total, "sources": known}})
}

func (s *Server) saveSource(c *gin.Context) {
	if err := s.Store.Lock.Do(c.Request.Context(), "subscription-source:"+c.Param("id"), func(context.Context) error {
		s.saveSourceLocked(c)
		return nil
	}); err != nil {
		failInternal(c, 503, "save_failed", err)
	}
}

func (s *Server) saveSourceLocked(c *gin.Context) {
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
	if !checkSubscriptionFilters(c, req.FilterInclude, req.FilterExclude) {
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
		if _, err := subgroup.CheckURL(*req.URL); err != nil && !(s.AllowPrivateSources && err.Error() == "source_url_private") {
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
	if create != nil && dryRun(c, dryResult{Preview: []string{"新建订阅源；保存后到「订阅源」页点刷新拉取节点（或在草稿里加一步 source.refresh）"}}) ||
		create == nil && dryRun(c, dryResult{Preview: []string{"修改订阅源「" + old.Name + "」；改了地址时清掉旧的节点缓存与用量"}}) {
		return
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
	s.audit(c, "subscription.source.save", "source:"+itoa(saved.ID), "ok")
	c.JSON(200, sourceView(saved))
}

func (s *Server) deleteSource(c *gin.Context) {
	if err := s.Store.Lock.Do(c.Request.Context(), "subscription-profiles", func(context.Context) error {
		s.deleteSourceLocked(c)
		return nil
	}); err != nil {
		failInternal(c, 503, "delete_failed", err)
	}
}

func (s *Server) deleteSourceLocked(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	if !s.checkExitSourceDeletion(c, id, 0) {
		return
	}
	if c.GetBool(dryRunKey) {
		src, err := s.Store.Ent.SubSource.Get(ctx, id)
		if err != nil {
			failure(c, 404, "source_not_found")
			return
		}
		dryRun(c, dryResult{Preview: []string{"删除订阅源「" + src.Name + "」，并从所有订阅组里移除"}})
		return
	}
	tx, err := s.Store.Ent.Tx(ctx)
	if err != nil {
		failInternal(c, 503, "delete_failed", err)
		return
	}
	defer tx.Rollback()
	if err := tx.SubSource.DeleteOneID(id).Exec(ctx); err != nil {
		failure(c, 404, "source_not_found")
		return
	}
	if err := dropFromProfiles(ctx, tx.Client(), "source_ids", id); err != nil {
		failInternal(c, 503, "delete_failed", err)
		return
	}
	if err := tx.Commit(); err != nil {
		failInternal(c, 503, "delete_failed", err)
		return
	}
	_ = s.Store.KV.Del(ctx, sourceCacheKey(id))
	s.invalidateGroups(ctx)
	s.audit(c, "subscription.source.delete", "source:"+itoa(id), "ok")
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
			failInternal(c, 500, "save_failed", err)
			return
		}
	}
	s.invalidateGroups(ctx)
	c.Status(204)
}

func sourceCacheKey(id int) string { return "sub-source:" + itoa(id) }

// kvCache 按源与 UA 隔离，受基础配置的单项大小和生命周期限制。
type kvCache struct{ s *Server }

func (k kvCache) Load(ctx context.Context, id int) ([]string, bool) {
	return k.defaultCache(ctx, id).Load(ctx, id)
}

func (k kvCache) Store(ctx context.Context, id int, nodes []string) {
	k.defaultCache(ctx, id).Store(ctx, id, nodes)
}

func (k kvCache) defaultCache(ctx context.Context, id int) subgroup.Cache {
	if src, err := k.s.Store.Ent.SubSource.Get(ctx, id); err == nil {
		source := toSource(src)
		return k.ForSource(source, subgroup.SourceUserAgent(source, "clash-meta/2.5.0"))
	}
	return sourceIdentityCache{k.s, sourceCacheKey(id)}
}

func (k kvCache) ForSource(source subgroup.Source, ua string) subgroup.Cache {
	key := sourceCacheKey(source.ID) + ":" + version([]any{source.URL, ua, source.PlusAsSpace})
	return sourceIdentityCache{k.s, key}
}

type sourceIdentityCache struct {
	s   *Server
	key string
}

func (k sourceIdentityCache) Load(ctx context.Context, _ int) ([]string, bool) {
	raw, ok, err := k.s.Store.KV.Get(ctx, k.key)
	if err != nil || !ok || len(raw) > k.s.Config.Subscriptions.effective().CacheItemBytes {
		return nil, false
	}
	var nodes []string
	if json.Unmarshal([]byte(raw), &nodes) != nil {
		return nil, false
	}
	return nodes, true
}

func (k sourceIdentityCache) Store(ctx context.Context, _ int, nodes []string) {
	b, _ := json.Marshal(nodes)
	limits := k.s.Config.Subscriptions.effective()
	if len(b) > limits.CacheItemBytes {
		return
	}
	err := k.s.Store.WriteSubscriptionCacheBounded(ctx, k.key, string(b), time.Duration(limits.RawCacheHours)*time.Hour, store.SubscriptionCacheBudget{Bytes: limits.CacheTotalBytes, Entries: limits.CacheEntries})
	if !errors.Is(err, store.ErrSubscriptionCacheCapacity) {
		k.s.tasks.note("subscription-raw-cache", err)
	}
}

func toSource(v *ent.SubSource) subgroup.Source {
	return subgroup.Source{ID: v.ID, Name: v.Name, URL: v.URL, UserAgent: v.UserAgent, FilterInclude: v.FilterInclude, FilterExclude: v.FilterExclude,
		Operators: v.Operators, PlusAsSpace: v.PlusAsSpace, NodeCache: v.NodeCache, KnownNodes: v.KnownNodes,
		ConfigVersion: version([]any{v.URL, v.UserAgent, v.PlusAsSpace, v.NodeCache, v.FilterInclude, v.FilterExclude, v.Operators, v.Name})}
}

// recordResults 保存订阅源本次的节点数、用量、标题与错误。
func (s *Server) recordResults(ctx context.Context, results []subgroup.SourceResult) {
	for _, r := range results {
		started := time.Now().UTC()
		err := s.Store.Lock.Do(ctx, "subscription-source:"+itoa(r.SourceID), func(ctx context.Context) error {
			src, err := s.Store.Ent.SubSource.Get(ctx, r.SourceID)
			if ent.IsNotFound(err) {
				return nil
			}
			if err != nil {
				return err
			}
			if r.ConfigVersion != "" && r.ConfigVersion != toSource(src).ConfigVersion {
				return nil
			}
			return s.recordSourceResult(ctx, r)
		})
		stage := subgroup.Stage{SourceID: r.SourceID, Operation: "source.persist", Started: started, Finished: time.Now().UTC()}
		if err != nil {
			stage.Error = "source_result_save_failed"
		}
		recordSubscriptionStage(ctx, 0, stage, "center.recordResults")
		s.tasks.note("subscription-source-result", err)
	}
}

func (s *Server) recordSourceResult(ctx context.Context, r subgroup.SourceResult) error {
	now := time.Now()
	upd := s.Store.Ent.SubSource.UpdateOneID(r.SourceID).SetLastError(r.Error)
	if r.Error == "" {
		upd.SetNodeCount(r.Nodes).SetKnownNodes(r.Nodes).SetFetchedAt(now)
		upd.SetUserInfo(r.UserInfo)
		if r.Title != "" {
			upd.SetTitle(r.Title)
		}
	}
	if err := upd.Exec(ctx); err != nil {
		return err
	}
	if r.Error != "" && r.Error != "node_count_drop_rejected" {
		_, err := s.enqueueObservation(ctx, observationInput{Point: "subscription.health", Resource: "source:" + itoa(r.SourceID), Text: r.Error, Origin: "center.recordResults"})
		s.tasks.note("observation-producer", err)
	}
	return nil
}

func (s *Server) subFetcher() subgroup.Fetcher {
	return subgroup.Fetcher{AllowPrivate: s.AllowPrivateSources, Fetch: s.fetchSubscription}
}

func (s *Server) refreshSource(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	ctx := s.subscriptionContext(c.Request.Context(), 0)
	src, err := s.Store.Ent.SubSource.Get(ctx, id)
	if err != nil {
		failure(c, 404, "source_not_found")
		return
	}
	if dryRun(c, dryResult{Preview: []string{"重新拉取订阅源「" + src.Name + "」，更新节点缓存与用量"}}) {
		return
	}
	ctx, finish := s.startSubscriptionRun(ctx, 0)
	defer finish(nil)
	list, results := subgroup.Combine(ctx, s.subFetcher(), kvCache{s}, []subgroup.Source{toSource(src)}, nil, subgroup.Options{UserAgent: "clash-meta/2.5.0"})
	s.recordResults(ctx, results)
	s.invalidateGroups(ctx)
	src, _ = s.Store.Ent.SubSource.Get(ctx, id)
	if src == nil {
		failure(c, 404, "source_not_found")
		return
	}
	nodes := []string{}
	for _, l := range strings.Split(strings.TrimSpace(list), "\n") {
		if l != "" {
			nodes = append(nodes, l)
		}
	}
	c.JSON(200, gin.H{"source": sourceView(src), "result": results[0], "nodes": nodes})
}

func (s *Server) subscriptionContext(ctx context.Context, profileID int) context.Context {
	ctx = withTrace(ctx, taskTrace(ctx))
	return subgroup.WithObserver(ctx, func(ctx context.Context, stage subgroup.Stage) {
		origin := "subgroup.Combine"
		if stage.SourceID > 0 {
			origin = "subgroup.fetchSource"
		}
		recordSubscriptionStage(ctx, profileID, stage, origin)
		outcome := "succeeded"
		if stage.Error != "" {
			outcome = "failed"
		}
		slog.InfoContext(ctx, "subscription stage", "trace", taskTrace(ctx), "profile", profileID, "source", stage.SourceID,
			"stage", stage.Operation, "outcome", outcome, "cache_fallback", stage.CacheFallback, "ms", stage.Finished.Sub(stage.Started).Milliseconds())
	})
}

// ---- 手动节点 ----

func manualView(v *ent.SubNode) gin.H {
	return gin.H{"id": v.ID, "name": v.Name, "url": v.URL, "group": v.Group, "enabled": v.Enabled, "filter_include": v.FilterInclude,
		"filter_exclude": v.FilterExclude, "operators": v.Operators, "plus_as_space": v.PlusAsSpace, "position": v.Position}
}

func (s *Server) listManualNodes(c *gin.Context) {
	items, err := s.Store.Ent.SubNode.Query().Order(ent.Asc(subnode.FieldPosition), ent.Asc(subnode.FieldID)).All(c.Request.Context())
	if err != nil {
		failInternal(c, 503, "query_failed", err)
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
	if !checkSubscriptionFilters(c, req.FilterInclude, req.FilterExclude) {
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
	if dryRun(c, dryResult{Preview: []string{"保存手动节点；包含它的订阅组随即更新"}}) {
		return
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
	links := subgroup.ParseContent(req.Content)
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
		failInternal(c, 500, "save_failed", err)
		return
	}
	s.invalidateGroups(ctx)
	s.audit(c, "subscription.nodes.import", "", fmt.Sprintf("ok count=%d", len(links)))
	c.JSON(200, gin.H{"imported": len(links)})
}

func (s *Server) deleteManualNode(c *gin.Context) {
	if err := s.Store.Lock.Do(c.Request.Context(), "subscription-profiles", func(context.Context) error {
		s.deleteManualNodeLocked(c)
		return nil
	}); err != nil {
		failInternal(c, 503, "delete_failed", err)
	}
}

func (s *Server) deleteManualNodeLocked(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	if c.GetBool(dryRunKey) {
		v, err := s.Store.Ent.SubNode.Get(ctx, id)
		if err != nil {
			failure(c, 404, "node_not_found")
			return
		}
		dryRun(c, dryResult{Preview: []string{"删除手动节点「" + firstNonEmpty(v.Name, "未命名") + "」，并从所有订阅组里移除"}})
		return
	}
	tx, err := s.Store.Ent.Tx(ctx)
	if err != nil {
		failInternal(c, 503, "delete_failed", err)
		return
	}
	defer tx.Rollback()
	if err := tx.SubNode.DeleteOneID(id).Exec(ctx); err != nil {
		failure(c, 404, "node_not_found")
		return
	}
	if err := dropFromProfiles(ctx, tx.Client(), "node_ids", id); err != nil {
		failInternal(c, 503, "delete_failed", err)
		return
	}
	if err := tx.Commit(); err != nil {
		failInternal(c, 503, "delete_failed", err)
		return
	}
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
		failInternal(c, 500, "save_failed", err)
		return
	}
	s.invalidateGroups(ctx)
	c.Status(204)
}

// dropFromProfiles 删除源或节点时同步移出订阅组的引用。
func dropFromProfiles(ctx context.Context, db *ent.Client, field string, id int) error {
	profiles, err := db.SubProfile.Query().All(ctx)
	if err != nil {
		return err
	}
	for _, p := range profiles {
		list := p.SourceIds
		if field == "node_ids" {
			list = p.NodeIds
		}
		kind := "source"
		if field == "node_ids" {
			kind = "node"
		}
		members, changed := []contracts.SubscriptionMember{}, false
		for _, m := range p.Members {
			if m.Kind == kind && m.ID == id {
				changed = true
			} else {
				members = append(members, m)
			}
		}
		if !contains(list, id) && !changed {
			continue
		}
		kept := []int{}
		for _, v := range list {
			if v != id {
				kept = append(kept, v)
			}
		}
		update := p.Update()
		if p.Members != nil {
			update.SetMembers(members)
		}
		if field == "node_ids" {
			update.SetNodeIds(kept)
		} else {
			update.SetSourceIds(kept)
		}
		if err := update.Exec(ctx); err != nil {
			return err
		}
	}
	return nil
}

// ---- 规则模板 ----

func (s *Server) listTemplates(c *gin.Context) {
	items, err := s.Store.Ent.RuleTemplate.Query().Order(ent.Asc(ruletemplate.FieldID)).All(c.Request.Context())
	if err != nil {
		failInternal(c, 503, "query_failed", err)
		return
	}
	custom := make([]gin.H, 0, len(items))
	for _, it := range items {
		custom = append(custom, gin.H{"id": it.ID, "name": it.Name, "format": it.Format, "content": it.Content, "updated_at": it.UpdatedAt})
	}
	c.JSON(200, gin.H{"custom": custom, "builtin": subgroup.BuiltinTemplates()})
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
	if err := subgroup.CheckTemplate(req.Format, req.Content); err != nil {
		failDetail(c, 400, "invalid_template", err.Error(), nil)
		return
	}
	ctx := c.Request.Context()
	if id != 0 {
		if _, err := s.Store.Ent.RuleTemplate.Get(ctx, id); err != nil {
			failure(c, 404, "template_not_found")
			return
		}
	}
	if dryRun(c, dryResult{Preview: []string{"规则模板已通过格式检查；用它的订阅组随即按新内容输出"}}) {
		return
	}
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
	if st, _ := s.subscriptionSettings(ctx); st.Template == ref {
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
	if c.GetBool(dryRunKey) {
		if _, err := s.Store.Ent.RuleTemplate.Get(ctx, id); err != nil {
			failure(c, 404, "template_not_found")
			return
		}
		dryRun(c, dryResult{Preview: []string{"删除这个规则模板（没有订阅组在用）"}})
		return
	}
	if err := s.Store.Ent.RuleTemplate.DeleteOneID(id).Exec(ctx); err != nil {
		failure(c, 404, "template_not_found")
		return
	}
	c.Status(204)
}

// resolveTemplate 把模板引用解析为内容：builtin:<id>、custom:<id>、remote:<https 地址>（拉取后缓存 1 小时）。
func (s *Server) resolveTemplate(ctx context.Context, ref string) (subgroup.Template, error) {
	kind, value, _ := strings.Cut(strings.TrimSpace(ref), ":")
	switch kind {
	case "":
		return subgroup.Template{}, nil
	case "builtin":
		t, ok := subgroup.BuiltinTemplate(value)
		if !ok {
			return t, fmt.Errorf("内置模板 %s 不存在", value)
		}
		return t, nil
	case "custom":
		id, _ := strconv.Atoi(value)
		t, err := s.Store.Ent.RuleTemplate.Get(ctx, id)
		if err != nil {
			return subgroup.Template{}, fmt.Errorf("自定义模板 %s 不存在", value)
		}
		return subgroup.Template{Kind: "custom", Text: t.Content, INI: t.Format == ruletemplate.FormatIni}, nil
	case "remote":
		u, err := subgroup.CheckURL(value)
		if err != nil || u.Scheme != "https" {
			return subgroup.Template{}, fmt.Errorf("远程模板须为公网 HTTPS 地址")
		}
		key := "sub-template:" + value
		if raw, ok, _ := s.Store.KV.Get(ctx, key); ok {
			return subgroup.Template{Kind: "remote", Text: raw, INI: subgroup.IsINIPath(value)}, nil
		}
		resp, err := s.subFetcher().Get(ctx, value, "Weir")
		if err != nil {
			return subgroup.Template{}, fmt.Errorf("远程模板拉取失败：%v", err)
		}
		_ = s.Store.KV.Set(ctx, key, string(resp.Body), time.Hour)
		return subgroup.Template{Kind: "remote", Text: string(resp.Body), INI: subgroup.IsINIPath(value)}, nil
	}
	return subgroup.Template{}, fmt.Errorf("模板引用 %q 无效", ref)
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
	ctx, complete := s.startSubscriptionRun(c.Request.Context(), 0)
	started := time.Now().UTC()
	var processingError error
	defer func() {
		subscriptionStageResult(ctx, 0, "subscription.operators.preview", "center.previewOperators", started, processingError)
		complete(processingError)
	}()
	if err := checkOperators(req.Operators); err != nil {
		processingError = errors.New("group_rules_invalid")
		failDetail(c, 400, "invalid_operators", err.Error(), nil)
		return
	}
	out, err := subgroup.RunOperatorsChecked(req.Links, req.Operators, "clash-meta/2.5.0")
	if err != nil {
		processingError = errors.New("group_rules_invalid")
		failDetail(c, 400, "invalid_operators", err.Error(), nil)
		return
	}
	c.JSON(200, gin.H{"output": strings.Join(out, "\n"), "count": len(out)})
}
