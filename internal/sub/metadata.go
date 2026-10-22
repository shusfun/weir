package sub

import (
	"strings"

	js "github.com/imzyb/MiGate/internal/sub/jscompat"
)

// 移植自 functions/modules/utils/metadata-extractor.js

var (
	reMultiplier = js.MustRegExp(`(?:^|\s|[\[\(\x])([0-9]+(?:\.[0-9]+)?)(?:\s*倍|x|X)(?:\s|$|[\]\)])`, "i")
	reTag        = js.MustRegExp(`(?:\[|\(|#|\s)([a-zA-Z0-9_\-]+)(?:\]|\)|\s|$)`, "g")
	reEmojiFlag  = js.MustRegExp(`[\u{1F1E6}-\u{1F1FF}]{2}`, "gu")
	reAllDigits  = js.MustRegExp(`^\d+$`, "")
	reCleanMeta  = js.MustRegExp(`[\[\]\(\)#]`, "g")
	reMultiSpace = js.MustRegExp(`\s{2,}`, "g")
)

// ExtractNodeMetadata 对应 extractNodeMetadata，返回字段顺序与上游一致的对象。
func ExtractNodeMetadata(name any) *js.Object {
	s, isStr := name.(string)
	if !isStr || s == "" {
		return js.Obj("originalName", name, "cleanName", name, "multiplier", 1.0, "tags", js.Arr(),
			"region", "其他", "flag", "")
	}
	cleanName := s
	multiplier := 1.0
	if m := reMultiplier.MatchFirst(s); m != nil {
		multiplier = js.ParseFloat(m.Str(1))
		// String.prototype.replace(string, ...) 只替换第一次出现
		cleanName = js.Trim(strings.Replace(cleanName, m.Str(0), " ", 1))
	}
	flag := ""
	if flags := reEmojiFlag.MatchAllStrings(s); len(flags) > 0 {
		flag = flags[0]
	}
	region := ExtractNodeRegion(s)
	autoFlag := flag
	if autoFlag == "" {
		autoFlag = GetRegionEmoji(region)
	}
	tags := []string{}
	re := reTag.Clone()
	for {
		m := re.Exec(s)
		if m == nil {
			break
		}
		tag := m.Str(1)
		if !reAllDigits.Test(tag) && js.UTF16Len(tag) > 2 {
			tags = append(tags, tag)
		}
	}
	cleanName = reEmojiFlag.Replace(cleanName, "")
	cleanName = reCleanMeta.Replace(cleanName, "")
	cleanName = js.Trim(reMultiSpace.Replace(cleanName, " "))
	if cleanName == "" {
		cleanName = s
	}
	return js.Obj("originalName", s, "cleanName", cleanName, "multiplier", multiplier, "region", region,
		"flag", autoFlag, "tags", js.StrArr(uniqueStrings(tags)))
}

// uniqueStrings 对应 Array.from(new Set(list))。
func uniqueStrings(list []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range list {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
