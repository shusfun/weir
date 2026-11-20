package center

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/subnode"
	"github.com/shusfun/weir/internal/store/ent/subsource"
	"github.com/shusfun/weir/internal/subgroup"
	"github.com/shusfun/weir/packages/contracts"
)

// profileDraft 是订阅组的编辑内容：保存与编辑预览共用，缺省字段保持原样。
type profileDraft struct {
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

type draftFailure struct {
	status       int
	code, detail string
}

type profileDraftApplied struct{ membersCleared bool }

// applyProfileDraft 校验编辑内容并写到 candidate 上（不落库）。保存与预览都经过这里，规则只有一份。
func (s *Server) applyProfileDraft(ctx context.Context, candidate *ent.SubProfile, req profileDraft) (profileDraftApplied, *draftFailure) {
	var applied profileDraftApplied
	fail := func(status int, code, detail string) (profileDraftApplied, *draftFailure) {
		return applied, &draftFailure{status: status, code: code, detail: detail}
	}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" || len(name) > 100 {
			return fail(400, "invalid_name", "")
		}
		candidate.Name = name
	}
	if req.Enabled != nil {
		candidate.Enabled = *req.Enabled
	}
	if req.AllSources != nil {
		candidate.AllSources = *req.AllSources
	}
	if req.Members != nil && (req.SourceIDs != nil || req.NodeIDs != nil) {
		return fail(400, "invalid_profile_members", "")
	}
	// 选择的源与节点必须存在；保存顺序即输出顺序。
	if req.SourceIDs != nil {
		ids := dedupe(*req.SourceIDs)
		if n, err := s.Store.Ent.SubSource.Query().Where(subsource.IDIn(ids...)).Count(ctx); err != nil || n != len(ids) {
			return fail(400, "unknown_source", "")
		}
		candidate.SourceIds = ids
	}
	if req.NodeIDs != nil {
		ids := dedupe(*req.NodeIDs)
		if n, err := s.Store.Ent.SubNode.Query().Where(subnode.IDIn(ids...)).Count(ctx); err != nil || n != len(ids) {
			return fail(400, "unknown_node", "")
		}
		candidate.NodeIds = ids
	}
	if req.Members != nil {
		candidate.Members = append([]contracts.SubscriptionMember{}, (*req.Members)...)
		var sources, nodes []int
		for _, member := range candidate.Members {
			switch member.Kind {
			case "source":
				sources = append(sources, member.ID)
			case "node":
				nodes = append(nodes, member.ID)
			}
		}
		candidate.SourceIds, candidate.NodeIds = sources, nodes
	} else if req.SourceIDs != nil || req.NodeIDs != nil || candidate.AllSources {
		candidate.Members = nil
		applied.membersCleared = true
	}
	if err := s.checkProfileGraph(ctx, candidate); err != nil {
		return fail(400, "invalid_profile_members", err.Error())
	}
	if req.Operators != nil {
		if err := checkOperators(*req.Operators); err != nil {
			return fail(400, "invalid_operators", err.Error())
		}
		candidate.Operators = *req.Operators
	}
	if req.Settings != nil {
		if (*req.Settings)["engine"] == "external" {
			return fail(400, "external_conversion_removed", "")
		}
		ps, err := decodeProfileSettings(*req.Settings)
		if err != nil {
			code := "invalid_profile_settings"
			switch msg := err.Error(); {
			case strings.HasPrefix(msg, "invalid_filter_include:"):
				code = "invalid_filter_include"
			case strings.HasPrefix(msg, "invalid_filter_exclude:"):
				code = "invalid_filter_exclude"
			case strings.HasPrefix(msg, "invalid_pick:"):
				code = "invalid_pick"
			case strings.HasPrefix(msg, "invalid_") || strings.HasPrefix(msg, "custom_dns_") || strings.HasPrefix(msg, "too_many_region_"):
				code = "invalid_" + subscriptionRenderingField(err)
			}
			return fail(400, code, err.Error())
		}
		if ps.RuleLevel != nil && !validRuleLevel(*ps.RuleLevel) {
			return fail(400, "invalid_profile_settings", "")
		}
		if ps.Template != nil {
			if _, err = s.resolveTemplate(ctx, *ps.Template); err != nil {
				return fail(400, "invalid_template", err.Error())
			}
		}
		candidate.Settings = *req.Settings
	}
	if len(req.ExpiresAt) > 0 {
		if string(req.ExpiresAt) == "null" {
			candidate.ExpiresAt = nil
		} else {
			var t time.Time
			if json.Unmarshal(req.ExpiresAt, &t) != nil {
				return fail(400, "invalid_expires_at", "")
			}
			candidate.ExpiresAt = &t
		}
	}
	return applied, nil
}

type cacheOnlyKey struct{}

// withCacheOnly 让这次组合只用各源已有的缓存，不发起拉取（嵌套的订阅组同样如此）。
func withCacheOnly(ctx context.Context) context.Context {
	return context.WithValue(ctx, cacheOnlyKey{}, true)
}

func cacheOnly(ctx context.Context) bool {
	v, _ := ctx.Value(cacheOnlyKey{}).(bool)
	return v
}

// sourceHealthLookup 把订阅源检测结果接到订阅组的挑选与预览上：检测时的内容版本与这次所用的原始节点不符，
// 或者根本没有检测记录，节点一律算未检测（保留），不当作可用或不可用。
func (s *Server) sourceHealthLookup(ctx context.Context, sourceID int, raw []string) func(string) subgroup.NodeHealth {
	h, err := s.loadSourceHealth(ctx, sourceID)
	if err != nil {
		slog.WarnContext(ctx, "subscription source health unreadable", "trace", taskTrace(ctx), "source", sourceID, "error", err)
		return nil
	}
	if h == nil {
		return nil
	}
	generation := sourceGeneration(raw)
	return func(identity string) subgroup.NodeHealth {
		n := h.nodeStatusFor(generation, identity)
		return subgroup.NodeHealth{Status: n.Status, LatencyMS: n.LatencyMS}
	}
}

type previewNodeView struct {
	Name       string   `json:"name"`
	Region     string   `json:"region"`
	Multiplier *float64 `json:"multiplier"`
	Kind       string   `json:"kind"`
	SourceID   int      `json:"source_id,omitempty"`
	SourceName string   `json:"source_name,omitempty"`
	Status     string   `json:"status"`
	LatencyMS  *int     `json:"latency_ms"`
}

type previewSourceView struct {
	SourceID  int    `json:"source_id"`
	Name      string `json:"name"`
	Nodes     int    `json:"nodes"`
	FromCache bool   `json:"from_cache"`
	Error     string `json:"error,omitempty"`
}

// previewProfileNodes 预览编辑中的订阅组草稿：只用各源已有的缓存组合，不拉取、不写任何缓存或源状态，
// 返回最终节点（名称、地区、倍率、来源、检测状态、延迟）与每一步去掉的数量。
func (s *Server) previewProfileNodes(c *gin.Context) {
	var req struct {
		ID int `json:"id"`
		profileDraft
	}
	if !bind(c, &req) {
		return
	}
	ctx := c.Request.Context()
	candidate := &ent.SubProfile{Enabled: true}
	if req.ID > 0 {
		old, err := s.Store.Ent.SubProfile.Get(ctx, req.ID)
		if err != nil {
			failure(c, 404, "profile_not_found")
			return
		}
		*candidate = *old
	}
	if _, bad := s.applyProfileDraft(ctx, candidate, req.profileDraft); bad != nil {
		failDetail(c, bad.status, bad.code, bad.detail, nil)
		return
	}
	ctx = withCacheOnly(context.WithValue(ctx, previewKey{}, true))
	ctx, finishRun := s.startSubscriptionRun(ctx, candidate.ID)
	finish := operationStage(ctx, "subscription.profile.preview_nodes", "profile_id", candidate.ID)
	var runErr error
	defer func() { finish(runErr); finishRun(runErr) }()
	remaining := maxSubscriptionMembers
	var report subgroup.Report
	_, results, _, _, err := s.groupNodesAt(ctx, candidate, "clash-meta/2.5.0", false, map[int]bool{}, 0, &remaining, &report)
	if err != nil {
		runErr = err
		failDetail(c, 422, "preview_failed", err.Error(), nil)
		return
	}
	names := map[int]string{}
	ids := make([]int, 0, len(results))
	for _, r := range results {
		ids = append(ids, r.SourceID)
	}
	for _, n := range report.Nodes {
		if n.SourceID > 0 {
			ids = append(ids, n.SourceID)
		}
	}
	if len(ids) > 0 {
		rows, err := s.Store.Ent.SubSource.Query().Where(subsource.IDIn(dedupe(ids)...)).All(ctx)
		if err != nil {
			runErr = err
			failInternal(c, 503, "query_failed", err)
			return
		}
		for _, row := range rows {
			names[row.ID] = firstNonEmpty(row.Name, fmt.Sprintf("订阅源 #%d", row.ID))
		}
	}
	nodes := make([]previewNodeView, 0, len(report.Nodes))
	for _, n := range report.Nodes {
		a := subgroup.AttrsOf(n.Link)
		nodes = append(nodes, previewNodeView{Name: a.Name, Region: a.Region, Multiplier: a.Multiplier, Kind: n.Kind, SourceID: n.SourceID,
			SourceName: names[n.SourceID], Status: n.Health.Status, LatencyMS: n.Health.LatencyMS})
	}
	sources := make([]previewSourceView, 0, len(results))
	for _, r := range results {
		sources = append(sources, previewSourceView{SourceID: r.SourceID, Name: names[r.SourceID], Nodes: r.Nodes, FromCache: r.FromCache, Error: r.Error})
	}
	c.JSON(200, gin.H{"nodes": nodes, "merged": report.Merged, "steps": report.Steps, "available": report.Available, "sources": sources, "trace_id": taskTrace(ctx)})
}

func (s *Server) profileEditRoutes(admin *gin.RouterGroup) {
	admin.POST("/subscriptions/profiles/preview-nodes", s.previewProfileNodes)
	admin.GET("/users/:uid/subscription-groups", s.getUserGroups)
	admin.PUT("/users/:uid/subscription-groups", s.putUserGroups)
}
