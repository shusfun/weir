package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Outbound 是管理员在某 Gate 上手动配置的出口（链式代理的出口由节点 / 自建链式代理自动生成，不在此表）。
type Outbound struct{ ent.Schema }

func (Outbound) Fields() []ent.Field {
	return []ent.Field{
		field.Int("gate_id"),
		field.String("tag").MaxLen(64),
		field.String("name").MaxLen(100).Default(""),
		field.String("protocol").MaxLen(32),
		field.String("address").MaxLen(255).Default(""),
		field.Int("port").Default(0),
		field.String("username").MaxLen(255).Default(""),
		field.String("password").Sensitive().Default(""),
		field.JSON("settings", map[string]any{}).Optional(),
		field.Bool("enabled").Default(true),
		field.Int("position").Default(0),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (Outbound) Indexes() []ent.Index {
	return []ent.Index{index.Fields("gate_id", "tag").Unique()}
}

// Route 是管理员自定义的分流规则：匹配入站 / 用户 / 域名 / IP，送往出口；priority 小者优先。
type Route struct{ ent.Schema }

func (Route) Fields() []ent.Field {
	return []ent.Field{
		field.Int("gate_id"),
		field.Int("node_id").Optional().Nillable(),
		field.Int("user_id").Optional().Nillable(),
		field.String("outbound_tag").MaxLen(64),
		field.String("domain").Default(""),
		field.String("ip").Default(""),
		field.Bool("enabled").Default(true),
		field.Int("priority").Default(0),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (Route) Indexes() []ent.Index {
	return []ent.Index{index.Fields("gate_id")}
}
