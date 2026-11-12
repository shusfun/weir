package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/shusfun/weir/internal/accountplan"
	"time"
)

type AccessCode struct{ ent.Schema }

func (AccessCode) Fields() []ent.Field {
	return []ent.Field{
		field.Enum("kind").Values("invite", "redeem"),
		field.String("digest").MaxLen(64).Unique().Sensitive(),
		field.Int("plan_id").Optional().Nillable(),
		field.Int("plan_revision").Default(0),
		field.JSON("grant", accountplan.Grant{}),
		field.Time("expires_at").Optional().Nillable(),
		field.Time("used_at").Optional().Nillable(),
		field.Int("used_by").Optional().Nillable(),
		field.Bool("revoked").Default(false),
		field.Int("created_by"),
		field.String("trace_id").MaxLen(32).Default(""),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}
func (AccessCode) Indexes() []ent.Index {
	return []ent.Index{index.Fields("kind", "created_at"), index.Fields("used_by")}
}
