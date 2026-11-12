package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/shusfun/weir/internal/accountplan"
	"time"
)

type AccountPlan struct{ ent.Schema }

func (AccountPlan) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").MaxLen(100),
		field.Int("revision").Positive().Default(1),
		field.Bool("enabled").Default(true),
		field.JSON("grant", accountplan.Grant{}),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
