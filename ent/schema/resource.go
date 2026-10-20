package schema

import (
 "time"
 "entgo.io/ent"
 "entgo.io/ent/schema/field"
 "entgo.io/ent/schema/index"
)
type Resource struct { ent.Schema }
func (Resource) Fields() []ent.Field { return []ent.Field{
 field.String("kind"), field.String("key"), field.Int("owner_id").Default(0),
 field.JSON("data",map[string]any{}).Sensitive(),
 field.Time("created_at").Default(time.Now),
} }
func (Resource) Indexes() []ent.Index { return []ent.Index{index.Fields("kind","key").Unique()} }
