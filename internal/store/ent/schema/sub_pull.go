package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// SubPull 是一次拉取订阅的记录，供「订阅管理」统计：时间、格式、客户端、来源地址与结果。保留 30 天。
// 只有链接的主人与管理员能看到；来源地址是这条链接被谁拉取的业务数据，不写进服务日志。
type SubPull struct{ ent.Schema }

func (SubPull) Fields() []ent.Field {
	return []ent.Field{
		field.Int("link_id"),
		field.Int("user_id"),
		field.Time("at").Default(time.Now),
		field.String("target").MaxLen(16).Default(""),
		field.String("client").MaxLen(64).Default(""), // 按 User-Agent 认出的客户端名称；认不出为空
		field.String("user_agent").MaxLen(256).Default(""),
		field.String("ip").MaxLen(64).Default(""),
		// status：ok 正常返回；not_started / expired 不在生效时间内；failed 生成失败
		field.String("status").MaxLen(16).Default("ok"),
	}
}

func (SubPull) Indexes() []ent.Index {
	return []ent.Index{index.Fields("link_id", "at"), index.Fields("user_id", "at"), index.Fields("at")}
}
