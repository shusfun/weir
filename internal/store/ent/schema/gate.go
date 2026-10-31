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
		// center_url 是 Gate 心跳里报告的、它正在连接的中心地址；与中心记录的公共地址不同即「待切换」。
		field.String("center_url").MaxLen(255).Default(""),
		field.String("version").Default(""),
		field.String("hostname").Default(""),
		field.String("arch").Default("amd64"),
		field.Time("last_seen_at").Optional().Nillable(),
		field.JSON("listening_ports", []map[string]any{}).Optional(),
		field.JSON("kernels_running", map[string]string{}).Optional(),
		field.JSON("kernel_pins", map[string]string{}).Optional(),
		field.JSON("host", map[string]any{}).Optional(),
		// panel 是 Gate 上报的 1Panel 状态（是否检测到、是否已填 API 密钥、版本、错误）；密钥只在 Gate 本机。
		field.JSON("panel", map[string]any{}).Optional(),
		// firewall 是 Gate 上报的本机防火墙（ufw / firewalld / iptables 是否启用、放行了哪些端口）；读不到时为空，显示「未知」。
		field.JSON("firewall", map[string]any{}).Optional(),
		// self_update：最近一次自更新失败的原因（Gate 心跳上报；更新成功或已同版本时清空）。
		field.JSON("self_update", map[string]any{}).Optional(),
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
