package kernelconf

import (
	"errors"
	"fmt"
	"math"
	"net"
	"sort"
	"strconv"
	"strings"
)

// 驱动模板：结构化的 YAML 值，渲染成内核配置（JSON）。写法：
//
//	"${表达式}"            整个字符串只有一个表达式时保留原类型（数字、列表、对象）；否则按文本拼接
//	key?: 值               值为空（null、""、0、false、空列表、空对象）时省略这个键
//	{$if: 条件, $then: 值, $else: 值}   没有 $else 且条件不成立时省略（作为键值时省略键，作为列表元素时丢弃）
//	{$each: 列表, $as: 名称, $do: 值}   对每个元素渲染 $do，得到列表；放在列表里时展开进外层列表
//	{$use: 模板名, $with: {名称: 值}}   渲染另一个具名模板（在 templates 下）
//	{$merge: [对象, ...]}              依次合并多个对象，后面的覆盖前面的
//
// 表达式：路径（in.clients、c.password）、字面量（'x'、1、true、null、[a, b]）、
// 比较（== != < <= > >=）、in、not、and、or、括号，以及过滤器 值 | 名称(参数)。
// 模板只能读到引擎提供的变量，不能调用任意函数，也不能读文件或联网。

// omitted 表示这个值被省略（与 null 不同）。
type omittedT struct{}

var omitted = omittedT{}

// spread 是 $each（或带 $spread 的 $use）的结果：放进列表时展开进外层列表，作为键值时就是普通列表。
type spread []any

type tmplEnv struct {
	templates map[string]any
	vars      map[string]any
	depth     int
}

const maxDepth = 64

func (e *tmplEnv) with(name string, v any) *tmplEnv {
	vars := make(map[string]any, len(e.vars)+1)
	for k, x := range e.vars {
		vars[k] = x
	}
	vars[name] = v
	return &tmplEnv{templates: e.templates, vars: vars, depth: e.depth}
}

// renderTemplate 渲染具名模板（入口），结果里不留展开标记。
func renderTemplate(templates map[string]any, name string, vars map[string]any) (any, error) {
	t, ok := templates[name]
	if !ok {
		return nil, fmt.Errorf("没有模板 %q", name)
	}
	e := &tmplEnv{templates: templates, vars: vars}
	v, err := e.render(t)
	if err != nil {
		return nil, err
	}
	if v == omitted {
		return nil, fmt.Errorf("模板 %s 没有生成内容", name)
	}
	return plain(v), nil
}

// render 渲染一个模板值。
func (e *tmplEnv) render(node any) (any, error) {
	if e.depth > maxDepth {
		return nil, errors.New("模板嵌套过深（可能循环引用）")
	}
	switch n := node.(type) {
	case string:
		return e.interpolate(n)
	case []any:
		out := []any{}
		for _, item := range n {
			v, err := e.render(item)
			if err != nil {
				return nil, err
			}
			if v == omitted {
				continue
			}
			if s, ok := v.(spread); ok {
				out = append(out, s...)
				continue
			}
			out = append(out, v)
		}
		return out, nil
	case map[string]any:
		return e.renderMap(n)
	default:
		return n, nil
	}
}

// plain 把展开标记换回普通列表（作为键值或最终结果时）。
func plain(v any) any {
	if s, ok := v.(spread); ok {
		return []any(s)
	}
	return v
}

func (e *tmplEnv) renderMap(n map[string]any) (any, error) {
	if c, ok := n["$if"]; ok {
		if err := onlyKeys(n, "$if", "$then", "$else"); err != nil {
			return nil, err
		}
		v, err := e.expr(c)
		if err != nil {
			return nil, err
		}
		if truthy(v) {
			return e.render(n["$then"])
		}
		if alt, ok := n["$else"]; ok {
			return e.render(alt)
		}
		return omitted, nil
	}
	if list, ok := n["$each"]; ok {
		if err := onlyKeys(n, "$each", "$as", "$do"); err != nil {
			return nil, err
		}
		name, _ := n["$as"].(string)
		if name == "" {
			return nil, errors.New("$each 需要 $as")
		}
		v, err := e.expr(list)
		if err != nil {
			return nil, err
		}
		items, err := asList(v)
		if err != nil {
			return nil, fmt.Errorf("$each：%w", err)
		}
		out := spread{}
		for _, item := range items {
			r, err := e.with(name, item).render(n["$do"])
			if err != nil {
				return nil, err
			}
			if r == omitted {
				continue
			}
			if inner, ok := r.(spread); ok {
				out = append(out, inner...)
				continue
			}
			out = append(out, r)
		}
		return out, nil
	}
	if name, ok := n["$use"]; ok {
		if err := onlyKeys(n, "$use", "$with", "$spread"); err != nil {
			return nil, err
		}
		raw, _ := name.(string)
		nv, err := e.interpolate(raw)
		if err != nil {
			return nil, err
		}
		s := text(nv)
		t, ok := e.templates[s]
		if !ok {
			return nil, fmt.Errorf("没有模板 %q", s)
		}
		sub := &tmplEnv{templates: e.templates, vars: e.vars, depth: e.depth + 1}
		if w, ok := n["$with"].(map[string]any); ok {
			for k, x := range w {
				v, err := e.render(x)
				if err != nil {
					return nil, err
				}
				sub = sub.with(k, v)
			}
		}
		v, err := sub.render(t)
		if err != nil {
			return nil, fmt.Errorf("%s：%w", s, err)
		}
		if sp, _ := n["$spread"].(bool); sp {
			if list, ok := plain(v).([]any); ok {
				return spread(list), nil
			}
		}
		return v, nil
	}
	if parts, ok := n["$merge"]; ok {
		if err := onlyKeys(n, "$merge"); err != nil {
			return nil, err
		}
		list, ok := parts.([]any)
		if !ok {
			return nil, errors.New("$merge 需要列表")
		}
		out := map[string]any{}
		for _, p := range list {
			v, err := e.render(p)
			if err != nil {
				return nil, err
			}
			if v == omitted || v == nil {
				continue
			}
			m, ok := plain(v).(map[string]any)
			if !ok {
				return nil, errors.New("$merge 的每一项都要是对象")
			}
			for k, x := range m {
				out[k] = x
			}
		}
		return out, nil
	}
	out := map[string]any{}
	keys := make([]string, 0, len(n))
	for k := range n {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if strings.HasPrefix(k, "$") {
			return nil, fmt.Errorf("未知指令 %s", k)
		}
		optional := strings.HasSuffix(k, "?")
		key := strings.TrimSuffix(k, "?")
		v, err := e.render(n[k])
		if err != nil {
			return nil, fmt.Errorf("%s：%w", key, err)
		}
		v = plain(v)
		if v == omitted || optional && empty(v) {
			continue
		}
		out[key] = v
	}
	return out, nil
}

func onlyKeys(m map[string]any, allowed ...string) error {
	for k := range m {
		ok := false
		for _, a := range allowed {
			ok = ok || k == a
		}
		if !ok {
			return fmt.Errorf("%s 不能与 %s 写在一起", k, allowed[0])
		}
	}
	return nil
}

// expr 计算 $if / $each 的条件或列表：字符串按表达式求值（可以带或不带 ${}），其他值原样。
func (e *tmplEnv) expr(node any) (any, error) {
	s, ok := node.(string)
	if !ok {
		return e.render(node)
	}
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "${") && strings.HasSuffix(s, "}") && strings.Count(s, "${") == 1 {
		s = s[2 : len(s)-1]
	}
	return evalExpr(s, e.vars)
}

// interpolate 处理字符串里的 ${}：整个字符串就是一个表达式时返回原类型。
func (e *tmplEnv) interpolate(s string) (any, error) {
	if !strings.Contains(s, "${") {
		return s, nil
	}
	if strings.HasPrefix(s, "${") && strings.HasSuffix(s, "}") && strings.Count(s, "${") == 1 && strings.Index(s, "}") == len(s)-1 {
		return evalExpr(s[2:len(s)-1], e.vars)
	}
	var b strings.Builder
	rest := s
	for {
		i := strings.Index(rest, "${")
		if i < 0 {
			b.WriteString(rest)
			break
		}
		b.WriteString(rest[:i])
		j := strings.Index(rest[i:], "}")
		if j < 0 {
			return nil, fmt.Errorf("%q 缺少 }", s)
		}
		v, err := evalExpr(rest[i+2:i+j], e.vars)
		if err != nil {
			return nil, err
		}
		b.WriteString(text(v))
		rest = rest[i+j+1:]
	}
	return b.String(), nil
}

func empty(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return x == ""
	case bool:
		return !x
	case int:
		return x == 0
	case int64:
		return x == 0
	case float64:
		return x == 0
	case []any:
		return len(x) == 0
	case spread:
		return len(x) == 0
	case map[string]any:
		return len(x) == 0
	}
	return false
}

func truthy(v any) bool { return !empty(v) }

func text(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case float64:
		if x == math.Trunc(x) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	}
	return fmt.Sprint(v)
}

func asList(v any) ([]any, error) {
	switch x := v.(type) {
	case nil:
		return nil, nil
	case []any:
		return x, nil
	case spread:
		return []any(x), nil
	case []string:
		out := make([]any, len(x))
		for i, s := range x {
			out[i] = s
		}
		return out, nil
	case []map[string]any:
		out := make([]any, len(x))
		for i, m := range x {
			out[i] = m
		}
		return out, nil
	}
	return nil, fmt.Errorf("不是列表（%T）", v)
}

// —— 表达式 ——

type tok struct {
	kind string // id num str op
	val  string
}

func lex(s string) ([]tok, error) {
	var out []tok
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n':
			i++
		case c == '\'' || c == '"':
			j := strings.IndexByte(s[i+1:], c)
			if j < 0 {
				return nil, errors.New("字符串缺少结束引号")
			}
			out = append(out, tok{"str", s[i+1 : i+1+j]})
			i += j + 2
		case c >= '0' && c <= '9' || c == '-' && i+1 < len(s) && s[i+1] >= '0' && s[i+1] <= '9' && (len(out) == 0 || out[len(out)-1].kind == "op"):
			j := i + 1
			for j < len(s) && (s[j] >= '0' && s[j] <= '9' || s[j] == '.') {
				j++
			}
			out = append(out, tok{"num", s[i:j]})
			i = j
		case c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z':
			j := i + 1
			for j < len(s) && (s[j] == '_' || s[j] == '.' || s[j] >= 'a' && s[j] <= 'z' || s[j] >= 'A' && s[j] <= 'Z' || s[j] >= '0' && s[j] <= '9') {
				j++
			}
			out = append(out, tok{"id", s[i:j]})
			i = j
		default:
			two := ""
			if i+1 < len(s) {
				two = s[i : i+2]
			}
			switch two {
			case "==", "!=", "<=", ">=":
				out = append(out, tok{"op", two})
				i += 2
				continue
			}
			if strings.IndexByte("()[],<>|", c) >= 0 {
				out = append(out, tok{"op", string(c)})
				i++
				continue
			}
			return nil, fmt.Errorf("表达式里有无法识别的字符 %q", c)
		}
	}
	return out, nil
}

type parser struct {
	toks []tok
	pos  int
	vars map[string]any
}

func evalExpr(s string, vars map[string]any) (any, error) {
	toks, err := lex(s)
	if err != nil {
		return nil, fmt.Errorf("%q：%w", s, err)
	}
	p := &parser{toks: toks, vars: vars}
	v, err := p.or()
	if err == nil && p.pos < len(p.toks) {
		err = fmt.Errorf("多余的 %q", p.toks[p.pos].val)
	}
	if err != nil {
		return nil, fmt.Errorf("%q：%w", s, err)
	}
	return v, nil
}

func (p *parser) peek() tok {
	if p.pos < len(p.toks) {
		return p.toks[p.pos]
	}
	return tok{}
}

func (p *parser) eat(kind, val string) bool {
	t := p.peek()
	if t.kind == kind && t.val == val {
		p.pos++
		return true
	}
	return false
}

func (p *parser) or() (any, error) {
	l, err := p.and()
	for err == nil && p.eat("id", "or") {
		var r any
		if r, err = p.and(); err == nil {
			l = truthy(l) || truthy(r)
		}
	}
	return l, err
}

func (p *parser) and() (any, error) {
	l, err := p.not()
	for err == nil && p.eat("id", "and") {
		var r any
		if r, err = p.not(); err == nil {
			l = truthy(l) && truthy(r)
		}
	}
	return l, err
}

func (p *parser) not() (any, error) {
	if p.eat("id", "not") {
		v, err := p.not()
		return !truthy(v), err
	}
	return p.cmp()
}

func (p *parser) cmp() (any, error) {
	l, err := p.pipe()
	if err != nil {
		return nil, err
	}
	t := p.peek()
	if t.kind == "op" && (t.val == "==" || t.val == "!=" || t.val == "<" || t.val == "<=" || t.val == ">" || t.val == ">=") || t.kind == "id" && t.val == "in" {
		p.pos++
		r, err := p.pipe()
		if err != nil {
			return nil, err
		}
		switch t.val {
		case "==":
			return equal(l, r), nil
		case "!=":
			return !equal(l, r), nil
		case "in":
			list, err := asList(r)
			if err != nil {
				return nil, err
			}
			for _, x := range list {
				if equal(l, x) {
					return true, nil
				}
			}
			return false, nil
		}
		a, okA := number(l)
		b, okB := number(r)
		if !okA || !okB {
			return nil, fmt.Errorf("%s 只能比较数字", t.val)
		}
		switch t.val {
		case "<":
			return a < b, nil
		case "<=":
			return a <= b, nil
		case ">":
			return a > b, nil
		}
		return a >= b, nil
	}
	return l, nil
}

func (p *parser) pipe() (any, error) {
	v, err := p.atom()
	for err == nil && p.eat("op", "|") {
		t := p.peek()
		if t.kind != "id" {
			return nil, errors.New("| 后面要写过滤器名")
		}
		p.pos++
		var args []any
		if p.eat("op", "(") {
			for !p.eat("op", ")") {
				a, err := p.or()
				if err != nil {
					return nil, err
				}
				args = append(args, a)
				p.eat("op", ",")
				if p.pos >= len(p.toks) {
					return nil, errors.New("缺少 )")
				}
			}
		}
		v, err = applyFilter(t.val, v, args)
	}
	return v, err
}

func (p *parser) atom() (any, error) {
	t := p.peek()
	switch {
	case t.kind == "op" && t.val == "(":
		p.pos++
		v, err := p.or()
		if err == nil && !p.eat("op", ")") {
			err = errors.New("缺少 )")
		}
		return v, err
	case t.kind == "op" && t.val == "[":
		p.pos++
		out := []any{}
		for !p.eat("op", "]") {
			v, err := p.or()
			if err != nil {
				return nil, err
			}
			out = append(out, v)
			p.eat("op", ",")
			if p.pos >= len(p.toks) {
				return nil, errors.New("缺少 ]")
			}
		}
		return out, nil
	case t.kind == "str":
		p.pos++
		return t.val, nil
	case t.kind == "num":
		p.pos++
		f, err := strconv.ParseFloat(t.val, 64)
		return f, err
	case t.kind == "id":
		p.pos++
		switch t.val {
		case "true":
			return true, nil
		case "false":
			return false, nil
		case "null":
			return nil, nil
		}
		return lookup(p.vars, t.val)
	}
	return nil, errors.New("表达式不完整")
}

// lookup 按点分路径取值；路径中间不存在时返回 null（模板里常用来判断有没有）。
func lookup(vars map[string]any, path string) (any, error) {
	parts := strings.Split(path, ".")
	v, ok := vars[parts[0]]
	if !ok {
		return nil, fmt.Errorf("没有变量 %s", parts[0])
	}
	for _, p := range parts[1:] {
		m, ok := v.(map[string]any)
		if !ok {
			return nil, nil
		}
		v = m[p]
	}
	return v, nil
}

func number(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	}
	return 0, false
}

func equal(a, b any) bool {
	if x, ok := number(a); ok {
		y, ok := number(b)
		return ok && x == y
	}
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return fmt.Sprint(a) == fmt.Sprint(b)
}

// applyFilter 是可用的过滤器；只做字符串、列表与地址的整理，不做 I/O。
func applyFilter(name string, v any, args []any) (any, error) {
	arg := func(i int) any {
		if i < len(args) {
			return args[i]
		}
		return nil
	}
	switch name {
	case "default":
		if empty(v) {
			return arg(0), nil
		}
		return v, nil
	case "trim":
		return strings.TrimSpace(text(v)), nil
	case "lower":
		return strings.ToLower(strings.TrimSpace(text(v))), nil
	case "int":
		if f, ok := number(v); ok {
			return math.Trunc(f), nil
		}
		f, err := strconv.ParseFloat(strings.TrimSpace(text(v)), 64)
		if err != nil {
			return 0.0, nil
		}
		return math.Trunc(f), nil
	case "split":
		// 按逗号拆开、去掉空白与空项（ALPN、SNI 列表）。
		out := []any{}
		for _, p := range strings.Split(text(v), ",") {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
		return out, nil
	case "rules":
		// 分流规则的值：逗号、换行、制表符都算分隔。
		out := []any{}
		for _, p := range strings.FieldsFunc(text(v), func(r rune) bool { return r == ',' || r == '\n' || r == '\r' || r == '\t' }) {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
		return out, nil
	case "lines":
		out := []any{}
		for _, p := range strings.Split(strings.TrimSpace(text(v)), "\n") {
			out = append(out, p)
		}
		return out, nil
	case "cidr":
		// 单个 IP 补上 /32 或 /128；本来就是网段的原样保留。
		conv := func(s string) string {
			if strings.Contains(s, "/") {
				return s
			}
			ip := net.ParseIP(s)
			if ip == nil {
				return s
			}
			if ip.To4() != nil {
				return ip.String() + "/32"
			}
			return ip.String() + "/128"
		}
		if list, err := asList(v); err == nil && list != nil {
			out := make([]any, len(list))
			for i, x := range list {
				out[i] = conv(text(x))
			}
			return out, nil
		}
		return conv(text(v)), nil
	case "join":
		list, err := asList(v)
		if err != nil {
			return nil, err
		}
		parts := make([]string, len(list))
		for i, x := range list {
			parts[i] = text(x)
		}
		return strings.Join(parts, text(arg(0))), nil
	case "list":
		// 单个值包成列表；已经是列表的原样返回；空值得到空列表。
		if empty(v) {
			return []any{}, nil
		}
		if list, err := asList(v); err == nil && list != nil {
			return list, nil
		}
		return []any{v}, nil
	case "map":
		// 取列表里每个对象的某个字段。
		list, err := asList(v)
		if err != nil {
			return nil, err
		}
		key := text(arg(0))
		out := []any{}
		for _, x := range list {
			if m, ok := x.(map[string]any); ok {
				out = append(out, m[key])
			}
		}
		return out, nil
	case "where":
		// 留下某个字段等于给定值的对象。
		list, err := asList(v)
		if err != nil {
			return nil, err
		}
		key := text(arg(0))
		out := []any{}
		for _, x := range list {
			if m, ok := x.(map[string]any); ok && equal(m[key], arg(1)) {
				out = append(out, x)
			}
		}
		return out, nil
	case "len":
		if list, err := asList(v); err == nil {
			return float64(len(list)), nil
		}
		return float64(len(text(v))), nil
	case "flat":
		// 把列表的列表展开成一层。
		list, err := asList(v)
		if err != nil {
			return nil, err
		}
		out := []any{}
		for _, x := range list {
			if inner, err := asList(x); err == nil && inner != nil {
				out = append(out, inner...)
			} else {
				out = append(out, x)
			}
		}
		return out, nil
	case "first":
		// 第一个非空值：值 | first(备选1, 备选2)。
		if !empty(v) {
			return v, nil
		}
		for _, a := range args {
			if !empty(a) {
				return a, nil
			}
		}
		return nil, nil
	}
	return nil, fmt.Errorf("没有过滤器 %s", name)
}
