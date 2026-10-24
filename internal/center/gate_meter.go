package center

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/budget"
	"github.com/shusfun/weir/internal/store/ent/credential"
	"github.com/shusfun/weir/internal/store/ent/node"
	"github.com/shusfun/weir/packages/contracts"
)

// meterDelta 按纪元与累计值算出本次新增用量：纪元变化（内核重启）时新纪元从零起算；
// 同一纪元内累计值回退视为异常，不计入、不扣减。
func meterDelta(prevEpoch string, prevUp, prevDown int64, epoch string, up, down int64) (int64, bool) {
	if up < 0 || down < 0 {
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
	if !bind(c, &rep) {
		return
	}
	g, ctx := gateFrom(c), c.Request.Context()
	if rep.Epoch == "" {
		failure(c, 400, "invalid_meter")
		return
	}
	var affected map[int]bool
	err := s.Store.Lock.Do(ctx, "meter:"+itoa(g.ID), func(ctx context.Context) error {
		cur, err := s.Store.Ent.Gate.Get(ctx, g.ID)
		if err != nil {
			return err
		}
		if rep.Epoch == cur.MeterEpoch && rep.Seq <= cur.MeterSeq {
			return nil
		}
		affected, err = s.applyMeter(ctx, cur, rep)
		if err != nil {
			return err
		}
		return cur.Update().SetMeterEpoch(rep.Epoch).SetMeterSeq(rep.Seq).Exec(ctx)
	})
	if err != nil {
		failure(c, 500, "meter_failed")
		return
	}
	for uid := range affected {
		if gates, err := s.rebalanceUser(ctx, uid); err == nil {
			for _, gid := range gates {
				s.waiters.notify(gid)
			}
		}
	}
	c.JSON(200, gin.H{"ok": true})
}

func (s *Server) applyMeter(ctx context.Context, g *ent.Gate, rep contracts.MeterReport) (map[int]bool, error) {
	affected := map[int]bool{}
	nodeIDs, err := s.Store.Ent.Node.Query().Where(node.GateIDEQ(g.ID)).IDs(ctx)
	if err != nil || len(nodeIDs) == 0 {
		return affected, err
	}
	now := time.Now()
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
		if err = cred.Update().SetMeterEpoch(rep.Epoch).SetMeterUp(sample.Up).SetMeterDown(sample.Down).Exec(ctx); err != nil {
			return nil, err
		}
		if delta == 0 || cred.Internal || cred.UserID == nil {
			continue
		}
		uid := *cred.UserID
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
