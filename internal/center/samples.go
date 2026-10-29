package center

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/shusfun/weir/internal/store/ent/gateaction"
	"github.com/shusfun/weir/internal/store/ent/sample"
	"github.com/shusfun/weir/packages/contracts"
)

// 流量与主机历史：每分钟一条，保留 25 小时。某分钟没有记录即「未知」，图表不补零、缺口不连线。

const sampleKeep = 25 * time.Hour

func minuteOf(t time.Time) time.Time { return t.UTC().Truncate(time.Minute) }

// recordTraffic 累加一分钟内的流量；delta 为 0 也写一条，表示这一分钟「已知为 0」。
func (s *Server) recordTraffic(ctx context.Context, scope sample.Scope, id int, delta int64, now time.Time) {
	_ = s.Store.Ent.Sample.Create().SetScope(scope).SetSubjectID(id).SetAt(minuteOf(now)).SetTraffic(delta).
		OnConflictColumns(sample.FieldScope, sample.FieldSubjectID, sample.FieldAt).AddTraffic(delta).Exec(ctx)
}

// recordHost 记下 VPS 这一分钟的主机观测；读取失败的字段保持为空（未知）。
func (s *Server) recordHost(ctx context.Context, gid int, h *contracts.HostSample, now time.Time) {
	if h == nil {
		return
	}
	c := s.Store.Ent.Sample.Create().SetScope(sample.ScopeGate).SetSubjectID(gid).SetAt(minuteOf(now)).SetNillableCPU(h.CPUPercent)
	if h.MemUsed != nil && h.MemTotal != nil {
		c.SetMemUsed(int64(*h.MemUsed)).SetMemTotal(int64(*h.MemTotal))
	}
	if h.DiskUsed != nil && h.DiskTotal != nil {
		c.SetDiskUsed(int64(*h.DiskUsed)).SetDiskTotal(int64(*h.DiskTotal))
	}
	_ = c.OnConflictColumns(sample.FieldScope, sample.FieldSubjectID, sample.FieldAt).
		UpdateCPU().UpdateMemUsed().UpdateMemTotal().UpdateDiskUsed().UpdateDiskTotal().Exec(ctx)
}

// PruneSamples 删除超过保留期的记录（每 10 分钟一次）。
func (s *Server) PruneSamples(ctx context.Context, now time.Time) error {
	if ok, err := s.Store.KV.SetNX(ctx, "sample-prune", "1", 10*time.Minute); err != nil || !ok {
		return err
	}
	_, err := s.Store.Ent.Sample.Delete().Where(sample.AtLT(now.Add(-sampleKeep))).Exec(ctx)
	// 链路页每 30 秒续一次实时上报：这类动作只在当时有用，一小时后删掉，不留在动作记录里。
	_, _ = s.Store.Ent.GateAction.Delete().Where(gateaction.KindEQ(contracts.ActionLiveStart), gateaction.CreatedAtLT(now.Add(-time.Hour))).Exec(ctx)
	return err
}

// Point 是图表上的一个点；指针为空表示这段时间没有数据（未知）。
type Point struct {
	At       time.Time `json:"at"`
	Traffic  *int64    `json:"traffic"`
	CPU      *float64  `json:"cpu,omitempty"`
	MemUsed  *int64    `json:"mem_used,omitempty"`
	MemTotal *int64    `json:"mem_total,omitempty"`
	Disk     *int64    `json:"disk_used,omitempty"`
	DiskMax  *int64    `json:"disk_total,omitempty"`
}

// history 按时间段分桶：1 小时 1 分钟一格、6 小时 5 分钟、24 小时 15 分钟。流量求和，CPU 取平均，内存与磁盘取最后一次。
func (s *Server) history(ctx context.Context, scope sample.Scope, id int, hours int) ([]Point, error) {
	step := map[int]time.Duration{1: time.Minute, 6: 5 * time.Minute, 24: 15 * time.Minute}[hours]
	if step == 0 {
		hours, step = 1, time.Minute
	}
	end := minuteOf(time.Now()).Add(time.Minute)
	start := end.Add(-time.Duration(hours) * time.Hour)
	rows, err := s.Store.Ent.Sample.Query().Where(sample.ScopeEQ(scope), sample.SubjectIDEQ(id), sample.AtGTE(start), sample.AtLT(end)).Order(sample.ByAt()).All(ctx)
	if err != nil {
		return nil, err
	}
	n := int(end.Sub(start) / step)
	out := make([]Point, n)
	cpuN := make([]int, n)
	for i := range out {
		out[i].At = start.Add(time.Duration(i) * step)
	}
	for _, r := range rows {
		i := int(r.At.Sub(start) / step)
		if i < 0 || i >= n {
			continue
		}
		p := &out[i]
		t := r.Traffic
		if p.Traffic != nil {
			t += *p.Traffic
		}
		p.Traffic = &t
		if r.CPU != nil {
			c := *r.CPU
			if p.CPU != nil {
				c += *p.CPU
			}
			p.CPU = &c
			cpuN[i]++
		}
		if r.MemUsed != nil {
			p.MemUsed, p.MemTotal = r.MemUsed, r.MemTotal
		}
		if r.DiskUsed != nil {
			p.Disk, p.DiskMax = r.DiskUsed, r.DiskTotal
		}
	}
	for i := range out {
		if cpuN[i] > 1 {
			avg := *out[i].CPU / float64(cpuN[i])
			out[i].CPU = &avg
		}
	}
	return out, nil
}

func (s *Server) historyHandler(scope sample.Scope, param string) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := atoiOr(c.Param(param))
		if id <= 0 {
			failure(c, 400, "invalid_id")
			return
		}
		hours := atoiOr(c.DefaultQuery("hours", "1"))
		points, err := s.history(c.Request.Context(), scope, id, hours)
		if err != nil {
			failInternal(c, 503, "query_failed", err)
			return
		}
		c.JSON(200, gin.H{"points": points})
	}
}

func (s *Server) sampleRoutes(admin *gin.RouterGroup) {
	admin.GET("/gates/:id/history", s.historyHandler(sample.ScopeGate, "id"))
	admin.GET("/users/:uid/history", s.historyHandler(sample.ScopeUser, "uid"))
}
