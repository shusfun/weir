package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// Gate 是一台已配对的执行端。config_revision 是中心最新期望配置的版本，
// applied_revision 是 Gate 回报已生效的版本；两者不同即「待生效」。
type Gate struct{ ent.Schema }

func (Gate) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").MaxLen(100),
		field.String("public_key").Unique(),
		field.Enum("status").Values("active", "revoked").Default("active"),
		field.String("public_host").MaxLen(255).Default(""),
		field.String("version").Default(""),
		field.String("hostname").Default(""),
		field.String("arch").Default("amd64"),
		field.Time("last_seen_at").Optional().Nillable(),
		field.JSON("listening_ports", []map[string]any{}).Optional(),
		field.JSON("kernels_running", map[string]string{}).Optional(),
		field.JSON("kernel_pins", map[string]string{}).Optional(),
		field.JSON("host", map[string]any{}).Optional(),
		field.Int64("config_revision").Default(1),
		field.Int64("applied_revision").Default(0),
		field.Int64("failed_revision").Default(0),
		field.String("last_error").Default(""),
		field.Int("xray_stats_port").Default(0),
		field.Int("singbox_stats_port").Default(0),
		field.String("meter_epoch").Default(""),
		field.Int64("meter_seq").Default(0),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}
