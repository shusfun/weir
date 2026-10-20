package control

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"time"

	"github.com/imzyb/MiGate/ent"
	"github.com/imzyb/MiGate/ent/resource"
	"github.com/imzyb/MiGate/internal/subscription"
)

var sourceCacheFields = []string{"links", "node_count", "last_update", "last_attempt", "last_error", "next_refresh", "upstream_usage", "last_rejection"}

func validateSource(data map[string]any) error {
	for _, key := range []string{"filter_include", "filter_exclude"} {
		if value, exists := data[key]; exists {
			if _, ok := value.(string); !ok {
				return errors.New("invalid source filter type")
			}
		}
	}
	if err := subscription.ValidateSourceFilters(text(data["filter_include"]), text(data["filter_exclude"])); err != nil {
		return err
	}
	for _, key := range []string{"enabled", "allow_stale", "plus_as_space", "node_cache", "exclude_traffic"} {
		if value, exists := data[key]; exists {
			if _, ok := value.(bool); !ok {
				return errors.New("invalid source boolean option")
			}
		}
	}
	if value, exists := data["user_agent"]; exists {
		ua, ok := value.(string)
		if !ok || subscription.ValidateUserAgent(ua) != nil {
			return errors.New("invalid source user agent")
		}
	}
	for _, key := range []string{"website", "notes", "group"} {
		if value, exists := data[key]; exists {
			s, ok := value.(string)
			if !ok || len(s) > 2000 || key == "group" && (!catalogText(s, 120, false)) {
				return errors.New("invalid source metadata")
			}
		}
	}
	if website := text(data["website"]); website != "" {
		w, e := url.Parse(website)
		if e != nil || w.Hostname() == "" || w.User != nil || (w.Scheme != "https" && w.Scheme != "http") {
			return errors.New("invalid source website")
		}
	}
	u, err := url.Parse(text(data["url"]))
	if err != nil || u.Hostname() == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return errors.New("invalid source URL")
	}
	for key, limits := range map[string][2]float64{"refresh_seconds": {60, 86400}, "max_stale_seconds": {60, 604800}} {
		if value, ok := data[key]; ok {
			n := number(value)
			if n < limits[0] || n > limits[1] {
				return errors.New("invalid source interval")
			}
		}
	}
	return nil
}

func sourceInterval(data map[string]any) time.Duration {
	n := number(data["refresh_seconds"])
	if n < 60 || n > 86400 {
		n = 3600
	}
	return time.Duration(n) * time.Second
}

type sourceFetch func(context.Context, string, string) (subscription.FetchedInventory, error)

func fetchSource(ctx context.Context, address, userAgent string) (subscription.FetchedInventory, error) {
	// HTTPFetcher revalidates redirects and pins a public resolved dial target.
	return (subscription.HTTPFetcher{UserAgent: userAgent}).FetchInventory(ctx, address, false)
}

func (s *Server) refreshSourceCache(ctx context.Context, id int, fetch sourceFetch) (*ent.Resource, error) {
	s.sourceMu.Lock()
	defer s.sourceMu.Unlock()
	r, err := s.Store.Resource.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if r.Kind != "sources" {
		return nil, &ent.NotFoundError{}
	}
	now := time.Now().UTC()
	r.Data["last_attempt"] = now.Format(time.RFC3339)
	delete(r.Data, "last_rejection")
	response, fetchErr := fetch(ctx, sourceRequestAddress(r.Data, now), text(r.Data["user_agent"]))
	if fetchErr == nil {
		plusAsSpace, _ := r.Data["plus_as_space"].(bool)
		links, decodeErr := subscription.DecodeInventoryWithNameOptions(response.Body, plusAsSpace)
		parsed, parseErr := subscription.ParseLinks(links)
		if decodeErr != nil || parseErr != nil || len(links) == 0 || len(links) > 10000 || len(parsed.Skipped) > 0 {
			fetchErr = errors.New("source_parse_failed")
		} else {
			if r.Data["node_cache"] == true {
				parsed.Nodes = realSourceNodes(parsed.Nodes)
				previous := int(number(r.Data["node_count"]))
				if len(parsed.Nodes) == 0 || suspiciousSourceDrop(previous, len(parsed.Nodes)) {
					r.Data["last_rejection"] = "suspicious_node_drop"
					fetchErr = errors.New("source_protection_rejected")
				}
			}
			if fetchErr == nil {
				out := make([]string, len(parsed.Nodes))
				for i, node := range parsed.Nodes {
					out[i] = node.RawLink
				}
				r.Data["links"] = out
				r.Data["node_count"] = len(out)
				r.Data["last_update"] = now.Format(time.RFC3339)
				usage := response.Usage
				if usage.Status == "" {
					usage.Status = "absent"
				}
				r.Data["upstream_usage"] = usage
				delete(r.Data, "last_rejection")
			}
		}
	}
	interval := sourceInterval(r.Data)
	if fetchErr != nil {
		// Keep the last successful inventory, but never label it fresh.
		r.Data["last_error"] = "refresh_failed"
		if interval > 5*time.Minute {
			interval = 5 * time.Minute
		}
	} else {
		r.Data["last_error"] = ""
	}
	r.Data["next_refresh"] = now.Add(interval).Format(time.RFC3339)
	saved, err := s.Store.Resource.UpdateOne(r).SetData(r.Data).Save(ctx)
	if err != nil {
		return nil, err
	}
	return saved, fetchErr
}

func sourceLinks(data map[string]any, now time.Time) ([]string, bool, error) {
	if enabled, ok := data["enabled"].(bool); ok && !enabled {
		return nil, false, errors.New("source disabled")
	}
	updated, err := time.Parse(time.RFC3339, text(data["last_update"]))
	if err != nil || updated.After(now.Add(time.Minute)) {
		return nil, false, errors.New("source refresh required")
	}
	age := now.Sub(updated)
	stale := text(data["last_error"]) != "" || age > sourceInterval(data)
	if stale {
		allowed, _ := data["allow_stale"].(bool)
		allowed = allowed || data["node_cache"] == true
		maxAge := number(data["max_stale_seconds"])
		if maxAge < 60 || maxAge > 604800 {
			maxAge = 86400
		}
		if !allowed || age > time.Duration(maxAge)*time.Second {
			return nil, true, errors.New("source cache stale")
		}
	}
	raw, err := json.Marshal(data["links"])
	if err != nil {
		return nil, stale, err
	}
	var links []string
	if err = json.Unmarshal(raw, &links); err != nil || len(links) == 0 {
		return nil, stale, errors.New("invalid source cache")
	}
	filtered, err := subscription.FilterSourceLinks(links, text(data["filter_include"]), text(data["filter_exclude"]))
	return filtered, stale, err
}

func (s *Server) sourceWorker(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.configMu.RLock()
			if s.Config.Installed {
				s.refreshDueSources(ctx)
				s.expirePoolTasks(ctx)
				s.syncDuePools(ctx)
			}
			s.configMu.RUnlock()
		}
	}
}

func (s *Server) refreshDueSources(ctx context.Context) {
	// Cursor pagination avoids permanently starving inventories after the first page.
	after := 0
	for ctx.Err() == nil {
		items, err := s.Store.Resource.Query().Where(resource.KindEQ("sources"), resource.IDGT(after)).Order(ent.Asc("id")).Limit(100).All(ctx)
		if err != nil || len(items) == 0 {
			return
		}
		for _, r := range items {
			after = r.ID
			if enabled, ok := r.Data["enabled"].(bool); ok && !enabled {
				continue
			}
			next, _ := time.Parse(time.RFC3339, text(r.Data["next_refresh"]))
			if time.Now().Before(next) {
				continue
			}
			callCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
			s.refreshSourceCache(callCtx, r.ID, fetchSource)
			cancel()
		}
	}
}
