package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// GateAction 是下发给 Gate 的一次性动作（只限白名单种类），按 ID 幂等，过期即失效。
type GateAction struct{ ent.Schema }

func (GateAction) Fields() []ent.Field {
	return []ent.Field{
		field.Int("gate_id"),
		field.String("kind").MaxLen(32),
		field.JSON("payload", map[string]any{}).Optional(),
		field.Enum("status").Values("pending", "running", "done", "failed", "expired").Default("pending"),
		field.JSON("result", map[string]any{}).Optional(),
		field.String("error").Default(""),
		field.Time("expires_at"),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("finished_at").Optional().Nillable(),
	}
}

func (GateAction) Indexes() []ent.Index {
	return []ent.Index{index.Fields("gate_id", "status")}
}
