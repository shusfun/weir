package subgroup

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/shusfun/weir/internal/sub"
)

// NodeAttrs 是从节点名解析出的属性：订阅源按它统计，订阅组按它挑选，两处共用这一个实现。
// 机场节点经常改名、增减，属性每次都从最新的名字重新解析，不存库。
type NodeAttrs struct {
	Name   string `json:"name"`
	Region string `json:"region"`
	// Multiplier 为 nil 表示名字里没有标倍率（显示「未标注」），不当作 1 倍。
	Multiplier *float64 `json:"multiplier,omitempty"`
}

// 倍率写法：x2、2x、×0.5、*1.5、2倍、倍率0.5、[0.5x]、0.5X。数字取 0.01–100。
var multiplierPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(?:倍率|rate)\s*[:：]?\s*(\d+(?:\.\d+)?)`),
	regexp.MustCompile(`(\d+(?:\.\d+)?)\s*倍`),
	regexp.MustCompile(`(?i)(?:^|[^a-z0-9.])[x×*]\s*(\d+(?:\.\d+)?)(?:[^0-9.]|$)`),
	regexp.MustCompile(`(?i)(?:^|[^a-z0-9.])(\d+(?:\.\d+)?)\s*[x×](?:[^a-z0-9]|$)`),
}

// ParseMultiplier 从节点名里找倍率；没有或数值不合理时返回 nil。
func ParseMultiplier(name string) *float64 {
	for _, re := range multiplierPatterns {
		m := re.FindStringSubmatch(name)
		if m == nil {
			continue
		}
		v, err := strconv.ParseFloat(m[1], 64)
		if err != nil || v < 0.01 || v > 100 {
			continue
		}
		return &v
	}
	return nil
}

// AttrsOf 解析一条节点链接的属性。
func AttrsOf(link string) NodeAttrs {
	name := strings.TrimSpace(sub.LinkName(link))
	return NodeAttrs{Name: name, Region: sub.ExtractNodeRegion(name), Multiplier: ParseMultiplier(name)}
}
