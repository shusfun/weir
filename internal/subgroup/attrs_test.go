package subgroup

import "testing"

func TestNodeAttrsMultiplier(t *testing.T) {
	cases := map[string]float64{
		"香港 01 x2":     2,
		"日本 2x 高速":     2,
		"美国 ×0.5":      0.5,
		"新加坡 *1.5":     1.5,
		"台湾 3倍":        3,
		"韩国 倍率0.8":     0.8,
		"英国 [0.5x]":    0.5,
		"德国 | 倍率: 1.2": 1.2,
	}
	for name, want := range cases {
		got := ParseMultiplier(name)
		if got == nil || *got != want {
			t.Errorf("%q: got %v want %v", name, got, want)
		}
	}
	for _, name := range []string{"香港 01", "IPLC-HK-02", "Tokyo 3", "剩余流量：100GB", "x-ray 节点"} {
		if got := ParseMultiplier(name); got != nil {
			t.Errorf("%q must be unlabeled, got %v", name, *got)
		}
	}
	a := AttrsOf("trojan://p@h.example:443#%E9%A6%99%E6%B8%AF%2001%20x2")
	if a.Name != "香港 01 x2" || a.Region == "" || a.Multiplier == nil || *a.Multiplier != 2 {
		t.Fatalf("attrs=%+v", a)
	}
}
