package schema

import (
 "time"
 "entgo.io/ent"
 "entgo.io/ent/schema/field"
)

type User struct { ent.Schema }
func (User) Fields() []ent.Field { return []ent.Field{
 field.String("email").Unique().MaxLen(254),
 field.String("password_hash").Sensitive(),
 field.String("role").Default("user"),
 field.Bool("enabled").Default(true),
 field.Bool("email_verified").Default(false),
 field.Int64("quota").Default(0).NonNegative(),
 field.Int64("used").Default(0).NonNegative(),
 field.Int64("expires").Default(0),
 field.String("subscription_token").Unique().Sensitive(),
 field.String("totp_secret").Default("").Sensitive(),
 field.Int64("totp_tick").Default(0),
 field.Time("created_at").Default(time.Now),
} }
