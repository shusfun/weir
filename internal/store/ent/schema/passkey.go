package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Passkey 保存 WebAuthn 凭据（序列化后的 webauthn.Credential）。
type Passkey struct{ ent.Schema }

func (Passkey) Fields() []ent.Field {
	return []ent.Field{
		field.Int("user_id"),
		field.String("credential_id").Unique(),
		field.String("name").MaxLen(100).Default(""),
		field.Bytes("credential").Sensitive(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("last_used_at").Optional().Nillable(),
	}
}

func (Passkey) Indexes() []ent.Index {
	return []ent.Index{index.Fields("user_id")}
}
