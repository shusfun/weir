package schema

import (
 "time"
 "entgo.io/ent"
 "entgo.io/ent/schema/field"
 "entgo.io/ent/schema/index"
)
type Grant struct { ent.Schema }
func (Grant) Fields() []ent.Field { return []ent.Field{
 field.Int("user_id"), field.Int("device_id"), field.Int("inbound_id"),
 field.String("status").Default("pending"), field.Bool("enabled").Default(true),
 field.Int64("revision").Default(1),
 field.JSON("credential",map[string]any{}).Sensitive(),
 field.Int64("allocated").Default(0),field.Int64("used").Default(0),
 field.Int64("sequence").Default(0),field.String("meter_epoch").Default(""),
 field.Time("created_at").Default(time.Now),
} }
func (Grant) Indexes() []ent.Index { return []ent.Index{index.Fields("user_id","device_id","inbound_id").Unique()} }
