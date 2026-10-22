package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// Certificate 是节点 TLS 使用的证书：上传、自签或 DNS-01 签发；私钥加密保存。
type Certificate struct{ ent.Schema }

func (Certificate) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").MaxLen(100),
		field.String("domain").MaxLen(255),
		field.Enum("source").Values("upload", "self_signed", "acme").Default("upload"),
		field.Text("cert_pem"),
		field.Text("key_pem").Sensitive(),
		field.String("sha256").Default(""),
		field.Time("not_after").Optional().Nillable(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
