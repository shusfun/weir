package schema

import (
 "time"
 "entgo.io/ent"
 "entgo.io/ent/schema/field"
)

type Device struct { ent.Schema }
func (Device) Fields() []ent.Field { return []ent.Field{
 field.String("name").MaxLen(100), field.String("public_key").Unique(),
 field.String("pair_code").Sensitive().Default(""),
 field.String("status").Default("pending"),
 field.Time("pair_expires"), field.Time("last_seen").Default(time.Now),
 field.JSON("metadata",map[string]any{}).Optional(),
 field.Time("created_at").Default(time.Now),
} }
