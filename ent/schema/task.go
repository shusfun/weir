package schema

import (
 "time"
 "entgo.io/ent"
 "entgo.io/ent/schema/field"
 "entgo.io/ent/schema/index"
)
type Task struct { ent.Schema }
func (Task) Fields() []ent.Field { return []ent.Field{
 field.Int("device_id"), field.String("operation"),
 field.String("key").Unique(), field.Int64("revision").Default(1),
 field.JSON("payload",map[string]any{}).Sensitive(),
 field.String("status").Default("pending"),
 field.JSON("result",map[string]any{}).Optional(),
 field.Int("attempts").Default(0),
 field.Time("expires_at"), field.Time("leased_until").Default(time.Now),
 field.Time("created_at").Default(time.Now),
} }
func (Task) Indexes() []ent.Index { return []ent.Index{index.Fields("device_id","status")} }
