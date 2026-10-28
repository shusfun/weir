package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Chain 是用户自建的链式代理：以本人已有节点为入口，出口为家宽 SOCKS5 或本人有权限的另一节点。
// 它表现为一个新节点，可被该用户的任意链接勾选。出口密码加密存储，管理员可看原文。
type Chain struct{ ent.Schema }

func (Chain) Fields() []ent.Field {
	return []ent.Field{
		field.Int("user_id"),
		field.Int("entry_node_id"),
		field.String("name").MaxLen(100),
		field.Enum("exit_kind").Values("socks5", "node", "source"),
		// exit_source：exit_kind 为 source 时的订阅源出口。用户自建只能用 profile_id（分配给他的订阅组），
		// 管理员代建可以用 source_id；mode 为 node（单个节点）或 filter（一组），match 为名称或正则。
		field.JSON("exit_source", map[string]any{}).Optional(),
		field.String("exit_address").MaxLen(255).Default(""),
		field.Int("exit_port").Default(0),
		field.String("exit_username").MaxLen(255).Default(""),
		field.String("exit_password").Sensitive().Default(""),
		field.Int("exit_node_id").Optional().Nillable(),
		field.Bool("enabled").Default(true),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (Chain) Indexes() []ent.Index {
	return []ent.Index{index.Fields("user_id")}
}
