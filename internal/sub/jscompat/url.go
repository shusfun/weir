package jscompat

import (
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/idna"
)

// ErrInvalidURL 对应 new URL() 抛出的 TypeError。
var ErrInvalidURL = errors.New("TypeError: Invalid URL")

// URL 是 WHATWG URL 标准的实现（与 Node / Workers 的 URL 行为一致），只覆盖上游用到的绝对 URL 解析。
type URL struct {
	Scheme     string
	Username   string
	Password   string
	Host       *string // nil 表示无 host
	Port       *int
	Path       []string
	OpaquePath *string
	Query      *string
	Fragment   *string
}

var specialPorts = map[string]int{"ftp": 21, "file": -1, "http": 80, "https": 443, "ws": 80, "wss": 443}

func isSpecialScheme(s string) bool { _, ok := specialPorts[s]; return ok }

// IsSpecial 判断是否为特殊 scheme。
func (u *URL) IsSpecial() bool { return isSpecialScheme(u.Scheme) }

func inC0Control(c rune) bool { return c <= 0x1F || c > 0x7E }

func inFragmentSet(c rune) bool {
	return inC0Control(c) || c == ' ' || c == '"' || c == '<' || c == '>' || c == '`'
}

func inQuerySet(c rune) bool {
	return inC0Control(c) || c == ' ' || c == '"' || c == '#' || c == '<' || c == '>'
}

func inSpecialQuerySet(c rune) bool { return inQuerySet(c) || c == '\'' }

func inPathSet(c rune) bool { return inQuerySet(c) || c == '?' || c == '`' || c == '{' || c == '}' }

func inUserinfoSet(c rune) bool {
	return inPathSet(c) || c == '/' || c == ':' || c == ';' || c == '=' || c == '@' ||
		(c >= '[' && c <= '^') || c == '|'
}

func percentEncodeRune(sb *strings.Builder, c rune, set func(rune) bool) {
	if !set(c) {
		sb.WriteRune(c)
		return
	}
	var buf [4]byte
	n := utf8.EncodeRune(buf[:], c)
	for _, b := range buf[:n] {
		sb.WriteByte('%')
		sb.WriteByte(hexUpper[b>>4])
		sb.WriteByte(hexUpper[b&15])
	}
}

func percentEncodeString(s string, set func(rune) bool) string {
	var sb strings.Builder
	for _, c := range s {
		percentEncodeRune(&sb, c, set)
	}
	return sb.String()
}

// PercentDecodeBytes 对应 WHATWG 的 percent-decode。
func PercentDecodeBytes(s string) []byte {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '%' && i+2 < len(s) && unhex(s[i+1]) >= 0 && unhex(s[i+2]) >= 0 {
			out = append(out, byte(unhex(s[i+1])<<4|unhex(s[i+2])))
			i += 2
			continue
		}
		out = append(out, c)
	}
	return out
}

func isForbiddenHost(c rune) bool {
	switch c {
	case 0, '\t', '\n', '\r', ' ', '#', '/', ':', '<', '>', '?', '@', '[', '\\', ']', '^', '|':
		return true
	}
	return false
}

func isForbiddenDomain(c rune) bool {
	return isForbiddenHost(c) || c <= 0x1F || c == '%' || c == 0x7F
}

// ParseURL 对应 new URL(input)（不带 base）。
func ParseURL(input string) (*URL, error) {
	input = strings.TrimFunc(input, func(r rune) bool { return r <= 0x20 })
	input = strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, input)
	runes := []rune(input)
	u := &URL{}
	// scheme
	i := 0
	if len(runes) == 0 || !isASCIIAlpha(runes[0]) {
		return nil, ErrInvalidURL
	}
	for i < len(runes) && (isASCIIAlpha(runes[i]) || runes[i] >= '0' && runes[i] <= '9' || runes[i] == '+' || runes[i] == '-' || runes[i] == '.') {
		i++
	}
	if i >= len(runes) || runes[i] != ':' {
		return nil, ErrInvalidURL
	}
	u.Scheme = strings.ToLower(string(runes[:i]))
	i++
	rest := runes[i:]
	special := isSpecialScheme(u.Scheme)
	if u.Scheme == "file" {
		return nil, ErrInvalidURL // 上游不会解析 file URL
	}
	if special {
		// special authority slashes：跳过所有 / 与 \
		j := 0
		for j < len(rest) && (rest[j] == '/' || rest[j] == '\\') {
			j++
		}
		return u.parseAuthority(rest[j:])
	}
	if len(rest) >= 2 && rest[0] == '/' && rest[1] == '/' {
		return u.parseAuthority(rest[2:])
	}
	if len(rest) >= 1 && rest[0] == '/' {
		return u.parsePathOnwards(rest)
	}
	// opaque path
	var sb strings.Builder
	j := 0
	for ; j < len(rest) && rest[j] != '?' && rest[j] != '#'; j++ {
		percentEncodeRune(&sb, rest[j], inC0Control)
	}
	op := sb.String()
	u.OpaquePath = &op
	return u, u.parseQueryFragment(rest[j:])
}

func isASCIIAlpha(r rune) bool { return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' }

func (u *URL) parseAuthority(rs []rune) (*URL, error) {
	special := u.IsSpecial()
	end := 0
	for end < len(rs) {
		c := rs[end]
		if c == '/' || c == '?' || c == '#' || (special && c == '\\') {
			break
		}
		end++
	}
	auth := rs[:end]
	// 最后一个 @ 之前是凭据
	at := -1
	for k := len(auth) - 1; k >= 0; k-- {
		if auth[k] == '@' {
			at = k
			break
		}
	}
	hostPart := auth
	if at >= 0 {
		cred := auth[:at]
		hostPart = auth[at+1:]
		passSeen := false
		var user, pass strings.Builder
		for _, c := range cred {
			if c == ':' && !passSeen {
				passSeen = true
				continue
			}
			if passSeen {
				percentEncodeRune(&pass, c, inUserinfoSet)
			} else {
				percentEncodeRune(&user, c, inUserinfoSet)
			}
		}
		u.Username, u.Password = user.String(), pass.String()
		if len(hostPart) == 0 {
			return nil, ErrInvalidURL
		}
	}
	// host 与 port
	inBrackets := false
	colon := -1
	for k, c := range hostPart {
		if c == '[' {
			inBrackets = true
		} else if c == ']' {
			inBrackets = false
		} else if c == ':' && !inBrackets {
			colon = k
			break
		}
	}
	hostStr := string(hostPart)
	portStr := ""
	hasPort := false
	if colon >= 0 {
		hostStr = string(hostPart[:colon])
		portStr = string(hostPart[colon+1:])
		hasPort = true
		if hostStr == "" {
			return nil, ErrInvalidURL
		}
	}
	if special && hostStr == "" {
		return nil, ErrInvalidURL
	}
	host, err := parseHost(hostStr, !special)
	if err != nil {
		return nil, err
	}
	u.Host = &host
	if hasPort {
		for _, c := range portStr {
			if c < '0' || c > '9' {
				return nil, ErrInvalidURL
			}
		}
		if portStr != "" {
			p, err := strconv.Atoi(strings.TrimLeft(portStr, "0") + "0")
			if err != nil {
				return nil, ErrInvalidURL
			}
			p /= 10
			if len(strings.TrimLeft(portStr, "0")) > 5 || p > 65535 {
				return nil, ErrInvalidURL
			}
			if dp, ok := specialPorts[u.Scheme]; !ok || dp != p {
				u.Port = &p
			}
		}
	}
	return u.parsePathOnwards(rs[end:])
}

func (u *URL) parsePathOnwards(rs []rune) (*URL, error) {
	special := u.IsSpecial()
	end := 0
	for end < len(rs) && rs[end] != '?' && rs[end] != '#' {
		end++
	}
	pathRunes := rs[:end]
	if special || len(pathRunes) > 0 {
		segs := splitPath(pathRunes, special)
		u.Path = []string{}
		for idx, seg := range segs {
			last := idx == len(segs)-1
			lower := strings.ToLower(seg)
			isDouble := lower == ".." || lower == ".%2e" || lower == "%2e." || lower == "%2e%2e"
			isSingle := lower == "." || lower == "%2e"
			switch {
			case isDouble:
				if len(u.Path) > 0 {
					u.Path = u.Path[:len(u.Path)-1]
				}
				if last {
					u.Path = append(u.Path, "")
				}
			case isSingle:
				if last {
					u.Path = append(u.Path, "")
				}
			default:
				u.Path = append(u.Path, percentEncodeString(seg, inPathSet))
			}
		}
	}
	return u, u.parseQueryFragment(rs[end:])
}

// splitPath 把 "/a/b" 拆成 ["a","b"]；special 时 \ 也是分隔符。
func splitPath(rs []rune, special bool) []string {
	if len(rs) == 0 {
		return []string{""}
	}
	var segs []string
	var cur []rune
	start := 0
	if rs[0] == '/' || (special && rs[0] == '\\') {
		start = 1
	}
	for _, c := range rs[start:] {
		if c == '/' || (special && c == '\\') {
			segs = append(segs, string(cur))
			cur = cur[:0]
			continue
		}
		cur = append(cur, c)
	}
	segs = append(segs, string(cur))
	return segs
}

func (u *URL) parseQueryFragment(rs []rune) error {
	if len(rs) == 0 {
		return nil
	}
	if rs[0] == '?' {
		end := 1
		for end < len(rs) && rs[end] != '#' {
			end++
		}
		set := inQuerySet
		if u.IsSpecial() {
			set = inSpecialQuerySet
		}
		q := percentEncodeString(string(rs[1:end]), set)
		u.Query = &q
		rs = rs[end:]
	}
	if len(rs) > 0 && rs[0] == '#' {
		f := percentEncodeString(string(rs[1:]), inFragmentSet)
		u.Fragment = &f
	}
	return nil
}

func parseHost(input string, opaque bool) (string, error) {
	if strings.HasPrefix(input, "[") {
		if !strings.HasSuffix(input, "]") {
			return "", ErrInvalidURL
		}
		addr, ok := parseIPv6(input[1 : len(input)-1])
		if !ok {
			return "", ErrInvalidURL
		}
		return "[" + serializeIPv6(addr) + "]", nil
	}
	if opaque {
		for _, c := range input {
			if isForbiddenHost(c) {
				return "", ErrInvalidURL
			}
		}
		return percentEncodeString(input, inC0Control), nil
	}
	domain := utf8DecodeReplace(PercentDecodeBytes(input))
	ascii, err := domainToASCII(domain)
	if err != nil {
		return "", err
	}
	for _, c := range ascii {
		if isForbiddenDomain(c) {
			return "", ErrInvalidURL
		}
	}
	if endsInNumber(ascii) {
		v4, ok := parseIPv4(ascii)
		if !ok {
			return "", ErrInvalidURL
		}
		return v4, nil
	}
	return ascii, nil
}

var idnaProfile = idna.New(idna.MapForLookup(), idna.Transitional(false), idna.CheckHyphens(false),
	idna.CheckJoiners(true), idna.BidiRule(), idna.StrictDomainName(false), idna.VerifyDNSLength(false))

func domainToASCII(d string) (string, error) {
	if isASCII(d) {
		lower := strings.ToLower(d)
		if !strings.Contains(lower, "xn--") {
			return lower, nil
		}
	}
	out, err := idnaProfile.ToASCII(d)
	if err != nil {
		return "", ErrInvalidURL
	}
	return out, nil
}

func endsInNumber(s string) bool {
	parts := strings.Split(s, ".")
	if parts[len(parts)-1] == "" {
		if len(parts) == 1 {
			return false
		}
		parts = parts[:len(parts)-1]
	}
	last := parts[len(parts)-1]
	if last != "" && strings.Trim(last, "0123456789") == "" {
		return true
	}
	_, ok := parseIPv4Number(last)
	return ok
}

func parseIPv4Number(s string) (uint64, bool) {
	if s == "" {
		return 0, false
	}
	base := 10
	if len(s) >= 2 && (s[:2] == "0x" || s[:2] == "0X") {
		s, base = s[2:], 16
	} else if len(s) >= 2 && s[0] == '0' {
		s, base = s[1:], 8
	}
	if s == "" {
		return 0, true
	}
	v, err := strconv.ParseUint(s, base, 64)
	if err != nil {
		// 超大数字依然算“数字”，但会在后续判定溢出
		for _, c := range s {
			if digitVal(c) < 0 || digitVal(c) >= base {
				return 0, false
			}
		}
		return 1 << 40, true
	}
	return v, true
}

func parseIPv4(s string) (string, bool) {
	parts := strings.Split(s, ".")
	if parts[len(parts)-1] == "" && len(parts) > 1 {
		parts = parts[:len(parts)-1]
	}
	if len(parts) > 4 {
		return "", false
	}
	nums := make([]uint64, len(parts))
	for i, p := range parts {
		n, ok := parseIPv4Number(p)
		if !ok {
			return "", false
		}
		nums[i] = n
	}
	for i := 0; i < len(nums)-1; i++ {
		if nums[i] > 255 {
			return "", false
		}
	}
	last := nums[len(nums)-1]
	limit := uint64(1) << (8 * uint(5-len(nums)))
	if last >= limit {
		return "", false
	}
	ipv4 := last
	for i := 0; i < len(nums)-1; i++ {
		ipv4 += nums[i] << (8 * uint(3-i))
	}
	return strconv.FormatUint(ipv4>>24&255, 10) + "." + strconv.FormatUint(ipv4>>16&255, 10) + "." +
		strconv.FormatUint(ipv4>>8&255, 10) + "." + strconv.FormatUint(ipv4&255, 10), true
}

func parseIPv6(s string) ([8]uint16, bool) {
	var addr [8]uint16
	rs := []rune(s)
	piece, compress := 0, -1
	p := 0
	if p < len(rs) && rs[p] == ':' {
		if p+1 >= len(rs) || rs[p+1] != ':' {
			return addr, false
		}
		p += 2
		piece++
		compress = piece
	}
	for p < len(rs) {
		if piece == 8 {
			return addr, false
		}
		if rs[p] == ':' {
			if compress != -1 {
				return addr, false
			}
			p++
			piece++
			compress = piece
			continue
		}
		value, length := 0, 0
		for length < 4 && p < len(rs) && unhexRune(rs[p]) >= 0 {
			value = value*16 + unhexRune(rs[p])
			p++
			length++
		}
		if p < len(rs) && rs[p] == '.' {
			if length == 0 {
				return addr, false
			}
			p -= length
			if piece > 6 {
				return addr, false
			}
			seen := 0
			for p < len(rs) {
				v4 := -1
				if seen > 0 {
					if rs[p] == '.' && seen < 4 {
						p++
					} else {
						return addr, false
					}
				}
				if p >= len(rs) || rs[p] < '0' || rs[p] > '9' {
					return addr, false
				}
				for p < len(rs) && rs[p] >= '0' && rs[p] <= '9' {
					n := int(rs[p] - '0')
					if v4 == -1 {
						v4 = n
					} else if v4 == 0 {
						return addr, false
					} else {
						v4 = v4*10 + n
					}
					if v4 > 255 {
						return addr, false
					}
					p++
				}
				addr[piece] = addr[piece]*0x100 + uint16(v4)
				seen++
				if seen == 2 || seen == 4 {
					piece++
				}
			}
			if seen != 4 {
				return addr, false
			}
			break
		} else if p < len(rs) && rs[p] == ':' {
			p++
			if p >= len(rs) {
				return addr, false
			}
		} else if p < len(rs) {
			return addr, false
		}
		addr[piece] = uint16(value)
		piece++
	}
	if compress != -1 {
		swaps := piece - compress
		piece = 7
		for piece != 0 && swaps > 0 {
			addr[piece], addr[compress+swaps-1] = addr[compress+swaps-1], addr[piece]
			piece--
			swaps--
		}
	} else if piece != 8 {
		return addr, false
	}
	return addr, true
}

func unhexRune(r rune) int {
	if r > 0x7F {
		return -1
	}
	return unhex(byte(r))
}

func serializeIPv6(a [8]uint16) string {
	// 找到第一个最长的连续 0 段（长度至少为 2）
	best, bestLen := -1, 1
	for i := 0; i < 8; {
		if a[i] != 0 {
			i++
			continue
		}
		j := i
		for j < 8 && a[j] == 0 {
			j++
		}
		if j-i > bestLen {
			best, bestLen = i, j-i
		}
		i = j
	}
	var sb strings.Builder
	ignore0 := false
	for i := 0; i < 8; i++ {
		if ignore0 && a[i] == 0 {
			continue
		}
		ignore0 = false
		if best == i {
			if i == 0 {
				sb.WriteString("::")
			} else {
				sb.WriteString(":")
			}
			ignore0 = true
			continue
		}
		sb.WriteString(strconv.FormatUint(uint64(a[i]), 16))
		if i != 7 {
			sb.WriteByte(':')
		}
	}
	return sb.String()
}

// Hostname 对应 url.hostname。
func (u *URL) Hostname() string {
	if u.Host == nil {
		return ""
	}
	return *u.Host
}

// PortString 对应 url.port。
func (u *URL) PortString() string {
	if u.Port == nil {
		return ""
	}
	return strconv.Itoa(*u.Port)
}

// Pathname 对应 url.pathname。
func (u *URL) Pathname() string {
	if u.OpaquePath != nil {
		return *u.OpaquePath
	}
	if len(u.Path) == 0 {
		return ""
	}
	return "/" + strings.Join(u.Path, "/")
}

// Search 对应 url.search。
func (u *URL) Search() string {
	if u.Query == nil || *u.Query == "" {
		return ""
	}
	return "?" + *u.Query
}

// Hash 对应 url.hash。
func (u *URL) Hash() string {
	if u.Fragment == nil || *u.Fragment == "" {
		return ""
	}
	return "#" + *u.Fragment
}

// Protocol 对应 url.protocol。
func (u *URL) Protocol() string { return u.Scheme + ":" }

// SetHash 对应 `url.hash = value`。
func (u *URL) SetHash(v string) {
	if v == "" {
		u.Fragment = nil
		return
	}
	v = strings.TrimPrefix(v, "#")
	v = strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, v)
	f := percentEncodeString(v, inFragmentSet)
	u.Fragment = &f
}

// Href 对应 url.href / url.toString()。
func (u *URL) Href() string {
	var sb strings.Builder
	sb.WriteString(u.Scheme)
	sb.WriteByte(':')
	if u.Host != nil {
		sb.WriteString("//")
		if u.Username != "" || u.Password != "" {
			sb.WriteString(u.Username)
			if u.Password != "" {
				sb.WriteByte(':')
				sb.WriteString(u.Password)
			}
			sb.WriteByte('@')
		}
		sb.WriteString(*u.Host)
		if u.Port != nil {
			sb.WriteByte(':')
			sb.WriteString(strconv.Itoa(*u.Port))
		}
	}
	if u.Host == nil && u.OpaquePath == nil && len(u.Path) > 1 && u.Path[0] == "" {
		sb.WriteString("/.")
	}
	sb.WriteString(u.Pathname())
	if u.Query != nil {
		sb.WriteByte('?')
		sb.WriteString(*u.Query)
	}
	if u.Fragment != nil {
		sb.WriteByte('#')
		sb.WriteString(*u.Fragment)
	}
	return sb.String()
}

// SearchParams 返回与 URL 关联的 URLSearchParams，修改后会写回 URL 的 query。
func (u *URL) SearchParams() *URLSearchParams {
	q := ""
	if u.Query != nil {
		q = *u.Query
	}
	sp := parseFormURLEncoded(q)
	sp.owner = u
	return sp
}

// URLSearchParams 对应同名 Web API。
type URLSearchParams struct {
	list  [][2]string
	owner *URL
}

// NewSearchParams 创建空的参数表。
func NewSearchParams() *URLSearchParams { return &URLSearchParams{} }

// ParseSearchParams 对应 new URLSearchParams(string)（会去掉开头的 ?）。
func ParseSearchParams(s string) *URLSearchParams {
	return parseFormURLEncoded(strings.TrimPrefix(s, "?"))
}

// parseFormURLEncoded 实现 application/x-www-form-urlencoded 解析（不去掉开头的 ?，URL.searchParams 使用此版本）。
func parseFormURLEncoded(s string) *URLSearchParams {
	sp := &URLSearchParams{}
	for _, seq := range strings.Split(s, "&") {
		if seq == "" {
			continue
		}
		name, value, _ := strings.Cut(seq, "=")
		name = strings.ReplaceAll(name, "+", " ")
		value = strings.ReplaceAll(value, "+", " ")
		sp.list = append(sp.list, [2]string{
			utf8DecodeReplace(PercentDecodeBytes(name)),
			utf8DecodeReplace(PercentDecodeBytes(value)),
		})
	}
	return sp
}

// Get 对应 get()，ok=false 表示 null。
func (sp *URLSearchParams) Get(name string) (string, bool) {
	for _, kv := range sp.list {
		if kv[0] == name {
			return kv[1], true
		}
	}
	return "", false
}

// GetValue 对应 get()，null 返回 Null 便于在动态值中使用。
func (sp *URLSearchParams) GetValue(name string) any {
	if v, ok := sp.Get(name); ok {
		return v
	}
	return Null
}

// Str 对应 `params.get(name)` 在真值判断场景下的用法：null 视为空串。
func (sp *URLSearchParams) Str(name string) string {
	v, _ := sp.Get(name)
	return v
}

// Has 对应 has()。
func (sp *URLSearchParams) Has(name string) bool {
	_, ok := sp.Get(name)
	return ok
}

// Set 对应 set()。
func (sp *URLSearchParams) Set(name, value string) {
	found := false
	out := sp.list[:0:0]
	for _, kv := range sp.list {
		if kv[0] == name {
			if found {
				continue
			}
			found = true
			kv[1] = value
		}
		out = append(out, kv)
	}
	if !found {
		out = append(out, [2]string{name, value})
	}
	sp.list = out
	sp.update()
}

// Append 对应 append()。
func (sp *URLSearchParams) Append(name, value string) {
	sp.list = append(sp.list, [2]string{name, value})
	sp.update()
}

// Delete 对应 delete()。
func (sp *URLSearchParams) Delete(name string) {
	out := sp.list[:0:0]
	for _, kv := range sp.list {
		if kv[0] != name {
			out = append(out, kv)
		}
	}
	sp.list = out
	sp.update()
}

// Entries 返回所有键值对。
func (sp *URLSearchParams) Entries() [][2]string { return append([][2]string(nil), sp.list...) }

func (sp *URLSearchParams) update() {
	if sp.owner == nil {
		return
	}
	s := sp.String()
	if s == "" {
		sp.owner.Query = nil
	} else {
		sp.owner.Query = &s
	}
}

// String 对应 toString()（application/x-www-form-urlencoded 序列化）。
func (sp *URLSearchParams) String() string {
	var sb strings.Builder
	for i, kv := range sp.list {
		if i > 0 {
			sb.WriteByte('&')
		}
		formEncode(&sb, kv[0])
		sb.WriteByte('=')
		formEncode(&sb, kv[1])
	}
	return sb.String()
}

func formEncode(sb *strings.Builder, s string) {
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == ' ':
			sb.WriteByte('+')
		case isAlnum(c) || c == '*' || c == '-' || c == '.' || c == '_':
			sb.WriteByte(c)
		default:
			sb.WriteByte('%')
			sb.WriteByte(hexUpper[c>>4])
			sb.WriteByte(hexUpper[c&15])
		}
	}
}
