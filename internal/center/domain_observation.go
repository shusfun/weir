package center

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/node"
	"github.com/shusfun/weir/internal/store/ent/resource"
	"github.com/shusfun/weir/packages/contracts"
)

const domainBaselineKind = "domain-observed-baseline"
const domainDailyKind = "domain-observed-daily"
const domainCoverageKind = "domain-observed-coverage"
const domainDecisionKind = "domain-observed-decision"

type observedDomainData struct {
	Day        string    `json:"day"`
	Domain     string    `json:"domain"`
	Engine     string    `json:"engine"`
	NodeID     int64     `json:"node_id"`
	Bytes      int64     `json:"bytes,string"`
	Users      *int      `json:"users"`
	ObservedAt time.Time `json:"observed_at"`
	Epoch      string    `json:"epoch,omitempty"`
}

func domainSampleKey(gid int, d contracts.DomainObservation) string {
	return fmt.Sprintf("%s:%d:%s:%d:%s", d.Day, gid, d.Engine, d.NodeID, d.Domain)
}

func validDomainReport(rep contracts.MeterReport, now time.Time) bool {
	if len(rep.Domains) > 4000 || len(rep.DomainCoverage) > 3 {
		return false
	}
	seen := map[string]bool{}
	for _, d := range rep.Domains {
		day, err := time.Parse("2006-01-02", d.Day)
		key := domainSampleKey(0, d)
		if err != nil || day.After(now.UTC()) || day.Before(now.UTC().AddDate(0, 0, -45)) || d.Domain == "" || contracts.ObservedDomain(d.Domain) != d.Domain || d.NodeID < 0 || d.Bytes < 0 || d.Users != nil && (*d.Users < 1 || *d.Users > 100000) || (d.Engine != "sing-box" && d.Engine != "mihomo") || d.ObservedAt.IsZero() || d.ObservedAt.After(now.Add(5*time.Minute)) || d.ObservedAt.UTC().Format("2006-01-02") != d.Day || seen[key] {
			return false
		}
		seen[key] = true
	}
	seen = map[string]bool{}
	for _, d := range rep.DomainCoverage {
		if (d.Engine != "sing-box" && d.Engine != "mihomo" && d.Engine != "xray") || seen[d.Engine] || (d.Status != "partial" && d.Status != "unavailable" && d.Status != "unsupported") || len(d.Reason) > 80 || d.ObservedAt.IsZero() || d.ObservedAt.After(now.Add(5*time.Minute)) {
			return false
		}
		seen[d.Engine] = true
	}
	return true
}

func putObservationResource(ctx context.Context, db *ent.Client, kind, key string, gid int, value any) error {
	row, err := db.Resource.Query().Where(resource.KindEQ(kind), resource.KeyEQ(key)).Only(ctx)
	if ent.IsNotFound(err) {
		return db.Resource.Create().SetKind(kind).SetKey(key).SetOwnerID(gid).SetData(toMap(value)).Exec(ctx)
	}
	if err != nil {
		return err
	}
	return row.Update().SetData(toMap(value)).Exec(ctx)
}

func (s *Server) applyDomainObservations(ctx context.Context, g *ent.Gate, rep contracts.MeterReport, now time.Time) (err error) {
	finish := operationStage(ctx, "traffic.domains.report", "gate_id", g.ID, "samples", len(rep.Domains))
	defer func() { finish(err) }()
	for _, d := range rep.Domains {
		if d.NodeID > 0 {
			exists, e := s.Store.Ent.Node.Query().Where(node.IDEQ(int(d.NodeID)), node.GateIDEQ(g.ID), node.KernelEQ(d.Engine)).Exist(ctx)
			if e != nil {
				return e
			}
			if !exists {
				continue
			}
		} else {
			// Auxiliary metadata cannot attribute its original node. Only a
			// configured observer on this Gate may report that bounded subset.
			row, e := s.Store.Ent.Resource.Query().Where(resource.KindEQ(domainObserverKind), resource.KeyEQ(itoa(g.ID))).Only(ctx)
			if ent.IsNotFound(e) {
				continue
			}
			if e != nil {
				return e
			}
			items, _ := row.Data["items"].(map[string]any)
			if items[d.Engine] == nil {
				continue
			}
		}
		key := domainSampleKey(g.ID, d)
		row, e := s.Store.Ent.Resource.Query().Where(resource.KindEQ(domainBaselineKind), resource.KeyEQ(key)).Only(ctx)
		if e != nil && !ent.IsNotFound(e) {
			return e
		}
		var old observedDomainData
		if row != nil {
			if e = decodeResource(row, &old); e != nil {
				return e
			}
		}
		delta := d.Bytes
		if old.Epoch == rep.Epoch {
			if d.Bytes < old.Bytes || d.ObservedAt.Before(old.ObservedAt) {
				return errors.New("domain observation regressed")
			}
			delta -= old.Bytes
		}
		baseline := observedDomainData{Day: d.Day, Domain: d.Domain, Engine: d.Engine, NodeID: d.NodeID, Bytes: d.Bytes, Users: d.Users, ObservedAt: d.ObservedAt, Epoch: rep.Epoch}
		if e = putObservationResource(ctx, s.Store.Ent, domainBaselineKind, key, g.ID, baseline); e != nil {
			return e
		}
		row, e = s.Store.Ent.Resource.Query().Where(resource.KindEQ(domainDailyKind), resource.KeyEQ(key)).Only(ctx)
		if e != nil && !ent.IsNotFound(e) {
			return e
		}
		daily := baseline
		daily.Epoch = ""
		daily.Bytes = 0
		if row != nil {
			if e = decodeResource(row, &daily); e != nil {
				return e
			}
		}
		if delta > math.MaxInt64-daily.Bytes {
			return errors.New("domain observation overflow")
		}
		daily.Bytes += delta
		if d.Users != nil && (daily.Users == nil || *d.Users > *daily.Users) {
			daily.Users = d.Users
		}
		if d.ObservedAt.After(daily.ObservedAt) {
			daily.ObservedAt = d.ObservedAt
		}
		if e = putObservationResource(ctx, s.Store.Ent, domainDailyKind, key, g.ID, daily); e != nil {
			return e
		}
	}
	for _, coverage := range rep.DomainCoverage {
		key := itoa(g.ID) + ":" + coverage.Engine
		if err = putObservationResource(ctx, s.Store.Ent, domainCoverageKind, key, g.ID, struct {
			contracts.DomainCoverage
			Revision int64 `json:"revision"`
		}{coverage, rep.Revision}); err != nil {
			return err
		}
	}
	return nil
}

type domainDecision struct {
	Domain  string    `json:"domain"`
	Ignored bool      `json:"ignored"`
	UserID  int       `json:"user_id,omitempty"`
	At      time.Time `json:"at"`
}

type domainCandidate struct {
	Domain     string           `json:"domain"`
	Bytes      int64            `json:"bytes"`
	Users      *int             `json:"users"`
	ObservedAt time.Time        `json:"observed_at"`
	Ignored    bool             `json:"ignored"`
	Nodes      []gin.H          `json:"nodes"`
	Days       []gin.H          `json:"days"`
	Decisions  []domainDecision `json:"decisions"`
}

func (s *Server) domainRoutes(admin *gin.RouterGroup) {
	admin.GET("/domain-candidates", s.listDomainCandidates)
	admin.POST("/domain-candidates/:domain/ignore", s.ignoreDomainCandidate)
	admin.POST("/domain-candidates/:domain/direct", s.asJob("加入订阅直连规则", "已取消，清单保持原样", s.directDomainCandidate))
}

func (s *Server) listDomainCandidates(c *gin.Context) {
	ctx := c.Request.Context()
	now := time.Now().UTC()
	finish := operationStage(ctx, "traffic.domains.aggregate")
	q := s.Store.Ent.Resource.Query().Where(resource.KindEQ(domainDailyKind), resource.KeyGTE(now.AddDate(0, 0, -29).Format("2006-01-02")+":"))
	if raw := c.Query("gate"); raw != "" {
		gid, err := strconv.Atoi(raw)
		if err != nil || gid <= 0 {
			failure(c, 400, "invalid_id")
			return
		}
		q.Where(resource.OwnerIDEQ(gid))
	}
	if raw := c.Query("domain"); raw != "" {
		if contracts.ObservedDomain(raw) != raw {
			failure(c, 400, "invalid_domain")
			return
		}
		q.Where(resource.KeyHasSuffix(":" + raw))
	}
	rows, err := q.All(ctx)
	if err != nil {
		finish(err)
		failInternal(c, 500, "query_failed", err)
		return
	}
	items := map[string]*domainCandidate{}
	days := map[string]map[string]int64{}
	nodes := map[string]map[string]int64{}
	for _, row := range rows {
		var d observedDomainData
		if err = decodeResource(row, &d); err != nil {
			finish(err)
			failInternal(c, 500, "query_failed", err)
			return
		}
		v := items[d.Domain]
		if v == nil {
			v = &domainCandidate{Domain: d.Domain, Nodes: []gin.H{}, Days: []gin.H{}, Decisions: []domainDecision{}}
			items[d.Domain] = v
			days[d.Domain] = map[string]int64{}
			nodes[d.Domain] = map[string]int64{}
		}
		if v.Bytes > math.MaxInt64-d.Bytes {
			err = errors.New("domain aggregate overflow")
			finish(err)
			failInternal(c, 500, "query_failed", err)
			return
		}
		v.Bytes += d.Bytes
		if d.Users != nil && (v.Users == nil || *d.Users > *v.Users) {
			v.Users = d.Users
		}
		if d.ObservedAt.After(v.ObservedAt) {
			v.ObservedAt = d.ObservedAt
		}
		days[d.Domain][d.Day] += d.Bytes
		nodes[d.Domain][itoa(row.OwnerID)+":"+strconv.FormatInt(d.NodeID, 10)] += d.Bytes
	}
	decisions, err := s.Store.Ent.Resource.Query().Where(resource.KindEQ(domainDecisionKind)).All(ctx)
	if err != nil {
		finish(err)
		failInternal(c, 500, "query_failed", err)
		return
	}
	for _, row := range decisions {
		var d domainDecision
		if err = decodeResource(row, &d); err != nil {
			finish(err)
			failInternal(c, 500, "query_failed", err)
			return
		}
		if v := items[d.Domain]; v != nil {
			if d.UserID == 0 {
				v.Ignored = d.Ignored
			} else {
				v.Decisions = append(v.Decisions, d)
			}
		}
	}
	list := []*domainCandidate{}
	for domain, v := range items {
		for day, b := range days[domain] {
			v.Days = append(v.Days, gin.H{"day": day, "bytes": b})
		}
		sort.Slice(v.Days, func(i, j int) bool { return v.Days[i]["day"].(string) < v.Days[j]["day"].(string) })
		for key, b := range nodes[domain] {
			var gid, nid int
			fmt.Sscanf(key, "%d:%d", &gid, &nid)
			v.Nodes = append(v.Nodes, gin.H{"gate_id": gid, "node_id": nid, "bytes": b})
		}
		sort.Slice(v.Nodes, func(i, j int) bool { return v.Nodes[i]["bytes"].(int64) > v.Nodes[j]["bytes"].(int64) })
		if len(v.Nodes) > 5 {
			v.Nodes = v.Nodes[:5]
		}
		list = append(list, v)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Bytes == list[j].Bytes {
			return list[i].Domain < list[j].Domain
		}
		return list[i].Bytes > list[j].Bytes
	})
	total := len(list)
	if len(list) > 100 {
		list = list[:100]
	}
	coverageQuery := s.Store.Ent.Resource.Query().Where(resource.KindEQ(domainCoverageKind))
	if raw := c.Query("gate"); raw != "" {
		gid, _ := strconv.Atoi(raw)
		coverageQuery.Where(resource.OwnerIDEQ(gid))
	}
	coverageRows, err := coverageQuery.All(ctx)
	if err != nil {
		finish(err)
		failInternal(c, 500, "query_failed", err)
		return
	}
	coverage := []gin.H{}
	seenCoverage := map[string]bool{}
	for _, row := range coverageRows {
		var v struct {
			contracts.DomainCoverage
			Revision int64 `json:"revision"`
		}
		if err = decodeResource(row, &v); err != nil {
			finish(err)
			failInternal(c, 500, "query_failed", err)
			return
		}
		g, e := s.Store.Ent.Gate.Get(ctx, row.OwnerID)
		if ent.IsNotFound(e) {
			continue
		}
		if e != nil {
			finish(e)
			failInternal(c, 500, "query_failed", e)
			return
		}
		seenCoverage[itoa(row.OwnerID)+":"+v.Engine] = true
		coverage = append(coverage, gin.H{"gate_id": row.OwnerID, "engine": v.Engine, "status": v.Status, "reason": domainCoverageReason(v.Reason), "observed_at": v.ObservedAt, "stale": now.Sub(v.ObservedAt) > 2*time.Minute || v.Revision != g.AppliedRevision})
	}
	gates, err := s.Store.Ent.Gate.Query().All(ctx)
	if err != nil {
		finish(err)
		failInternal(c, 500, "query_failed", err)
		return
	}
	for _, g := range gates {
		if raw := c.Query("gate"); raw != "" && raw != itoa(g.ID) {
			continue
		}
		engines, err := s.Store.Ent.Node.Query().Where(node.GateIDEQ(g.ID)).Select(node.FieldKernel).Strings(ctx)
		if err != nil {
			finish(err)
			failInternal(c, 500, "query_failed", err)
			return
		}
		for _, engine := range engines {
			key := itoa(g.ID) + ":" + engine
			if seenCoverage[key] {
				continue
			}
			seenCoverage[key] = true
			reason := "此内核尚无域名观测回执，请核对 Gate 与驱动能力"
			if engine == "xray" {
				reason = "Xray 不提供原生域名字节，流量未知"
			}
			coverage = append(coverage, gin.H{"gate_id": g.ID, "engine": engine, "status": "unsupported", "reason": reason, "observed_at": nil, "stale": false})
		}
	}
	sort.Slice(coverage, func(i, j int) bool {
		a, b := coverage[i], coverage[j]
		if a["gate_id"] == b["gate_id"] {
			return a["engine"].(string) < b["engine"].(string)
		}
		return a["gate_id"].(int) < b["gate_id"].(int)
	})
	finish(nil)
	c.JSON(200, gin.H{"items": list, "total": total, "coverage": coverage, "reason": "近 30 天连接快照中的已观测流量，可能遗漏短连接；涉及人数是已识别人数下限。Xray 原生域名字节、机场直用及无法归属的连接为未知，不参与额度扣费。趋势变化不能全部归因于规则调整。"})
}

func domainCoverageReason(reason string) string {
	switch reason {
	case "connection_snapshots":
		return "连接快照可能遗漏短连接；只统计已识别的国内目标"
	case "restart_baseline":
		return "进程重启后建立基线，已有连接此前流量未计入"
	case "domain_limit":
		return "达到聚合容量或人数识别上限，部分观测未计入"
	case "country_data_unavailable":
		return "国内 IP 数据缺失或过期，已停止采集"
	case "native_api_rejected":
		return "内核拒绝观测请求"
	case "snapshot_limit":
		return "内核快照超过读取上限，当前样本未计入"
	case "metadata_unavailable":
		return "内核快照未提供可识别的节点或目标信息，相关流量未知"
	case "native_snapshot_invalid":
		return "内核观测格式不受支持"
	default:
		return "原生观测暂不可用，流量未知"
	}
}

func (s *Server) candidateDomain(c *gin.Context) (string, bool) {
	domain := contracts.ObservedDomain(c.Param("domain"))
	if domain == "" || domain != c.Param("domain") {
		failure(c, 400, "invalid_domain")
		return "", false
	}
	exists, err := s.Store.Ent.Resource.Query().Where(resource.KindEQ(domainDailyKind), resource.KeyHasSuffix(":"+domain), resource.KeyGTE(time.Now().UTC().AddDate(0, 0, -29).Format("2006-01-02")+":")).Exist(c.Request.Context())
	if err != nil {
		failInternal(c, 500, "query_failed", err)
		return "", false
	}
	if !exists {
		failure(c, 404, "domain_candidate_not_found")
		return "", false
	}
	return domain, true
}

func (s *Server) ignoreDomainCandidate(c *gin.Context) {
	domain, ok := s.candidateDomain(c)
	if !ok {
		return
	}
	var req struct {
		Ignored bool `json:"ignored"`
	}
	if !bind(c, &req) {
		return
	}
	if dryRun(c, dryResult{Preview: []string{"修改国内域名候选的忽略状态，不改变订阅规则"}}) {
		return
	}
	ctx := c.Request.Context()
	finish := operationStage(ctx, "traffic.domains.ignore")
	err := s.Store.Lock.Do(ctx, "domain-decision:"+domain, func(ctx context.Context) error {
		return putObservationResource(ctx, s.Store.Ent, domainDecisionKind, domain, 0, domainDecision{Domain: domain, Ignored: req.Ignored, At: time.Now().UTC()})
	})
	finish(err)
	if err != nil {
		failInternal(c, 500, "update_failed", err)
		return
	}
	s.audit(c, "domain.ignore", domain, "ok")
	c.JSON(200, gin.H{"ignored": req.Ignored})
}

func (s *Server) directDomainCandidate(c *gin.Context) {
	domain, ok := s.candidateDomain(c)
	if !ok {
		return
	}
	var req struct {
		UserID    int  `json:"user_id"`
		Revision  *int `json:"revision"`
		Confirmed bool `json:"confirmed"`
	}
	if !bind(c, &req) {
		return
	}
	if !req.Confirmed || req.UserID <= 0 {
		failure(c, 400, "confirmation_required")
		return
	}
	u, err := s.Store.Ent.User.Get(c.Request.Context(), req.UserID)
	if ent.IsNotFound(err) {
		failure(c, 404, "user_not_found")
		return
	}
	if err != nil {
		failInternal(c, 500, "query_failed", err)
		return
	}
	pol := loadPolicy(u)
	entries := append([]contracts.CustomEntry(nil), pol.Custom.Entries...)
	for _, e := range entries {
		if e.Kind == "domain_suffix" && e.Value == domain {
			failure(c, 409, "domain_rule_exists")
			return
		}
	}
	entries = append(entries, contracts.CustomEntry{ID: randomHex(12), Kind: "domain_suffix", Value: domain, Target: "direct", Enabled: true})
	finish := operationStage(c.Request.Context(), "traffic.domains.direct", "user_id", u.ID)
	s.writeCustomEntries(c, u, req.Revision, entries, "", nil, func(ctx context.Context, db *ent.Client) error {
		return putObservationResource(ctx, db, domainDecisionKind, domain+":"+itoa(u.ID), 0, domainDecision{Domain: domain, UserID: u.ID, At: time.Now().UTC()})
	})
	if c.Writer.Status() >= 400 {
		finish(errors.New("domain rule rejected"))
	} else {
		finish(nil)
	}
}
