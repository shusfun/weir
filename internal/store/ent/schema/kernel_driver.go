package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// KernelDriver 是内核驱动的一次保存（覆盖内置或新增）。每次保存新增一行：同名最大 revision 是当前生效的，
// 其余是历史，可看差异、可退回。text 为空表示恢复内置；disabled 表示停用。
type KernelDriver struct{ ent.Schema }

func (KernelDriver) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").MaxLen(48),
		field.String("kernel").MaxLen(16),
		field.Int("revision"),
		field.Text("text").Default(""),
		field.Bool("disabled").Default(false),
		field.String("note").MaxLen(500).Default(""),
		field.String("source").MaxLen(16).Default("admin"), // admin / dsh
		field.Int("actor").Default(0),
		field.Time("created_at").Default(time.Now),
	}
}

func (KernelDriver) Indexes() []ent.Index {
	return []ent.Index{index.Fields("name", "revision").Unique()}
}
