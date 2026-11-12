package subgroup

import (
	"net/url"
	"strings"
	"testing"
)

func TestConverterInlineInputAndExplicitSettings(t *testing.T) {
	link := "trojan://password@node.example:443#name"
	options := RenderOptions{RuleLevel: "none", EnableUDP: true, EnableTFO: true}
	raw, err := ConverterURL("https://converter.example/sub", link, "clash", options)
	if err != nil { t.Fatal(err) }
	u, err := url.Parse(raw)
	if err != nil || u.Query().Get("url") != link || u.Query().Get("target") != "clash" || u.Query().Get("tfo") != "true" || u.Query().Get("list") != "true" {
		t.Fatalf("invalid inline request: %v", err)
	}
	options.ExternalTemplate = "remote:https://templates.example/rules.ini"
	options.RuleLevel = "std"
	raw, err = ConverterURL("https://converter.example/sub", link, "clash", options)
	if err != nil { t.Fatal(err) }
	u, _ = url.Parse(raw)
	if u.Query().Get("config") != "https://templates.example/rules.ini" || u.Query().Has("list") { t.Fatal("template was silently replaced") }
}

func TestConverterRejectsUnsupportedAndFetchInputs(t *testing.T) {
	link := "trojan://password@node.example:443#name"
	for _, tc := range []struct { name, endpoint, nodes, target string; options RenderOptions }{
		{"private", "http://127.0.0.1/sub", link, "clash", RenderOptions{RuleLevel:"none"}},
		{"query", "https://converter.example/sub?token=secret", link, "clash", RenderOptions{RuleLevel:"none"}},
		{"fetch", "https://converter.example/sub", "https://private.example/sub", "clash", RenderOptions{RuleLevel:"none"}},
		{"file", "https://converter.example/sub", "file:///etc/passwd", "clash", RenderOptions{RuleLevel:"none"}},
		{"target", "https://converter.example/sub", link, "egern", RenderOptions{RuleLevel:"none"}},
		{"local template", "https://converter.example/sub", link, "clash", RenderOptions{ExternalTemplate:"custom:1"}},
		{"dns", "https://converter.example/sub", link, "clash", RenderOptions{RuleLevel:"none", CustomDNS:"enable: true"}},
		{"size", "https://converter.example/sub", link+strings.Repeat("a", 64<<10), "clash", RenderOptions{RuleLevel:"none"}},
	} { t.Run(tc.name, func(t *testing.T) { if _, err := ConverterURL(tc.endpoint,tc.nodes,tc.target,tc.options); err == nil { t.Fatal("unsupported input accepted") } }) }
}
