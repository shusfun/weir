package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Sample 是按分钟记录的观测：VPS 的流量与主机（CPU、内存、磁盘），用户的流量。保留 25 小时，供 1 / 6 / 24 小时图表。
// 某分钟没有记录就是「未知」（VPS 离线、计数不可用），图表不补零、缺口不连线；有记录且流量为 0 才是 0。
type Sample struct{ ent.Schema }

func (Sample) Fields() []ent.Field {
	return []ent.Field{
		field.Enum("scope").Values("gate", "user"),
		field.Int("subject_id"),
		field.Time("at"), // 分钟起点（UTC）
		field.Int64("traffic").Default(0),
		field.Float("cpu").Optional().Nillable(),
		field.Int64("mem_used").Optional().Nillable(),
		field.Int64("mem_total").Optional().Nillable(),
		field.Int64("disk_used").Optional().Nillable(),
		field.Int64("disk_total").Optional().Nillable(),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (Sample) Indexes() []ent.Index {
	return []ent.Index{index.Fields("scope", "subject_id", "at").Unique(), index.Fields("at")}
}
