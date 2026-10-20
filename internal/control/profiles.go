package control

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/imzyb/MiGate/ent"
	pipeline "github.com/imzyb/MiGate/services/subscriptions"
	"math"
	"strings"
	"time"
)

func (s *Server) validateProfileReferences(ctx context.Context, data map[string]any) error {
	for key, kind := range map[string]string{"source_ids": "sources", "node_ids": "nodes", "operator_ids": "operators", "user_ids": "users", "template_ids": "templates"} {
		value, exists := data[key]
		if !exists {
			continue
		}
		ids, ok := value.([]any)
		if !ok || len(ids) > 500 || key == "operator_ids" && len(ids) > 100 {
			return fmt.Errorf("invalid reference list")
		}
		seen := map[int]bool{}
		for _, value := range ids {
			n, ok := value.(float64)
			if !ok || n <= 0 || n > math.MaxInt32 || math.Trunc(n) != n || seen[int(n)] {
				return fmt.Errorf("invalid reference ID")
			}
			id := int(n)
			seen[id] = true
			if kind == "users" {
				if _, err := s.Store.User.Get(ctx, id); err != nil {
					return fmt.Errorf("user reference missing")
				}
			} else {
				r, err := s.Store.Resource.Get(ctx, id)
				if err != nil || r.Kind != kind {
					return fmt.Errorf("resource reference missing")
				}
			}
		}
	}
	return nil
}

func (s *Server) profileOperators(ctx context.Context, data map[string]any) ([]pipeline.Operator, error) {
	result := []pipeline.Operator{}
	if ids, ok := data["operator_ids"].([]any); ok {
		if len(ids) > 100 {
			return nil, fmt.Errorf("operator reference limit")
		}
		for _, id := range ids {
			r, err := s.Store.Resource.Get(ctx, int(number(id)))
			if err != nil || r.Kind != "operators" {
				return nil, fmt.Errorf("operator reference missing")
			}
			raw, err := json.Marshal(r.Data["operators"])
			if err != nil {
				return nil, err
			}
			var ops []pipeline.Operator
			if err = json.Unmarshal(raw, &ops); err != nil {
				return nil, err
			}
			result = append(result, ops...)
		}
	}
	if value, ok := data["operators"]; ok && value != nil {
		raw, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		var ops []pipeline.Operator
		if err = json.Unmarshal(raw, &ops); err != nil {
			return nil, err
		}
		result = append(result, ops...)
	}
	if len(result) > 100 {
		return nil, fmt.Errorf("operator count limit")
	}
	if len(result) == 0 && data["skip_default_operators"] != true {
		defaults, e := s.readSubscriptionDefaults(ctx)
		if e != nil {
			return nil, e
		}
		result = defaults.Operators
	}
	return result, nil
}

// Admin previews external inventories separately from metered user grants.
// Merely importing an upstream subscription never grants a user access to it.
func (s *Server) previewProfile(c *gin.Context) {
	var req struct {
		ID     int    `json:"id"`
		Target string `json:"target"`
	}
	if !bind(c, &req) {
		return
	}
	result, status, code := s.renderProfile(c.Request.Context(), req.ID, req.Target)
	if code != "" {
		failure(c, status, code)
		return
	}
	c.JSON(status, result)
}

func (s *Server) renderProfile(ctx context.Context, id int, target string) (gin.H, int, string) {
	profile, err := s.Store.Resource.Get(ctx, id)
	if err != nil || profile.Kind != "profiles" {
		return nil, 404, "profile_not_found"
	}
	links := []string{}
	metadata := []pipeline.NodeMetadata{}
	warnings := []string{}
	usageSources := []*ent.Resource{}
	for _, reference := range [][2]string{{"source_ids", "sources"}, {"node_ids", "nodes"}} {
		field, kind := reference[0], reference[1]
		ids, _ := profile.Data[field].([]any)
		if len(ids) > 500 {
			return nil, 400, "resource_reference_limit"
		}
		for _, id := range ids {
			var r *ent.Resource
			r, err = s.Store.Resource.Get(ctx, int(number(id)))
			if err != nil || r.Kind != kind {
				return nil, 422, "profile_resource_missing"
			}
			if kind == "nodes" {
				links = append(links, text(r.Data["url"]))
				metadata = append(metadata, pipeline.NodeMetadata{Manual: true, Group: text(r.Data["group"]), Provider: text(r.Data["name"])})
			} else {
				cached, stale, cacheErr := sourceLinks(r.Data, time.Now())
				if cacheErr != nil {
					return nil, 422, "source_refresh_required"
				}
				if stale {
					warnings = append(warnings, fmt.Sprintf("source:%d:stale_cache", r.ID))
				}
				links = append(links, cached...)
				for range cached {
					metadata = append(metadata, pipeline.NodeMetadata{Group: text(r.Data["group"]), Provider: text(r.Data["name"])})
				}
				usageSources = append(usageSources, r)
			}
		}
	}
	naming, err := s.profileNaming(ctx, profile.Data)
	if err != nil {
		return nil, 422, "subscription_naming_unavailable"
	}
	links, err = applyInventoryPrefixes(links, metadata, naming)
	if err != nil {
		return nil, 422, "profile_pipeline_invalid"
	}
	operators, err := s.profileOperators(ctx, profile.Data)
	if err == nil {
		links, err = pipeline.RunWithMetadata(links, operators, target, metadata)
	}
	if err != nil {
		return nil, 422, "profile_pipeline_invalid"
	}
	var output []byte
	template, err := s.profileTemplate(ctx, profile.Data, target)
	if err != nil {
		return nil, 422, "output_template_invalid"
	}
	if template != nil {
		output, err = renderOutputTemplate(links, *template)
	} else {
		switch target {
		case "egern":
			output, err = egernOutput(links)
		case "surge", "loon", "quanx":
			output, err = iniOutput(links, target)
		case "clash", "mihomo":
			output, err = clashOutput(links)
		case "singbox":
			output, err = singboxOutput(links)
		case "", "links":
			output = []byte(strings.Join(links, "\n"))
		case "base64":
			output = []byte(base64.StdEncoding.EncodeToString([]byte(strings.Join(links, "\n"))))
		default:
			return nil, 400, "unsupported_target"
		}
	}
	if err != nil {
		return nil, 422, "format_conversion_failed"
	}
	return gin.H{"links": links, "output": string(output), "node_count": len(links), "warnings": warnings, "upstream_usage": mergedSourceUsage(usageSources, time.Now())}, 200, ""
}
