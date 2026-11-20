package store

import (
	"context"
	"database/sql"

	"github.com/shusfun/weir/packages/contracts"
)

// worker_attempts 与 worker_communication 使用复合主键，Ent 只支持单列主键（复合主键仅限边表），
// 因此它们在 Ent schema 中标记为跳过迁移，表结构由这里维护。
// worker_tasks_projection 是部分索引：PostgreSQL 会改写谓词文本，交给 Atlas 会在每次启动时被判定为变化并重建，
// 所以也在这里用 IF NOT EXISTS 建立。谓词必须与 taskqueue.ProjectionPending 一致。
var workerSchema = []string{
	`CREATE TABLE IF NOT EXISTS worker_attempts (task_id TEXT NOT NULL, attempt INTEGER NOT NULL, gate_id BIGINT NOT NULL, started_at BIGINT NOT NULL, finished_at BIGINT NOT NULL DEFAULT 0, outcome TEXT NOT NULL, result_bytes BIGINT, PRIMARY KEY(task_id, attempt))`,
	`CREATE INDEX IF NOT EXISTS worker_attempts_window ON worker_attempts(started_at,gate_id)`,
	`CREATE TABLE IF NOT EXISTS worker_communication (bucket BIGINT NOT NULL,gate_id BIGINT NOT NULL,route TEXT NOT NULL,requests BIGINT NOT NULL,received BIGINT NOT NULL,sent BIGINT NOT NULL,errors BIGINT NOT NULL,PRIMARY KEY(bucket,gate_id,route))`,
	`CREATE INDEX IF NOT EXISTS worker_tasks_projection ON worker_tasks(projection_after,id) WHERE status IN ('succeeded','failed','expired','unknown','cancelled') AND projection_after>=0 AND (legacy_id<>0 OR kind IN ('` + contracts.TaskLayaPrepare + `','` + contracts.TaskLayaInstall + `','` + contracts.TaskLayaRemove + `'))`,
}

// migrateWorkerTables 在 Atlas 建好 worker_tasks 之后执行。
func migrateWorkerTables(ctx context.Context, db *sql.DB) error {
	for _, statement := range workerSchema {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}
