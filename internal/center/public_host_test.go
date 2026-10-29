package center

import (
	"strings"
	"testing"
)

// testHosts 是测试里 VPS 公网地址可用的域名与解析结果；值为空表示 DNS 临时故障。
var testHosts = map[string]string{
	"mi.caowo.baby":     "154.9.26.146",
	"xn--fiqs8s.com":    "203.0.113.7",
	"lan.example.com":   "192.168.1.10",
	"flaky.example.com": "",
}

func TestNormalizePublicHost(t *testing.T) {
	for in, want := range map[string]string{
		"https://mi.caowo.baby/":            "mi.caowo.baby",
		"  HTTP://Mi.Caowo.Baby:8443/a?b#c": "mi.caowo.baby",
		"user:pw@mi.caowo.baby":             "mi.caowo.baby",
		"mi.caowo.baby.":                    "mi.caowo.baby",
		"ｍｉ。ｃａｏｗｏ．ｂａｂｙ":                     "mi.caowo.baby",
		"154.9.26.146:443":                  "154.9.26.146",
		"１５４．９．２６．１４６":                      "154.9.26.146",
		"[2001:db8::1]:443":                 "2001:db8::1",
		"2001:DB8::1":                       "2001:db8::1",
		"中国.com":                            "xn--fiqs8s.com",
		"mi caowo.baby":                     "",
		"mi_caowo.baby":                     "",
		"localhost":                         "",
		"https://":                          "",
		"[2001:db8::1":                      "",
	} {
		if got := normalizePublicHost(in); got != want {
			t.Errorf("normalizePublicHost(%q) = %q, want %q", in, got, want)
		}
	}
}

// 公网地址按整理后的值保存；整理后仍不能用的分别给出原因，原值不变。
func TestUpdateGatePublicHost(t *testing.T) {
	_, admin, g := setupGate(t)
	path := "/api/v1/admin/gates/" + itoa(int(g.id))
	for _, c := range [][2]string{
		{"https://mi.caowo.baby/", "mi.caowo.baby"},
		{"154.9.26.146:443", "154.9.26.146"},
		{"中国.com", "xn--fiqs8s.com"},
	} {
		if v := mustJSON(t, admin, "PATCH", path, map[string]any{"public_host": c[0]}, 200); v["public_host"] != c[1] {
			t.Fatalf("%q saved as %v, want %q", c[0], v["public_host"], c[1])
		}
	}
	for in, want := range map[string]struct {
		status int
		code   string
	}{
		"https://mi.caowo.bab": {422, "public_host_unresolved"},
		"mi caowo.baby":        {400, "invalid_public_host"},
		"127.0.0.1":            {422, "public_host_not_public"},
		"10.0.0.5":             {422, "public_host_not_public"},
		"100.64.1.1":           {422, "public_host_not_public"},
		"lan.example.com":      {422, "public_host_not_public"},
		"flaky.example.com":    {503, "public_host_lookup_failed"},
	} {
		w := admin.do("PATCH", path, map[string]any{"public_host": in}, nil)
		if w.Code != want.status || !strings.Contains(w.Body.String(), want.code) {
			t.Fatalf("%q = %d %s, want %d %s", in, w.Code, w.Body.String(), want.status, want.code)
		}
	}
	if v := mustJSON(t, admin, "GET", path, nil, 200); v["public_host"] != "xn--fiqs8s.com" {
		t.Fatalf("rejected input changed public_host to %v", v["public_host"])
	}
	if v := mustJSON(t, admin, "PATCH", path, map[string]any{"public_host": " "}, 200); v["public_host"] != "" {
		t.Fatalf("blank public_host = %v", v["public_host"])
	}
}
