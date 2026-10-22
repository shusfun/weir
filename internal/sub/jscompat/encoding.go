package jscompat

import (
	"encoding/base64"
	"errors"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// ErrInvalidCharacter 对应 atob/btoa 抛出的 InvalidCharacterError（消息与 Node 一致）。
var ErrInvalidCharacter = errors.New("Invalid character")

// ErrNotCorrectlyEncoded 对应 atob 在长度非法时抛出的错误。
var ErrNotCorrectlyEncoded = errors.New("The string to be decoded is not correctly encoded.")

// ErrURIMalformed 对应 decodeURIComponent 抛出的 URIError。
var ErrURIMalformed = errors.New("URIError: URI malformed")

// BytesToLatin1 把字节序列转换为 JS 的“二进制字符串”（每个字节对应一个 U+00XX 字符）。
func BytesToLatin1(b []byte) string {
	var sb strings.Builder
	sb.Grow(len(b))
	for _, c := range b {
		sb.WriteRune(rune(c))
	}
	return sb.String()
}

// Latin1ToBytes 把二进制字符串转换回字节；遇到大于 0xFF 的字符返回错误。
func Latin1ToBytes(s string) ([]byte, error) {
	out := make([]byte, 0, len(s))
	for _, r := range s {
		if r > 0xFF {
			return nil, ErrInvalidCharacter
		}
		out = append(out, byte(r))
	}
	return out, nil
}

func isASCIIWhitespace(c byte) bool {
	return c == '\t' || c == '\n' || c == '\f' || c == '\r' || c == ' '
}

// AtobBytes 按 HTML 规范的 forgiving-base64 解码，返回原始字节。
func AtobBytes(s string) ([]byte, error) {
	var sb strings.Builder
	for _, r := range s {
		if r < 0x80 && isASCIIWhitespace(byte(r)) {
			continue
		}
		sb.WriteRune(r)
	}
	data := sb.String()
	// 与 Node（simdutf）的判定顺序一致：先剥离末尾填充，再校验字符，最后校验长度
	padding := 0
	for padding < len(data) && data[len(data)-1-padding] == '=' {
		padding++
	}
	if padding > 2 {
		return nil, ErrInvalidCharacter
	}
	data = data[:len(data)-padding]
	for i := 0; i < len(data); i++ {
		c := data[i]
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '+' || c == '/') {
			return nil, ErrInvalidCharacter
		}
	}
	if len(data)%4 == 1 {
		return nil, ErrNotCorrectlyEncoded
	}
	if padding > 0 && (len(data)+padding)%4 != 0 {
		return nil, ErrInvalidCharacter
	}
	// 规范要求忽略末尾多余的填充位，RawStdEncoding 默认严格，需放宽
	out, err := base64.RawStdEncoding.WithPadding(base64.NoPadding).Strict().DecodeString(data)
	if err == nil {
		return out, nil
	}
	return lenientDecode(data), nil
}

func lenientDecode(data string) []byte {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	var buf uint32
	bits := 0
	out := make([]byte, 0, len(data)*3/4)
	for i := 0; i < len(data); i++ {
		buf = buf<<6 | uint32(strings.IndexByte(alphabet, data[i]))
		bits += 6
		if bits >= 8 {
			bits -= 8
			out = append(out, byte(buf>>uint(bits)))
		}
	}
	return out
}

// Atob 对应 atob()，返回二进制字符串。
func Atob(s string) (string, error) {
	b, err := AtobBytes(s)
	if err != nil {
		return "", err
	}
	return BytesToLatin1(b), nil
}

// Btoa 对应 btoa()；输入必须是二进制字符串。
func Btoa(s string) (string, error) {
	b, err := Latin1ToBytes(s)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

// BtoaBytes 对字节做标准 Base64 编码。
func BtoaBytes(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

// UTF8Encode 对应 TextEncoder().encode 与 `unescape(encodeURIComponent(s))` 的字节结果。
func UTF8Encode(s string) []byte { return []byte(s) }

// UTF8ToLatin1 对应 `unescape(encodeURIComponent(s))`：把 UTF-8 字节作为二进制字符串。
func UTF8ToLatin1(s string) string { return BytesToLatin1([]byte(s)) }

// TextDecode 对应 new TextDecoder('utf-8').decode(bytes)：去除 BOM，非法序列按 WHATWG 规则替换为 U+FFFD。
func TextDecode(b []byte) string {
	if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		b = b[3:]
	}
	return utf8DecodeReplace(b)
}

// utf8DecodeReplace 实现 WHATWG UTF-8 解码器（按“最大子部分”替换非法序列）。
func utf8DecodeReplace(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	var sb strings.Builder
	var cp rune
	need, seen := 0, 0
	lower, upper := byte(0x80), byte(0xBF)
	for i := 0; i < len(b); {
		c := b[i]
		if need == 0 {
			i++
			switch {
			case c <= 0x7F:
				sb.WriteByte(c)
			case c >= 0xC2 && c <= 0xDF:
				need, cp = 1, rune(c&0x1F)
			case c >= 0xE0 && c <= 0xEF:
				if c == 0xE0 {
					lower = 0xA0
				}
				if c == 0xED {
					upper = 0x9F
				}
				need, cp = 2, rune(c&0xF)
			case c >= 0xF0 && c <= 0xF4:
				if c == 0xF0 {
					lower = 0x90
				}
				if c == 0xF4 {
					upper = 0x8F
				}
				need, cp = 3, rune(c&0x7)
			default:
				sb.WriteRune(utf8.RuneError)
			}
			continue
		}
		if c < lower || c > upper {
			cp, need, seen = 0, 0, 0
			lower, upper = 0x80, 0xBF
			sb.WriteRune(utf8.RuneError)
			continue // 不消费当前字节
		}
		lower, upper = 0x80, 0xBF
		cp = cp<<6 | rune(c&0x3F)
		seen++
		i++
		if seen == need {
			sb.WriteRune(cp)
			cp, need, seen = 0, 0, 0
		}
	}
	if need != 0 {
		sb.WriteRune(utf8.RuneError)
	}
	return sb.String()
}

// DecodeUTF8Bytes 是 TextDecode 的别名，便于阅读。
func DecodeUTF8Bytes(b []byte) string { return TextDecode(b) }

// Base64DecodeUTF8 对应 `new TextDecoder().decode(Uint8Array.from(atob(s), c => c.charCodeAt(0)))`。
func Base64DecodeUTF8(s string) (string, error) {
	b, err := AtobBytes(s)
	if err != nil {
		return "", err
	}
	return TextDecode(b), nil
}

// Base64EncodeUTF8 对应 `btoa(unescape(encodeURIComponent(s)))`。
func Base64EncodeUTF8(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

const hexUpper = "0123456789ABCDEF"

func isUnreservedComponent(c byte) bool {
	return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' ||
		strings.IndexByte("-_.!~*'()", c) >= 0
}

// EncodeURIComponent 对应 encodeURIComponent。
func EncodeURIComponent(s string) string {
	return encodeWith(s, func(c byte) bool { return isUnreservedComponent(c) })
}

// EncodeURI 对应 encodeURI。
func EncodeURI(s string) string {
	return encodeWith(s, func(c byte) bool { return isUnreservedComponent(c) || strings.IndexByte(";/?:@&=+$,#", c) >= 0 })
}

func encodeWith(s string, keep func(byte) bool) string {
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x80 && keep(c) {
			sb.WriteByte(c)
			continue
		}
		sb.WriteByte('%')
		sb.WriteByte(hexUpper[c>>4])
		sb.WriteByte(hexUpper[c&15])
	}
	return sb.String()
}

func unhex(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return -1
}

// DecodeURIComponent 对应 decodeURIComponent；非法输入返回 ErrURIMalformed。
func DecodeURIComponent(s string) (string, error) { return decodeURIWith(s, "") }

// DecodeURI 对应 decodeURI。
func DecodeURI(s string) (string, error) { return decodeURIWith(s, ";/?:@&=+$,#") }

// SafeDecodeURIComponent 解码失败时返回原值（上游大量使用的 try/catch 写法）。
func SafeDecodeURIComponent(s string) string {
	d, err := DecodeURIComponent(s)
	if err != nil {
		return s
	}
	return d
}

func decodeURIWith(s, reserved string) (string, error) {
	if strings.IndexByte(s, '%') < 0 {
		return s, nil
	}
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '%' {
			sb.WriteByte(c)
			continue
		}
		if i+2 >= len(s) {
			return "", ErrURIMalformed
		}
		h, l := unhex(s[i+1]), unhex(s[i+2])
		if h < 0 || l < 0 {
			return "", ErrURIMalformed
		}
		b := byte(h<<4 | l)
		start := i
		i += 2
		if b < 0x80 {
			if strings.IndexByte(reserved, b) >= 0 {
				sb.WriteString(s[start : i+1])
			} else {
				sb.WriteByte(b)
			}
			continue
		}
		n := 0
		for mask := byte(0x80); b&mask != 0 && n < 8; mask >>= 1 {
			n++
		}
		if n == 1 || n > 4 {
			return "", ErrURIMalformed
		}
		octets := []byte{b}
		if i+3*(n-1) >= len(s) {
			return "", ErrURIMalformed
		}
		for j := 1; j < n; j++ {
			i++
			if s[i] != '%' {
				return "", ErrURIMalformed
			}
			h, l := unhex(s[i+1]), unhex(s[i+2])
			if h < 0 || l < 0 {
				return "", ErrURIMalformed
			}
			nb := byte(h<<4 | l)
			if nb&0xC0 != 0x80 {
				return "", ErrURIMalformed
			}
			i += 2
			octets = append(octets, nb)
		}
		r, size := utf8.DecodeRune(octets)
		if r == utf8.RuneError || size != len(octets) {
			return "", ErrURIMalformed
		}
		sb.Write(octets)
	}
	return sb.String(), nil
}

// Escape 对应全局 escape()。
func Escape(s string) string {
	var sb strings.Builder
	for _, u := range utf16.Encode([]rune(s)) {
		if u < 0x80 && (isAlnum(byte(u)) || strings.IndexByte("@*_+-./", byte(u)) >= 0) {
			sb.WriteByte(byte(u))
			continue
		}
		if u < 256 {
			sb.WriteByte('%')
			sb.WriteByte(hexUpper[u>>4])
			sb.WriteByte(hexUpper[u&15])
			continue
		}
		sb.WriteString("%u")
		for shift := 12; shift >= 0; shift -= 4 {
			sb.WriteByte(hexUpper[(u>>uint(shift))&15])
		}
	}
	return sb.String()
}

func isAlnum(c byte) bool {
	return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9'
}

// Unescape 对应全局 unescape()。
func Unescape(s string) string {
	units := utf16.Encode([]rune(s))
	out := make([]uint16, 0, len(units))
	for i := 0; i < len(units); i++ {
		u := units[i]
		if u == '%' {
			if i+5 < len(units) && units[i+1] == 'u' {
				v, ok := hexUnits(units[i+2 : i+6])
				if ok {
					out = append(out, uint16(v))
					i += 5
					continue
				}
			}
			if i+2 < len(units) {
				v, ok := hexUnits(units[i+1 : i+3])
				if ok {
					out = append(out, uint16(v))
					i += 2
					continue
				}
			}
		}
		out = append(out, u)
	}
	return string(utf16.Decode(out))
}

func hexUnits(us []uint16) (int, bool) {
	v := 0
	for _, u := range us {
		if u > 0x7F {
			return 0, false
		}
		d := unhex(byte(u))
		if d < 0 {
			return 0, false
		}
		v = v<<4 | d
	}
	return v, true
}

// DecodeURIComponentEscape 对应 `decodeURIComponent(escape(s))`，即把二进制字符串按严格 UTF-8 解码。
func DecodeURIComponentEscape(s string) (string, error) { return DecodeURIComponent(Escape(s)) }
