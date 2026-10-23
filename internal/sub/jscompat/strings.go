package jscompat

import (
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// isJSSpace 判断是否属于 JS 的 WhiteSpace 或 LineTerminator（String.prototype.trim 使用的集合）。
func isJSSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0xA0, 0x1680, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000, 0xFEFF:
		return true
	}
	return r >= 0x2000 && r <= 0x200A
}

// IsJSSpace 导出给其他包使用。
func IsJSSpace(r rune) bool { return isJSSpace(r) }

// Trim 对应 String.prototype.trim。
func Trim(s string) string { return strings.TrimFunc(s, isJSSpace) }

// TrimStart 对应 String.prototype.trimStart。
func TrimStart(s string) string { return strings.TrimLeftFunc(s, isJSSpace) }

// TrimEnd 对应 String.prototype.trimEnd。
func TrimEnd(s string) string { return strings.TrimRightFunc(s, isJSSpace) }

var (
	upperCaser = cases.Upper(language.Und)
	lowerCaser = cases.Lower(language.Und)
)

// ToUpper 对应 String.prototype.toUpperCase（完整大小写映射，如 ß → SS）。
func ToUpper(s string) string {
	if isASCII(s) {
		return strings.ToUpper(s)
	}
	return upperCaser.String(s)
}

// ToLower 对应 String.prototype.toLowerCase。
func ToLower(s string) string {
	if isASCII(s) {
		return strings.ToLower(s)
	}
	return lowerCaser.String(s)
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// UTF16Len 返回字符串在 JS 中的 length（UTF-16 码元数）。
func UTF16Len(s string) int {
	n := 0
	for _, r := range s {
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// PadStart 对应 String.prototype.padStart（按 UTF-16 长度计算）。
func PadStart(s string, width int, pad string) string {
	l := UTF16Len(s)
	if width <= l || pad == "" {
		return s
	}
	need := width - l
	units := utf16.Encode([]rune(pad))
	var fill []uint16
	for len(fill) < need {
		fill = append(fill, units...)
	}
	fill = fill[:need]
	return string(utf16.Decode(fill)) + s
}

// CharAtUpperFirst 对应 `s.charAt(0).toUpperCase() + s.slice(1)`。
func CharAtUpperFirst(s string) string {
	if s == "" {
		return ""
	}
	r, size := utf8.DecodeRuneInString(s)
	return ToUpper(string(r)) + s[size:]
}

// Repeat 对应 String.prototype.repeat。
func Repeat(s string, n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat(s, n)
}

// SplitN 对应 String.prototype.split(sep, limit)，limit<0 表示不限制。
func SplitN(s, sep string, limit int) []string {
	var parts []string
	if sep == "" {
		for _, r := range s {
			parts = append(parts, string(r))
		}
	} else {
		parts = strings.Split(s, sep)
	}
	if limit >= 0 && len(parts) > limit {
		parts = parts[:limit]
	}
	return parts
}

// IsHanChar 判断是否在 [一-龥] 范围内（上游常用的中文判断区间）。
func IsHanChar(r rune) bool { return r >= 0x4e00 && r <= 0x9fa5 }

// ContainsHan 对应 /[一-龥]/.test(s)。
func ContainsHan(s string) bool {
	for _, r := range s {
		if IsHanChar(r) {
			return true
		}
	}
	return false
}

// IsASCIILetter 判断单个字符是否为 ASCII 字母。
func IsASCIILetter(r rune) bool { return r < 0x80 && unicode.IsLetter(r) }

// ReplaceString 对应 str.replace(searchString, replacementString)：只替换第一次出现，
// 替换串中的 $$、$&、$`、$' 按 JS 规则展开。
func ReplaceString(str, search, replacement string) string {
	idx := strings.Index(str, search)
	if idx < 0 {
		return str
	}
	m := &Match{Index: idx, End: idx + len(search), Groups: []string{search}, Has: []bool{true}, Input: str}
	return str[:idx] + expandReplacement(replacement, m) + str[idx+len(search):]
}

// Slice 对应 String.prototype.slice（按 UTF-16 码元计算，支持负数下标）。
func Slice(s string, start, end int) string {
	n := UTF16Len(s)
	norm := func(i int) int {
		if i < 0 {
			i += n
			if i < 0 {
				return 0
			}
		}
		if i > n {
			return n
		}
		return i
	}
	start, end = norm(start), norm(end)
	if start >= end {
		return ""
	}
	return Substring(s, start, end)
}

// IndexUTF16 把字节偏移换算为 UTF-16 下标；-1 原样返回。
func IndexUTF16(s string, byteIdx int) int {
	if byteIdx < 0 {
		return byteIdx
	}
	return UTF16Len(s[:byteIdx])
}

// Substring 对应 String.prototype.substring（按 UTF-16 码元计算下标）。
func Substring(s string, start, end int) string {
	if isASCII(s) {
		n := len(s)
		start, end = clampSub(start, n), clampSub(end, n)
		if start > end {
			start, end = end, start
		}
		return s[start:end]
	}
	units := utf16.Encode([]rune(s))
	n := len(units)
	start, end = clampSub(start, n), clampSub(end, n)
	if start > end {
		start, end = end, start
	}
	return string(utf16.Decode(units[start:end]))
}

func clampSub(i, n int) int {
	if i < 0 {
		return 0
	}
	if i > n {
		return n
	}
	return i
}
