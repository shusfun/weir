package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/shusfun/weir/packages/contracts"
)

type ProxyGroupVersion struct{ ent.Schema }

func (ProxyGroupVersion) Fields() []ent.Field {
	return []ent.Field{
		field.Int("owner_id").Immutable(),
		field.Int("group_id").Immutable(),
		field.JSON("snapshot", contracts.ProxyGroup{}).Immutable(),
		field.Time("valid_from").Default(time.Now).Immutable(),
		field.Time("valid_until").Optional().Nillable(),
		field.String("trace_id").MaxLen(32).Default("").Immutable(),
	}
}

func (ProxyGroupVersion) Indexes() []ent.Index {
	return []ent.Index{index.Fields("owner_id", "group_id", "valid_from").Unique(), index.Fields("owner_id", "valid_until")}
}
