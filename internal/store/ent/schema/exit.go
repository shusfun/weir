package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"time"
)

// Exit 是出口资料。系统直连仍属于某一台 VPS；其余出口不绑定入口，由引用它的 VPS 各自保存生效快照。
type Exit struct{ ent.Schema }

func (Exit) Fields() []ent.Field {
	return []ent.Field{
		field.Int("gate_id").Optional().Nillable(),
		field.Int("owner_user_id").Optional().Nillable(),
		field.String("remark").MaxLen(100).Default(""),
		field.Enum("kind").Values("socks5", "http", "https", "link", "node", "source", "freedom", "blackhole"),
		field.String("address").MaxLen(255).Default(""),
		field.Int("port").Default(0),
		field.String("username").MaxLen(255).Default(""),
		field.String("password").Sensitive().Default(""),
		field.String("link").Sensitive().Default(""),
		field.Int("exit_node_id").Optional().Nillable(),
		field.JSON("source", map[string]any{}).Optional(),
		field.Bool("enabled").Default(true),
		field.String("tag").MaxLen(64),
		field.String("policy_type").MaxLen(64).Default(""),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (Exit) Edges() []ent.Edge {
	return []ent.Edge{edge.To("nodes", Node.Type), edge.To("chains", Chain.Type), edge.To("routes", Route.Type), edge.To("bindings", ExitBinding.Type)}
}

func (Exit) Indexes() []ent.Index {
	return []ent.Index{index.Fields("gate_id", "tag").Unique(), index.Fields("owner_user_id", "policy_type")}
}
