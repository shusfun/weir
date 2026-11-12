package sub

import "testing"

func TestRegionPatternUsesRE2(t *testing.T) {
	for _, value := range []struct{ pattern, flags string }{{"(?=a)", "i"}, {`(a)\1`, "i"}, {"a", "ii"}, {"a", "g"}} {
		if _, err := CompileRegionPattern(value.pattern, value.flags); err == nil {
			t.Fatal("unsafe or stateful expression accepted", value)
		}
	}
	re, err := CompileRegionPattern("leaf", "")
	if err != nil || !re.MatchString("LEAF") {
		t.Fatal("default case-insensitive predicate lost", err)
	}
}
