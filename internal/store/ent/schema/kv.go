package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

// KV 是单机（SQLite）模式下的内置缓存表，PostgreSQL 模式改用 Redis，不建此表。
// 表与带谓词的过期索引由 store/raw_kv.go 建立，因此跳过 Atlas 迁移。
type KV struct{ ent.Schema }

func (KV) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Table("kv"), entsql.Skip()}
}

func (KV) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").StorageKey("key").Immutable(),
		field.Text("value"),
		// Unix 毫秒；0 表示不过期。
		field.Int64("expires_at").Default(0),
	}
}
