package sub

import (
	"math/rand"
	"strings"
	"testing"
	"unicode/utf8"

	js "github.com/shusfun/weir/internal/sub/jscompat"
)

// 以固定种子对样例链接做随机变异（插入 / 删除 / 替换特殊字符），覆盖解析器在非常规输入上的行为。
func mutatedLinks(n int) []string {
	rng := rand.New(rand.NewSource(20261022))
	alphabet := []string{"%", "#", "@", "?", "&", "=", ":", "/", "[", "]", " ", "+", "%2", "%E4", "中", "🇭🇰", "\t", ",", ";", "\\", "'", "\"", "a", "0", "-", "_", "."}
	var out []string
	for i := 0; i < n; i++ {
		base := []rune(sampleLinks[rng.Intn(len(sampleLinks))])
		for k := 0; k < 1+rng.Intn(3); k++ {
			pos := rng.Intn(len(base) + 1)
			switch rng.Intn(3) {
			case 0:
				ins := []rune(alphabet[rng.Intn(len(alphabet))])
				base = append(base[:pos:pos], append(ins, base[pos:]...)...)
			case 1:
				if pos < len(base) {
					base = append(base[:pos:pos], base[pos+1:]...)
				}
			default:
				if pos < len(base) {
					base[pos] = []rune(alphabet[rng.Intn(len(alphabet))])[0]
				}
			}
		}
		out = append(out, string(base))
	}
	return out
}

// hitsNodeSearchParamsBug 判断输入是否会触发 Node 22 URLSearchParams 的解码缺陷：
// 非法百分号转义之后出现合法转义与非 ASCII 字符时，Node 会把后者截断成单字节（如 “中” 变成 “-”），
// 这与 WHATWG 规范及 Workers 不符。Go 端按规范实现，这类输入不参与对照。
func hitsNodeSearchParamsBug(s string) bool {
	hasNonASCII := false
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			hasNonASCII = true
			break
		}
	}
	if !hasNonASCII {
		return false
	}
	// 解码后不是合法 UTF-8（含不完整的多字节转义）时会触发
	if !utf8.Valid(js.PercentDecodeBytes(strings.ReplaceAll(s, "+", " "))) {
		return true
	}
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && !(i+2 < len(s) && isHexByte(s[i+1]) && isHexByte(s[i+2])) {
			return true
		}
	}
	return false
}

func isHexByte(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

func TestMutationParity(t *testing.T) {
	var links []string
	for _, l := range mutatedLinks(3000) {
		if !hitsNodeSearchParamsBug(l) {
			links = append(links, l)
		}
	}
	var cases []oracleCase
	for i, link := range links {
		link := link
		n := js.NumberToString(float64(i))
		cases = append(cases,
			oracleCase{Name: "urlToClashProxy/m" + n, Mod: "utils/url-to-clash.js", Fn: "urlToClashProxy", Args: []any{link},
				Go: func() (any, error) { return URLToClashProxy(link), nil }},
			oracleCase{Name: "parseNodeInfo/m" + n, Mod: "modules/utils/geo-utils.js", Fn: "parseNodeInfo", Args: []any{link},
				Go: func() (any, error) { return ParseNodeInfo(link).Object(), nil }},
			oracleCase{Name: "fixNodeUrlEncoding/m" + n, Mod: "utils/node-utils.js", Fn: "fixNodeUrlEncoding", Args: []any{link},
				Go: func() (any, error) { return FixNodeURLEncoding(link, FixOptions{}), nil }},
			oracleCase{Name: "cleanerFix/m" + n, Mod: "modules/utils/node-cleaner.js", Fn: "fixNodeUrlEncoding", Args: []any{link},
				Go: func() (any, error) { return CleanerFixNodeURLEncoding(link, FixOptions{}), nil }},
			oracleCase{Name: "addFlagEmoji/m" + n, Mod: "utils/node-utils.js", Fn: "addFlagEmoji", Args: []any{link},
				Go: func() (any, error) { return AddFlagEmoji(link), nil }},
			oracleCase{Name: "applyManualNodeName/m" + n, Mod: "modules/utils/node-cleaner.js", Fn: "applyManualNodeName", Args: []any{link, "名#?"},
				Go: func() (any, error) { return ApplyManualNodeName(link, "名#?"), nil }},
		)
		if p := URLToClashProxy(link); p != nil {
			cases = append(cases, oracleCase{Name: "convertClashProxyToUrl/m" + n, Mod: "utils/clash-to-url.js", Fn: "convertClashProxyToUrl",
				Args: []any{p}, Go: func() (any, error) { return nullIfFalse(ConvertClashProxyToURL(p)), nil }})
		}
	}
	text := strings.Join(links, "\n")
	cases = append(cases, oracleCase{Name: "parseNodeList/mutated", Mod: "modules/utils/node-parser.js", Fn: "parseNodeList", Args: []any{text},
		Go: func() (any, error) { return objs(ParseNodeList(text, FixOptions{})), nil }})
	cases = append(cases, oracleCase{Name: "builtinClash/mutated", Mod: "modules/subscription/transformer-factory.js", Fn: "transformBuiltinSubscription",
		Args: []any{text, "clash", js.NewObject()}, Go: func() (any, error) {
			out, ok, err := TransformBuiltinSubscription(text, "clash", js.NewObject())
			return strOrNull(out, ok), err
		}})
	runOracle(t, cases)
}
