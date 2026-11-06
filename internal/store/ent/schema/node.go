package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
 "entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/index"
)

// Node 是 Gate 上的一个入站。preset 记录建节点时选用的内置方案；
// settings 保存协议参数（含 REALITY 私钥等，仅中心与 Gate 使用）。
type Node struct{ ent.Schema }

func (Node) Fields() []ent.Field {
	return []ent.Field{
		field.Int("gate_id"),
		field.String("name").MaxLen(100),
		field.String("preset").MaxLen(64).Default(""),
		field.String("kernel").MaxLen(16),
		field.String("protocol").MaxLen(32),
		field.Int("port"),
		field.String("network").MaxLen(16).Default("tcp"),
		field.String("security").MaxLen(16).Default("none"),
		field.JSON("settings", map[string]any{}).Optional(),
		field.Int("cert_id").Optional().Nillable(),
		field.Bool("enabled").Default(true),
		field.Int("exit_id"),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (Node) Indexes() []ent.Index {
	return []ent.Index{index.Fields("gate_id", "port").Unique()}
}

func (Node) Edges() []ent.Edge { return []ent.Edge{edge.From("exit", Exit.Type).Ref("nodes").Field("exit_id").Unique().Required()} }
