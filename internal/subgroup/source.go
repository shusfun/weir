package subgroup

import (
	"context"
	"strings"
	"time"

	"github.com/shusfun/weir/internal/sub"
)

// processSource applies current rules to raw nodes, including cache fallbacks.
func processSource(raw []string, s Source, o Options) ([]string, error) {
	nodes := append([]string(nil), raw...)
	var err error
	nodes, err = runOperators(nodes, s.Operators, o.UserAgent)
	if err != nil {
		return nil, err
	}
	nodes = filterRules(nodes, s.FilterInclude, s.FilterExclude)
	if o.PrependSources && s.Name != "" {
		for i, n := range nodes {
			nodes[i] = sub.PrependNodeName(n, s.Name)
		}
	}
	return nodes, nil
}

func rawNodeCount(nodes []string) int {
	count := 0
	for _, n := range nodes {
		if isRealProxyNode(n) {
			count++
		}
	}
	return count
}

func fetchSource(ctx context.Context, f Fetcher, cache Cache, s Source, o Options) ([]string, SourceResult) {
	res := SourceResult{SourceID: s.ID, ConfigVersion: s.ConfigVersion, Started: time.Now()}
	process := func(raw []string) []string {
		nodes, err := processSource(raw, s, o)
		if err != nil {
			res.processingError = err
			res.Error = "source_rules_invalid: " + err.Error()
		}
		return nodes
	}
	ua := SourceUserAgent(s, o.UserAgent)
	res.UserAgent = ua
	if scoped, ok := cache.(interface{ ForSource(Source, string) Cache }); ok {
		cache = scoped.ForSource(s, ua)
	}
	finish := beginStage(ctx, "subscription.source", s.ID)
	defer func() { finish(res.Error, res.FromCache) }()
	cached := func() ([]string, bool) {
		if !s.NodeCache || cache == nil {
			return nil, false
		}
		nodes, ok := cache.Load(ctx, s.ID)
		if ok {
			res.FromCache = true
			res.Nodes = rawNodeCount(nodes)
			res.basis = nodes
		}
		return process(nodes), ok
	}
	if o.CacheOnly {
		var nodes []string
		ok := false
		if cache != nil {
			nodes, ok = cache.Load(ctx, s.ID)
		}
		if !ok {
			res.Error = "source_not_cached"
			return nil, res
		}
		res.FromCache, res.Nodes, res.basis = true, rawNodeCount(nodes), nodes
		return process(nodes), res
	}
	resp, err := f.Get(WithSourceID(ctx, s.ID), s.URL, ua)
	res.Location, res.RouteReason = resp.Location, resp.RouteReason
	if err != nil {
		res.Error = err.Error()
		nodes, _ := cached()
		return nodes, res
	}
	res.UserInfo, res.Title = resp.UserInfo, resp.Title
	nodes, err := parseSourceContent(string(resp.Body), s.PlusAsSpace)
	if err != nil {
		res.Error = err.Error()
		fallback, _ := cached()
		return fallback, res
	}
	real := rawNodeCount(nodes)
	// An upstream 200 with no usable proxies can be an expired account or outage.
	// Do not replace the last good snapshot until the operator accepts the impact.
	if real == 0 && !s.AcceptEmpty {
		res.Error = "source_empty_rejected"
		if cache != nil {
			if old, ok := cache.Load(ctx, s.ID); ok {
				res.FromCache, res.Nodes, res.basis = true, rawNodeCount(old), old
				return process(old), res
			}
		}
		return nil, res
	}
	// 机场节点经常增减：节点数变化直接采用新结果，变化量由调用方展示，不再拦截骤减。
	if cache != nil && !o.DeferCacheWrite {
		cache.Store(ctx, s.ID, nodes)
	}
	res.Nodes = real
	res.RawNodes = append([]string{}, nodes...)
	res.basis = res.RawNodes
	processed := process(nodes)
	return processed, res
}

func SourceUserAgent(s Source, client string) string {
	if ua := strings.TrimSpace(s.UserAgent); ua != "" {
		return ua
	}
	return sub.GetProcessedUserAgent(client, s.URL)
}
