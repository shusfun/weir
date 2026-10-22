package jscompat

import (
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/dlclark/regexp2"
)

// RegExp 模拟 JS RegExp 对象（基于 regexp2 的 ECMAScript 模式），
// 包含 lastIndex 状态，以保持 g / y 标志下 test/exec 的行为。
//
// 与 JS 一致：不带 u 标志的正则按 UTF-16 码元匹配（例如 [^a] 会分别匹配代理对的两半），
// 带 u 标志的正则按码点匹配。
type RegExp struct {
	Source    string
	Flags     string
	re        *regexp2.Regexp
	Global    bool
	Sticky    bool
	unicode   bool
	LastIndex int // 以匹配单位（码元或码点）计
	names     map[string]int
}

// 匹配超时：JS 没有超时，这里为防止恶意正则拖垮服务而设置上限，超时按未匹配处理。
const regexTimeout = 2 * time.Second

// ErrRegExpSyntax 对应 new RegExp 抛出的 SyntaxError。
var ErrRegExpSyntax = errors.New("SyntaxError: invalid regular expression")

var regexCache sync.Map // key: source + "\x00" + flags

func (r *RegExp) copyFresh() *RegExp {
	return &RegExp{Source: r.Source, Flags: r.Flags, re: r.re, Global: r.Global, Sticky: r.Sticky, unicode: r.unicode, names: r.names}
}

// NewRegExp 对应 new RegExp(pattern, flags)；语法或标志非法时返回错误。
func NewRegExp(pattern, flags string) (*RegExp, error) {
	key := pattern + "\x00" + flags
	if cached, ok := regexCache.Load(key); ok {
		return cached.(*RegExp).copyFresh(), nil
	}
	opts := regexp2.RegexOptions(regexp2.ECMAScript)
	seen := map[rune]bool{}
	r := &RegExp{Source: pattern, Flags: flags}
	for _, f := range flags {
		if seen[f] {
			return nil, ErrRegExpSyntax
		}
		seen[f] = true
		switch f {
		case 'g':
			r.Global = true
		case 'i':
			opts |= regexp2.IgnoreCase
		case 'm':
			opts |= regexp2.Multiline
		case 's':
			opts |= regexp2.Singleline
		case 'u', 'v':
			opts |= regexp2.Unicode
			r.unicode = true
		case 'y':
			r.Sticky = true
		case 'd':
		default:
			return nil, ErrRegExpSyntax
		}
	}
	if seen['u'] && seen['v'] {
		return nil, ErrRegExpSyntax
	}
	if err := checkJSOnlySyntax(pattern); err != nil {
		return nil, err
	}
	translated, names, err := rewriteNamedGroups(translateJSPattern(pattern))
	if err != nil {
		return nil, err
	}
	if !r.unicode {
		translated = escapeAstralAsSurrogates(translated)
	}
	re, err := regexp2.Compile(translated, opts)
	if err != nil {
		return nil, ErrRegExpSyntax
	}
	re.MatchTimeout = regexTimeout
	r.re = re
	r.names = names
	regexCache.Store(key, r.copyFresh())
	return r, nil
}

// regexp2 无法可靠处理码元值落在 D800–DFFF 的 rune，因此非 u 模式下把代理码元映射到
// 第 15 平面私用区（U+F0000 起），模式与被匹配字符串同步映射，结果再还原。
const surrogateBase = 0xF0000

func mapUnit(u rune) rune {
	if u >= 0xD800 && u <= 0xDFFF {
		return surrogateBase + (u - 0xD800)
	}
	return u
}

func unmapUnit(r rune) rune {
	if r >= surrogateBase && r <= surrogateBase+0x7FF {
		return 0xD800 + (r - surrogateBase)
	}
	return r
}

// escapeAstralAsSurrogates 把模式中的补充平面字符与 \uD8xx 形式的代理转义改写为映射后的码元，
// 使非 u 模式按 UTF-16 码元匹配。
func escapeAstralAsSurrogates(p string) string {
	if !hasAstral(p) && !strings.Contains(p, `\u`) {
		return p
	}
	rs := []rune(p)
	var sb strings.Builder
	for i := 0; i < len(rs); i++ {
		c := rs[i]
		if c == '\\' && i+1 < len(rs) {
			if rs[i+1] == 'u' && i+5 < len(rs) {
				if v, err := strconv.ParseUint(string(rs[i+2:i+6]), 16, 32); err == nil && v >= 0xD800 && v <= 0xDFFF {
					sb.WriteRune(mapUnit(rune(v)))
					i += 5
					continue
				}
			}
			sb.WriteRune(c)
			sb.WriteRune(rs[i+1])
			i++
			continue
		}
		if c >= 0x10000 {
			hi, lo := utf16.EncodeRune(c)
			sb.WriteRune(mapUnit(hi))
			sb.WriteRune(mapUnit(lo))
			continue
		}
		sb.WriteRune(c)
	}
	return sb.String()
}

func hasAstral(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0xF0 {
			return true
		}
	}
	return false
}

// rewriteNamedGroups 把命名分组改写为普通分组，使分组编号与 JS 一致（.NET 会把命名分组排在最后）。
func rewriteNamedGroups(p string) (string, map[string]int, error) {
	if !strings.Contains(p, "(?<") {
		return p, nil, nil
	}
	var sb strings.Builder
	names := map[string]int{}
	count := 0
	inClass := false
	for i := 0; i < len(p); i++ {
		c := p[i]
		if c == '\\' && i+1 < len(p) {
			sb.WriteByte(c)
			sb.WriteByte(p[i+1])
			i++
			continue
		}
		if inClass {
			if c == ']' {
				inClass = false
			}
			sb.WriteByte(c)
			continue
		}
		if c == '[' {
			inClass = true
			sb.WriteByte(c)
			continue
		}
		if c == '(' {
			rest := p[i+1:]
			switch {
			case strings.HasPrefix(rest, "?<=") || strings.HasPrefix(rest, "?<!"):
			case strings.HasPrefix(rest, "?<"):
				end := strings.IndexByte(rest, '>')
				if end < 0 {
					return "", nil, ErrRegExpSyntax
				}
				name := rest[2:end]
				if name == "" {
					return "", nil, ErrRegExpSyntax
				}
				if _, dup := names[name]; dup {
					return "", nil, ErrRegExpSyntax
				}
				count++
				names[name] = count
				sb.WriteByte('(')
				i += end + 1
				continue
			case strings.HasPrefix(rest, "?"):
			default:
				count++
			}
		}
		sb.WriteByte(c)
	}
	out := sb.String()
	if len(names) == 0 {
		return out, nil, nil
	}
	// \k<name> 改写为数字反向引用
	if strings.Contains(out, `\k<`) {
		for name, idx := range names {
			out = strings.ReplaceAll(out, `\k<`+name+`>`, `(?:\`+strconv.Itoa(idx)+`)`)
		}
	}
	return out, names, nil
}

// Clone 复制一个独立的 RegExp（lastIndex 归零），用于在共享常量上执行有状态的 exec 循环。
func (r *RegExp) Clone() *RegExp { return r.copyFresh() }

// MustRegExp 用于上游写死的正则字面量。
func MustRegExp(pattern, flags string) *RegExp {
	r, err := NewRegExp(pattern, flags)
	if err != nil {
		panic("invalid builtin regexp: " + pattern + ": " + err.Error())
	}
	return r
}

// checkJSOnlySyntax 拒绝 .NET 支持但 JS 不支持的分组语法，使两边对非法正则的判断一致。
func checkJSOnlySyntax(p string) error {
	inClass := false
	for i := 0; i < len(p); i++ {
		c := p[i]
		if c == '\\' {
			i++
			continue
		}
		if inClass {
			if c == ']' {
				inClass = false
			}
			continue
		}
		if c == '[' {
			inClass = true
			continue
		}
		if c == '(' && i+1 < len(p) && p[i+1] == '?' {
			rest := p[i+2:]
			switch {
			case strings.HasPrefix(rest, ":"), strings.HasPrefix(rest, "="), strings.HasPrefix(rest, "!"),
				strings.HasPrefix(rest, "<="), strings.HasPrefix(rest, "<!"):
			case strings.HasPrefix(rest, "<"):
				// 命名分组 (?<name>...)
			default:
				return ErrRegExpSyntax
			}
		}
	}
	return nil
}

// translateJSPattern 处理 regexp2 与 JS 在少数语法上的差异。
func translateJSPattern(p string) string {
	// JS 中 [^] 匹配任意字符，[] 永不匹配；regexp2 需要显式写法
	if !strings.Contains(p, "[]") && !strings.Contains(p, "[^]") {
		return p
	}
	var sb strings.Builder
	inClass := false
	for i := 0; i < len(p); i++ {
		c := p[i]
		if c == '\\' && i+1 < len(p) {
			sb.WriteByte(c)
			sb.WriteByte(p[i+1])
			i++
			continue
		}
		if !inClass && c == '[' {
			if strings.HasPrefix(p[i:], "[^]") {
				sb.WriteString(`[\s\S]`)
				i += 2
				continue
			}
			if strings.HasPrefix(p[i:], "[]") {
				sb.WriteString(`(?!)`)
				i++
				continue
			}
			inClass = true
		} else if inClass && c == ']' {
			inClass = false
		}
		sb.WriteByte(c)
	}
	return sb.String()
}

// Match 是一次匹配结果。Index / End 为 Go 字符串的字节偏移（落在代理对中间时取近似位置）。
type Match struct {
	Index  int      // 匹配起点（字节）
	End    int      // 匹配终点（字节）
	Groups []string // 0 为整体匹配；未参与匹配的分组为 ""
	Has    []bool   // 分组是否参与匹配（区分 undefined 与空串）
	Names  map[string]int
	Input  string
	// 以匹配单位计的位置，供替换 / 分割使用
	uStart, uEnd int
}

// Group 返回第 i 个分组，未参与匹配时返回 nil（undefined）。
func (m *Match) Group(i int) any {
	if i < 0 || i >= len(m.Groups) || !m.Has[i] {
		return nil
	}
	return m.Groups[i]
}

// Str 返回第 i 个分组字符串（undefined 视为空串）。
func (m *Match) Str(i int) string {
	if i < 0 || i >= len(m.Groups) {
		return ""
	}
	return m.Groups[i]
}

// subject 是被匹配字符串在匹配单位下的表示。
type subject struct {
	s      string
	units  []rune // 码点或 UTF-16 码元
	utf16  bool
	byteAt []int // units 下标 → 字节偏移
}

func (r *RegExp) newSubject(s string) *subject {
	sub := &subject{s: s}
	if r.unicode || !hasAstral(s) {
		sub.units = []rune(s)
	} else {
		sub.utf16 = true
		for _, u := range utf16.Encode([]rune(s)) {
			sub.units = append(sub.units, mapUnit(rune(u)))
		}
	}
	return sub
}

func (sub *subject) offset(i int) int {
	if sub.byteAt == nil {
		sub.byteAt = make([]int, 0, len(sub.units)+1)
		pos := 0
		for k := 0; k < len(sub.units); k++ {
			sub.byteAt = append(sub.byteAt, pos)
			u := unmapUnit(sub.units[k])
			if sub.utf16 && u >= 0xD800 && u <= 0xDBFF && k+1 < len(sub.units) && unmapUnit(sub.units[k+1]) >= 0xDC00 && unmapUnit(sub.units[k+1]) <= 0xDFFF {
				// 代理对的低位与高位共享同一个起点之后的位置
				sub.byteAt = append(sub.byteAt, pos)
				pos += 4
				k++
				continue
			}
			if sub.utf16 && u >= 0xD800 && u <= 0xDFFF {
				pos += 3 // 孤立代理在 Go 中按 U+FFFD 输出
				continue
			}
			pos += utf8.RuneLen(u)
		}
		sub.byteAt = append(sub.byteAt, pos)
	}
	if i >= len(sub.byteAt) {
		return len(sub.s)
	}
	return sub.byteAt[i]
}

// text 把 units[a:b] 还原为字符串；孤立代理变为 U+FFFD。
func (sub *subject) text(a, b int) string {
	if a >= b {
		return ""
	}
	if !sub.utf16 {
		return string(sub.units[a:b])
	}
	u16 := make([]uint16, b-a)
	for i, u := range sub.units[a:b] {
		u16[i] = uint16(unmapUnit(u))
	}
	return string(utf16.Decode(u16))
}

func (r *RegExp) convert(m *regexp2.Match, sub *subject) *Match {
	groups := m.Groups()
	maxNum := 0
	for _, n := range r.re.GetGroupNumbers() {
		if n > maxNum {
			maxNum = n
		}
	}
	out := &Match{Groups: make([]string, maxNum+1), Has: make([]bool, maxNum+1), Input: sub.s, Names: r.names}
	for gi := range groups {
		g := &groups[gi]
		num := r.re.GroupNumberFromName(g.Name)
		if num < 0 || num > maxNum {
			continue
		}
		if len(g.Captures) > 0 {
			out.Groups[num] = sub.text(g.Index, g.Index+g.Length)
			out.Has[num] = true
		}
	}
	out.uStart, out.uEnd = m.Index, m.Index+m.Length
	out.Index = sub.offset(out.uStart)
	out.End = sub.offset(out.uEnd)
	return out
}

func (r *RegExp) find(sub *subject, start int) *regexp2.Match {
	if start > len(sub.units) {
		return nil
	}
	m, err := r.re.FindRunesMatchStartingAt(sub.units, start)
	if err != nil || m == nil {
		return nil
	}
	if r.Sticky && m.Index != start {
		return nil
	}
	return m
}

// Exec 对应 RegExp.prototype.exec（遵循 lastIndex 语义），未匹配返回 nil。
func (r *RegExp) Exec(s string) *Match {
	sub := r.newSubject(s)
	start := 0
	if r.Global || r.Sticky {
		start = r.LastIndex
		if start > len(sub.units) {
			r.LastIndex = 0
			return nil
		}
	}
	m := r.find(sub, start)
	if m == nil {
		if r.Global || r.Sticky {
			r.LastIndex = 0
		}
		return nil
	}
	if r.Global || r.Sticky {
		r.LastIndex = m.Index + m.Length
	}
	return r.convert(m, sub)
}

// Test 对应 RegExp.prototype.test。
func (r *RegExp) Test(s string) bool { return r.Exec(s) != nil }

// MatchFirst 对应非全局的 String.prototype.match（忽略 lastIndex，从头匹配一次）。
func (r *RegExp) MatchFirst(s string) *Match {
	sub := r.newSubject(s)
	m := r.find(sub, 0)
	if m == nil {
		return nil
	}
	return r.convert(m, sub)
}

func (r *RegExp) findAll(sub *subject) []*Match {
	var out []*Match
	pos := 0
	for pos <= len(sub.units) {
		m := r.find(sub, pos)
		if m == nil {
			break
		}
		out = append(out, r.convert(m, sub))
		next := m.Index + m.Length
		if m.Length == 0 {
			next++
		}
		pos = next
	}
	return out
}

// FindAll 返回所有不重叠匹配（与 g 标志下的 matchAll/replace 迭代一致）。
func (r *RegExp) FindAll(s string) []*Match { return r.findAll(r.newSubject(s)) }

// MatchAllStrings 对应带 g 标志的 String.prototype.match，返回所有整体匹配；无匹配返回 nil。
func (r *RegExp) MatchAllStrings(s string) []string {
	ms := r.FindAll(s)
	if len(ms) == 0 {
		return nil
	}
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.Groups[0]
	}
	return out
}

// Search 对应 String.prototype.search，返回字节偏移，未找到为 -1。
func (r *RegExp) Search(s string) int {
	m := r.MatchFirst(s)
	if m == nil {
		return -1
	}
	return m.Index
}

// Replace 对应 String.prototype.replace(regex, replacement)，支持 $1 / $& / $<name> 等替换语法。
func (r *RegExp) Replace(s, replacement string) string {
	return r.ReplaceFunc(s, func(m *Match) string { return expandReplacement(replacement, m) })
}

// ReplaceFunc 对应 replace(regex, fn)。
func (r *RegExp) ReplaceFunc(s string, fn func(*Match) string) string {
	sub := r.newSubject(s)
	var ms []*Match
	if r.Global {
		ms = r.findAll(sub)
		if r.LastIndex != 0 {
			r.LastIndex = 0
		}
	} else if m := r.find(sub, 0); m != nil {
		ms = []*Match{r.convert(m, sub)}
	}
	if len(ms) == 0 {
		return s
	}
	var sb strings.Builder
	pos := 0
	for _, m := range ms {
		sb.WriteString(sub.text(pos, m.uStart))
		sb.WriteString(fn(m))
		pos = m.uEnd
	}
	sb.WriteString(sub.text(pos, len(sub.units)))
	return sb.String()
}

// expandReplacement 实现 GetSubstitution。
func expandReplacement(repl string, m *Match) string {
	if strings.IndexByte(repl, '$') < 0 {
		return repl
	}
	var out strings.Builder
	n := len(m.Groups) - 1
	for i := 0; i < len(repl); i++ {
		c := repl[i]
		if c != '$' || i+1 >= len(repl) {
			out.WriteByte(c)
			continue
		}
		nx := repl[i+1]
		switch {
		case nx == '$':
			out.WriteByte('$')
			i++
		case nx == '&':
			out.WriteString(m.Groups[0])
			i++
		case nx == '`':
			out.WriteString(m.Input[:m.Index])
			i++
		case nx == '\'':
			out.WriteString(m.Input[m.End:])
			i++
		case nx == '<':
			if m.Names == nil {
				out.WriteByte('$')
				continue
			}
			end := strings.IndexByte(repl[i+2:], '>')
			if end < 0 {
				out.WriteByte('$')
				continue
			}
			name := repl[i+2 : i+2+end]
			if idx, ok := m.Names[name]; ok {
				out.WriteString(m.Str(idx))
			}
			i += end + 2
		case nx >= '0' && nx <= '9':
			d1 := int(nx - '0')
			if i+2 < len(repl) && repl[i+2] >= '0' && repl[i+2] <= '9' {
				d2 := d1*10 + int(repl[i+2]-'0')
				if d2 >= 1 && d2 <= n {
					out.WriteString(m.Str(d2))
					i += 2
					continue
				}
			}
			if d1 >= 1 && d1 <= n {
				out.WriteString(m.Str(d1))
				i++
				continue
			}
			out.WriteByte('$')
		default:
			out.WriteByte('$')
		}
	}
	return out.String()
}

// Split 对应 String.prototype.split(regex, limit)，捕获分组会被插入结果；limit<0 表示不限。
// 未参与匹配的分组按空串返回，需要区分 undefined 时使用 SplitValues。
func (r *RegExp) Split(s string, limit int) []string {
	vals := r.SplitValues(s, limit)
	out := make([]string, len(vals))
	for i, v := range vals {
		if str, ok := v.(string); ok {
			out[i] = str
		}
	}
	return out
}

// SplitValues 与 Split 相同，但未参与匹配的捕获分组返回 nil（undefined）。
func (r *RegExp) SplitValues(s string, limit int) []any {
	if limit == 0 {
		return []any{}
	}
	sub := r.newSubject(s)
	if len(sub.units) == 0 {
		if m, _ := r.re.FindRunesMatchStartingAt(sub.units, 0); m != nil && m.Index == 0 && m.Length == 0 {
			return []any{}
		}
		return []any{""}
	}
	var out []any
	p := 0
	q := p
	for q < len(sub.units) {
		m, _ := r.re.FindRunesMatchStartingAt(sub.units, q)
		if m == nil {
			break
		}
		e := m.Index + m.Length
		if m.Length == 0 && m.Index >= len(sub.units) {
			break
		}
		if e == p {
			q = m.Index + 1
			continue
		}
		out = append(out, sub.text(p, m.Index))
		if limit >= 0 && len(out) == limit {
			return out
		}
		cm := r.convert(m, sub)
		for gi := 1; gi < len(cm.Groups); gi++ {
			out = append(out, cm.Group(gi))
			if limit >= 0 && len(out) == limit {
				return out
			}
		}
		p = e
		q = p
		if m.Length == 0 {
			q = p + 1
		}
	}
	out = append(out, sub.text(p, len(sub.units)))
	if limit >= 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// String 对应 RegExp.prototype.toString。
func (r *RegExp) String() string { return "/" + r.Source + "/" + r.Flags }

// RuneLen 返回字符串的 rune 数（用于与 lastIndex 比较）。
func RuneLen(s string) int { return utf8.RuneCountInString(s) }
