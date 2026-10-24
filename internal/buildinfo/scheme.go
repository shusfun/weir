package buildinfo

import (
	"fmt"
	"strconv"
	"strings"
)

// Scheme 是唯一发布版本。主版本 0–9，次版本 0–99，修订号 0–999。
type Scheme struct {
	Major int
	Minor int
	Patch int
}

func ParseScheme(raw string) (Scheme, bool) {
	text := strings.TrimSpace(raw)
	text = strings.TrimPrefix(text, "Weir version:")
	text = strings.TrimPrefix(text, "MiGate version:")
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "v")
	parts := strings.Split(text, ".")
	if len(parts) != 3 {
		return Scheme{}, false
	}
	numbers := [3]int{}
	limits := [3]int{9, 99, 999}
	for i, part := range parts {
		if part == "" || (len(part) > 1 && part[0] == '0') {
			return Scheme{}, false
		}
		value, err := strconv.Atoi(part)
		if err != nil || value < 0 || value > limits[i] {
			return Scheme{}, false
		}
		numbers[i] = value
	}
	return Scheme{Major: numbers[0], Minor: numbers[1], Patch: numbers[2]}, true
}

func (s Scheme) String() string {
	return fmt.Sprintf("%d.%d.%d", s.Major, s.Minor, s.Patch)
}

func (s Scheme) Compare(other Scheme) int {
	if s.Major != other.Major {
		return cmpInt(s.Major, other.Major)
	}
	if s.Minor != other.Minor {
		return cmpInt(s.Minor, other.Minor)
	}
	return cmpInt(s.Patch, other.Patch)
}

// Next 只把修订号加 1。修订号到 999 才进位次版本，次版本到 99 才进位主版本。
// 已经是 9.99.999 时拒绝继续递增。
func (s Scheme) Next() (Scheme, bool) {
	if s.Major == 9 && s.Minor == 99 && s.Patch == 999 {
		return s, false
	}
	if s.Patch < 999 {
		s.Patch++
		return s, true
	}
	s.Patch = 0
	if s.Minor < 99 {
		s.Minor++
		return s, true
	}
	s.Minor = 0
	s.Major++
	return s, true
}

func cmpInt(left, right int) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}
