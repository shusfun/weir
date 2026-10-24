package ai

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParsePatchRejectsSecretsAndPlaceholders(t *testing.T) {
	p, err := ParsePatch("好的：\n```json\n{\"set\":{\"ws_path\":\"/x\",\"tls_alpn\":null},\"explain\":\"换路径\"}\n```")
	if err != nil || p.Set["ws_path"] != "/x" || p.Explain != "换路径" {
		t.Fatalf("patch = %+v %v", p, err)
	}
	for _, bad := range []string{`{"set":{"reality_private_key":"x"}}`, `{"set":{"ss_password":"y"}}`, `{"set":{"note":"<redacted>"}}`, `{"set":{}}`, `not json`} {
		if _, err := ParsePatch(bad); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
	before := map[string]any{"ws_path": "/a", "reality_private_key": "PRIV", "tls_alpn": "h2"}
	after := Apply(before, p)
	if after["reality_private_key"] != "PRIV" || after["tls_alpn"] != nil {
		t.Fatalf("apply = %v", after)
	}
	d := Diff(before, after)
	b, _ := json.Marshal(d)
	if len(d) != 2 || strings.Contains(string(b), "PRIV") {
		t.Fatalf("diff = %s", b)
	}
	if r := Redact(before); r["reality_private_key"] != placeholder || r["ws_path"] != "/a" {
		t.Fatalf("redact = %v", r)
	}
}
