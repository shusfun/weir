package jscompat

import (
	"errors"
	"math"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// ErrJSONSyntax 对应 JSON.parse 抛出的 SyntaxError。
var ErrJSONSyntax = errors.New("SyntaxError: invalid JSON")

// JSONParse 对应 JSON.parse，返回 *Object / *Array / string / float64 / bool / Null。
func JSONParse(s string) (any, error) {
	p := &jsonParser{s: s}
	p.ws()
	v, err := p.value()
	if err != nil {
		return nil, err
	}
	p.ws()
	if p.i != len(p.s) {
		return nil, ErrJSONSyntax
	}
	return v, nil
}

type jsonParser struct {
	s string
	i int
}

func (p *jsonParser) ws() {
	for p.i < len(p.s) {
		switch p.s[p.i] {
		case ' ', '\t', '\n', '\r':
			p.i++
		default:
			return
		}
	}
}

func (p *jsonParser) value() (any, error) {
	if p.i >= len(p.s) {
		return nil, ErrJSONSyntax
	}
	switch c := p.s[p.i]; {
	case c == '{':
		p.i++
		o := NewObject()
		p.ws()
		if p.i < len(p.s) && p.s[p.i] == '}' {
			p.i++
			return o, nil
		}
		for {
			p.ws()
			if p.i >= len(p.s) || p.s[p.i] != '"' {
				return nil, ErrJSONSyntax
			}
			k, err := p.str()
			if err != nil {
				return nil, err
			}
			p.ws()
			if p.i >= len(p.s) || p.s[p.i] != ':' {
				return nil, ErrJSONSyntax
			}
			p.i++
			p.ws()
			v, err := p.value()
			if err != nil {
				return nil, err
			}
			o.Set(k, v)
			p.ws()
			if p.i >= len(p.s) {
				return nil, ErrJSONSyntax
			}
			if p.s[p.i] == ',' {
				p.i++
				continue
			}
			if p.s[p.i] == '}' {
				p.i++
				return o, nil
			}
			return nil, ErrJSONSyntax
		}
	case c == '[':
		p.i++
		a := &Array{}
		p.ws()
		if p.i < len(p.s) && p.s[p.i] == ']' {
			p.i++
			return a, nil
		}
		for {
			p.ws()
			v, err := p.value()
			if err != nil {
				return nil, err
			}
			a.Push(v)
			p.ws()
			if p.i >= len(p.s) {
				return nil, ErrJSONSyntax
			}
			if p.s[p.i] == ',' {
				p.i++
				continue
			}
			if p.s[p.i] == ']' {
				p.i++
				return a, nil
			}
			return nil, ErrJSONSyntax
		}
	case c == '"':
		return p.str()
	case c == 't':
		return p.lit("true", true)
	case c == 'f':
		return p.lit("false", false)
	case c == 'n':
		return p.lit("null", Null)
	case c == '-' || c >= '0' && c <= '9':
		return p.num()
	}
	return nil, ErrJSONSyntax
}

func (p *jsonParser) lit(word string, v any) (any, error) {
	if strings.HasPrefix(p.s[p.i:], word) {
		p.i += len(word)
		return v, nil
	}
	return nil, ErrJSONSyntax
}

func (p *jsonParser) num() (any, error) {
	start := p.i
	if p.s[p.i] == '-' {
		p.i++
	}
	if p.i >= len(p.s) {
		return nil, ErrJSONSyntax
	}
	if p.s[p.i] == '0' {
		p.i++
	} else if p.s[p.i] >= '1' && p.s[p.i] <= '9' {
		for p.i < len(p.s) && p.s[p.i] >= '0' && p.s[p.i] <= '9' {
			p.i++
		}
	} else {
		return nil, ErrJSONSyntax
	}
	if p.i < len(p.s) && p.s[p.i] == '.' {
		p.i++
		d := p.i
		for p.i < len(p.s) && p.s[p.i] >= '0' && p.s[p.i] <= '9' {
			p.i++
		}
		if p.i == d {
			return nil, ErrJSONSyntax
		}
	}
	if p.i < len(p.s) && (p.s[p.i] == 'e' || p.s[p.i] == 'E') {
		p.i++
		if p.i < len(p.s) && (p.s[p.i] == '+' || p.s[p.i] == '-') {
			p.i++
		}
		d := p.i
		for p.i < len(p.s) && p.s[p.i] >= '0' && p.s[p.i] <= '9' {
			p.i++
		}
		if p.i == d {
			return nil, ErrJSONSyntax
		}
	}
	f, err := strconv.ParseFloat(p.s[start:p.i], 64)
	if err != nil {
		if ne, ok := err.(*strconv.NumError); ok && ne.Err == strconv.ErrRange {
			return f, nil
		}
		return nil, ErrJSONSyntax
	}
	return f, nil
}

func (p *jsonParser) str() (string, error) {
	p.i++ // 跳过开头引号
	var sb strings.Builder
	var pending []uint16
	flush := func() {
		if len(pending) > 0 {
			sb.WriteString(string(utf16.Decode(pending)))
			pending = pending[:0]
		}
	}
	for p.i < len(p.s) {
		c := p.s[p.i]
		switch {
		case c == '"':
			flush()
			p.i++
			return sb.String(), nil
		case c == '\\':
			if p.i+1 >= len(p.s) {
				return "", ErrJSONSyntax
			}
			e := p.s[p.i+1]
			p.i += 2
			if e == 'u' {
				if p.i+4 > len(p.s) {
					return "", ErrJSONSyntax
				}
				v, err := strconv.ParseUint(p.s[p.i:p.i+4], 16, 16)
				if err != nil {
					return "", ErrJSONSyntax
				}
				pending = append(pending, uint16(v))
				p.i += 4
				continue
			}
			flush()
			switch e {
			case '"', '\\', '/':
				sb.WriteByte(e)
			case 'b':
				sb.WriteByte('\b')
			case 'f':
				sb.WriteByte('\f')
			case 'n':
				sb.WriteByte('\n')
			case 'r':
				sb.WriteByte('\r')
			case 't':
				sb.WriteByte('\t')
			default:
				return "", ErrJSONSyntax
			}
		case c < 0x20:
			return "", ErrJSONSyntax
		default:
			flush()
			r, size := utf8.DecodeRuneInString(p.s[p.i:])
			sb.WriteRune(r)
			p.i += size
		}
	}
	return "", ErrJSONSyntax
}

// JSONStringify 对应 JSON.stringify(v)；返回 ok=false 表示结果为 undefined。
func JSONStringify(v any) (string, bool) { return JSONStringifyIndent(v, "") }

// MustStringify 对应 JSON.stringify(v)，undefined 时返回空串。
func MustStringify(v any) string {
	s, _ := JSONStringify(v)
	return s
}

// JSONStringifyIndent 对应 JSON.stringify(v, null, indent)。
func JSONStringifyIndent(v any, indent string) (string, bool) {
	var sb strings.Builder
	ok := writeJSON(&sb, v, indent, "")
	return sb.String(), ok
}

func writeJSON(sb *strings.Builder, v any, indent, cur string) bool {
	switch x := v.(type) {
	case nil:
		return false
	case nullType:
		sb.WriteString("null")
	case bool:
		if x {
			sb.WriteString("true")
		} else {
			sb.WriteString("false")
		}
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			sb.WriteString("null")
		} else {
			sb.WriteString(NumberToString(x))
		}
	case int:
		sb.WriteString(strconv.Itoa(x))
	case string:
		writeJSONString(sb, x)
	case *Array:
		if x == nil {
			sb.WriteString("null")
			return true
		}
		if len(x.Items) == 0 {
			sb.WriteString("[]")
			return true
		}
		inner := cur + indent
		sb.WriteByte('[')
		for i, item := range x.Items {
			if i > 0 {
				sb.WriteByte(',')
			}
			if indent != "" {
				sb.WriteByte('\n')
				sb.WriteString(inner)
			}
			if !writeJSON(sb, item, indent, inner) {
				sb.WriteString("null")
			}
		}
		if indent != "" {
			sb.WriteByte('\n')
			sb.WriteString(cur)
		}
		sb.WriteByte(']')
	case *Object:
		if x == nil {
			sb.WriteString("null")
			return true
		}
		inner := cur + indent
		sb.WriteByte('{')
		n := 0
		for _, kv := range x.Entries() {
			var part strings.Builder
			if !writeJSON(&part, kv.Value, indent, inner) {
				continue
			}
			if n > 0 {
				sb.WriteByte(',')
			}
			if indent != "" {
				sb.WriteByte('\n')
				sb.WriteString(inner)
			}
			writeJSONString(sb, kv.Key)
			sb.WriteByte(':')
			if indent != "" {
				sb.WriteByte(' ')
			}
			sb.WriteString(part.String())
			n++
		}
		if n > 0 && indent != "" {
			sb.WriteByte('\n')
			sb.WriteString(cur)
		}
		sb.WriteByte('}')
	case *RegExp:
		sb.WriteString("{}")
	default:
		return false
	}
	return true
}

func writeJSONString(sb *strings.Builder, s string) {
	sb.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			sb.WriteString(`\"`)
		case '\\':
			sb.WriteString(`\\`)
		case '\b':
			sb.WriteString(`\b`)
		case '\f':
			sb.WriteString(`\f`)
		case '\n':
			sb.WriteString(`\n`)
		case '\r':
			sb.WriteString(`\r`)
		case '\t':
			sb.WriteString(`\t`)
		default:
			if r < 0x20 {
				sb.WriteString(`\u00`)
				sb.WriteByte(hexLower[r>>4])
				sb.WriteByte(hexLower[r&15])
			} else {
				sb.WriteRune(r)
			}
		}
	}
	sb.WriteByte('"')
}

const hexLower = "0123456789abcdef"
