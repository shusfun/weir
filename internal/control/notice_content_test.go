package control

import (
	"strings"
	"testing"
)

func TestNoticeContentWhitelist(t *testing.T) {
	input := `<p><strong>维护通知</strong><br>请先阅读 <a href="https://example.com/help?a=1&amp;b=2" target="_blank" rel="opener">使用指南</a></p><ul><li><code>config</code></li></ul><pre>line 1
line 2</pre>`
	nodes, e := parseNoticeContent(input, "html")
	if e != nil {
		t.Fatal(e)
	}
	if len(nodes) != 3 || nodes[0].Tag != "p" || nodes[0].Children[3].Href != "https://example.com/help?a=1&b=2" || nodes[2].Tag != "pre" {
		t.Fatal("markup structure lost", nodes)
	}
	for _, raw := range []string{`<script>alert(1)</script>`, `<img src=x onerror=alert(1)>`, `<svg><a href="https://example.com">X</a></svg>`, `<p style="color:red">X</p>`, `<a href="javascript:alert(1)">X</a>`, `<a href="java&#x73;cript:alert(1)">X</a>`, `<a href="//example.com">X</a>`, `<a href="http://example.com">X</a>`, `<a href="https://user:pass@example.com">X</a>`, `<a href="https://example.com" onclick="alert(1)">X</a>`, `<a href="https://example.com" href="javascript:alert(1)">X</a>`, `<a target="frame">X</a>`, `<!--comment--><p>X</p>`, `<iframe>X</iframe>`, `<p class="fixed">X</p>`, `<a href="https://example.com\evil">X</a>`, strings.Repeat("<b>", 14) + "X" + strings.Repeat("</b>", 14), strings.Repeat("<br>", 257)} {
		if _, e := parseNoticeContent(raw, "html"); e == nil {
			t.Fatal("unsafe markup accepted", raw)
		}
	}
	if _, e := parseNoticeContent("<b>literal</b>", "text"); e != nil {
		t.Fatal(e)
	}
	if _, e := parseNoticeContent("ok", "markdown"); e == nil {
		t.Fatal("unknown format accepted")
	}
}
