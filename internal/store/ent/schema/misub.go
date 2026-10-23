package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// SubSource 是机场订阅源（MiSub 的 http 订阅）。网站、备注只是元数据，中心不访问网站。
type SubSource struct{ ent.Schema }

func (SubSource) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").MaxLen(100).Default(""),
		field.String("url").MaxLen(2048),
		field.Bool("enabled").Default(true),
		field.String("user_agent").MaxLen(256).Default(""),
		field.Text("filter_include").Default(""),
		field.Text("filter_exclude").Default(""),
		field.JSON("operators", []any{}).Optional(),
		field.Bool("plus_as_space").Default(false),
		field.Bool("node_cache").Default(true),
		field.Bool("exclude_traffic").Default(false), // 不计入管理员的上游流量合计
		field.String("website").MaxLen(2048).Default(""),
		field.String("remark").MaxLen(500).Default(""),
		field.Int("position").Default(0),
		// 运行时信息：最近一次拉取的结果。
		field.Int("node_count").Default(0),
		field.Int("known_nodes").Default(0),
		field.String("user_info").Default(""),
		field.String("title").Default(""),
		field.String("last_error").Default(""),
		field.Time("fetched_at").Optional().Nillable(),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

// SubNode 是手动添加的节点链接。
type SubNode struct{ ent.Schema }

func (SubNode) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").MaxLen(200).Default(""),
		field.Text("url"),
		field.String("group").MaxLen(100).Default(""),
		field.Bool("enabled").Default(true),
		field.Text("filter_include").Default(""),
		field.Text("filter_exclude").Default(""),
		field.JSON("operators", []any{}).Optional(),
		field.Bool("plus_as_space").Default(false),
		field.Int("position").Default(0),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

// SubProfile 是订阅组：选订阅源与手动节点（或全部），配处理链、规则模板与输出设置；
// 可分配给用户（各自独立链接）或以分享令牌匿名发布。
type SubProfile struct{ ent.Schema }

func (SubProfile) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").MaxLen(100),
		field.Bool("enabled").Default(true),
		field.Bool("all_sources").Default(false), // 包含全部已启用的订阅源与手动节点
		field.JSON("source_ids", []int{}).Optional(),
		field.JSON("node_ids", []int{}).Optional(),
		field.JSON("operators", []any{}).Optional(),
		field.JSON("settings", map[string]any{}).Optional(), // 前缀、模板、规则等级、筛选、旗帜
		field.Time("expires_at").Optional().Nillable(),
		field.String("share_token").Default(""),
		field.Time("share_expires_at").Optional().Nillable(),
		field.Int("downloads").Default(0),
		field.Int("position").Default(0),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (SubProfile) Indexes() []ent.Index {
	return []ent.Index{index.Fields("share_token")}
}

// SubGrant 把订阅组分配给用户：每个用户一个独立令牌，可单独撤销。
type SubGrant struct{ ent.Schema }

func (SubGrant) Fields() []ent.Field {
	return []ent.Field{
		field.Int("profile_id"),
		field.Int("user_id"),
		field.String("token").Unique(),
		field.Time("last_used_at").Optional().Nillable(),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (SubGrant) Indexes() []ent.Index {
	return []ent.Index{index.Fields("profile_id", "user_id").Unique(), index.Fields("user_id")}
}

// RuleTemplate 是自定义规则模板（ini 或 Clash YAML 完整配置）。
type RuleTemplate struct{ ent.Schema }

func (RuleTemplate) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").MaxLen(100),
		field.Enum("format").Values("ini", "yaml").Default("ini"),
		field.Text("content"),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
