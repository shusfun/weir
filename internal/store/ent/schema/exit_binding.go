package schema

import (
	"encoding/json"
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// ExitBinding 是某台 VPS 上正在生效的出口快照。主动测试不写这条记录。
// 连接参数变更后，只有这台 VPS 验证通过才替换快照；失败时保留原快照并写下原因。
type ExitBinding struct{ ent.Schema }

func (ExitBinding) Fields() []ent.Field {
	return []ent.Field{
		field.Int("exit_id"),
		field.Int("gate_id"),
		field.Enum("status").Values("applied", "failed", "waiting").Default("applied"),
		field.String("reason").MaxLen(500).Default(""),
		field.String("desired_digest").MaxLen(64).Default(""),
		field.Time("verified_at").Optional().Nillable(),
		field.Enum("kind").Values("socks5", "http", "https", "link", "node", "source", "freedom", "blackhole"),
		field.String("remark").MaxLen(100).Default(""),
		field.String("address").MaxLen(255).Default(""),
		field.String("username").MaxLen(255).Default(""),
		field.String("password").Sensitive().Default(""),
		field.String("link").Sensitive().Default(""),
		field.Int("port").Default(0),
		field.Int("exit_node_id").Optional().Nillable(),
		field.JSON("source", map[string]any{}).Optional(),
		field.JSON("resolved", json.RawMessage{}).Optional().Sensitive(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (ExitBinding) Edges() []ent.Edge {
	return []ent.Edge{edge.From("exit", Exit.Type).Ref("bindings").Field("exit_id").Unique().Required()}
}

func (ExitBinding) Indexes() []ent.Index {
	return []ent.Index{index.Fields("exit_id", "gate_id").Unique(), index.Fields("gate_id")}
}
