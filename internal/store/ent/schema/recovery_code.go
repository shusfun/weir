package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// RecoveryCode 只保存恢复码摘要；用过即标记，不可重用。
type RecoveryCode struct{ ent.Schema }

func (RecoveryCode) Fields() []ent.Field {
	return []ent.Field{
		field.Int("user_id"),
		field.String("digest").Sensitive(),
		field.Time("used_at").Optional().Nillable(),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (RecoveryCode) Indexes() []ent.Index {
	return []ent.Index{index.Fields("user_id", "digest").Unique()}
}
