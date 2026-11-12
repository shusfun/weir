package jscompat

import (
	"errors"
	"regexp"
	"strings"
)

// NewConfigRegExp uses RE2 for administrator and subscription supplied
// patterns. The ECMAScript engine remains limited to built-in literals.
// Replacement syntax retains $1, $<name>, $&, $$ and prefix/suffix tokens.
func NewConfigRegExp(pattern, flags string) (*RegExp, error) {
	if len(pattern) > 8192 || len(flags) > 5 {
		return nil, ErrRegExpSyntax
	}
	r := &RegExp{Source: pattern, Flags: flags, unicode: true}
	seen := map[rune]bool{}
	var modes strings.Builder
	for _, flag := range flags {
		if seen[flag] {
			return nil, ErrRegExpSyntax
		}
		seen[flag] = true
		switch flag {
		case 'g':
			r.Global = true
		case 'u':
		case 'i', 'm', 's':
			modes.WriteRune(flag)
		default:
			return nil, ErrRegExpSyntax
		}
	}
	compiled := pattern
	if modes.Len() > 0 {
		compiled = "(?" + modes.String() + ")" + compiled
	}
	var err error
	r.re2, err = regexp.Compile(compiled)
	if err != nil {
		return nil, ErrRegExpSyntax
	}
	for i, name := range r.re2.SubexpNames() {
		if name == "" {
			continue
		}
		if r.names == nil {
			r.names = map[string]int{}
		}
		if _, exists := r.names[name]; exists {
			return nil, ErrRegExpSyntax
		}
		r.names[name] = i
	}
	return r, nil
}

func (r *RegExp) configMatch(s string, indices []int) *Match {
	if len(indices) == 0 {
		return nil
	}
	m := &Match{Index: indices[0], End: indices[1], Input: s, Names: r.names, Groups: make([]string, len(indices)/2), Has: make([]bool, len(indices)/2)}
	for i := range m.Groups {
		if indices[2*i] >= 0 {
			m.Has[i] = true
			m.Groups[i] = s[indices[2*i]:indices[2*i+1]]
		}
	}
	return m
}

func (r *RegExp) execConfig(s string) *Match {
	if !r.Global {
		return r.MatchFirst(s)
	}
	start := 0
	if r.Global {
		start = r.LastIndex
	}
	if start < 0 || start > len(s) {
		r.LastIndex = 0
		return nil
	}
	// Match against the complete input so anchors keep their original meaning.
	for _, indices := range r.re2.FindAllStringSubmatchIndex(s, -1) {
		if indices[0] < start {
			continue
		}
		m := r.configMatch(s, indices)
		if r.Global {
			r.LastIndex = m.End
		}
		return m
	}
	if r.Global {
		r.LastIndex = 0
	}
	return nil
}

var ErrRegExpOutputLimit = errors.New("regexp_output_limit")

type limitedReplacement struct {
	strings.Builder
	limit int
	err   error
}

func (b *limitedReplacement) WriteString(s string) {
	if b.err != nil {
		return
	}
	if len(s) > b.limit-b.Len() {
		b.err = ErrRegExpOutputLimit
		return
	}
	b.Builder.WriteString(s)
}

// WriteByte 满足 io.ByteWriter；超限只记在 b.err 上，调用方结束时统一检查。
func (b *limitedReplacement) WriteByte(ch byte) error {
	if b.err != nil {
		return b.err
	}
	if b.Len() >= b.limit {
		b.err = ErrRegExpOutputLimit
		return b.err
	}
	return b.Builder.WriteByte(ch)
}

// ReplaceBounded limits both capture-index allocations and replacement growth.
// It accepts only the RE2 engine used for configurable expressions.
func (r *RegExp) ReplaceBounded(s, replacement string, maxBytes int) (string, error) {
	if r.re2 == nil {
		return "", ErrRegExpSyntax
	}
	if maxBytes < 1 || len(s) > maxBytes || len(replacement) > maxBytes {
		return "", ErrRegExpOutputLimit
	}
	limit := 1
	if r.Global {
		r.LastIndex = 0
		limit = max(1, (1<<20)/(2*(r.re2.NumSubexp()+1)))
	}
	indices := r.re2.FindAllStringSubmatchIndex(s, limit+1)
	if r.Global && len(indices) > limit {
		return "", ErrRegExpOutputLimit
	}
	if !r.Global && len(indices) > 1 {
		indices = indices[:1]
	}
	out := limitedReplacement{limit: maxBytes}
	pos := 0
	for _, index := range indices {
		m := r.configMatch(s, index)
		out.WriteString(s[pos:m.Index])
		if out.err != nil {
			return "", out.err
		}
		replacement, err := expandReplacementBounded(replacement, m, maxBytes-out.Len())
		if err != nil {
			return "", err
		}
		out.WriteString(replacement)
		pos = m.End
	}
	out.WriteString(s[pos:])
	if out.err != nil {
		return "", out.err
	}
	return out.String(), nil
}
