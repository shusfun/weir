package sub

import (
	"errors"
	"regexp"
	"strings"
)

// Save validation and rendering share a linear-time predicate. Stateful
// JavaScript global/sticky flags cannot be applied independently to nodes.
func CompileRegionPattern(pattern, flags string) (*regexp.Regexp, error) {
	if len(pattern) == 0 || len(pattern) > 512 {
		return nil, errors.New("invalid region pattern length")
	}
	if flags == "" {
		flags = "i"
	}
	seen := map[rune]bool{}
	for _, flag := range flags {
		if !strings.ContainsRune("ims", flag) || seen[flag] {
			return nil, errors.New("region flags must contain unique i, m or s")
		}
		seen[flag] = true
	}
	return regexp.Compile("(?" + flags + ")" + pattern)
}
