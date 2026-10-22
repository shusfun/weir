package jscompat

import "testing"

// 不依赖 Node 的基础用例；完整的逐字节对照见 internal/sub 的 oracle 测试。

func TestNumberToString(t *testing.T) {
	cases := map[float64]string{0: "0", 1e21: "1e+21", 1e-7: "1e-7", 123.456: "123.456", 0.000001: "0.000001", 5e-324: "5e-324", -2.5: "-2.5"}
	for in, want := range cases {
		if got := NumberToString(in); got != want {
			t.Errorf("NumberToString(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestAtob(t *testing.T) {
	if s, err := Atob("YWJj ZA"); err != nil || s != "abcd" {
		t.Fatalf("Atob = %q, %v", s, err)
	}
	for _, bad := range []string{"YQ=", "Y", "a-b", "YWJj="} {
		if _, err := Atob(bad); err == nil {
			t.Errorf("Atob(%q) 应当失败", bad)
		}
	}
}

func TestURL(t *testing.T) {
	u, err := ParseURL("trojan://pa^ss@Host.COM:443?sni=a b#n a")
	if err != nil {
		t.Fatal(err)
	}
	if u.Href() != "trojan://pa%5Ess@Host.COM:443?sni=a%20b#n%20a" || u.Hostname() != "Host.COM" || u.PortString() != "443" {
		t.Fatalf("解析结果不符: %s", u.Href())
	}
	if _, err := ParseURL("vless://u@host:99999"); err == nil {
		t.Fatal("端口越界应当失败")
	}
	if u, _ := ParseURL("https://EXAMPLE.com:443/a/../b"); u.Href() != "https://example.com/b" {
		t.Fatalf("特殊 scheme 规范化不符: %s", u.Href())
	}
}

func TestYAMLRoundTrip(t *testing.T) {
	v := Obj("name", "🇭🇰 香港", "port", 443.0, "yes", "yes", "list", Arr("*.lan", 1.0, true, Null), "empty", NewObject())
	out, err := YAMLDump(v, YAMLDumpOptions{DoubleQuote: true})
	if err != nil {
		t.Fatal(err)
	}
	want := "name: 🇭🇰 香港\nport: 443\n\"yes\": \"yes\"\nlist:\n  - \"*.lan\"\n  - 1\n  - true\n  - null\nempty: {}\n"
	if out != want {
		t.Fatalf("YAMLDump =\n%s\nwant\n%s", out, want)
	}
	back, err := YAMLLoad(out)
	if err != nil || Describe(back) != Describe(v) {
		t.Fatalf("YAMLLoad 往返不一致: %s vs %s (%v)", Describe(back), Describe(v), err)
	}
}

func TestRegExpJSSemantics(t *testing.T) {
	if got := MustRegExp(`(?<a>x)(y)`, "").Replace("xy", "$2$<a>$1"); got != "yxx" {
		t.Errorf("命名分组编号应与 JS 一致，得到 %q", got)
	}
	if got := MustRegExp(`[^a-z]`, "g").Replace("🇭🇰a", "_"); got != "____a" {
		t.Errorf("非 u 模式应按 UTF-16 码元匹配，得到 %q", got)
	}
	if got := MustRegExp(`[^a-z]`, "gu").Replace("🇭🇰a", "_"); got != "__a" {
		t.Errorf("u 模式应按码点匹配，得到 %q", got)
	}
	if _, err := NewRegExp(`(?i)a`, ""); err == nil {
		t.Error("JS 不支持内联标志，应当报错")
	}
}
