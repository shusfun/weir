package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// RateSample 是「用户 × 入口节点（× 链式代理）」每分钟的下载量与有流量的秒数，来自 Gate 按凭据上报的计数。
// 速率 = bytes / seconds，只算有流量的上报窗口；没有流量的分钟不写，读出来就是「没有数据」，不当成 0。
// 保留 25 小时，供代理页按速率排序。chain_id 为 0 是直连那条。
type RateSample struct{ ent.Schema }

func (RateSample) Fields() []ent.Field {
	return []ent.Field{
		field.Int("user_id"),
		field.Int("node_id"),
		field.Int("chain_id").Default(0),
		field.Time("at"), // 分钟起点（UTC）
		field.Int64("bytes").Default(0),
		field.Float("seconds").Default(0),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (RateSample) Indexes() []ent.Index {
	return []ent.Index{index.Fields("user_id", "node_id", "chain_id", "at").Unique(), index.Fields("node_id", "at"), index.Fields("at")}
}
