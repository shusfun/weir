package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Credential 是「链接 × 节点」上的独立凭据；chain_id 非空时属于用户自建链式代理。
// internal 为真时是系统内部凭据（链式代理入口到出口那一跳），不计入任何用户额度。
// meter_* 记录 Gate 上一次上报的累计值，用于按差值记账。
type Credential struct{ ent.Schema }

func (Credential) Fields() []ent.Field {
	return []ent.Field{
		field.Int("node_id"),
		field.Int("link_id").Optional().Nillable(),
		field.Int("chain_id").Optional().Nillable(),
		field.Int("user_id").Optional().Nillable(),
		field.String("stats_key").Unique(),
		field.String("uuid").Sensitive().Default(""),
		field.String("password").Sensitive().Default(""),
		field.Bool("internal").Default(false),
		field.String("meter_epoch").Default(""),
		field.Int64("meter_up").Default(0),
		field.Int64("meter_down").Default(0),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (Credential) Indexes() []ent.Index {
	return []ent.Index{index.Fields("node_id"), index.Fields("link_id"), index.Fields("user_id")}
}
