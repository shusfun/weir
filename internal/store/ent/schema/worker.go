package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// worker_* 表最初由手写 DDL 建立，这里的类型、默认值和索引名逐一对齐已有表，
// 让 Atlas 对已有库只做比对、不改结构。时间统一存 Unix 毫秒。
var textType = map[string]string{dialect.Postgres: "text"}

func notIncremental() entsql.Annotation {
	off := false
	return entsql.Annotation{Incremental: &off}
}

// WorkerTask 是派给 Gate 的一项任务。body 是宿主加密后的密文，调度元数据里不含任务输入。
type WorkerTask struct{ ent.Schema }

func (WorkerTask) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Table("worker_tasks")}
}

func (WorkerTask) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").SchemaType(textType).Immutable(),
		field.Text("kind"),
		field.Int32("version"),
		field.Text("trace_id"),
		field.Text("idem").Unique(),
		field.Text("payload_hash"),
		field.Text("body"),
		field.Text("requirements"),
		field.Text("candidates"),
		field.Int64("deadline"),
		field.Int64("created_at"),
		field.Text("status"),
		field.Int("gate_id").Default(0),
		field.Int32("attempt").Default(0),
		field.Text("lease_token").Default(""),
		field.Int64("lease_until").Default(0),
		field.Text("result").Default(""),
		field.Text("error").Default(""),
		field.Int("legacy_id").Default(0),
		field.Int64("candidate_at").Default(0),
		field.Text("progress").Default(""),
		field.Int64("projection_after").Default(0),
	}
}

// worker_tasks_projection 是带谓词的部分索引，不在这里声明：PostgreSQL 会改写谓词文本，
// Atlas 按文本比对会在每次启动时重建它。它由 store/raw_worker_schema.go 维护。
func (WorkerTask) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("status", "created_at").StorageKey("worker_tasks_dispatch"),
		index.Fields("gate_id", "status").StorageKey("worker_tasks_gate"),
		index.Fields("legacy_id").StorageKey("worker_tasks_legacy"),
	}
}

// WorkerActivity 记录任务的派发、改派与结束原因。
type WorkerActivity struct{ ent.Schema }

func (WorkerActivity) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Table("worker_activity")}
}

func (WorkerActivity) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").SchemaType(textType).Immutable(),
		field.Text("task_id"),
		field.Int("gate_id"),
		field.Int32("attempt"),
		field.Int64("at"),
		field.Text("reason"),
	}
}

func (WorkerActivity) Indexes() []ent.Index {
	return []ent.Index{index.Fields("task_id", "at").StorageKey("worker_activity_task")}
}

// WorkerCapability 缓存 Gate 最近一次通过校验的能力文档，主键是 gate_id。
type WorkerCapability struct{ ent.Schema }

func (WorkerCapability) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Table("worker_capabilities")}
}

func (WorkerCapability) Fields() []ent.Field {
	return []ent.Field{
		field.Int("id").StorageKey("gate_id").Annotations(notIncremental()),
		field.Text("hash"),
		field.Text("document"),
	}
}

// WorkerAttempt 是任务的每次领取。表的主键是 (task_id, attempt)，Ent 只支持单列主键（复合主键仅限
// 边表），所以这里把 task_id 映射为 Ent 的 ID，表结构由 store/raw_worker_schema.go 建立并跳过 Atlas。
// 查询与更新必须同时带 task_id 与 attempt 条件，不能用 UpdateOne / DeleteOne。
type WorkerAttempt struct{ ent.Schema }

func (WorkerAttempt) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Table("worker_attempts"), entsql.Skip()}
}

func (WorkerAttempt) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").StorageKey("task_id").SchemaType(textType).Immutable(),
		field.Int32("attempt"),
		field.Int("gate_id"),
		field.Int64("started_at"),
		field.Int64("finished_at").Default(0),
		field.Text("outcome"),
		// 未测量的旧尝试保持 NULL，不当成 0 字节。
		field.Int64("result_bytes").Optional().Nillable(),
	}
}

// WorkerCommunication 按分钟汇总 Gate 与中心之间的请求量。主键是 (bucket, gate_id, route)，
// 原因同 WorkerAttempt：bucket 映射为 Ent 的 ID，写入一律走 OnConflict 汇总。
type WorkerCommunication struct{ ent.Schema }

func (WorkerCommunication) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Table("worker_communication"), entsql.Skip()}
}

func (WorkerCommunication) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id").StorageKey("bucket").Annotations(notIncremental()),
		field.Int("gate_id"),
		field.Text("route"),
		field.Int64("requests"),
		field.Int64("received"),
		field.Int64("sent"),
		field.Int64("errors"),
	}
}
