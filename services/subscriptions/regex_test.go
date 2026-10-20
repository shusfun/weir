package subscriptions

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestRegexRenameCaptureTemplate(t *testing.T) {
	ctx := map[string]any{"regionZh": "香港"}
	var rules []json.RawMessage
	json.Unmarshal([]byte(`[{"pattern":"(?<city>HK)-(\\d+)","replacement":"{regionZh} $2 $$ $<city>"}]`), &rules)
	name, err := regexRename("hk-12 HK-13", rules, ctx)
	if err != nil || name != "香港 12 $ hk 香港 13 $ HK" {
		t.Fatalf("%s %v", name, err)
	}
	got, err := RenderOperatorTemplate("{g_city:UPPER}-{g2}", ctx)
	if err != nil || got != "HK-12" {
		t.Fatalf("%s %v", got, err)
	}
}
func TestUpstreamReplacementComparison(t *testing.T) {
	if os.Getenv("MIGATE_UPSTREAM_TESTS") != "1" {
		t.Skip("Node only for local reference")
	}
	samples := []map[string]string{}
	for _, replacement := range []string{"$1x", "$2", "$12", "$99", "$$", "$&", "$`", "$'", "$<city>", "[$1]-$<city>"} {
		for _, flags := range []string{"i", "gi"} {
			rule := regexRule{Pattern: `(?<city>hk)-(\d+)`, Flags: flags, Replacement: replacement}
			raw, _ := json.Marshal(rule)
			name := "pre HK-12 hk-34 post"
			got, err := regexRename(name, []json.RawMessage{raw}, map[string]any{})
			if err != nil {
				t.Fatal(err)
			}
			samples = append(samples, map[string]string{"name": name, "pattern": rule.Pattern, "flags": flags, "replacement": replacement, "expected": got})
		}
	}
	raw, _ := json.Marshal(samples)
	file := filepath.Join(t.TempDir(), "regex.json")
	os.WriteFile(file, raw, 0600)
	script := `const fs=require('fs');for(const s of JSON.parse(fs.readFileSync(process.argv[1],'utf8'))){const v=s.name.replace(new RegExp(s.pattern,s.flags),s.replacement).trim();if(v!==s.expected)throw Error(JSON.stringify({s,v}));}`
	if out, err := exec.Command("node", "-e", script, file).CombinedOutput(); err != nil {
		t.Fatalf("%s %v", out, err)
	}
}
