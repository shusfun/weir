// Package jscompat 提供与 JavaScript 语义一致的值模型与内建函数实现，
// 供 MiSub 订阅引擎逐文件移植时保持与上游行为一致。
//
// 约定：Go 的 nil 表示 JS 的 undefined，Null 表示 JS 的 null；
// 数字统一使用 float64；对象使用保持插入顺序的 *Object，数组使用 *Array（引用语义）。
// 这里只是语义兼容层，不包含任何脚本解释器。
package jscompat

import (
	"math"
	"sort"
	"strconv"
	"strings"
)

type nullType struct{}

// Null 表示 JS 的 null。
var Null = nullType{}

// IsNull 判断是否为 JS null。
func IsNull(v any) bool { _, ok := v.(nullType); return ok }

// IsNullish 对应 `v == null`（undefined 或 null）。
func IsNullish(v any) bool { return v == nil || IsNull(v) }

// Object 是保持 JS 属性顺序的普通对象。
type Object struct {
	keys []string
	vals map[string]any
}

// NewObject 创建空对象。
func NewObject() *Object { return &Object{vals: map[string]any{}} }

// Obj 以键值对快速构造对象，例如 Obj("a", 1.0, "b", "x")。
func Obj(kv ...any) *Object {
	o := NewObject()
	for i := 0; i+1 < len(kv); i += 2 {
		o.Set(kv[i].(string), kv[i+1])
	}
	return o
}

// Get 读取属性；不存在或值为 undefined 时返回 nil。
func (o *Object) Get(k string) any {
	if o == nil {
		return nil
	}
	return o.vals[k]
}

// Has 判断自有属性是否存在（含值为 undefined 的属性）。
func (o *Object) Has(k string) bool {
	if o == nil {
		return false
	}
	_, ok := o.vals[k]
	return ok
}

// Set 设置属性；已存在的属性保持原位置。
func (o *Object) Set(k string, v any) {
	if _, ok := o.vals[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.vals[k] = v
}

// Delete 删除属性。
func (o *Object) Delete(k string) {
	if o == nil {
		return
	}
	if _, ok := o.vals[k]; !ok {
		return
	}
	delete(o.vals, k)
	for i, key := range o.keys {
		if key == k {
			o.keys = append(o.keys[:i:i], o.keys[i+1:]...)
			break
		}
	}
}

// Len 返回属性个数，对应 Object.keys(o).length。
func (o *Object) Len() int {
	if o == nil {
		return 0
	}
	return len(o.keys)
}

// Keys 按 JS 规则返回属性名：整数索引键升序在前，其余按插入顺序。
func (o *Object) Keys() []string {
	if o == nil {
		return nil
	}
	var idx []string
	var rest []string
	for _, k := range o.keys {
		if isArrayIndex(k) {
			idx = append(idx, k)
		} else {
			rest = append(rest, k)
		}
	}
	if len(idx) == 0 {
		return append([]string(nil), o.keys...)
	}
	sort.SliceStable(idx, func(i, j int) bool {
		a, _ := strconv.ParseUint(idx[i], 10, 64)
		b, _ := strconv.ParseUint(idx[j], 10, 64)
		return a < b
	})
	return append(idx, rest...)
}

// Entries 返回 [key, value] 序列，顺序同 Keys。
func (o *Object) Entries() []KV {
	keys := o.Keys()
	out := make([]KV, 0, len(keys))
	for _, k := range keys {
		out = append(out, KV{k, o.vals[k]})
	}
	return out
}

// KV 是对象的一个属性。
type KV struct {
	Key   string
	Value any
}

// Clone 浅拷贝，对应 {...o}（值为 undefined 的属性也会被复制）。
func (o *Object) Clone() *Object {
	n := NewObject()
	n.Assign(o)
	return n
}

// Assign 把 src 的自有属性按顺序写入 o，对应 Object.assign / 展开运算。
func (o *Object) Assign(src any) *Object {
	s, ok := src.(*Object)
	if !ok || s == nil {
		return o
	}
	for _, k := range s.Keys() {
		o.Set(k, s.vals[k])
	}
	return o
}

func isArrayIndex(k string) bool {
	if k == "" || len(k) > 10 {
		return false
	}
	if k == "0" {
		return true
	}
	if k[0] == '0' {
		return false
	}
	for i := 0; i < len(k); i++ {
		if k[i] < '0' || k[i] > '9' {
			return false
		}
	}
	n, err := strconv.ParseUint(k, 10, 64)
	return err == nil && n < 4294967295
}

// Array 是具有引用语义的 JS 数组。
type Array struct {
	Items []any
}

// Arr 构造数组。
func Arr(items ...any) *Array { return &Array{Items: items} }

// StrArr 由字符串切片构造数组。
func StrArr(items []string) *Array {
	a := &Array{Items: make([]any, len(items))}
	for i, s := range items {
		a.Items[i] = s
	}
	return a
}

// Len 返回长度。
func (a *Array) Len() int {
	if a == nil {
		return 0
	}
	return len(a.Items)
}

// Push 追加元素。
func (a *Array) Push(v ...any) { a.Items = append(a.Items, v...) }

// At 读取元素，越界返回 undefined。
func (a *Array) At(i int) any {
	if a == nil || i < 0 || i >= len(a.Items) {
		return nil
	}
	return a.Items[i]
}

// Copy 浅拷贝，对应 [...a]。
func (a *Array) Copy() *Array {
	if a == nil {
		return &Array{}
	}
	return &Array{Items: append([]any(nil), a.Items...)}
}

// Strings 把元素逐个按 String() 转换。
func (a *Array) Strings() []string {
	if a == nil {
		return nil
	}
	out := make([]string, len(a.Items))
	for i, v := range a.Items {
		out[i] = ToString(v)
	}
	return out
}

// Join 对应 Array.prototype.join。
func (a *Array) Join(sep string) string {
	if a == nil {
		return ""
	}
	parts := make([]string, len(a.Items))
	for i, v := range a.Items {
		if IsNullish(v) {
			parts[i] = ""
		} else {
			parts[i] = ToString(v)
		}
	}
	return strings.Join(parts, sep)
}

// IsArray 对应 Array.isArray。
func IsArray(v any) bool { a, ok := v.(*Array); return ok && a != nil }

// AsArray 若是数组则返回，否则 nil。
func AsArray(v any) *Array {
	if a, ok := v.(*Array); ok {
		return a
	}
	return nil
}

// AsObject 若是普通对象则返回，否则 nil。
func AsObject(v any) *Object {
	if o, ok := v.(*Object); ok {
		return o
	}
	return nil
}

// IsObject 对应 `v && typeof v === 'object' && !Array.isArray(v)`。
func IsObject(v any) bool { o, ok := v.(*Object); return ok && o != nil }

// IsObjectLike 对应 `typeof v === 'object' && v !== null`（含数组）。
func IsObjectLike(v any) bool {
	switch x := v.(type) {
	case *Object:
		return x != nil
	case *Array:
		return x != nil
	case *RegExp:
		return x != nil
	}
	return false
}

// TypeOf 对应 typeof 运算符。
func TypeOf(v any) string {
	switch v.(type) {
	case nil:
		return "undefined"
	case string:
		return "string"
	case float64, int:
		return "number"
	case bool:
		return "boolean"
	}
	return "object"
}

// IsString 对应 typeof v === 'string'。
func IsString(v any) bool { _, ok := v.(string); return ok }

// IsNumber 对应 typeof v === 'number'。
func IsNumber(v any) bool {
	switch v.(type) {
	case float64, int:
		return true
	}
	return false
}

// Truthy 对应 JS 的布尔转换。
func Truthy(v any) bool {
	switch x := v.(type) {
	case nil, nullType:
		return false
	case bool:
		return x
	case string:
		return x != ""
	case float64:
		return x != 0 && !math.IsNaN(x)
	case int:
		return x != 0
	case *Object:
		return x != nil
	case *Array:
		return x != nil
	case *RegExp:
		return x != nil
	}
	return true
}

// Or 对应 a || b：返回第一个真值，否则返回最后一个。
func Or(vals ...any) any {
	for i, v := range vals {
		if Truthy(v) || i == len(vals)-1 {
			return v
		}
	}
	return nil
}

// Coalesce 对应 a ?? b。
func Coalesce(vals ...any) any {
	for i, v := range vals {
		if !IsNullish(v) || i == len(vals)-1 {
			return v
		}
	}
	return nil
}

// StrictEquals 对应 ===。
func StrictEquals(a, b any) bool {
	switch x := a.(type) {
	case nil:
		return b == nil
	case nullType:
		return IsNull(b)
	case string:
		y, ok := b.(string)
		return ok && x == y
	case bool:
		y, ok := b.(bool)
		return ok && x == y
	case float64, int:
		if !IsNumber(b) {
			return false
		}
		return toFloat(a) == toFloat(b)
	case *Object:
		y, ok := b.(*Object)
		return ok && x == y
	case *Array:
		y, ok := b.(*Array)
		return ok && x == y
	case *RegExp:
		y, ok := b.(*RegExp)
		return ok && x == y
	}
	return false
}

func toFloat(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	}
	return math.NaN()
}

// ToString 对应 String(v)。
func ToString(v any) string {
	switch x := v.(type) {
	case nil:
		return "undefined"
	case nullType:
		return "null"
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	case float64:
		return NumberToString(x)
	case int:
		return strconv.Itoa(x)
	case *Array:
		return x.Join(",")
	case *Object:
		return "[object Object]"
	case *RegExp:
		return x.String()
	}
	return ""
}

// NumberToString 按 ECMAScript Number::toString 规则格式化数字。
func NumberToString(f float64) string {
	switch {
	case math.IsNaN(f):
		return "NaN"
	case math.IsInf(f, 1):
		return "Infinity"
	case math.IsInf(f, -1):
		return "-Infinity"
	case f == 0:
		return "0"
	}
	sign := ""
	if f < 0 {
		sign = "-"
		f = -f
	}
	e := strconv.FormatFloat(f, 'e', -1, 64)
	mant, expStr, _ := strings.Cut(e, "e")
	digits := strings.Replace(mant, ".", "", 1)
	exp, _ := strconv.Atoi(expStr)
	k := len(digits)
	n := exp + 1
	var out string
	switch {
	case k <= n && n <= 21:
		out = digits + strings.Repeat("0", n-k)
	case 0 < n && n <= 21:
		out = digits[:n] + "." + digits[n:]
	case -6 < n && n <= 0:
		out = "0." + strings.Repeat("0", -n) + digits
	default:
		es := "+"
		if n-1 < 0 {
			es = "-"
		}
		ev := n - 1
		if ev < 0 {
			ev = -ev
		}
		if k == 1 {
			out = digits + "e" + es + strconv.Itoa(ev)
		} else {
			out = digits[:1] + "." + digits[1:] + "e" + es + strconv.Itoa(ev)
		}
	}
	return sign + out
}

// ToNumber 对应 Number(v)。
func ToNumber(v any) float64 {
	switch x := v.(type) {
	case nil:
		return math.NaN()
	case nullType:
		return 0
	case bool:
		if x {
			return 1
		}
		return 0
	case float64:
		return x
	case int:
		return float64(x)
	case string:
		return StringToNumber(x)
	case *Array:
		return StringToNumber(x.Join(","))
	}
	return math.NaN()
}

// StringToNumber 对应字符串到数字的转换（Number("...")）。
func StringToNumber(s string) float64 {
	s = Trim(s)
	if s == "" {
		return 0
	}
	if len(s) > 2 && s[0] == '0' {
		base := 0
		switch s[1] {
		case 'x', 'X':
			base = 16
		case 'o', 'O':
			base = 8
		case 'b', 'B':
			base = 2
		}
		if base != 0 {
			var r float64
			for _, c := range s[2:] {
				d := digitVal(c)
				if d < 0 || d >= base {
					return math.NaN()
				}
				r = r*float64(base) + float64(d)
			}
			return r
		}
	}
	switch s {
	case "Infinity", "+Infinity":
		return math.Inf(1)
	case "-Infinity":
		return math.Inf(-1)
	}
	if !isDecimalLiteral(s) {
		return math.NaN()
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		// 溢出时 ParseFloat 返回 ±Inf 和错误，JS 同样得到 Infinity
		if ne, ok := err.(*strconv.NumError); ok && ne.Err == strconv.ErrRange {
			return f
		}
		return math.NaN()
	}
	return f
}

// isDecimalLiteral 校验 StrDecimalLiteral 语法（不含 Infinity）。
func isDecimalLiteral(s string) bool {
	i := 0
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	intDigits := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
		intDigits++
	}
	fracDigits := 0
	if i < len(s) && s[i] == '.' {
		i++
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
			fracDigits++
		}
	}
	if intDigits == 0 && fracDigits == 0 {
		return false
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		i++
		if i < len(s) && (s[i] == '+' || s[i] == '-') {
			i++
		}
		expDigits := 0
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
			expDigits++
		}
		if expDigits == 0 {
			return false
		}
	}
	return i == len(s)
}

func digitVal(c rune) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'z':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'Z':
		return int(c-'A') + 10
	}
	return -1
}

// ParseInt 对应全局 parseInt(v, radix)；radix 为 0 表示未指定。
func ParseInt(v any, radix int) float64 {
	s := TrimStart(ToString(v))
	sign := 1.0
	if s != "" && (s[0] == '+' || s[0] == '-') {
		if s[0] == '-' {
			sign = -1
		}
		s = s[1:]
	}
	stripPrefix := true
	if radix != 0 {
		if radix < 2 || radix > 36 {
			return math.NaN()
		}
		if radix != 16 {
			stripPrefix = false
		}
	} else {
		radix = 10
	}
	if stripPrefix && len(s) >= 2 && s[0] == '0' && (s[1] == 'x' || s[1] == 'X') {
		s = s[2:]
		radix = 16
	}
	end := 0
	for end < len(s) {
		d := digitVal(rune(s[end]))
		if d < 0 || d >= radix {
			break
		}
		end++
	}
	if end == 0 {
		return math.NaN()
	}
	digits := s[:end]
	if radix == 10 {
		f, err := strconv.ParseFloat(digits, 64)
		if err != nil && f == 0 {
			return math.NaN()
		}
		return sign * f
	}
	var r float64
	for _, c := range digits {
		r = r*float64(radix) + float64(digitVal(c))
	}
	return sign * r
}

// ParseFloat 对应全局 parseFloat(v)。
func ParseFloat(v any) float64 {
	s := TrimStart(ToString(v))
	for _, p := range []string{"Infinity", "+Infinity", "-Infinity"} {
		if strings.HasPrefix(s, p) {
			if p[0] == '-' {
				return math.Inf(-1)
			}
			return math.Inf(1)
		}
	}
	// 取最长的合法十进制前缀
	best := ""
	i := 0
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	j := i
	for j < len(s) && s[j] >= '0' && s[j] <= '9' {
		j++
	}
	intEnd := j
	if j < len(s) && s[j] == '.' {
		k := j + 1
		for k < len(s) && s[k] >= '0' && s[k] <= '9' {
			k++
		}
		if k > j+1 || intEnd > i {
			j = k
		}
	}
	if j == i || (j == i+1 && s[i] == '.') {
		return math.NaN()
	}
	best = s[:j]
	if j < len(s) && (s[j] == 'e' || s[j] == 'E') {
		k := j + 1
		if k < len(s) && (s[k] == '+' || s[k] == '-') {
			k++
		}
		m := k
		for m < len(s) && s[m] >= '0' && s[m] <= '9' {
			m++
		}
		if m > k {
			best = s[:m]
		}
	}
	f, err := strconv.ParseFloat(best, 64)
	if err != nil {
		if ne, ok := err.(*strconv.NumError); ok && ne.Err == strconv.ErrRange {
			return f
		}
		return math.NaN()
	}
	return f
}

// IsInteger 对应 Number.isInteger。
func IsInteger(v any) bool {
	if !IsNumber(v) {
		return false
	}
	f := toFloat(v)
	return !math.IsInf(f, 0) && !math.IsNaN(f) && math.Trunc(f) == f
}

// IsFinite 对应 Number.isFinite（不做类型转换）。
func IsFinite(v any) bool {
	if !IsNumber(v) {
		return false
	}
	f := toFloat(v)
	return !math.IsInf(f, 0) && !math.IsNaN(f)
}

// IsNaN 对应全局 isNaN（会做数字转换）。
func IsNaN(v any) bool { return math.IsNaN(ToNumber(v)) }

// MathRound 对应 Math.round。
func MathRound(f float64) float64 {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return f
	}
	return math.Floor(f + 0.5)
}
