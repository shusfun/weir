package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// User 是登录账号，同时承载套餐（额度、期限、每月重置）与节点分配方式。
type User struct{ ent.Schema }

func (User) Fields() []ent.Field {
	return []ent.Field{
		field.String("email").MaxLen(254).Unique(),
		field.String("name").MaxLen(100).Default(""),
		field.String("password_hash").Sensitive(),
		field.Enum("role").Values("admin", "user").Default("user"),
		field.Bool("enabled").Default(true),
		// 套餐：quota 为 0 表示不限额度；expires_at 为空表示不限期限。
		field.Int64("quota").NonNegative().Default(0),
		field.Int64("used").NonNegative().Default(0),
		field.Time("expires_at").Optional().Nillable(),
		field.Bool("monthly_reset").Default(false),
		// all_nodes 为真时自动包含以后新加的节点。
		field.Bool("all_nodes").Default(false),
		field.Bool("allow_chain").Default(true),
		field.String("totp_secret").Sensitive().Default(""),
		field.Int64("totp_step").Default(0),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (User) Indexes() []ent.Index {
	return []ent.Index{index.Fields("role")}
}
