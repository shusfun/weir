package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// Setting 按键保存一组系统设置；revision 用于并发修改检查。
type Setting struct{ ent.Schema }

func (Setting) Fields() []ent.Field {
	return []ent.Field{
		field.String("key").MaxLen(64).Unique(),
		field.JSON("value", map[string]any{}),
		field.Int64("revision").Default(1),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
