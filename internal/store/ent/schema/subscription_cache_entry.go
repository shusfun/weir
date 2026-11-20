package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// SubscriptionCacheEntry 记录渲染缓存在 KV 中预占的字节数，用于限制缓存总量。
// 表最初由手写 DDL 建立，列类型与索引名与之对齐。
type SubscriptionCacheEntry struct{ ent.Schema }

func (SubscriptionCacheEntry) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Table("subscription_cache_entries")}
}

func (SubscriptionCacheEntry) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").StorageKey("key").SchemaType(textType).Immutable(),
		field.Int64("bytes"),
		// Unix 毫秒。
		field.Int64("expires_at"),
	}
}

func (SubscriptionCacheEntry) Indexes() []ent.Index {
	return []ent.Index{index.Fields("expires_at").StorageKey("subscription_cache_entries_expiry")}
}
