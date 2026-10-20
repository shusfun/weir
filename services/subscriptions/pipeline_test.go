package subscriptions

import (
	"encoding/json"
	"strings"
	"testing"
)

// Adapted from MiSub 4952c16 tests/unit/operator-runner.test.js (MIT).
func TestUpstreamFilterAndRestrictedScript(t *testing.T) {
	links := []string{"ss://YWVzLTEyOC1nY206cGFzcw@example.com:8388#HK-01", "ss://YWVzLTEyOC1nY206cGFzcw@example.com:8389#US-01", "ss://YWVzLTEyOC1nY206cGFzcw@example.com:8390#JP-01"}
	var operators []Operator
	if e := json.Unmarshal([]byte(`[{"type":"filter","params":{"include":{"enabled":true,"rules":["HK|US"]},"exclude":{"enabled":true,"rules":["US"]}}},{"type":"script","params":{"dsl":[{"action":"rename","template":"{name} Scripted"},{"action":"filter","field":"name","op":"contains","value":"HK-01"}]}}]`), &operators); e != nil {
		t.Fatal(e)
	}
	out, e := Run(links, operators, "clash")
	if e != nil {
		t.Fatal(e)
	}
	if len(out) != 1 || !strings.Contains(out[0], "HK-01%20Scripted") {
		t.Fatalf("unexpected output %v", out)
	}
	// Credentials and protocol query fields must survive a rename.
	raw := "vless://11111111-1111-4111-a111-111111111111@example.com:443?security=reality&pbk=public-key&sid=abcd&type=tcp&flow=xtls-rprx-vision#HK-01"
	out, e = Run([]string{raw}, operators[1:], "singbox")
	if e != nil {
		t.Fatal(e)
	}
	if len(out) != 1 || !strings.Contains(out[0], "pbk=public-key") || !strings.Contains(out[0], "sid=abcd") {
		t.Fatal("protocol fields lost")
	}
}
func TestDedupRetainsLongerNameAndStableOrder(t *testing.T) {
	var ops []Operator
	json.Unmarshal([]byte(`[{"type":"dedup","params":{"includeProtocol":true}}]`), &ops)
	out, e := Run([]string{"ss://YWVzLTEyOC1nY206cGFzcw@example.com:8388#A", "ss://YWVzLTEyOC1nY206cGFzcw@example.com:8388#Longer"}, ops, "base64")
	if e != nil || len(out) != 1 || !strings.HasSuffix(out[0], "#Longer") {
		t.Fatalf("%v %v", out, e)
	}
}
func TestForbiddenCodeAndComplexity(t *testing.T) {
	for _, input := range []string{"process.exit()", "fetch('https://example.com')", "name.constructor('code')", "upper(" + strings.Repeat("upper(", 40) + "name" + strings.Repeat(")", 41)} {
		if _, e := evalValue(input, map[string]any{"name": "a"}, 0); e == nil {
			t.Fatalf("unsafe expression accepted %s", input)
		}
	}
	var ops []Operator
	json.Unmarshal([]byte(`[{"type":"script","params":{"code":"return $proxies"}}]`), &ops)
	if _, e := Run(nil, ops, ""); e == nil {
		t.Fatal("legacy code accepted")
	}
}
