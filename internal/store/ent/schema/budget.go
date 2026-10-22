package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Budget 是某用户在某台 Gate 上已分配的累计额度。allocated 只在 Gate 确认应用后才算生效：
// confirmed 为 Gate 已应用的额度，下调额度在确认前仍按旧值占用，避免多台 Gate 合计超额。
// rate 是该 Gate 上该用户最近的使用速度（字节/秒，指数平滑），用于决定分块大小。
type Budget struct{ ent.Schema }

func (Budget) Fields() []ent.Field {
	return []ent.Field{
		field.Int("user_id"),
		field.Int("gate_id"),
		field.Int64("allocated").Default(0),
		field.Int64("confirmed").Default(0),
		field.Int64("used").Default(0),
		field.Int64("revision").Default(0),
		field.Float("rate").Default(0),
		field.Time("rated_at").Optional().Nillable(),
	}
}

func (Budget) Indexes() []ent.Index {
	return []ent.Index{index.Fields("user_id", "gate_id").Unique()}
}
