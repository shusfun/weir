package subgroup

import (
	"context"
	"strings"

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
	res := SourceResult{SourceID: s.ID, ConfigVersion: s.ConfigVersion}
	process := func(raw []string) []string {
		nodes, err := processSource(raw, s, o)
		if err != nil {
			res.processingError = err
			res.Error = "source_rules_invalid: " + err.Error()
		}
		return nodes
	}
	ua := SourceUserAgent(s, o.UserAgent)
	if scoped, ok := cache.(interface{ ForSource(Source, string) Cache }); ok {
		cache = scoped.ForSource(s, ua)
		// The shared runtime count can describe another client's representation.
		// A scoped cache supplies its own baseline for sharp-drop protection.
		s.KnownNodes = 0
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
		}
		return process(nodes), ok
	}
	resp, err := f.Get(ctx, s.URL, ua)
	if err != nil {
		res.Error = err.Error()
		nodes, _ := cached()
		return nodes, res
	}
	res.UserInfo, res.Title = resp.UserInfo, resp.Title
	res.Location = resp.Location
	nodes, err := parseSourceContent(string(resp.Body), s.PlusAsSpace)
	if err != nil {
		res.Error = err.Error()
		fallback, _ := cached()
		return fallback, res
	}
	real := rawNodeCount(nodes)
	// A successful empty response is meaningful; only non-empty sharp drops can use protection.
	if s.NodeCache && real > 0 && suspiciousDrop(s.KnownNodes, real) {
		res.Error = "node_count_drop_rejected"
		if fallback, ok := cached(); ok {
			return fallback, res
		}
	}
	if s.NodeCache && cache != nil {
		if old, ok := cache.Load(ctx, s.ID); real > 0 && ok && suspiciousDrop(rawNodeCount(old), real) {
			res.Error, res.FromCache, res.Nodes = "node_count_drop_rejected", true, rawNodeCount(old)
			processed := process(old)
			return processed, res
		}
		cache.Store(ctx, s.ID, nodes)
	}
	res.Nodes = real
	processed := process(nodes)
	return processed, res
}

func SourceUserAgent(s Source, client string) string {
	if ua := strings.TrimSpace(s.UserAgent); ua != "" {
		return ua
	}
	return sub.GetProcessedUserAgent(client, s.URL)
}
