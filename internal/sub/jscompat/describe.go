package jscompat

import (
	"math"
	"strings"
)

// JSMap 表示 JS Map（仅用于对照测试的结果描述）。
type JSMap struct {
	Keys []any
	Vals []any
}

// RawJSON 在描述时原样输出，用于向对照脚本传递特殊参数（如 {"$url": "..."}）。
type RawJSON string

// JSSet 表示 JS Set（仅用于对照测试的结果描述）。
type JSSet struct{ Items []any }

// Describe 把值序列化为规范 JSON，区分 undefined、NaN、Map、Set、RegExp 等 JSON 无法表达的值。
// Node 端的对照脚本使用同样的规则，二者输出逐字节比较。
func Describe(v any) string {
	var sb strings.Builder
	describe(&sb, v)
	return sb.String()
}

func describe(sb *strings.Builder, v any) {
	switch x := v.(type) {
	case nil:
		sb.WriteString(`{"$u":1}`)
	case nullType:
		sb.WriteString("null")
	case bool, string, int:
		s, _ := JSONStringify(x)
		sb.WriteString(s)
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			sb.WriteString(`{"$num":"` + NumberToString(x) + `"}`)
		} else {
			sb.WriteString(NumberToString(x))
		}
	case *Array:
		if x == nil {
			sb.WriteString("null")
			return
		}
		sb.WriteByte('[')
		for i, item := range x.Items {
			if i > 0 {
				sb.WriteByte(',')
			}
			describe(sb, item)
		}
		sb.WriteByte(']')
	case []string:
		describe(sb, StrArr(x))
	case []any:
		describe(sb, &Array{Items: x})
	case *Object:
		if x == nil {
			sb.WriteString("null")
			return
		}
		sb.WriteByte('{')
		for i, kv := range x.Entries() {
			if i > 0 {
				sb.WriteByte(',')
			}
			writeJSONString(sb, kv.Key)
			sb.WriteByte(':')
			describe(sb, kv.Value)
		}
		sb.WriteByte('}')
	case *RegExp:
		if x == nil {
			sb.WriteString("null")
			return
		}
		sb.WriteString(`{"$re":`)
		writeJSONString(sb, x.String())
		sb.WriteByte('}')
	case Date:
		sb.WriteString(`{"$date":"` + x.ISO() + `"}`)
	case *JSMap:
		sb.WriteString(`{"$map":[`)
		for i := range x.Keys {
			if i > 0 {
				sb.WriteByte(',')
			}
			sb.WriteByte('[')
			describe(sb, x.Keys[i])
			sb.WriteByte(',')
			describe(sb, x.Vals[i])
			sb.WriteByte(']')
		}
		sb.WriteString("]}")
	case *JSSet:
		sb.WriteString(`{"$set":`)
		describe(sb, &Array{Items: x.Items})
		sb.WriteByte('}')
	case RawJSON:
		sb.WriteString(string(x))
	default:
		sb.WriteString(`{"$unknown":1}`)
	}
}
