package taskqueue

import (
	"context"
	"time"

	"github.com/shusfun/weir/internal/store/ent/predicate"
	"github.com/shusfun/weir/internal/store/ent/workertask"
	"github.com/shusfun/weir/packages/contracts"
)

// terminalStatuses 是不会再变化的任务状态。
var terminalStatuses = []string{"succeeded", "failed", "expired", "unknown", "cancelled"}

// ProjectionPending 是仍需把结果投影回业务记录的任务：已结束、未标记完成（projection_after>=0），
// 并且是旧动作的适配行或 Laya 托管任务。部分索引 worker_tasks_projection 的谓词与此一致，
// 定义在 store/raw_worker_schema.go，两处必须同步修改。
func ProjectionPending() predicate.WorkerTask {
	return workertask.And(
		workertask.StatusIn(terminalStatuses...),
		workertask.ProjectionAfterGTE(0),
		workertask.Or(workertask.LegacyIDNEQ(0), workertask.KindIn(contracts.TaskLayaPrepare, contracts.TaskLayaInstall, contracts.TaskLayaRemove)),
	)
}

func (q *Queue) PendingProjections(ctx context.Context, now time.Time) ([]string, error) {
	return q.Ent.WorkerTask.Query().Where(ProjectionPending(), workertask.ProjectionAfterLTE(now.UnixMilli())).
		Order(workertask.ByProjectionAfter(), workertask.ByID()).Limit(100).IDs(ctx)
}

func (q *Queue) NeedsProjection(ctx context.Context, id string) (bool, error) {
	return q.Ent.WorkerTask.Query().Where(workertask.ID(id), ProjectionPending()).Exist(ctx)
}

func (q *Queue) ResolveProjection(ctx context.Context, id string, retryAt time.Time) error {
	after := int64(-1)
	if !retryAt.IsZero() {
		after = retryAt.UnixMilli()
	}
	return q.Ent.WorkerTask.Update().Where(workertask.ID(id), workertask.ProjectionAfterGTE(0)).SetProjectionAfter(after).Exec(ctx)
}
