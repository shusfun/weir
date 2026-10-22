package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Link 是用户的一个订阅链接（每人最多 10 个）。all_nodes 为真时自动包含用户以后获得的节点；
// 否则只含 node_ids / chain_ids 指定的节点。额度按用户共用，used 只记本链接用量。
type Link struct{ ent.Schema }

func (Link) Fields() []ent.Field {
	return []ent.Field{
		field.Int("user_id"),
		field.String("name").MaxLen(100).Default(""),
		field.String("token").Unique(),
		field.Bool("all_nodes").Default(true),
		field.JSON("node_ids", []int{}).Optional(),
		field.JSON("chain_ids", []int{}).Optional(),
		field.Bool("enabled").Default(true),
		field.Int64("used").Default(0),
		field.Time("last_used_at").Optional().Nillable(),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (Link) Indexes() []ent.Index {
	return []ent.Index{index.Fields("user_id")}
}
