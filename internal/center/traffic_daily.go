package center

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/resource"
)

const dailyUsageKind = "traffic-daily"

func (s *Server) pruneDailyUsage(ctx context.Context, now time.Time, days int) error {
	return s.pruneResources(ctx, resource.KindIn(dailyUsageKind, ruleDailyKind, domainDailyKind, domainBaselineKind), resource.KeyLT(now.UTC().AddDate(0, 0, -days).Format("2006-01-02")+":"))
}

type dailyUsage struct {
	Day        string    `json:"day"`
	GateID     int       `json:"gate_id"`
	UserID     int       `json:"user_id"`
	NodeID     int       `json:"node_id"`
	ChainID    int       `json:"chain_id"`
	Bytes      int64     `json:"bytes,string"`
	ObservedAt time.Time `json:"observed_at"`
}

// The caller uses the meter transaction, so aggregates and acknowledged
// cumulative counters either advance together or both roll back.
func (s *Server) recordDailyUsage(ctx context.Context, cred *ent.Credential, gid int, delta int64, now time.Time) error {
	if cred.Internal || cred.ProbeParentID != nil || cred.UserID == nil {
		return nil
	}
	chain := 0
	if cred.ChainID != nil {
		chain = *cred.ChainID
	}
	v := dailyUsage{Day: now.UTC().Format("2006-01-02"), GateID: gid, UserID: *cred.UserID, NodeID: cred.NodeID, ChainID: chain, ObservedAt: now.UTC()}
	key := fmt.Sprintf("%s:%d:%d:%d:%d", v.Day, gid, v.UserID, v.NodeID, chain)
	row, err := s.Store.Ent.Resource.Query().Where(resource.KindEQ(dailyUsageKind), resource.KeyEQ(key)).Only(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return err
	}
	if row != nil {
		if err = decodeResource(row, &v); err != nil {
			return err
		}
	}
	if delta < 0 || v.Bytes > math.MaxInt64-delta {
		return fmt.Errorf("daily usage overflow")
	}
	v.Bytes += delta
	v.ObservedAt = now.UTC()
	if row != nil {
		return row.Update().SetData(toMap(v)).Exec(ctx)
	}
	return s.Store.Ent.Resource.Create().SetKind(dailyUsageKind).SetKey(key).SetOwnerID(v.UserID).SetData(toMap(v)).Exec(ctx)
}

func decodeResource(row *ent.Resource, value any) error {
	b, err := json.Marshal(row.Data)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, value)
}

type usageRank struct {
	UserID     int       `json:"user_id"`
	NodeID     int       `json:"node_id"`
	ChainID    int       `json:"chain_id"`
	Bytes      int64     `json:"bytes"`
	ObservedAt time.Time `json:"observed_at"`
}

func (s *Server) usageRoutes(auth, admin *gin.RouterGroup) {
	auth.GET("/me/usage", func(c *gin.Context) { s.getDailyUsage(c, false) })
	admin.GET("/users/:uid/usage", func(c *gin.Context) { s.getDailyUsage(c, true) })
	admin.GET("/nodes/:id/usage", s.getNodeUsage)
}

func (s *Server) getDailyUsage(c *gin.Context, admin bool) {
	u, ok := s.subject(c, admin)
	if !ok {
		return
	}
	s.respondDailyUsage(c, u.ID, 0)
}

func (s *Server) getNodeUsage(c *gin.Context) {
	n, ok := s.nodeParam(c)
	if !ok {
		return
	}
	s.respondDailyUsage(c, 0, n.ID)
}

func (s *Server) respondDailyUsage(c *gin.Context, uid, nid int) {
	ctx := c.Request.Context()
	finish := operationStage(ctx, "traffic.aggregate", "user_id", uid, "node_id", nid)
	from := time.Now().UTC().AddDate(0, 0, -29).Format("2006-01-02")
	query := s.Store.Ent.Resource.Query().Where(resource.KindEQ(dailyUsageKind), resource.KeyGTE(from+":"))
	if uid != 0 {
		query.Where(resource.OwnerIDEQ(uid))
	}
	rows, err := query.All(ctx)
	if err != nil {
		finish(err)
		failInternal(c, 503, "query_failed", err)
		return
	}
	days := []dailyUsage{}
	ranks := map[string]*usageRank{}
	for _, row := range rows {
		var v dailyUsage
		if err = decodeResource(row, &v); err != nil {
			finish(err)
			failInternal(c, 503, "query_failed", err)
			return
		}
		if nid != 0 && v.NodeID != nid {
			continue
		}
		days = append(days, v)
		key := fmt.Sprintf("%d:%d:%d", v.UserID, v.NodeID, v.ChainID)
		r := ranks[key]
		if r == nil {
			r = &usageRank{UserID: v.UserID, NodeID: v.NodeID, ChainID: v.ChainID}
			ranks[key] = r
		}
		if r.Bytes > math.MaxInt64-v.Bytes {
			err = fmt.Errorf("usage overflow")
			finish(err)
			failInternal(c, 503, "query_failed", err)
			return
		}
		r.Bytes += v.Bytes
		if v.ObservedAt.After(r.ObservedAt) {
			r.ObservedAt = v.ObservedAt
		}
	}
	out := make([]*usageRank, 0, len(ranks))
	for _, r := range ranks {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Bytes == out[j].Bytes {
			return fmt.Sprint(out[i].UserID, ":", out[i].NodeID, ":", out[i].ChainID) < fmt.Sprint(out[j].UserID, ":", out[j].NodeID, ":", out[j].ChainID)
		}
		return out[i].Bytes > out[j].Bytes
	})
	finish(nil)
	c.JSON(200, gin.H{"items": out, "days": days, "from": from, "timezone": "UTC", "coverage": "observed", "reason": "仅含已收到的自有 VPS 凭据计数；断线补报计入收到当天，机场直用及未观测时段未知"})
}
