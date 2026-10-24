package sub

import (
	"strings"
	"testing"

	js "github.com/shusfun/weir/internal/sub/jscompat"
)

// 校验 jscompat 语义层（WHATWG URL、js-yaml、JSON、编码、正则、数字格式化、排序）与 Node 一致。

const adapters = "../adapters/builtins.mjs"

func goURLParts(input string) any {
	u, err := js.ParseURL(input)
	if err != nil {
		return js.Null
	}
	params := js.Arr()
	for _, kv := range u.SearchParams().Entries() {
		params.Push(js.Arr(kv[0], kv[1]))
	}
	return js.Obj("href", u.Href(), "protocol", u.Protocol(), "username", u.Username, "password", u.Password,
		"hostname", u.Hostname(), "port", u.PortString(), "pathname", u.Pathname(), "search", u.Search(), "hash", u.Hash(), "params", params)
}

func TestURLParity(t *testing.T) {
	inputs := []string{
		"trojan://pa^ss{}w@Host.COM:443?sni=a b&x=^`#n a^m`e", "https://EXAMPLE.com:443/a b/^x/`{}/../c?q=a b'#f g",
		"snell://psk@[2001:DB8:0:0:1::1]:8443?version=4", "vless://uuid@ho st:1", "vless://uuid@host:99999", "vless://uuid@host:12a",
		"vless://@host:1", "vless://uuid@:1", "foo://h/./a/../b/%2e%2E/c", "http://0x7f.1/", "http://例子.测试/", "trojan://p@h%20x:1",
		"trojan://p@h|x:1", "trojan://a:b:c@h:1", "trojan://a@b@h:1", "tuic://u:p@1.2.3.4:443?a=1#n", "hy2://p@h:443/?a=1", "HTTP://h",
		"vless://u@h:443#%E4%B8%AD x", "vless://u@h:0443", "vless://u@h:", "vless://u@H%41:1", "https://h%41.com/", "vless://u@[::ffff:1.2.3.4]:1",
		"udp://8.8.8.8:53", "https://dns.google/dns-query", "tls://1.1.1.1", "udp://[2001:4860::8888]:53", "wireguard://k%2B@h:1",
		"  vless://u@h:1\t ", "vless:/u@h:1", "vless:u@h:1", "https://[::1]:443/x?y#z", "https://1.2.3.4.5/", "https://999.1.1.1/",
		"https://a..b/", "http://h:80/", "ws://h:80/", "https://user:pa%40ss@h/", "snell://psk@h:1?a=1+2&b=%zz&&c&d=%E4%B8",
		"vless://uuid@[fe80::1%25eth0]:1", "https://xn--fsqu00a.xn--0zwm56d/", "https://ÉXAMPLE.com/Ä?ö#ü", "http://h/%7e%41", "mailto:x@y",
		"https://h/a\\b\\..\\c", "custom://h\\x", "https://h:65535", "https://h:65536", "socks5://u:p@[::1]:1080",
	}
	var cases []oracleCase
	for _, in := range inputs {
		in := in
		cases = append(cases, oracleCase{Name: "url/" + in, Mod: adapters, Fn: "urlParts", Args: []any{in}, Go: func() (any, error) { return goURLParts(in), nil }})
	}
	for _, h := range []string{"#新 名", "a b`<", "", "#", "##x", "%E4%B8"} {
		h := h
		cases = append(cases, oracleCase{Name: "urlSetHash/" + h, Mod: adapters, Fn: "urlSetHash", Args: []any{"trojan://p@h:1?x=1#old", h},
			Go: func() (any, error) {
				u, _ := js.ParseURL("trojan://p@h:1?x=1#old")
				u.SetHash(h)
				return u.Href(), nil
			}})
	}
	for _, in := range []string{"snell://psk@h:1?version=4&ecn=true&obfs-host=a.b:c/d,e#n", "snell://psk@h:1?ecn=1", "https://h/p?ecn=1&a=%20b+c"} {
		in := in
		cases = append(cases, oracleCase{Name: "urlDeleteParam/" + in, Mod: adapters, Fn: "urlDeleteParam", Args: []any{in, "ecn"},
			Go: func() (any, error) {
				u, _ := js.ParseURL(in)
				u.SearchParams().Delete("ecn")
				return u.Href(), nil
			}})
	}
	for _, init := range []string{"a=1+2&b=%zz&&c&d=%E4%B8", "?a=x&a=y", "a", "=x", "a=%26&b=中 文"} {
		init := init
		cases = append(cases, oracleCase{Name: "searchParams/" + init, Mod: adapters, Fn: "searchParams", Args: []any{init, "k", "a b:/,~*-._!'()中"},
			Go: func() (any, error) {
				p := js.ParseSearchParams(init)
				before := js.Arr()
				for _, kv := range p.Entries() {
					before.Push(js.Arr(kv[0], kv[1]))
				}
				get := p.GetValue("a")
				has := p.Has("a")
				p.Set("k", "a b:/,~*-._!'()中")
				return js.Obj("before", before, "get", get, "has", has, "out", p.String()), nil
			}})
	}
	runOracle(t, cases)
}

const yamlLoadFixture = `
a: 1
b: 012
c: 0o17
d: 0x1F
e: .5
f: 1_000
g: -.inf
h: .NaN
i: ~
j: Null
k: yes
l: True
m: "true"
n: '1'
o: 2024-01-02
p: 2024-01-02 03:04:05.678+08:00
q: |
  line1
  line2
r: >
  folded
  text
s: !!str 123
t: [1, two, "3", {x: y}]
u: &anchor {k: v, n: 1}
v: *anchor
w:
  <<: *anchor
  n: 2
x: 1e3
y: +12
z: 0b101
"quoted key": value
? complex
: value2
emoji: 🇭🇰 香港
`

func TestYAMLParity(t *testing.T) {
	var cases []oracleCase
	loads := map[string]string{
		"fixture": yamlLoadFixture, "clash": clashYAMLFixture, "dup": "a: 1\na: 2\n", "multi": "a: 1\n---\nb: 2\n", "scalar": "hello",
		"empty": "", "list": "- 1\n- [a, b]\n", "merge-list": "x: &x {a: 1}\ny: &y {b: 2}\nz:\n  <<: [*x, *y]\n  c: 3\n", "bad": "{a: [1, 2",
		"tab": "a:\tb\n", "nulls": "a:\nb: ''\nc: null\n", "bool-like": "a: on\nb: Off\nc: y\n",
	}
	for name, text := range loads {
		text := text
		cases = append(cases, oracleCase{Name: "yamlLoad/" + name, Mod: adapters, Fn: "yamlLoad", Args: []any{text},
			Go: func() (any, error) {
				v, err := js.YAMLLoad(text)
				if err != nil {
					return js.Obj("error", true), nil
				}
				return js.Obj("ok", v), nil
			}})
	}
	tricky := js.Obj(
		"plain", "hello world", "empty", "", "space", " lead", "trail", "trail ", "colon", "a: b", "colonEnd", "a:", "hash", "a #b", "hashNoSpace", "a#b",
		"dash", "- x", "dashOnly", "-", "q", "?x", "bool", "true", "yes", "yes", "no", "No", "null", "null", "tilde", "~", "num", "123", "float", "1.5",
		"exp", "1e3", "hex", "0x1F", "oct", "0o17", "base60", "1:20", "date", "2024-01-02", "merge", "<<", "star", "*ref", "amp", "&a", "bang", "!tag",
		"pct", "%x", "at", "@x", "tick", "`x", "quote", `"x"`, "squote", "'x'", "multi", "line1\nline2\n", "multiNoEnd", "a\nb", "lead-space-multi", "  a\nb\n",
		"keep", "a\n\n", "tab", "a\tb", "ctrl", "a\x01b", "bom", "\ufeffx", "nel", "a\u0085b", "ls", "a\u2028b", "emoji", "🇭🇰 香港", "brace", "{a}",
		"bracket", "[a]", "comma", "a, b", "pipe", "|x", "gt", ">x", "eq", "=x", "numKey", 1.0, "negZero", js.Null, "int", 7890.0, "neg", -3.0, "frac", 0.25,
		"big", 1e21, "small", 1e-7, "nan", js.StringToNumber("x"), "bools", js.Arr(true, false, js.Null, nil), "nested", js.Obj("a", js.Arr(js.Obj("b", js.Arr()), js.NewObject())),
		"123", "numeric key", "a b", "key with space", "k:v", "colon key", "", "empty key", "undef", nil, "long", strings.Repeat("x", 1100),
	)
	for _, opts := range []*js.Object{js.NewObject(), js.Obj("quotingType", `"`), js.Obj("quotingType", `"`, "forceQuotes", true)} {
		opts := opts
		cases = append(cases, oracleCase{Name: "yamlDump/" + js.MustStringify(opts), Mod: adapters, Fn: "yamlDump", Args: []any{tricky, opts},
			Go: func() (any, error) {
				return js.YAMLDump(tricky, js.YAMLDumpOptions{DoubleQuote: js.StrictEquals(opts.Get("quotingType"), `"`), ForceQuotes: js.Truthy(opts.Get("forceQuotes"))})
			}})
	}
	cases = append(cases, oracleCase{Name: "yamlDump/clash-roundtrip", Mod: adapters, Fn: "yamlDump", Args: []any{mustYAML(t, clashYAMLFixture), js.Obj("quotingType", `"`)},
		Go: func() (any, error) {
			return js.YAMLDump(mustYAML(t, clashYAMLFixture), js.YAMLDumpOptions{DoubleQuote: true})
		}})
	runOracle(t, cases)
}

func TestJSONAndCodecParity(t *testing.T) {
	var cases []oracleCase
	values := []any{js.Obj("a", 1.0, "b", js.Arr(1.0, "x", js.Null, nil, true), "u", nil, "s", "q\"\\\n\t\x01\u2028😀", "n", js.Obj(), "e", js.Arr(), "nan", js.StringToNumber("x")),
		"str", 1e21, 123.456, 0.1, -0.0, js.Arr(), nil}
	for i, v := range values {
		v := v
		for _, indent := range []any{nil, 2.0} {
			indent := indent
			cases = append(cases, oracleCase{Name: "jsonStringify/" + js.NumberToString(float64(i)) + "/" + js.ToString(indent), Mod: adapters, Fn: "jsonStringify",
				Args: []any{v, indent}, Go: func() (any, error) {
					ind := ""
					if indent != nil {
						ind = "  "
					}
					s, ok := js.JSONStringifyIndent(v, ind)
					if !ok {
						return nil, nil
					}
					return s, nil
				}})
		}
	}
	for _, s := range []string{`{"a":1,"b":[1,2,{"c":"\u4e2d\ud83d\ude00"}],"a":3}`, `[1e400, -0, 1.5e-10]`, `{"a":}`, ` "x" `, `{"2":1,"1":2,"b":3,"a":4}`, `tru`, `"\x"`, `01`, `{"a":1}x`} {
		s := s
		cases = append(cases, oracleCase{Name: "jsonParse/" + s, Mod: adapters, Fn: "jsonParse", Args: []any{s}, Go: func() (any, error) {
			v, err := js.JSONParse(s)
			if err != nil {
				return js.Obj("error", true), nil
			}
			return js.Obj("ok", v), nil
		}})
	}
	inputs := []string{"", "abc", "YWJj", "YQ=", "YWJjZA", "a b%20c", "%E4%B8%AD", "%E4%B8", "%zz", "%C0%80", "中文 &=?#", "  \u3000trim\ufeff ", "ß ǅ İ",
		"😀", "0x1F", "  12px", "-0", "1e3", ".5", "Infinity", "-Infinity", "1_000", "0b11", "  ", "+5", "12.5.6", "5e", "0o17", "%u4E2D"}
	for _, in := range inputs {
		in := in
		cases = append(cases, oracleCase{Name: "codec/" + in, Mod: adapters, Fn: "codec", Args: []any{in}, Go: func() (any, error) {
			o := js.Obj("enc", js.EncodeURIComponent(in), "encURI", js.EncodeURI(in), "esc", js.Escape(in), "unesc", js.Unescape(in))
			if d, err := js.DecodeURIComponent(in); err == nil {
				o.Set("dec", d)
			} else {
				o.Set("dec", js.Null)
			}
			if a, err := js.Atob(in); err == nil {
				o.Set("atob", a)
			} else {
				o.Set("atob", "ERR:"+err.Error())
			}
			o.Set("b64", js.Base64EncodeUTF8(in))
			o.Set("trim", js.Trim(in))
			o.Set("upper", js.ToUpper(in))
			o.Set("lower", js.ToLower(in))
			o.Set("len", float64(js.UTF16Len(in)))
			o.Set("pad", js.PadStart(in, 12, "ab"))
			o.Set("num", js.StringToNumber(in))
			o.Set("int", js.ParseInt(in, 0))
			o.Set("int16", js.ParseInt(in, 16))
			o.Set("float", js.ParseFloat(in))
			return o, nil
		}})
	}
	for _, n := range []float64{0, 1, -1, 0.1, 1e21, 1e-7, 123456789012345680000, 5e-324, 1.7976931348623157e308, 2.5e-5, 100, 0.000001, 1.23e-6, 12345.678} {
		n := n
		cases = append(cases, oracleCase{Name: "numberToString/" + js.NumberToString(n), Mod: adapters, Fn: "numberToString", Args: []any{n},
			Go: func() (any, error) { return js.NumberToString(n), nil }})
	}
	sortIn := []string{"b", "a", "香港", "台湾", "日本", "10", "9", "A", "node 2", "node 10", "Node 1", "é", "e", "Z", "_x", "🇭🇰 HK", "ｆｕｌｌ", "full"}
	cases = append(cases, oracleCase{Name: "localeSort", Mod: adapters, Fn: "localeSort", Args: []any{strs(sortIn...)},
		Go: func() (any, error) {
			items := strs(sortIn...).Items
			js.SortStable(items, func(a, b any) float64 { return float64(js.LocaleCompareNumericBase(a.(string), b.(string))) })
			return &js.Array{Items: items}, nil
		}})
	runOracle(t, cases)
}

func TestRegexParity(t *testing.T) {
	type rc struct{ s, pattern, flags, repl string }
	inputs := []rc{
		{"🇭🇰 香港 IPLC 01", `(\d+)`, "g", "[$1]"}, {"🇭🇰 香港", `[^a-z]`, "gu", "_"}, {"aaa", `a*?`, "g", "-"},
		{"abc", ``, "g", "-"}, {"a,b,,c", `,`, "", "$$"}, {"x-y_z", `(?<first>\w)-(?<second>\w)`, "", "$<second>$<first>$<none>"},
		{"HK HKG hk", `\bhk\b`, "gi", "港"}, {"a\nb", `^b`, "m", "B"}, {"a\nb", `a.b`, "s", "X"}, {"ab", `(?<=a)b`, "", "B"}, {"ab", `a(?!c)`, "g", "A"},
		{"aXbXc", `x`, "gi", "$&$&"}, {"abc", `b`, "", "$`|$'"}, {"abc", `(a)(b)?(z)?`, "", "[$1][$2][$3][$4][$10]"}, {"bad", `(`, "", ""},
		{"bad", `a`, "gg", ""}, {"bad", `a`, "x", ""}, {"(?i)a", `(?i)a`, "", ""}, {"a.b", `\.`, "g", "\\"}, {"a1b2", `\d`, "y", "#"},
		{"日本 东京", `[一-龥]+`, "g", "<$&>"}, {"  pad  ", `^\s+|\s+$`, "g", ""}, {"a　b\u00a0c", `\s`, "g", "_"}, {"Straße", `SS`, "i", "ss"},
		{"🇭🇰🇯🇵", `^[\uD83C][\uDDE6-\uDDFF][\uD83C][\uDDE6-\uDDFF]\s*`, "g", ""}, {"x🇭🇰y", `.`, "gu", "."},
		{"a1b22c333", `(\d)(\d)?`, "g", "<$2$1>"}, {"one two", `(\w+) (\w+)`, "", "$2 $1"}, {"k=v=w", `=(.*)`, "", ""}, {"a-b-c", `-`, "", ""},
		{"aaa", `a`, "g", "$0"}, {"test", `\k<x>(?<x>t)`, "", "#"}, {"[]", `[]`, "g", "x"}, {"ab", `[^]`, "g", "x"},
	}
	var cases []oracleCase
	for _, in := range inputs {
		in := in
		for _, limit := range []any{nil, 2.0} {
			limit := limit
			cases = append(cases, oracleCase{Name: "regex/" + in.pattern + "/" + in.flags + "/" + in.s + "/" + js.ToString(limit), Mod: adapters, Fn: "regex",
				Args: []any{in.s, in.pattern, in.flags, in.repl, limit},
				Go: func() (any, error) {
					re, err := js.NewRegExp(in.pattern, in.flags)
					if err != nil {
						return js.Obj("invalid", true), nil
					}
					testRe, _ := js.NewRegExp(in.pattern, in.flags)
					first, _ := js.NewRegExp(in.pattern, strings.ReplaceAll(in.flags, "g", ""))
					var match any = js.Null
					if m := first.MatchFirst(in.s); m != nil {
						groups := js.Arr()
						for i := range m.Groups {
							groups.Push(m.Group(i))
						}
						var named any = js.Null
						if m.Names != nil {
							no := js.NewObject()
							for _, n := range sortedGroupNames(m.Names) {
								no.Set(n, m.Group(m.Names[n]))
							}
							named = no
						}
						match = js.Obj("groups", groups, "prefix", in.s[:m.Index], "named", named)
					}
					allFlags := in.flags
					if !strings.Contains(allFlags, "g") {
						allFlags += "g"
					}
					allRe, _ := js.NewRegExp(in.pattern, allFlags)
					var all any = js.Null
					if ms := allRe.MatchAllStrings(in.s); ms != nil {
						all = js.StrArr(ms)
					}
					splitRe, _ := js.NewRegExp(in.pattern, in.flags)
					lim := -1
					if limit != nil {
						lim = int(limit.(float64))
					}
					searchRe, _ := js.NewRegExp(in.pattern, in.flags)
					var search any = js.Null
					if i := searchRe.Search(in.s); i >= 0 {
						search = in.s[:i]
					}
					return js.Obj("test", testRe.Test(in.s), "match", match, "all", all, "replace", re.Replace(in.s, in.repl),
						"split", anys(splitRe.SplitValues(in.s, lim)), "search", search), nil
				}})
		}
	}
	runOracle(t, cases)
}

func TestMiscModuleParity(t *testing.T) {
	var cases []oracleCase
	for _, r := range append(GetSupportedRegions(), "不存在", "") {
		r := r
		cases = append(cases, oracleCase{Name: "getRegionKeywords/" + r, Mod: "modules/utils/geo-utils.js", Fn: "getRegionKeywords", Args: []any{r},
			Go: func() (any, error) { return strs(GetRegionKeywords(r)...), nil }})
		for _, name := range []string{"香港 HK 01", "Tokyo Japan", "美国洛杉矶", "usa"} {
			name := name
			cases = append(cases, oracleCase{Name: "containsRegion/" + r + "/" + name, Mod: "modules/utils/geo-utils.js", Fn: "containsRegion", Args: []any{name, r},
				Go: func() (any, error) { return ContainsRegion(name, r), nil }})
		}
	}
	cases = append(cases, oracleCase{Name: "getSupportedRegions", Mod: "modules/utils/geo-utils.js", Fn: "getSupportedRegions",
		Go: func() (any, error) { return strs(GetSupportedRegions()...), nil }})
	emojiObj := js.NewObject()
	for _, r := range append(GetSupportedRegions(), "其他") {
		if e, ok := RegionEmoji[r]; ok {
			emojiObj.Set(r, e)
		}
	}
	cases = append(cases, oracleCase{Name: "REGION_EMOJI", Mod: "modules/utils/geo-utils.js", Fn: "REGION_EMOJI", Go: func() (any, error) { return emojiObj, nil }})
	kw := js.NewObject()
	for _, e := range RegionKeywords {
		kw.Set(e.Region, strs(e.Keywords...))
	}
	cases = append(cases, oracleCase{Name: "REGION_KEYWORDS", Mod: "modules/utils/geo-utils.js", Fn: "REGION_KEYWORDS", Go: func() (any, error) { return kw, nil }})
	records := NodeURLsToRecords(sampleLinks, false, false)
	for i, r := range records {
		r := r
		for _, emoji := range []bool{true, false} {
			emoji := emoji
			cases = append(cases, oracleCase{Name: "ensureRegionInfo/" + js.NumberToString(float64(i)) + js.ToString(emoji), Mod: "utils/node-transformer.js",
				Fn: "ensureRegionInfo", Args: []any{r, emoji}, Go: func() (any, error) { return EnsureRegionInfo(r, emoji), nil }})
		}
	}
	nodes := ParseNodeList(strings.Join(sampleLinks, "\n"), FixOptions{})
	rules := ParseFilterRuleText("香港|日本\n---\nproto:ss,vless")
	for _, mode := range []string{"include", "exclude"} {
		mode := mode
		rs := rules.ExcludeRules
		if mode == "include" {
			rs = rules.IncludeRules
		}
		cases = append(cases, oracleCase{Name: "filterNodeObjects/" + mode, Mod: "modules/utils/node-cleaner.js", Fn: "filterNodeObjects",
			Args: []any{objs(nodes), rs.Value(), mode}, Go: func() (any, error) { return objs(FilterNodeObjects(nodes, rs, mode)), nil }})
	}
	cases = append(cases, oracleCase{Name: "deduplicateNames", Mod: "modules/subscription/template-pipeline.js", Fn: "renderClashFromIniTemplate",
		Args: []any{"", js.Obj("proxies", js.Arr(js.Obj("name", "a"), js.Obj("name", "a"), js.Obj("name", "b")))},
		Go: func() (any, error) {
			return RenderClashFromIniTemplate("", js.Obj("proxies", js.Arr(js.Obj("name", "a"), js.Obj("name", "a"), js.Obj("name", "b"))))
		}})
	runOracle(t, cases)
	n := NewUniqueNamer()
	var got []string
	for _, s := range []string{"a", "a", "b", "a", "a_1", "a"} {
		got = append(got, n.GetUniqueName(s))
	}
	if strings.Join(got, ",") != "a,a_1,b,a_2,a_1,a_3" {
		t.Errorf("getUniqueName 结果不符: %v", got)
	}
}
