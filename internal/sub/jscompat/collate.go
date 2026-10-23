package jscompat

import (
	"math"
	"sort"
	"strings"
	"sync"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"
)

var (
	collatorOnce sync.Once
	collatorMu   sync.Mutex
	numericBase  *collate.Collator
)

// LocaleCompareNumericBase 对应 a.localeCompare(b, undefined, {numeric: true, sensitivity: 'base'})。
// 上游运行在 Cloudflare Workers（默认区域 en-US），这里固定使用英文排序规则，避免随部署机器的系统区域变化。
func LocaleCompareNumericBase(a, b string) int {
	collatorOnce.Do(func() {
		numericBase = collate.New(language.AmericanEnglish, collate.Numeric, collate.IgnoreCase,
			collate.IgnoreDiacritics, collate.IgnoreWidth)
	})
	collatorMu.Lock()
	defer collatorMu.Unlock()
	return numericBase.CompareString(a, b)
}

// SortStable 对应 Array.prototype.sort(compareFn)（V8 的排序是稳定的）。
func SortStable(items []any, cmp func(a, b any) float64) {
	sort.SliceStable(items, func(i, j int) bool {
		r := cmp(items[i], items[j])
		return !math.IsNaN(r) && r < 0
	})
}

// SortStrings 对应无比较函数的 Array.prototype.sort（按 UTF-16 码元比较）。
func SortStrings(items []string) {
	sort.SliceStable(items, func(i, j int) bool { return CompareUTF16(items[i], items[j]) < 0 })
}

// CompareUTF16 按 UTF-16 码元顺序比较字符串（JS 的 < 运算符）。
func CompareUTF16(a, b string) int {
	if isASCII(a) && isASCII(b) {
		return strings.Compare(a, b)
	}
	ra, rb := []rune(a), []rune(b)
	for i := 0; i < len(ra) && i < len(rb); i++ {
		if ra[i] == rb[i] {
			continue
		}
		ua, ub := utf16Key(ra[i]), utf16Key(rb[i])
		if ua < ub {
			return -1
		}
		return 1
	}
	switch {
	case len(ra) < len(rb):
		return -1
	case len(ra) > len(rb):
		return 1
	}
	return 0
}

func utf16Key(r rune) uint32 {
	if r >= 0x10000 {
		hi := 0xD800 + ((r - 0x10000) >> 10)
		return uint32(hi)<<16 | uint32(0xDC00+((r-0x10000)&0x3FF))
	}
	return uint32(r) << 16
}
