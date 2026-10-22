package jscompat

import (
	"errors"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Date 表示 js-yaml 解析出的时间戳（JS Date）。
type Date struct{ T time.Time }

// ISO 对应 Date.prototype.toISOString。
func (d Date) ISO() string { return d.T.UTC().Format("2006-01-02T15:04:05.000Z") }

// YAMLLoad 对应 js-yaml 4 的 yaml.load(text)（DEFAULT_SCHEMA）。
// 解析树由 yaml.v3 生成，标量类型判定按 js-yaml 规则实现。
func YAMLLoad(text string) (any, error) {
	dec := yaml.NewDecoder(strings.NewReader(text))
	var doc yaml.Node
	err := dec.Decode(&doc)
	if errors.Is(err, io.EOF) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); err == nil {
		return nil, errors.New("expected a single document in the stream, but found more")
	} else if !errors.Is(err, io.EOF) {
		return nil, err
	}
	c := &yamlConverter{anchors: map[*yaml.Node]any{}}
	if len(doc.Content) == 0 {
		return nil, nil
	}
	return c.convert(doc.Content[0])
}

type yamlConverter struct {
	anchors map[*yaml.Node]any
}

func (c *yamlConverter) convert(n *yaml.Node) (any, error) {
	if v, ok := c.anchors[n]; ok {
		return v, nil
	}
	switch n.Kind {
	case yaml.AliasNode:
		return c.convert(n.Alias)
	case yaml.ScalarNode:
		v, err := resolveScalar(n)
		if err != nil {
			return nil, err
		}
		c.anchors[n] = v
		return v, nil
	case yaml.SequenceNode:
		a := &Array{}
		c.anchors[n] = a
		for _, item := range n.Content {
			v, err := c.convert(item)
			if err != nil {
				return nil, err
			}
			a.Push(v)
		}
		return a, nil
	case yaml.MappingNode:
		o := NewObject()
		c.anchors[n] = o
		seen := map[string]bool{}
		for i := 0; i+1 < len(n.Content); i += 2 {
			kn, vn := n.Content[i], n.Content[i+1]
			if kn.Kind == yaml.ScalarNode && kn.Tag == "!!merge" || (kn.Kind == yaml.ScalarNode && kn.Value == "<<" && kn.Style == 0 && kn.Tag != "!!str") {
				if err := c.merge(o, vn, seen); err != nil {
					return nil, err
				}
				continue
			}
			kv, err := c.convert(kn)
			if err != nil {
				return nil, err
			}
			key := yamlKeyString(kv)
			if seen[key] {
				return nil, errors.New("duplicated mapping key")
			}
			seen[key] = true
			v, err := c.convert(vn)
			if err != nil {
				return nil, err
			}
			o.Set(key, v)
		}
		return o, nil
	case yaml.DocumentNode:
		if len(n.Content) == 0 {
			return nil, nil
		}
		return c.convert(n.Content[0])
	}
	return nil, nil
}

// merge 实现 << 合并键：已有键不覆盖。
func (c *yamlConverter) merge(dst *Object, src *yaml.Node, seen map[string]bool) error {
	apply := func(n *yaml.Node) error {
		v, err := c.convert(n)
		if err != nil {
			return err
		}
		o, ok := v.(*Object)
		if !ok {
			return errors.New("cannot merge mappings; the provided source object is unacceptable")
		}
		for _, kv := range o.Entries() {
			if !dst.Has(kv.Key) {
				dst.Set(kv.Key, kv.Value)
			}
		}
		return nil
	}
	target := src
	if target.Kind == yaml.AliasNode {
		target = target.Alias
	}
	if target.Kind == yaml.SequenceNode {
		for _, item := range target.Content {
			if err := apply(item); err != nil {
				return err
			}
		}
		return nil
	}
	return apply(src)
}

func yamlKeyString(v any) string {
	switch x := v.(type) {
	case *Object:
		return "[object Object]"
	case Date:
		return x.T.String()
	}
	return ToString(v)
}

var (
	yamlFloatPattern     = regexp.MustCompile(`^(?:[-+]?(?:[0-9][0-9_]*)(?:\.[0-9_]*)?(?:[eE][-+]?[0-9]+)?|\.[0-9_]+(?:[eE][-+]?[0-9]+)?|[-+]?\.(?:inf|Inf|INF)|\.(?:nan|NaN|NAN))$`)
	yamlDatePattern      = regexp.MustCompile(`^([0-9][0-9][0-9][0-9])-([0-9][0-9])-([0-9][0-9])$`)
	yamlTimestampPattern = regexp.MustCompile(`^([0-9][0-9][0-9][0-9])-([0-9][0-9]?)-([0-9][0-9]?)(?:[Tt]|[ \t]+)([0-9][0-9]?):([0-9][0-9]):([0-9][0-9])(?:\.([0-9]*))?(?:[ \t]*(Z|([-+])([0-9][0-9]?)(?::([0-9][0-9]))?))?$`)
)

func resolveScalar(n *yaml.Node) (any, error) {
	val := n.Value
	tag := n.Tag
	explicit := n.Style&(yaml.TaggedStyle) != 0
	quoted := n.Style&(yaml.DoubleQuotedStyle|yaml.SingleQuotedStyle|yaml.LiteralStyle|yaml.FoldedStyle) != 0
	if !explicit && quoted {
		return val, nil
	}
	if explicit {
		switch tag {
		case "!!str":
			return val, nil
		case "!!null":
			if resolveYAMLNull(val) {
				return Null, nil
			}
		case "!!bool":
			if resolveYAMLBool(val) {
				return constructYAMLBool(val), nil
			}
		case "!!int":
			if resolveYAMLInt(val) {
				return constructYAMLInt(val), nil
			}
		case "!!float":
			if resolveYAMLFloat(val) {
				return constructYAMLFloat(val), nil
			}
		case "!!timestamp":
			if d, ok := constructYAMLTimestamp(val); ok {
				return d, nil
			}
		default:
			return nil, errors.New("unknown tag " + tag)
		}
		return nil, errors.New("cannot resolve a node with !<" + tag + "> explicit tag")
	}
	switch {
	case resolveYAMLNull(val):
		return Null, nil
	case resolveYAMLBool(val):
		return constructYAMLBool(val), nil
	case resolveYAMLInt(val):
		return constructYAMLInt(val), nil
	case resolveYAMLFloat(val):
		return constructYAMLFloat(val), nil
	}
	if d, ok := constructYAMLTimestamp(val); ok {
		return d, nil
	}
	return val, nil
}

func resolveYAMLNull(s string) bool {
	return s == "" || s == "~" || s == "null" || s == "Null" || s == "NULL"
}

func resolveYAMLBool(s string) bool {
	switch s {
	case "true", "True", "TRUE", "false", "False", "FALSE":
		return true
	}
	return false
}

func constructYAMLBool(s string) bool { return s == "true" || s == "True" || s == "TRUE" }

func isHex(c byte) bool { return unhex(c) >= 0 }

func resolveYAMLInt(data string) bool {
	max := len(data)
	if max == 0 {
		return false
	}
	index := 0
	hasDigits := false
	ch := data[index]
	if ch == '-' || ch == '+' {
		index++
		if index >= max {
			return false
		}
		ch = data[index]
	}
	if ch == '0' {
		if index+1 == max {
			return true
		}
		index++
		ch = data[index]
		if ch == 'b' || ch == 'x' || ch == 'o' {
			return validatePrefixedInt(data)
		}
	}
	if ch == '_' {
		return false
	}
	for ; index < max; index++ {
		ch = data[index]
		if ch == '_' {
			continue
		}
		if ch < '0' || ch > '9' {
			return false
		}
		hasDigits = true
	}
	return hasDigits && ch != '_'
}

// validatePrefixedInt 校验 0b / 0x / 0o 前缀整数。
func validatePrefixedInt(data string) bool {
	s := data
	if s[0] == '-' || s[0] == '+' {
		s = s[1:]
	}
	kind := s[1]
	body := s[2:]
	hasDigits := false
	var last byte
	for i := 0; i < len(body); i++ {
		c := body[i]
		last = c
		if c == '_' {
			continue
		}
		switch kind {
		case 'b':
			if c != '0' && c != '1' {
				return false
			}
		case 'x':
			if !isHex(c) {
				return false
			}
		case 'o':
			if c < '0' || c > '7' {
				return false
			}
		}
		hasDigits = true
	}
	return hasDigits && last != '_'
}

func constructYAMLInt(data string) float64 {
	value := strings.ReplaceAll(data, "_", "")
	sign := 1.0
	if value[0] == '-' || value[0] == '+' {
		if value[0] == '-' {
			sign = -1
		}
		value = value[1:]
	}
	if value == "0" {
		return 0 * sign
	}
	if value[0] == '0' && len(value) > 1 {
		switch value[1] {
		case 'b':
			return sign * ParseInt(value[2:], 2)
		case 'x':
			return sign * ParseInt(value[2:], 16)
		case 'o':
			return sign * ParseInt(value[2:], 8)
		}
	}
	return sign * ParseInt(value, 10)
}

func resolveYAMLFloat(s string) bool {
	if !yamlFloatPattern.MatchString(s) || strings.HasSuffix(s, "_") {
		return false
	}
	return true
}

func constructYAMLFloat(data string) float64 {
	value := strings.ToLower(strings.ReplaceAll(data, "_", ""))
	sign := 1.0
	if value[0] == '-' {
		sign = -1
	}
	if value[0] == '+' || value[0] == '-' {
		value = value[1:]
	}
	if value == ".inf" {
		return sign * math.Inf(1)
	}
	if value == ".nan" {
		return math.NaN()
	}
	return sign * ParseFloat(value)
}

func constructYAMLTimestamp(s string) (Date, bool) {
	if m := yamlDatePattern.FindStringSubmatch(s); m != nil {
		y, _ := strconv.Atoi(m[1])
		mo, _ := strconv.Atoi(m[2])
		d, _ := strconv.Atoi(m[3])
		return Date{time.Date(y, time.Month(mo), d, 0, 0, 0, 0, time.UTC)}, true
	}
	m := yamlTimestampPattern.FindStringSubmatch(s)
	if m == nil {
		return Date{}, false
	}
	atoi := func(x string) int { v, _ := strconv.Atoi(x); return v }
	frac := 0
	if m[7] != "" {
		f := m[7]
		if len(f) > 3 {
			f = f[:3]
		}
		for len(f) < 3 {
			f += "0"
		}
		frac = atoi(f)
	}
	t := time.Date(atoi(m[1]), time.Month(atoi(m[2])), atoi(m[3]), atoi(m[4]), atoi(m[5]), atoi(m[6]), frac*int(time.Millisecond), time.UTC)
	if m[9] != "" {
		delta := time.Duration(atoi(m[10])*60+atoi(m[11])) * time.Minute
		if m[9] == "-" {
			delta = -delta
		}
		t = t.Add(-delta)
	}
	return Date{t}, true
}
