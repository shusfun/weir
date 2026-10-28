package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// Certificate 是节点 TLS 使用的证书；私钥加密保存。来源：
//   - upload：管理员手动上传（高级选项）；
//   - self_signed：Hysteria2 / TUIC 未选证书时自签，同一台 VPS 共用一张（gate_id），有效期 10 年；
//   - panel：VPS 上的 1Panel 申请的本机 IP 证书，私钥只在 Gate 本机，中心只记证书与指纹；
//   - acme：中心用 ACME HTTP-01 为 VPS 公网 IP 签发（Gate 临时在 80 端口应答），到期前续签；
//   - edge：全项目共用的「节点证书域名」证书，中心经本机反代完成 HTTP 验证（兜底）。
type Certificate struct{ ent.Schema }

func (Certificate) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").MaxLen(100),
		field.String("domain").MaxLen(255),
		field.Enum("source").Values("upload", "self_signed", "acme", "panel", "edge").Default("upload"),
		// gate_id：属于哪台 VPS（自签 / 1Panel / IP 证书）；上传与节点证书域名为空。
		field.Int("gate_id").Optional().Nillable(),
		// status：ready 可用；pending 正在申请；failed 申请或续签失败（last_error）。过期由 not_after 判断。
		field.Enum("status").Values("ready", "pending", "failed").Default("ready"),
		field.String("last_error").Default(""),
		field.Time("renewed_at").Optional().Nillable(),
		field.Text("cert_pem"),
		field.Text("key_pem").Sensitive(),
		field.String("sha256").Default(""),
		field.Time("not_after").Optional().Nillable(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
