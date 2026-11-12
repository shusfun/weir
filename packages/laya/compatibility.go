package laya

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
)

type DegradationPolicy string

const (
	DegradeReject   DegradationPolicy = "reject"
	DegradeBaseOnly DegradationPolicy = "base_only"
)

type Compatibility struct {
	InstalledVersion string
	VerifiedRange    string
	OutOfRange       DegradationPolicy
}

type CompatibilityDecision struct {
	Verified         bool
	EnableBase       bool
	EnableBackground bool
	Reason           string
}

// CheckCompatibility applies a caller-supplied tested range and degradation policy.
func CheckCompatibility(c Compatibility) (CompatibilityDecision, error) {
	if c.OutOfRange != DegradeReject && c.OutOfRange != DegradeBaseOnly {
		return CompatibilityDecision{}, errors.New("explicit out-of-range policy is required")
	}
	inside, err := inRange(c.InstalledVersion, c.VerifiedRange)
	if err != nil {
		return CompatibilityDecision{}, err
	}
	if inside {
		return CompatibilityDecision{Verified: true, EnableBase: true, EnableBackground: true}, nil
	}
	switch c.OutOfRange {
	case DegradeReject:
		return CompatibilityDecision{Reason: "installed version is outside the caller's verified range"}, nil
	case DegradeBaseOnly:
		return CompatibilityDecision{EnableBase: true, Reason: "installed version is outside the caller's verified range; background analysis is paused"}, nil
	default:
		return CompatibilityDecision{}, errors.New("explicit out-of-range policy is required")
	}
}

type version struct {
	core [3]int
	pre  []string
}

var semanticVersion = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$`)

func parseVersion(s string) (version, bool) {
	var v version
	if !semanticVersion.MatchString(strings.TrimSpace(s)) {
		return v, false
	}
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	s, _, _ = strings.Cut(s, "+")
	core, pre, hasPre := strings.Cut(s, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return v, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return v, false
		}
		v.core[i] = n
	}
	if hasPre {
		if pre == "" {
			return v, false
		}
		v.pre = strings.Split(pre, ".")
		for _, identifier := range v.pre {
			if _, err := strconv.Atoi(identifier); err == nil && len(identifier) > 1 && identifier[0] == '0' {
				return v, false
			}
		}
	}
	return v, true
}
func compareVersion(a, b version) int {
	for i := range a.core {
		if a.core[i] < b.core[i] {
			return -1
		}
		if a.core[i] > b.core[i] {
			return 1
		}
	}
	if len(a.pre) == 0 && len(b.pre) > 0 {
		return 1
	}
	if len(b.pre) == 0 && len(a.pre) > 0 {
		return -1
	}
	for i := 0; i < len(a.pre) && i < len(b.pre); i++ {
		x, xe := strconv.Atoi(a.pre[i])
		y, ye := strconv.Atoi(b.pre[i])
		switch {
		case xe == nil && ye == nil:
			if x < y {
				return -1
			}
			if x > y {
				return 1
			}
		case xe == nil:
			return -1
		case ye == nil:
			return 1
		case a.pre[i] < b.pre[i]:
			return -1
		case a.pre[i] > b.pre[i]:
			return 1
		}
	}
	if len(a.pre) < len(b.pre) {
		return -1
	}
	if len(a.pre) > len(b.pre) {
		return 1
	}
	return 0
}
func inRange(vs, expression string) (bool, error) {
	v, ok := parseVersion(vs)
	if !ok {
		return false, errors.New("invalid installed version")
	}
	fields := strings.Fields(expression)
	if len(fields) == 0 {
		return false, errors.New("empty verified range")
	}
	match := true
	for _, f := range fields {
		op := "="
		for _, candidate := range []string{">=", "<=", ">", "<", "="} {
			if strings.HasPrefix(f, candidate) {
				op, f = candidate, f[len(candidate):]
				break
			}
		}
		bound, ok := parseVersion(f)
		if !ok {
			return false, errors.New("invalid verified range")
		}
		cmp := compareVersion(v, bound)
		match = match && map[string]bool{">=": cmp >= 0, "<=": cmp <= 0, ">": cmp > 0, "<": cmp < 0, "=": cmp == 0}[op]
	}
	return match, nil
}
