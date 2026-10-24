package dsh

import (
	"errors"
	"strconv"
	"strings"
)

// 语义化版本比较（含预发布号）：判断 Weir 插件声明的兼容范围是否包含当前 dsh 版本。

type semver struct {
	core [3]int
	pre  []string
}

func parseSemver(s string) (semver, bool) {
	var v semver
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if i := strings.IndexByte(s, '+'); i >= 0 {
		s = s[:i]
	}
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
	}
	return v, true
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// compareSemver 按语义化版本规则比较：正式版大于同号的预发布版，预发布号逐段比较（数字段小于字母段）。
func compareSemver(a, b semver) int {
	for i := range a.core {
		if c := cmpInt(a.core[i], b.core[i]); c != 0 {
			return c
		}
	}
	switch {
	case len(a.pre) == 0 && len(b.pre) == 0:
		return 0
	case len(a.pre) == 0:
		return 1
	case len(b.pre) == 0:
		return -1
	}
	for i := 0; i < len(a.pre) && i < len(b.pre); i++ {
		x, y := a.pre[i], b.pre[i]
		xn, xerr := strconv.Atoi(x)
		yn, yerr := strconv.Atoi(y)
		switch {
		case xerr == nil && yerr == nil:
			if c := cmpInt(xn, yn); c != 0 {
				return c
			}
		case xerr == nil:
			return -1
		case yerr == nil:
			return 1
		case x != y:
			return strings.Compare(x, y)
		}
	}
	return cmpInt(len(a.pre), len(b.pre))
}

// InRange 判断 version 是否满足 rng：空格分隔的比较式（>=、>、<=、<、=），全部满足才算，
// 例如 ">=0.2.0-0 <0.3.0-0" 包含 0.2.x 的全部正式版与预发布版、不含 0.3.0 的预发布版。
func InRange(version, rng string) (bool, error) {
	v, ok := parseSemver(version)
	if !ok {
		return false, errors.New("版本号无效：" + version)
	}
	fields := strings.Fields(rng)
	if len(fields) == 0 {
		return false, errors.New("兼容范围为空")
	}
	match := true
	for _, f := range fields {
		op := "="
		for _, o := range []string{">=", "<=", ">", "<", "="} {
			if strings.HasPrefix(f, o) {
				op, f = o, f[len(o):]
				break
			}
		}
		w, ok := parseSemver(f)
		if !ok {
			return false, errors.New("兼容范围无效：" + rng)
		}
		c := compareSemver(v, w)
		match = match && map[string]bool{">=": c >= 0, "<=": c <= 0, ">": c > 0, "<": c < 0, "=": c == 0}[op]
	}
	return match, nil
}

// ValidRange 检查兼容范围的写法。
func ValidRange(rng string) bool {
	_, err := InRange("0.0.0", rng)
	return err == nil
}
