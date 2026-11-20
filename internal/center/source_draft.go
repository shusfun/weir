package center

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/subgroup"
)

// 先试拉再保存：试拉用表单里选的 Gate 走同一条选路与检测流程，但不写库；结果放进 KV 十分钟，
// 保存时试拉参数未变，就直接用这次结果，不再重拉。

const sourceInspectionTTL = 10 * time.Minute

type sourceDraftRequest struct {
	URL         string `json:"url"`
	UserAgent   string `json:"user_agent"`
	PlusAsSpace bool   `json:"plus_as_space"`
	GateID      int    `json:"gate_id"`
}

type storedInspection struct {
	URL         string                `json:"url"`
	UserAgent   string                `json:"user_agent"`
	PlusAsSpace bool                  `json:"plus_as_space"`
	GateID      int                   `json:"gate_id"`
	Started     time.Time             `json:"started"`
	Raw         []string              `json:"raw"`
	Result      subgroup.SourceResult `json:"result"`
	Health      sourceHealth          `json:"health"`
	Summary     sourceHealthSummary   `json:"summary"`
}

func sourceInspectionKey(id string) string { return "source-inspection:" + id }

func (s *Server) inspectSourceDraft(c *gin.Context) {
	var req sourceDraftRequest
	if !bind(c, &req) {
		return
	}
	req.URL, req.UserAgent = strings.TrimSpace(req.URL), strings.TrimSpace(req.UserAgent)
	if _, err := subgroup.CheckURL(req.URL); err != nil && !(s.AllowPrivateSources && err.Error() == "source_url_private") {
		failure(c, 400, err.Error())
		return
	}
	ctx := c.Request.Context()
	finish := operationStage(ctx, "subscription.source.inspect", "draft", true, "gate_id", req.GateID)
	route, err := s.fetchRouteFor(ctx, req.GateID)
	if err != nil {
		finish(err)
		failInternal(c, 503, "query_failed", err)
		return
	}
	jobTitle(ctx, "试拉订阅源")
	src := subgroup.Source{URL: req.URL, UserAgent: req.UserAgent, PlusAsSpace: req.PlusAsSpace, AcceptEmpty: true}
	fetcher := subgroup.Fetcher{AllowPrivate: s.AllowPrivateSources, Fetch: func(ctx context.Context, raw, ua string) (subgroup.Response, error) {
		return s.fetchSubscriptionRouted(ctx, raw, ua, route)
	}}
	_, results := subgroup.Combine(ctx, fetcher, nil, []subgroup.Source{src}, nil, subgroup.Options{UserAgent: "clash-meta/2.5.0"})
	r := results[0]
	if r.Error != "" || r.RawNodes == nil {
		finish(errors.New(firstNonEmpty(r.Error, "source_no_supported_nodes")))
		c.JSON(200, gin.H{"ok": false, "error": firstNonEmpty(r.Error, "没有拉到节点"), "location": r.Location, "route_reason": r.RouteReason})
		return
	}
	in, err := s.probeSourceNodes(ctx, 0, r.RawNodes, r.Location, nil)
	if err != nil {
		finish(err)
		failInternal(c, 500, "inspect_failed", err)
		return
	}
	id := randomHex(12)
	blob, _ := json.Marshal(storedInspection{URL: req.URL, UserAgent: req.UserAgent, PlusAsSpace: req.PlusAsSpace, GateID: req.GateID, Started: r.Started, Raw: r.RawNodes, Result: r, Health: in.Health, Summary: in.Summary})
	if err = s.Store.KV.Set(ctx, sourceInspectionKey(id), string(blob), sourceInspectionTTL); err != nil {
		finish(err)
		failInternal(c, 503, "inspect_store_failed", err)
		return
	}
	finish(nil)
	c.JSON(200, gin.H{"ok": true, "inspection_id": id, "location": r.Location, "location_label": s.locationLabel(ctx, r.Location), "route_reason": r.RouteReason,
		"user_info": parseUserInfo(r.UserInfo), "title": r.Title, "summary": in.Summary, "nodes": in.Nodes, "gate_id": locationGate(r.Location)})
}

// applyInspection 在保存后套用仍与配置及拉取代际匹配的试拉结果。
func (s *Server) applyInspection(ctx context.Context, id string, saved *ent.SubSource) (bool, error) {
	raw, ok, err := s.Store.KV.Get(ctx, sourceInspectionKey(id))
	if err != nil || !ok {
		return false, err
	}
	var in storedInspection
	if err = json.Unmarshal([]byte(raw), &in); err != nil {
		return false, nil
	}
	if in.URL != saved.URL || in.UserAgent != saved.UserAgent || in.PlusAsSpace != saved.PlusAsSpace {
		return false, nil
	}
	if in.GateID != saved.GateID && !(in.GateID == 0 && locationGate(in.Result.Location) == saved.GateID) {
		return false, nil
	}
	var applied bool
	err = s.Store.Lock.Do(ctx, "subscription-source:"+itoa(saved.ID), func(ctx context.Context) error {
		current, err := s.Store.Ent.SubSource.Get(ctx, saved.ID)
		if err != nil {
			return err
		}
		src := toSource(current)
		if src.ConfigVersion != toSource(saved).ConfigVersion {
			return nil
		}
		r := in.Result
		r.Started = in.Started
		last, ok, err := s.catalogRefreshStarted(ctx, saved.ID)
		if err != nil || r.Started.IsZero() || ok && !r.Started.After(last) {
			return err
		}
		r.SourceID, r.ConfigVersion, r.RawNodes = saved.ID, src.ConfigVersion, in.Raw
		r.UserAgent = subgroup.SourceUserAgent(src, "clash-meta/2.5.0")
		if err = s.recordSourceResult(ctx, current, r); err != nil {
			return err
		}
		in.Health.SourceID = saved.ID
		if err = s.saveInspection(ctx, saved.ID, sourceInspection{Health: in.Health, Summary: in.Summary}); err != nil {
			return err
		}
		applied = true
		return nil
	})
	if applied {
		_ = s.Store.KV.Del(ctx, sourceInspectionKey(id))
	}
	return applied, err
}

func locationGate(location string) int {
	if id, ok := strings.CutPrefix(location, "gate:"); ok {
		return max(atoiOr(id), 0)
	}
	return 0
}

// locationLabel 把拉取位置写成页面上的说法。
func (s *Server) locationLabel(ctx context.Context, location string) string {
	switch {
	case location == "center-fallback":
		return "中心直接下载"
	case strings.HasPrefix(location, "gate:"):
		return s.gateLabel(ctx, locationGate(location))
	case location == "":
		return "未知"
	default:
		return location
	}
}
