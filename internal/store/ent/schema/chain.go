package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
 "entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/index"
)

// Chain 是用户自建的链式代理：以本人已有节点为入口，出口为 SOCKS5 / HTTP(S) 代理、单条节点链接、
// 分给他的订阅组里的节点或本人有权限的另一节点（不接受订阅地址）。
// 它表现为一个新节点，可被该用户的任意链接勾选。出口密码加密存储，管理员可看原文。
type Chain struct{ ent.Schema }

func (Chain) Fields() []ent.Field {
	return []ent.Field{
		field.Int("user_id"),
		field.Int("entry_node_id"),
		field.String("name").MaxLen(100),
		field.Int("exit_id"),
		field.Bool("enabled").Default(true),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (Chain) Indexes() []ent.Index {
	return []ent.Index{index.Fields("user_id")}
}

func (Chain) Edges() []ent.Edge { return []ent.Edge{edge.From("exit", Exit.Type).Ref("chains").Field("exit_id").Unique().Required()} }
