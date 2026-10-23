package jscompat

import (
	"errors"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf16"
)

// YAMLDumpOptions 对应 js-yaml dump 的选项子集（上游只使用 indent=2、lineWidth=-1、noRefs=true）。
type YAMLDumpOptions struct {
	DoubleQuote bool // quotingType: '"'
	ForceQuotes bool
}

// YAMLDump 移植自 js-yaml 4.1.1 lib/dumper.js，在上述选项下输出与上游逐字节一致。
func YAMLDump(v any, opts YAMLDumpOptions) (string, error) {
	st := &dumpState{indent: 2, double: opts.DoubleQuote, force: opts.ForceQuotes}
	ok, err := st.writeNode(0, v, true, true, false)
	if err != nil {
		return "", err
	}
	if ok {
		return st.dump + "\n", nil
	}
	return "", nil
}

type dumpState struct {
	indent int
	double bool
	force  bool
	tag    string // "" 表示 null，"?" 表示隐式类型
	dump   string
}

var deprecatedBooleans = map[string]bool{
	"y": true, "Y": true, "yes": true, "Yes": true, "YES": true, "on": true, "On": true, "ON": true,
	"n": true, "N": true, "no": true, "No": true, "NO": true, "off": true, "Off": true, "OFF": true,
}

var deprecatedBase60 = regexp.MustCompile(`^[-+]?[0-9_]+(?::[0-9_]+)+(?:\.[0-9_]*)?$`)

var escapeSequences = map[rune]string{
	0x00: `\0`, 0x07: `\a`, 0x08: `\b`, 0x09: `\t`, 0x0A: `\n`, 0x0B: `\v`, 0x0C: `\f`, 0x0D: `\r`,
	0x1B: `\e`, 0x22: `\"`, 0x5C: `\\`, 0x85: `\N`, 0xA0: `\_`, 0x2028: `\L`, 0x2029: `\P`,
}

func generateNextLine(indent, level int) string { return "\n" + strings.Repeat(" ", indent*level) }

func (st *dumpState) writeNode(level int, object any, block, compact, iskey bool) (bool, error) {
	st.tag = ""
	st.dump = ""
	inblock := block
	switch x := object.(type) {
	case nil:
		return false, nil
	case nullType:
		st.tag, st.dump = "?", "null"
		return true, nil
	case bool:
		st.tag = "?"
		if x {
			st.dump = "true"
		} else {
			st.dump = "false"
		}
		return true, nil
	case int:
		st.tag, st.dump = "?", strconv.Itoa(x)
		return true, nil
	case float64:
		st.tag, st.dump = "?", representNumber(x)
		return true, nil
	case Date:
		st.tag, st.dump = "?", x.ISO()
		return true, nil
	case string:
		st.writeScalar(x, level, iskey, inblock)
		return true, nil
	case *Object:
		if x == nil {
			st.tag, st.dump = "?", "null"
			return true, nil
		}
		if block && x.Len() != 0 {
			return true, st.writeBlockMapping(level, x, compact)
		}
		return true, st.writeFlowMapping(level, x)
	case *Array:
		if x == nil {
			st.tag, st.dump = "?", "null"
			return true, nil
		}
		if block && x.Len() != 0 {
			return true, st.writeBlockSequence(level, x, compact)
		}
		return true, st.writeFlowSequence(level, x)
	}
	return false, errors.New("unacceptable kind of an object to dump")
}

func representNumber(f float64) string {
	if f == math.Trunc(f) && !math.IsInf(f, 0) && !(f == 0 && math.Signbit(f)) {
		return NumberToString(f)
	}
	switch {
	case math.IsNaN(f):
		return ".nan"
	case math.IsInf(f, 1):
		return ".inf"
	case math.IsInf(f, -1):
		return "-.inf"
	case f == 0 && math.Signbit(f):
		return "-0.0"
	}
	res := NumberToString(f)
	if scientificWithoutDot.MatchString(res) {
		res = strings.Replace(res, "e", ".e", 1)
	}
	return res
}

var scientificWithoutDot = regexp.MustCompile(`^[-+]?[0-9]+e`)

func (st *dumpState) writeFlowSequence(level int, a *Array) error {
	var result strings.Builder
	for _, v := range a.Items {
		ok, err := st.writeNode(level, v, false, false, false)
		if err != nil {
			return err
		}
		if !ok && v == nil {
			ok, _ = st.writeNode(level, Null, false, false, false)
		}
		if ok {
			if result.Len() > 0 {
				result.WriteString(", ")
			}
			result.WriteString(st.dump)
		}
	}
	st.tag = ""
	st.dump = "[" + result.String() + "]"
	return nil
}

func (st *dumpState) writeBlockSequence(level int, a *Array, compact bool) error {
	var result strings.Builder
	for _, v := range a.Items {
		ok, err := st.writeNode(level+1, v, true, true, false)
		if err != nil {
			return err
		}
		if !ok && v == nil {
			ok, _ = st.writeNode(level+1, Null, true, true, false)
		}
		if ok {
			if !compact || result.Len() > 0 {
				result.WriteString(generateNextLine(st.indent, level))
			}
			if st.dump != "" && st.dump[0] == '\n' {
				result.WriteString("-")
			} else {
				result.WriteString("- ")
			}
			result.WriteString(st.dump)
		}
	}
	st.tag = ""
	if result.Len() == 0 {
		st.dump = "[]"
	} else {
		st.dump = result.String()
	}
	return nil
}

func (st *dumpState) writeFlowMapping(level int, o *Object) error {
	var result strings.Builder
	for _, kv := range o.Entries() {
		var pair strings.Builder
		if result.Len() > 0 {
			pair.WriteString(", ")
		}
		ok, err := st.writeNode(level, kv.Key, false, false, false)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		if UTF16Len(st.dump) > 1024 {
			pair.WriteString("? ")
		}
		pair.WriteString(st.dump + ": ")
		ok, err = st.writeNode(level, kv.Value, false, false, false)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		pair.WriteString(st.dump)
		result.WriteString(pair.String())
	}
	st.tag = ""
	st.dump = "{" + result.String() + "}"
	return nil
}

func (st *dumpState) writeBlockMapping(level int, o *Object, compact bool) error {
	var result strings.Builder
	for _, kv := range o.Entries() {
		var pair strings.Builder
		if !compact || result.Len() > 0 {
			pair.WriteString(generateNextLine(st.indent, level))
		}
		ok, err := st.writeNode(level+1, kv.Key, true, true, true)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		explicitPair := (st.tag != "" && st.tag != "?") || UTF16Len(st.dump) > 1024
		if explicitPair {
			if st.dump != "" && st.dump[0] == '\n' {
				pair.WriteString("?")
			} else {
				pair.WriteString("? ")
			}
		}
		pair.WriteString(st.dump)
		if explicitPair {
			pair.WriteString(generateNextLine(st.indent, level))
		}
		ok, err = st.writeNode(level+1, kv.Value, true, explicitPair, false)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		if st.dump != "" && st.dump[0] == '\n' {
			pair.WriteString(":")
		} else {
			pair.WriteString(": ")
		}
		pair.WriteString(st.dump)
		result.WriteString(pair.String())
	}
	st.tag = ""
	if result.Len() == 0 {
		st.dump = "{}"
	} else {
		st.dump = result.String()
	}
	return nil
}

const (
	stylePlain = iota + 1
	styleSingle
	styleLiteral
	styleFolded
	styleDouble
)

func isPrintable(c rune) bool {
	return (0x20 <= c && c <= 0x7E) ||
		((0xA1 <= c && c <= 0xD7FF) && c != 0x2028 && c != 0x2029) ||
		((0xE000 <= c && c <= 0xFFFD) && c != 0xFEFF) ||
		(0x10000 <= c && c <= 0x10FFFF)
}

func isWhitespaceChar(c rune) bool { return c == ' ' || c == '\t' }

func isNsCharOrWhitespace(c rune) bool {
	return isPrintable(c) && c != 0xFEFF && c != '\r' && c != '\n'
}

func isPlainSafe(c, prev rune, hasPrev, inblock bool) bool {
	cIsNsCharOrWhitespace := isNsCharOrWhitespace(c)
	cIsNsChar := cIsNsCharOrWhitespace && !isWhitespaceChar(c)
	var base bool
	if inblock {
		base = cIsNsCharOrWhitespace
	} else {
		base = cIsNsCharOrWhitespace && c != ',' && c != '[' && c != ']' && c != '{' && c != '}'
	}
	prevColon := hasPrev && prev == ':'
	return (base && c != '#' && !(prevColon && !cIsNsChar)) ||
		(hasPrev && isNsCharOrWhitespace(prev) && !isWhitespaceChar(prev) && c == '#') ||
		(prevColon && cIsNsChar)
}

func isPlainSafeFirst(c rune) bool {
	return isPrintable(c) && c != 0xFEFF && !isWhitespaceChar(c) &&
		!strings.ContainsRune("-?:,[]{}#&*!|=>'\"%@`", c)
}

func isPlainSafeLast(c rune) bool { return !isWhitespaceChar(c) && c != ':' }

// testImplicitResolving 判断字符串是否会被解析成其他隐式类型（null/bool/int/float/timestamp/merge）。
func testImplicitResolving(s string) bool {
	if s == "~" || s == "null" || s == "Null" || s == "NULL" {
		return true
	}
	if resolveYAMLBool(s) || resolveYAMLInt(s) || resolveYAMLFloat(s) {
		return true
	}
	if yamlDatePattern.MatchString(s) || yamlTimestampPattern.MatchString(s) {
		return true
	}
	return s == "<<"
}

func chooseScalarStyle(s string, singleLineOnly, forceQuotes, inblock bool, double bool) int {
	runes := []rune(s)
	plain := isPlainSafeFirst(runes[0]) && isPlainSafeLast(runes[len(runes)-1])
	hasLineBreak := false
	var prev rune
	hasPrev := false
	if singleLineOnly || forceQuotes {
		for _, c := range runes {
			if !isPrintable(c) {
				return styleDouble
			}
			plain = plain && isPlainSafe(c, prev, hasPrev, inblock)
			prev, hasPrev = c, true
		}
	} else {
		for _, c := range runes {
			if c == '\n' {
				hasLineBreak = true
			} else if !isPrintable(c) {
				return styleDouble
			}
			plain = plain && isPlainSafe(c, prev, hasPrev, inblock)
			prev, hasPrev = c, true
		}
	}
	if !hasLineBreak {
		if plain && !forceQuotes && !testImplicitResolving(s) {
			return stylePlain
		}
		if double {
			return styleDouble
		}
		return styleSingle
	}
	if !forceQuotes {
		return styleLiteral
	}
	if double {
		return styleDouble
	}
	return styleSingle
}

func (st *dumpState) writeScalar(s string, level int, iskey, inblock bool) {
	st.dump = func() string {
		if s == "" {
			if st.double {
				return `""`
			}
			return "''"
		}
		if deprecatedBooleans[s] || deprecatedBase60.MatchString(s) {
			if st.double {
				return `"` + s + `"`
			}
			return "'" + s + "'"
		}
		indent := st.indent * max(1, level)
		switch chooseScalarStyle(s, iskey, st.force && !iskey, inblock, st.double) {
		case stylePlain:
			return s
		case styleSingle:
			return "'" + strings.ReplaceAll(s, "'", "''") + "'"
		case styleLiteral:
			return "|" + blockHeader(s, st.indent) + dropEndingNewline(indentString(s, indent))
		default:
			return `"` + escapeString(s) + `"`
		}
	}()
}

func blockHeader(s string, indentPerLevel int) string {
	indicator := ""
	if needIndentIndicator(s) {
		indicator = strconv.Itoa(indentPerLevel)
	}
	clip := strings.HasSuffix(s, "\n")
	keep := clip && (strings.HasSuffix(s, "\n\n") || s == "\n")
	chomp := "-"
	if keep {
		chomp = "+"
	} else if clip {
		chomp = ""
	}
	return indicator + chomp + "\n"
}

func needIndentIndicator(s string) bool {
	return strings.HasPrefix(strings.TrimLeft(s, "\n"), " ")
}

func dropEndingNewline(s string) string { return strings.TrimSuffix(s, "\n") }

func indentString(s string, spaces int) string {
	ind := strings.Repeat(" ", spaces)
	var sb strings.Builder
	pos := 0
	for pos < len(s) {
		next := strings.IndexByte(s[pos:], '\n')
		var line string
		if next < 0 {
			line = s[pos:]
			pos = len(s)
		} else {
			line = s[pos : pos+next+1]
			pos += next + 1
		}
		if line != "" && line != "\n" {
			sb.WriteString(ind)
		}
		sb.WriteString(line)
	}
	return sb.String()
}

func escapeString(s string) string {
	var sb strings.Builder
	for _, c := range s {
		if seq, ok := escapeSequences[c]; ok {
			sb.WriteString(seq)
			continue
		}
		if isPrintable(c) {
			sb.WriteRune(c)
			continue
		}
		sb.WriteString(encodeHex(c))
	}
	return sb.String()
}

func encodeHex(c rune) string {
	if c >= 0x10000 {
		// JS 字符串中补充平面字符是代理对，此处仅在不可打印时出现（实际不会发生）
		hi, lo := utf16.EncodeRune(c)
		return encodeHex(hi) + encodeHex(lo)
	}
	h := strings.ToUpper(strconv.FormatInt(int64(c), 16))
	if c <= 0xFF {
		return `\x` + strings.Repeat("0", 2-len(h)) + h
	}
	return `\u` + strings.Repeat("0", 4-len(h)) + h
}
