package subscriptions

import (
	"encoding/json"
	"testing"
)

func TestUnknownOperatorSettingsRejected(t *testing.T) {
	for _, raw := range []string{
		`{"type":"filter","params":{"inlcude":{}}}`,
		`{"type":"filter","params":{"include":{"enabled":true,"rules":[{"pattern":"HK","unknown":true}]}}}`,
		`{"type":"filter","params":{"protocols":{"enabled":true,"values":[],"invert":true}}}`,
		`{"type":"rename","params":{"template":{"enabled":true,"template":"{name}","unknown":true}}}`,
		`{"type":"rename","params":{"regex":{"rules":[{"pattern":"HK","replaceAll":true}]}}}`,
		`{"type":"sort","params":{"keys":[{"key":"name","locale":"zh"}]}}`,
		`{"type":"dedup","params":{"keep":"first"}}`,
		`{"type":"script","params":{"dsl":[{"action":"rename","template":"{name}","shell":"id"}]}}`,
		`{"type":"script","params":{"dsl":[{"action":"filter","field":"name","op":"contains","value":"HK","invert":true}]}}`,
		`{"type":"script","params":{"dsl":[{"action":"filter","field":"unsupported"}]}}`,
		`{"type":"script","params":{"timeout":10}}`,
		`{"type":"dedup","params":null}`,
		`{"type":"dedup","params":{},"unknown":true}`,
	} {
		for _, links := range [][]string{nil, {"ss://YWVzLTEyOC1nY206cGFzcw@example.com:8388#HK"}} {
			var op Operator
			err := json.Unmarshal([]byte(raw), &op)
			if err == nil {
				_, err = Run(links, []Operator{op}, "base64")
			}
			if err == nil {
				t.Fatalf("silently accepted unsupported settings: %s", raw)
			}
		}
	}
}

func TestStrictOperatorCompatibility(t *testing.T) {
	// Historical camelCase keys and omitted optional dedup settings survive.
	var op Operator
	if err := json.Unmarshal([]byte(`{"type":"dedup","params":{"includeProtocol":true}}`), &op); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(nil, []Operator{op, {Type: "dedup"}}, "base64"); err != nil {
		t.Fatal(err)
	}
	var dst struct{ Enabled bool }
	if err := decodeOperator([]byte(`{"enabled":true} {}`), &dst); err == nil {
		t.Fatal("trailing settings accepted")
	}
}
