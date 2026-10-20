package schema

import (
 "time"
 "entgo.io/ent"
 "entgo.io/ent/schema/field"
)
type Audit struct { ent.Schema }
func (Audit) Fields() []ent.Field { return []ent.Field{
 field.Int("actor").Default(0),field.String("action"),field.String("target").Default(""),
 field.String("result").Default("ok"),field.String("ip").Default(""),
 field.Time("created_at").Default(time.Now),
} }
