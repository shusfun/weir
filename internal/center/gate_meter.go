package center

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/budget"
	"github.com/shusfun/weir/internal/store/ent/credential"
	"github.com/shusfun/weir/internal/store/ent/node"
	"github.com/shusfun/weir/internal/store/ent/resource"
	entsample "github.com/shusfun/weir/internal/store/ent/sample"
	"github.com/shusfun/weir/packages/contracts"
)

// meterDelta 按纪元与累计值算出本次新增用量：纪元变化（内核重启）时新纪元从零起算；
// 同一纪元内累计值回退视为异常，不计入、不扣减。
func meterDelta(prevEpoch string, prevUp, prevDown int64, epoch string, up, down int64) (int64, bool) {
	if up < 0 || down < 0 || up > math.MaxInt64-down {
		return 0, false
	}
	if epoch != prevEpoch {
		return up + down, true
	}
	if up < prevUp || down < prevDown {
		return 0, false
	}
	return up - prevUp + down - prevDown, true
}

// agentMeter 记账：差值同时计入用户总用量、链接用量与该用户在本 Gate 的额度用量。
// 同一纪元内序号不前进的上报（重放、重复提交）直接确认，不重复扣减。
func (s *Server) agentMeter(c *gin.Context) {
	var rep contracts.MeterReport
	if !bindWorker(c, &rep, 16<<20) {
		return
	}
	g, ctx := gateFrom(c), c.Request.Context()
	if !validMeterReport(rep) {
		failure(c, 400, "invalid_meter")
		return
	}
	var affected map[int]bool
	accepted := true
	err := s.Store.Lock.Do(ctx, "meter:"+itoa(g.ID), func(ctx context.Context) error {
		cur, err := s.Store.Ent.Gate.Get(ctx, g.ID)
		if err != nil {
			return err
		}
		if rep.Epoch == cur.MeterEpoch && rep.Seq <= cur.MeterSeq {
			return nil
		}
		if rep.Epoch != cur.MeterEpoch {
			seen, e := s.Store.Ent.Resource.Query().Where(resource.KindEQ("gate-meter-epoch"), resource.KeyEQ(itoa(g.ID)+":"+rep.Epoch)).Exist(ctx)
			if e != nil {
				return e
			}
			if seen {
				accepted = false
				return nil
			}
			if rep.Partial {
				return errMeterBaseline
			}
		}
		affected, err = s.applyMeter(ctx, cur, rep)
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, errMeterBaseline) {
			failure(c, 409, "meter_full_baseline_required")
			return
		}
		failInternal(c, 500, "meter_failed", err)
		return
	}
	for uid := range affected {
		if gates, err := s.rebalanceUser(ctx, uid); err == nil {
			for _, gid := range gates {
				s.waiters.notify(gid)
				_ = s.Store.Notifications.Publish(ctx, "gate:"+itoa(gid))
			}
		}
	}
	c.JSON(200, contracts.MeterAck{Epoch: rep.Epoch, Seq: rep.Seq, Accepted: accepted})
}

var errMeterBaseline = errors.New("meter full baseline required")

func validMeterReport(rep contracts.MeterReport) bool {
	if rep.Epoch == "" || len(rep.Epoch) > 100 || rep.Seq < 0 || (rep.Protocol != 0 && rep.Protocol != 2) || rep.Partial && rep.Protocol != 2 || len(rep.Usage) > 100000 || (rep.Status != "fresh" && rep.Status != "unavailable" && rep.Status != "") {
		return false
	}
	seen := map[string]bool{}
	for _, sample := range rep.Usage {
		if sample.StatsKey == "" || len(sample.StatsKey) > 200 || seen[sample.StatsKey] || sample.Up < 0 || sample.Down < 0 || sample.Up > math.MaxInt64-sample.Down {
			return false
		}
		seen[sample.StatsKey] = true
	}
	return true
}

func (s *Server) applyMeter(ctx context.Context, g *ent.Gate, rep contracts.MeterReport) (map[int]bool, error) {
	tx, err := s.Store.Ent.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if rep.Epoch != g.MeterEpoch {
		for _, epoch := range []string{g.MeterEpoch, rep.Epoch} {
			if epoch == "" {
				continue
			}
			key := itoa(g.ID) + ":" + epoch
			seen, e := tx.Resource.Query().Where(resource.KindEQ("gate-meter-epoch"), resource.KeyEQ(key)).Exist(ctx)
			if e != nil {
				return nil, e
			}
			if seen {
				continue
			}
			if err = tx.Resource.Create().SetKind("gate-meter-epoch").SetKey(key).SetData(map[string]any{"gate_id": g.ID}).Exec(ctx); err != nil {
				return nil, err
			}
		}
	}
	meterStore := *s.Store
	meterStore.Ent = tx.Client()
	worker := &Server{Store: &meterStore}
	daily := map[int]int64{}
	affected, err := worker.applyMeterRows(ctx, g, rep, daily)
	if err != nil {
		return nil, err
	}
	if err = tx.Gate.UpdateOneID(g.ID).SetMeterEpoch(rep.Epoch).SetMeterSeq(rep.Seq).Exec(ctx); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	// SQLite 缓存与业务表共用连接，缓存更新必须在事务释放连接之后执行。
	for id, delta := range daily {
		_, err := s.Store.KV.IncrBy(ctx, dayTrafficKey(time.Now(), id), delta, 50*time.Hour)
		s.tasks.note("daily-traffic", err)
	}
	return affected, nil
}

func (s *Server) applyMeterRows(ctx context.Context, g *ent.Gate, rep contracts.MeterReport, daily map[int]int64) (map[int]bool, error) {
	affected := map[int]bool{}
	nodeIDs, err := s.Store.Ent.Node.Query().Where(node.GateIDEQ(g.ID)).IDs(ctx)
	if err != nil || len(nodeIDs) == 0 {
		return affected, err
	}
	now := time.Now()
	// 历史：计数可用（fresh）时才记，这一分钟即使没有流量也记为 0；不可用时不记，图表显示为未知。
	fresh := rep.Status == "fresh" && g.Status == "active"
	gateTotal, users := int64(0), map[int]int64{}
	if fresh {
		creds, err := s.Store.Ent.Credential.Query().Where(credential.NodeIDIn(nodeIDs...), credential.InternalEQ(false), credential.UserIDNotNil()).All(ctx)
		if err != nil {
			return nil, err
		}
		for _, cred := range creds {
			users[*cred.UserID] = 0
		}
	}
	for _, sample := range rep.Usage {
		cred, err := s.Store.Ent.Credential.Query().Where(credential.StatsKeyEQ(sample.StatsKey), credential.NodeIDIn(nodeIDs...)).Only(ctx)
		if ent.IsNotFound(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		delta, ok := meterDelta(cred.MeterEpoch, cred.MeterUp, cred.MeterDown, rep.Epoch, sample.Up, sample.Down)
		if !ok {
			continue
		}
		if err = cred.Update().SetMeterEpoch(rep.Epoch).SetMeterUp(sample.Up).SetMeterDown(sample.Down).SetMeterAt(now).Exec(ctx); err != nil {
			return nil, err
		}
		if fresh && !cred.Internal && cred.UserID != nil {
			if err := s.recordRate(ctx, cred, rep.Epoch, sample.Down, now); err != nil {
				return nil, err
			}
		}
		if delta > 0 {
			// 按天累计（中心本地日期），链路页显示每条路径今天的流量；内部凭据也记，用于出口一侧的连线。
			daily[cred.ID] += delta
		}
		if cred.Internal || cred.UserID == nil {
			continue
		}
		uid := *cred.UserID
		gateTotal += delta
		users[uid] += delta
		if delta == 0 {
			continue
		}
		if err = s.Store.Ent.User.UpdateOneID(uid).AddUsed(delta).Exec(ctx); err != nil {
			return nil, err
		}
		if cred.LinkID != nil {
			if err = s.Store.Ent.Link.UpdateOneID(*cred.LinkID).AddUsed(delta).SetLastUsedAt(now).Exec(ctx); err != nil && !ent.IsNotFound(err) {
				return nil, err
			}
		}
		if err = s.addBudgetUsage(ctx, uid, g.ID, delta, now); err != nil {
			return nil, err
		}
		affected[uid] = true
	}
	if fresh {
		if err := s.recordTraffic(ctx, entsample.ScopeGate, g.ID, gateTotal, now); err != nil {
			return nil, err
		}
		for uid, delta := range users {
			if err := s.recordTraffic(ctx, entsample.ScopeUser, uid, delta, now); err != nil {
				return nil, err
			}
		}
	}
	return affected, nil
}

// addBudgetUsage 累加额度用量并更新使用速度（指数平滑），速度决定多 Gate 时的分块大小。
func (s *Server) addBudgetUsage(ctx context.Context, uid, gid int, delta int64, now time.Time) error {
	b, err := s.Store.Ent.Budget.Query().Where(budget.UserIDEQ(uid), budget.GateIDEQ(gid)).Only(ctx)
	if ent.IsNotFound(err) {
		b, err = s.Store.Ent.Budget.Create().SetUserID(uid).SetGateID(gid).Save(ctx)
	}
	if err != nil {
		return err
	}
	rate := b.Rate
	if b.RatedAt != nil {
		if secs := now.Sub(*b.RatedAt).Seconds(); secs >= 1 {
			rate = 0.7*b.Rate + 0.3*float64(delta)/secs
		}
	}
	return b.Update().AddUsed(delta).SetRate(rate).SetRatedAt(now).Exec(ctx)
}

func dayTrafficKey(t time.Time, credID int) string {
	return "traffic-day:" + t.Format("20060102") + ":" + itoa(credID)
}
