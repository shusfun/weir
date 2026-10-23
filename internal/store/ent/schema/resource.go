package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Resource 是附加功能的通用存储（公告、客户端目录、公开页面、留言等）：按 kind + key 唯一。
type Resource struct{ ent.Schema }

func (Resource) Fields() []ent.Field {
	return []ent.Field{
		field.String("kind").MaxLen(64),
		field.String("key").MaxLen(255),
		field.Int("owner_id").Default(0),
		field.JSON("data", map[string]any{}).Sensitive(),
		field.Time("created_at").Default(time.Now),
	}
}

func (Resource) Indexes() []ent.Index {
	return []ent.Index{index.Fields("kind", "key").Unique()}
}
