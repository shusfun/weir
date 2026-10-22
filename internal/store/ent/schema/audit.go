package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Audit 记录谁、何时、对什么、做了什么以及结果。
type Audit struct{ ent.Schema }

func (Audit) Fields() []ent.Field {
	return []ent.Field{
		field.Int("actor").Default(0),
		field.String("action").MaxLen(100),
		field.String("target").MaxLen(255).Default(""),
		field.String("result").MaxLen(32).Default("ok"),
		field.String("ip").MaxLen(64).Default(""),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (Audit) Indexes() []ent.Index {
	return []ent.Index{index.Fields("created_at"), index.Fields("actor")}
}
