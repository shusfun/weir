package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
 "entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/index"
)

// Route 是管理员自定义的分流规则：匹配入站 / 用户 / 域名 / IP，送往出口；priority 小者优先。
type Route struct{ ent.Schema }

func (Route) Fields() []ent.Field {
	return []ent.Field{
		field.Int("gate_id"),
		field.Int("node_id").Optional().Nillable(),
		field.Int("user_id").Optional().Nillable(),
		field.Int("exit_id"),
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

func (Route) Edges() []ent.Edge { return []ent.Edge{edge.From("exit", Exit.Type).Ref("routes").Field("exit_id").Unique().Required()} }
