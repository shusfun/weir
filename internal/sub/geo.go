package sub

import (
	"strings"
	"unicode/utf8"

	js "github.com/imzyb/MiGate/internal/sub/jscompat"
)

// 移植自 functions/modules/utils/geo-utils.js

// RegionKeywordEntry 是地区关键词表的一项。
type RegionKeywordEntry struct {
	Region   string
	Keywords []string
}

// RegionKeywords 对应 REGION_KEYWORDS（保持上游顺序，识别时按顺序匹配）。
var RegionKeywords = []RegionKeywordEntry{
	{"香港", []string{"HK", "HKG", "HKT", "香港", "Hong Kong", "HongKong"}},
	{"台湾", []string{"TW", "TPE", "台湾", "台北", "Taiwan", "Taipei"}},
	{"新加坡", []string{"SG", "SIN", "新加坡", "Singapore"}},
	{"日本", []string{"JP", "JPN", "NRT", "HND", "KIX", "FUK", "东京", "大阪", "日本", "Japan", "Tokyo", "Osaka", "Fukuoka", "Yokohama"}},
	{"美国", []string{"US", "USA", "LAX", "SFO", "SEA", "JFK", "NYC", "ORD", "DFW", "IAD", "美国", "United States", "America", "Los Angeles", "New York", "San Francisco", "Seattle", "Chicago", "洛杉矶", "纽约", "旧金山", "西雅图", "芝加哥"}},
	{"韩国", []string{"KR", "KOR", "ICN", "韩国", "首尔", "Korea", "Seoul"}},
	{"英国", []string{"UK", "GB", "LHR", "LGW", "LON", "英国", "Britain", "London", "Manchester", "伦敦", "曼彻斯特"}},
	{"德国", []string{"DE", "DEU", "FRA", "MUC", "德国", "Germany", "Frankfurt", "Berlin", "Munich", "法兰克福", "柏林", "慕尼黑"}},
	{"法国", []string{"FR", "CDG", "ORY", "法国", "France", "Paris", "Lyon", "Marseille", "巴黎", "里昂", "马赛"}},
	{"加拿大", []string{"CA", "CAN", "YVR", "YYZ", "加拿大", "Canada", "Toronto", "Vancouver", "Montreal", "多伦多", "温哥华", "蒙特利尔"}},
	{"澳大利亚", []string{"AU", "AUS", "SYD", "MEL", "BNE", "PER", "澳大利亚", "Australia", "Sydney", "Melbourne", "Brisbane", "Perth", "悉尼", "墨尔本", "布里斯班", "珀斯"}},
	{"荷兰", []string{"NL", "NLD", "AMS", "荷兰", "Netherlands", "Amsterdam", "Rotterdam", "阿姆斯特丹", "鹿特丹"}},
	{"俄罗斯", []string{"RU", "RUS", "SVO", "DME", "俄罗斯", "Russia", "Moscow", "Saint Petersburg", "莫斯科", "圣彼得堡"}},
	{"印度", []string{"IN", "IND", "BOM", "DEL", "BLR", "印度", "India", "Mumbai", "Delhi", "Bangalore", "孟买", "德里", "班加罗尔"}},
	{"土耳其", []string{"TR", "TUR", "IST", "土耳其", "Turkey", "Istanbul", "Ankara", "伊斯坦布尔", "安卡拉"}},
	{"孟加拉", []string{"BD", "BGD", "DAC", "孟加拉", "孟加拉国", "Bangladesh", "Dhaka", "达卡"}},
	{"巴基斯坦", []string{"PK", "PAK", "ISB", "LHE", "巴基斯坦", "Pakistan", "Islamabad", "Lahore", "伊斯兰堡", "拉合尔"}},
	{"马来西亚", []string{"MY", "MYS", "KUL", "马来西亚", "Malaysia", "Kuala Lumpur", "吉隆坡"}},
	{"泰国", []string{"TH", "THA", "BKK", "泰国", "Thailand", "Bangkok", "Pattaya", "曼谷", "芭提雅"}},
	{"越南", []string{"VN", "VNM", "SGN", "HAN", "越南", "Vietnam", "Ho Chi Minh City", "Hanoi", "胡志明市", "河内"}},
	{"菲律宾", []string{"PH", "PHL", "MNL", "CEB", "菲律宾", "Philippines", "Manila", "Cebu", "马尼拉", "宿务"}},
	{"印尼", []string{"ID", "IDN", "CGK", "印尼", "印度尼西亚", "Indonesia", "Jakarta", "Surabaya", "Bandung", "雅加达", "泗水", "万隆"}},
	{"文莱", []string{"BN", "BRN", "BWN", "文莱", "Brunei", "Bandar Seri Begawan", "斯里巴加湾市"}},
	{"柬埔寨", []string{"KH", "KHM", "PNH", "柬埔寨", "Cambodia", "Phnom Penh", "金边"}},
	{"老挝", []string{"LA", "LAO", "VTE", "老挝", "Laos", "Vientiane", "万象"}},
	{"缅甸", []string{"MM", "MMR", "RGN", "缅甸", "Myanmar", "Burma", "Yangon", "Rangoon", "仰光"}},
	{"瑞士", []string{"CH", "CHE", "ZRH", "GVA", "瑞士", "Switzerland", "Zurich", "Geneva", "苏黎世", "日内瓦"}},
	{"意大利", []string{"IT", "ITA", "FCO", "MXP", "意大利", "Italy", "Rome", "Milan", "Naples", "罗马", "米兰", "那不勒斯"}},
	{"西班牙", []string{"ES", "ESP", "MAD", "BCN", "西班牙", "Spain", "Madrid", "Barcelona", "马德里", "巴塞罗那"}},
	{"巴西", []string{"BR", "BRA", "GRU", "GIG", "巴西", "Brazil", "São Paulo", "Rio de Janeiro", "圣保罗", "里约"}},
	{"阿根廷", []string{"AR", "ARG", "EZE", "阿根廷", "Argentina", "Buenos Aires", "布宜诺斯艾利斯"}},
	{"墨西哥", []string{"MX", "MEX", "墨西哥", "Mexico", "Mexico City", "墨西哥城"}},
	{"哥伦比亚", []string{"CO", "COL", "BOG", "哥伦比亚", "Colombia", "Bogota", "Bogotá", "波哥大"}},
	{"秘鲁", []string{"PE", "PER", "LIM", "秘鲁", "Peru", "Lima", "利马"}},
	{"南非", []string{"ZA", "ZAF", "JNB", "CPT", "南非", "South Africa", "Johannesburg", "Cape Town", "约翰内斯堡", "开普敦"}},
	{"埃及", []string{"EG", "EGY", "CAI", "埃及", "Egypt", "Cairo", "开罗"}},
	{"以色列", []string{"IL", "ISR", "TLV", "以色列", "Israel", "Tel Aviv", "Jerusalem", "特拉维夫", "耶路撒冷"}},
	{"阿联酋", []string{"AE", "ARE", "DXB", "AUH", "阿联酋", "UAE", "Dubai", "Abu Dhabi", "Sharjah", "迪拜", "阿布扎比", "沙迦"}},
	{"阿曼", []string{"OM", "OMN", "MCT", "阿曼", "Oman", "Muscat", "马斯喀特"}},
	{"沙特", []string{"SA", "SAU", "RUH", "JED", "沙特", "Saudi Arabia", "Riyadh", "Jeddah", "利雅得", "吉达"}},
	{"波兰", []string{"PL", "POL", "WAW", "波兰", "Poland", "Warsaw", "Krakow", "华沙", "克拉科夫"}},
	{"捷克", []string{"CZ", "CZE", "PRG", "捷克", "Czech Republic", "Prague", "布拉格"}},
	{"匈牙利", []string{"HU", "HUN", "BUD", "匈牙利", "Hungary", "Budapest", "布达佩斯"}},
	{"罗马尼亚", []string{"RO", "ROU", "OTP", "罗马尼亚", "Romania", "Bucharest", "布加勒斯特"}},
	{"保加利亚", []string{"BG", "BGR", "SOF", "保加利亚", "Bulgaria", "Sofia", "索非亚"}},
	{"希腊", []string{"GR", "GRC", "ATH", "希腊", "Greece", "Athens", "Thessaloniki", "雅典", "塞萨洛尼基"}},
	{"葡萄牙", []string{"PT", "PRT", "LIS", "葡萄牙", "Portugal", "Lisbon", "Porto", "里斯本", "波尔图"}},
	{"瑞典", []string{"SE", "SWE", "ARN", "瑞典", "Sweden", "Stockholm", "Gothenburg", "斯德哥尔摩", "哥德堡"}},
	{"挪威", []string{"NO", "NOR", "OSL", "挪威", "Norway", "Oslo", "Bergen", "奥斯陆", "卑尔根"}},
	{"丹麦", []string{"DK", "DNK", "CPH", "丹麦", "Denmark", "Copenhagen", "Aarhus", "哥本哈根", "奥胡斯"}},
	{"芬兰", []string{"FI", "FIN", "HEL", "芬兰", "Finland", "Helsinki", "赫尔辛基"}},
	{"奥地利", []string{"AT", "AUT", "VIE", "奥地利", "Austria", "Vienna", "Salzburg", "维也纳", "萨尔茨堡"}},
	{"爱尔兰", []string{"IE", "IRL", "DUB", "爱尔兰", "Ireland", "Dublin", "都柏林"}},
	{"冰岛", []string{"IS", "ISL", "KEF", "冰岛", "Iceland", "Reykjavik", "雷克雅未克"}},
	{"卢森堡", []string{"LU", "LUX", "卢森堡", "Luxembourg", "Luxembourg City", "卢森堡市"}},
	{"马耳他", []string{"MT", "MLT", "MLA", "马耳他", "Malta", "Valletta", "瓦莱塔"}},
	{"斯洛伐克", []string{"SK", "SVK", "BTS", "斯洛伐克", "Slovakia", "Bratislava", "布拉迪斯拉发"}},
	{"斯洛文尼亚", []string{"SI", "SVN", "LJU", "斯洛文尼亚", "Slovenia", "Ljubljana", "卢布尔雅那"}},
	{"克罗地亚", []string{"HR", "HRV", "ZAG", "克罗地亚", "Croatia", "Zagreb", "萨格勒布"}},
	{"拉脱维亚", []string{"LV", "LVA", "RIX", "拉脱维亚", "Latvia", "Riga", "里加"}},
	{"立陶宛", []string{"LT", "LTU", "VNO", "立陶宛", "Lithuania", "Vilnius", "维尔纽斯"}},
	{"爱沙尼亚", []string{"EE", "EST", "TLL", "爱沙尼亚", "Estonia", "Tallinn", "塔林"}},
	{"塞浦路斯", []string{"CY", "CYP", "LCA", "塞浦路斯", "Cyprus", "Nicosia", "尼科西亚"}},
	{"列支敦士登", []string{"LI", "LIE", "列支敦士登", "Liechtenstein", "Vaduz", "瓦杜兹"}},
	{"阿尔巴尼亚", []string{"AL", "ALB", "TIA", "阿尔巴尼亚", "Albania", "Tirana", "地拉那"}},
	{"安道尔", []string{"AD", "AND", "安道尔", "Andorra", "Andorra la Vella", "安道尔城"}},
	{"白俄罗斯", []string{"BY", "BLR", "MSQ", "白俄罗斯", "Belarus", "Minsk", "明斯克"}},
	{"波黑", []string{"BA", "BIH", "SJJ", "波黑", "波斯尼亚", "Bosnia", "Bosnia and Herzegovina", "Sarajevo", "萨拉热窝"}},
	{"摩尔多瓦", []string{"MD", "MDA", "KIV", "摩尔多瓦", "Moldova", "Chisinau", "Chișinău", "基希讷乌"}},
	{"摩纳哥", []string{"MC", "MCO", "摩纳哥", "Monaco", "Monte Carlo", "蒙特卡洛"}},
	{"黑山", []string{"ME", "MNE", "TGD", "黑山", "Montenegro", "Podgorica", "波德戈里察"}},
	{"北马其顿", []string{"MK", "MKD", "SKP", "北马其顿", "North Macedonia", "Macedonia", "Skopje", "斯科普里"}},
	{"塞尔维亚", []string{"RS", "SRB", "BEG", "塞尔维亚", "Serbia", "Belgrade", "贝尔格莱德"}},
	{"乌克兰", []string{"UA", "UKR", "KBP", "乌克兰", "Ukraine", "Kyiv", "Kiev", "基辅"}},
	{"科索沃", []string{"XK", "XKS", "PRN", "科索沃", "Kosovo", "Pristina", "普里什蒂纳"}},
	{"圣马力诺", []string{"SM", "SMR", "圣马力诺", "San Marino"}},
	{"梵蒂冈", []string{"VA", "VAT", "梵蒂冈", "Vatican", "Vatican City", "梵蒂冈城"}},
	{"格鲁吉亚", []string{"GE", "GEO", "TBS", "格鲁吉亚", "Georgia", "Tbilisi", "第比利斯"}},
	{"亚美尼亚", []string{"AM", "ARM", "EVN", "亚美尼亚", "Armenia", "Yerevan", "埃里温"}},
	{"阿塞拜疆", []string{"AZ", "AZE", "GYD", "阿塞拜疆", "Azerbaijan", "Baku", "巴库"}},
	{"比利时", []string{"BE", "BEL", "BRU", "比利时", "Belgium", "Brussels", "安特卫普", "布鲁塞尔", "Antwerp"}},
}

// RegionEmoji 对应 REGION_EMOJI。
var RegionEmoji = map[string]string{
	"香港": "🇭🇰", "台湾": "🇨🇳", "新加坡": "🇸🇬", "日本": "🇯🇵", "美国": "🇺🇸", "韩国": "🇰🇷", "英国": "🇬🇧",
	"德国": "🇩🇪", "法国": "🇫🇷", "加拿大": "🇨🇦", "澳大利亚": "🇦🇺", "荷兰": "🇳🇱", "俄罗斯": "🇷🇺", "印度": "🇮🇳",
	"土耳其": "🇹🇷", "孟加拉": "🇧🇩", "巴基斯坦": "🇵🇰", "马来西亚": "🇲🇾", "泰国": "🇹🇭", "越南": "🇻🇳", "菲律宾": "🇵🇭",
	"印尼": "🇮🇩", "文莱": "🇧🇳", "柬埔寨": "🇰🇭", "老挝": "🇱🇦", "缅甸": "🇲🇲", "瑞士": "🇨🇭", "意大利": "🇮🇹",
	"西班牙": "🇪🇸", "巴西": "🇧🇷", "阿根廷": "🇦🇷", "墨西哥": "🇲🇽", "哥伦比亚": "🇨🇴", "秘鲁": "🇵🇪", "南非": "🇿🇦",
	"埃及": "🇪🇬", "以色列": "🇮🇱", "阿联酋": "🇦🇪", "阿曼": "🇴🇲", "沙特": "🇸🇦", "波兰": "🇵🇱", "捷克": "🇨🇿",
	"匈牙利": "🇭🇺", "罗马尼亚": "🇷🇴", "希腊": "🇬🇷", "葡萄牙": "🇵🇹", "瑞典": "🇸🇪", "挪威": "🇳🇴", "丹麦": "🇩🇰",
	"芬兰": "🇫🇮", "奥地利": "🇦🇹", "爱尔兰": "🇮🇪", "冰岛": "🇮🇸", "卢森堡": "🇱🇺", "马耳他": "🇲🇹", "斯洛伐克": "🇸🇰",
	"斯洛文尼亚": "🇸🇮", "克罗地亚": "🇭🇷", "拉脱维亚": "🇱🇻", "立陶宛": "🇱🇹", "爱沙尼亚": "🇪🇪", "塞浦路斯": "🇨🇾",
	"列支敦士登": "🇱🇮", "阿尔巴尼亚": "🇦🇱", "安道尔": "🇦🇩", "白俄罗斯": "🇧🇾", "波黑": "🇧🇦", "摩尔多瓦": "🇲🇩",
	"摩纳哥": "🇲🇨", "黑山": "🇲🇪", "北马其顿": "🇲🇰", "塞尔维亚": "🇷🇸", "乌克兰": "🇺🇦", "科索沃": "🇽🇰",
	"圣马力诺": "🇸🇲", "梵蒂冈": "🇻🇦", "格鲁吉亚": "🇬🇪", "亚美尼亚": "🇦🇲", "阿塞拜疆": "🇦🇿", "比利时": "🇧🇪",
	"其他": "🌍",
}

func regionKeywordsOf(region string) []string {
	for _, e := range RegionKeywords {
		if e.Region == region {
			return e.Keywords
		}
	}
	return nil
}

func normalizeBase64Geo(input any) string {
	s := strings.Join(strings.FieldsFunc(js.Trim(strOr(input)), js.IsJSSpace), "")
	if s == "" {
		return ""
	}
	if strings.Contains(s, "%") {
		if d, err := js.DecodeURIComponent(s); err == nil {
			s = d
		}
	}
	s = strings.NewReplacer("-", "+", "_", "/").Replace(s)
	for len(s)%4 != 0 {
		s += "="
	}
	return s
}

var reLikelyBase64 = js.MustRegExp(`^[A-Za-z0-9+/=_-]+$`, "")

func isLikelyBase64(input string) bool {
	s := js.Trim(input)
	if s == "" {
		return false
	}
	if !reLikelyBase64.Test(s) {
		return false
	}
	return js.UTF16Len(s) >= 6
}

// tryDecodeBase64 返回二进制字符串；失败返回 ok=false（对应 null）。
func tryDecodeBase64(input string) (string, bool) {
	if !isLikelyBase64(input) {
		return "", false
	}
	d, err := js.Atob(normalizeBase64Geo(input))
	if err != nil {
		return "", false
	}
	return d, true
}

func geoParseHostPort(value string) (string, string) {
	segment := value
	if segment == "" {
		return "", ""
	}
	if cut := strings.IndexAny(segment, "/?#"); cut != -1 {
		segment = segment[:cut]
	}
	if strings.HasPrefix(segment, "[") {
		if closeBracket := strings.Index(segment, "]"); closeBracket != -1 {
			server := segment[1:closeBracket]
			after := segment[closeBracket+1:]
			port := ""
			if strings.HasPrefix(after, ":") {
				port = strings.Split(after[1:], "/")[0]
			}
			return server, port
		}
	}
	parts := strings.Split(segment, ":")
	if len(parts) >= 2 {
		return parts[0], strings.Split(parts[1], "/")[0]
	}
	return segment, ""
}

// strOr 对应 String(v || "")。
func strOr(v any) string {
	if !js.Truthy(v) {
		return ""
	}
	return js.ToString(v)
}

func isASCIILetterByte(r rune) bool { return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' }

func isShortLatinKeyword(k string) bool {
	if js.UTF16Len(k) > 3 || k == "" {
		return false
	}
	for _, r := range k {
		if !isASCIILetterByte(r) {
			return false
		}
	}
	return true
}

// ExtractNodeRegion 对应 extractNodeRegion。
func ExtractNodeRegion(nodeName any) string {
	name, ok := nodeName.(string)
	if !ok || name == "" {
		return "其他"
	}
	normalized := js.ToLower(name)
	for _, e := range RegionKeywords {
		for _, keyword := range e.Keywords {
			lower := js.ToLower(keyword)
			if isShortLatinKeyword(lower) {
				idx := strings.Index(normalized, lower)
				if idx != -1 {
					before := lastRuneBefore(normalized, idx)
					after := firstRuneFrom(normalized, idx+len(lower))
					if !isASCIILetterByte(before) && !isASCIILetterByte(after) {
						return e.Region
					}
				}
			} else if strings.Contains(normalized, lower) {
				return e.Region
			}
		}
	}
	return "其他"
}

// lastRuneBefore 取 idx 之前的一个字符；JS 中 str[idx-1] 是 UTF-16 码元，对 ASCII 字母判断等价。
func lastRuneBefore(s string, idx int) rune {
	if idx <= 0 {
		return 0
	}
	r, _ := utf8.DecodeLastRuneInString(s[:idx])
	return r
}

func firstRuneFrom(s string, idx int) rune {
	if idx >= len(s) {
		return 0
	}
	for _, r := range s[idx:] {
		return r
	}
	return 0
}

// GetSupportedRegions 对应 getSupportedRegions。
func GetSupportedRegions() []string {
	out := make([]string, len(RegionKeywords))
	for i, e := range RegionKeywords {
		out[i] = e.Region
	}
	return out
}

// GetRegionKeywords 对应 getRegionKeywords。
func GetRegionKeywords(region string) []string {
	if k := regionKeywordsOf(region); k != nil {
		return k
	}
	return []string{}
}

// GetRegionEmoji 对应 getRegionEmoji。
func GetRegionEmoji(region any) string {
	r, isStr := region.(string)
	if e, ok := RegionEmoji[r]; ok && isStr {
		return e
	}
	if r == "其他" || !js.Truthy(region) {
		return "🌍"
	}
	return ""
}

// ContainsRegion 对应 containsRegion。
func ContainsRegion(nodeName, region string) bool {
	if nodeName == "" || region == "" {
		return false
	}
	keywords := regionKeywordsOf(region)
	if keywords == nil {
		return false
	}
	normalized := js.ToLower(nodeName)
	for _, k := range keywords {
		if strings.Contains(normalized, js.ToLower(k)) {
			return true
		}
	}
	return false
}

// NodeInfo 对应 parseNodeInfo 的返回值；Port 可能是字符串或数字（来自 VMess JSON）。
type NodeInfo struct {
	Protocol string
	Name     any // 通常为字符串
	Region   string
	Server   any
	Port     any
	URL      any
}

// Object 转换为 JS 对象（字段顺序与上游一致）。
func (n NodeInfo) Object() *js.Object {
	return js.Obj("protocol", n.Protocol, "name", n.Name, "region", n.Region, "server", n.Server, "port", n.Port, "url", n.URL)
}

var (
	reProtocolPrefix = js.MustRegExp(`^(.*?):\/\/`, "")
	reRemarksParam   = js.MustRegExp(`[?&](remarks|des|remark)=([^&#]+)`, "")
	reSchemePrefix   = js.MustRegExp(`^[^:]*:\/\/`, "")
	reNameSplit      = js.MustRegExp(`[:@?#]`, "")
	reSSRPort        = js.MustRegExp(`^:(\d+):`, "")
	reSSRRemarks     = js.MustRegExp(`(?:^|&)remarks=([^&]*)`, "")
)

func stripJSSpace(s string) string {
	return strings.Map(func(r rune) rune {
		if js.IsJSSpace(r) {
			return -1
		}
		return r
	}, s)
}

func urlSafeToStd(s string) string { return strings.NewReplacer("-", "+", "_", "/").Replace(s) }

func padBase64(s string) string {
	for len(s)%4 != 0 {
		s += "="
	}
	return s
}

// ParseNodeInfo 对应 parseNodeInfo。
func ParseNodeInfo(nodeURL any) NodeInfo {
	u, ok := nodeURL.(string)
	if !ok || u == "" {
		return NodeInfo{Protocol: "unknown", Name: "未命名节点", Region: "其他", URL: nodeURL}
	}
	protocol := "unknown"
	if m := reProtocolPrefix.MatchFirst(u); m != nil {
		protocol = js.ToLower(m.Str(1))
	}
	nodeName := ""
	if hashIndex := strings.LastIndex(u, "#"); hashIndex != -1 {
		nodeName = js.SafeDecodeURIComponent(u[hashIndex+1:])
	}
	if nodeName == "" {
		if m := reRemarksParam.MatchFirst(u); m != nil && m.Str(2) != "" {
			nodeName = js.SafeDecodeURIComponent(m.Str(2))
		}
	}
	if nodeName == "" {
		without := reSchemePrefix.Replace(u, "")
		parts := reNameSplit.Split(without, -1)
		nodeName = parts[0]
		if nodeName == "" {
			nodeName = "未命名节点"
		}
	}
	nodeName = strings.ReplaceAll(nodeName, "🇹🇼", "🇨🇳")

	var server any = ""
	var port any = ""
	var nameValue any // VMess 的 ps 可能不是字符串，按原值返回
	func() {
		defer func() { _ = recover() }()
		switch protocol {
		case "vmess":
			base64Part := strings.Replace(u, "vmess://", "", 1)
			if base64Part != "" && !strings.Contains(base64Part, "@") {
				safeBody := padBase64(urlSafeToStd(base64Part))
				b, err := js.AtobBytes(safeBody)
				if err != nil {
					return
				}
				cfg, err := js.JSONParse(js.TextDecode(b))
				if err != nil {
					return
				}
				if js.IsNullish(cfg) {
					// JS 中访问 null 的属性会抛出异常，被 catch 吞掉
					return
				}
				server = js.Or(objGet(cfg, "add"), objGet(cfg, "host"), "")
				port = js.Or(objGet(cfg, "port"), "")
				if ps := objGet(cfg, "ps"); js.Truthy(ps) {
					nameValue = ps
				}
			}
		case "ss":
			body := u[5:]
			if h := strings.Index(body, "#"); h != -1 {
				body = body[:h]
			}
			if q := strings.Index(body, "?"); q != -1 {
				body = body[:q]
			}
			serverPart := ""
			if atLast := strings.LastIndex(body, "@"); atLast != -1 {
				serverPart = body[atLast+1:]
			} else if decoded, ok := tryDecodeBase64(body); ok && strings.Contains(decoded, "@") {
				serverPart = decoded[strings.LastIndex(decoded, "@")+1:]
			} else {
				serverPart = body
			}
			s, p := geoParseHostPort(serverPart)
			server, port = s, p
		case "ssr":
			payload := u[6:]
			if h := strings.Index(payload, "#"); h != -1 {
				payload = payload[:h]
			}
			if q := strings.Index(payload, "?"); q != -1 {
				payload = payload[:q]
			}
			decoded, ok := tryDecodeBase64(payload)
			if !ok || decoded == "" {
				return
			}
			ssrServer, ssrPort := "", ""
			if strings.HasPrefix(decoded, "[") {
				if closeBracket := strings.Index(decoded, "]"); closeBracket != -1 {
					ssrServer = decoded[1:closeBracket]
					if m := reSSRPort.MatchFirst(decoded[closeBracket+1:]); m != nil {
						ssrPort = m.Str(1)
					}
				}
			} else {
				colonParts := strings.Split(decoded, ":")
				if len(colonParts) >= 2 {
					ssrServer, ssrPort = colonParts[0], colonParts[1]
				}
			}
			server, port = ssrServer, ssrPort
			paramStart := -1
			if slashQ := strings.Index(decoded, "/?"); slashQ != -1 {
				paramStart = slashQ + 2
			} else if qi := strings.Index(decoded, "?"); qi != -1 {
				paramStart = qi + 1
			}
			if paramStart != -1 {
				paramStr := decoded[paramStart:]
				if m := reSSRRemarks.MatchFirst(paramStr); m != nil && m.Str(1) != "" {
					remarksB64 := stripJSSpace(m.Str(1))
					b, err := js.AtobBytes(normalizeBase64Geo(remarksB64))
					if err == nil {
						if rd := js.TextDecode(b); rd != "" {
							nodeName = js.Trim(rd)
						}
					} else if fb, ok := tryDecodeBase64(remarksB64); ok && fb != "" {
						nodeName = js.Trim(fb)
					}
				}
			}
		case "wireguard":
			body := u[len("wireguard://"):]
			if h := strings.Index(body, "#"); h != -1 {
				nodeName = js.SafeDecodeURIComponent(body[h+1:])
				body = body[:h]
			}
			if at := strings.LastIndex(body, "@"); at != -1 {
				serverPart := body[at+1:]
				addrPart := serverPart
				if q := strings.Index(serverPart, "?"); q != -1 {
					addrPart = serverPart[:q]
				}
				if strings.HasPrefix(addrPart, "[") {
					if closeBracket := strings.Index(addrPart, "]"); closeBracket != -1 {
						server = addrPart[1:closeBracket]
						after := addrPart[closeBracket+1:]
						if strings.HasPrefix(after, ":") {
							port = after[1:]
						}
					}
				} else {
					parts := strings.Split(addrPart, ":")
					if len(parts) >= 2 {
						server, port = parts[0], parts[1]
					}
				}
			}
		default:
			// 上游写法为 substring(indexOf('://') + 3)；找不到时等价于 substring(2)（按 UTF-16 计）
			var body string
			if idx := strings.Index(u, "://"); idx != -1 {
				body = u[idx+3:]
			} else {
				body = js.Substring(u, 2, js.UTF16Len(u))
			}
			if h := strings.Index(body, "#"); h != -1 {
				body = body[:h]
			}
			if q := strings.Index(body, "?"); q != -1 {
				body = body[:q]
			}
			atIndex := strings.LastIndex(body, "@")
			serverPart := body
			if atIndex != -1 {
				serverPart = body[atIndex+1:]
			}
			if atIndex == -1 && protocol == "vless" && js.UTF16Len(body) > 20 {
				if b, err := js.AtobBytes(padBase64(urlSafeToStd(body))); err == nil {
					decoded := js.TextDecode(b)
					if strings.Contains(decoded, "@") {
						serverPart = decoded[strings.LastIndex(decoded, "@")+1:]
					}
				}
			}
			if strings.HasPrefix(serverPart, "[") {
				if closeBracket := strings.Index(serverPart, "]"); closeBracket != -1 {
					server = serverPart[1:closeBracket]
					after := serverPart[closeBracket+1:]
					if strings.HasPrefix(after, ":") {
						port = strings.Split(after[1:], "/")[0]
					}
				}
			} else {
				parts := strings.Split(serverPart, ":")
				if len(parts) >= 2 {
					server = parts[0]
					port = strings.Split(parts[1], "/")[0]
				}
			}
		}
	}()
	var name any = nodeName
	if nameValue != nil {
		name = nameValue
	}
	return NodeInfo{Protocol: protocol, Name: name, Region: ExtractNodeRegion(name), Server: server, Port: port, URL: nodeURL}
}

// objGet 对应属性访问 v[key]，非对象（字符串、数字等）返回 undefined。
func objGet(v any, key string) any {
	switch x := v.(type) {
	case *js.Object:
		return x.Get(key)
	case *js.Array:
		if key == "length" {
			return float64(x.Len())
		}
		if i, ok := arrayIndexKey(key); ok {
			return x.At(i)
		}
	case string:
		if key == "length" {
			return float64(js.UTF16Len(x))
		}
		if i, ok := arrayIndexKey(key); ok && i < js.UTF16Len(x) {
			return js.Substring(x, i, i+1)
		}
	}
	return nil
}

func arrayIndexKey(k string) (int, bool) {
	n := 0
	if k == "" {
		return 0, false
	}
	for i := 0; i < len(k); i++ {
		if k[i] < '0' || k[i] > '9' {
			return 0, false
		}
		n = n*10 + int(k[i]-'0')
	}
	return n, true
}

// CalculateRegionStats 对应 geo-utils / node-parser 中的 calculateRegionStats。
func CalculateRegionStats(nodes []*js.Object) *js.Object {
	stats := js.NewObject()
	total := float64(len(nodes))
	for _, node := range nodes {
		region := ExtractNodeRegion(js.Or(node.Get("name"), ""))
		stats.Set(region, js.ToNumber(js.Or(stats.Get(region), 0.0))+1)
	}
	for _, kv := range stats.Entries() {
		count := kv.Value.(float64)
		stats.Set(kv.Key, js.Obj("count", count, "percentage", js.MathRound(count/total*100)))
	}
	return stats
}
