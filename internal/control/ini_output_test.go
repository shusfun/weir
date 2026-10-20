package control

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Order of named options is irrelevant; positional options remain ordered.
func normalizeINILine(line string) string {
	parts := strings.Split(line, ", ")
	pos, opts := []string{}, []string{}
	for i, p := range parts {
		// Remote decorative icons aren't part of the connection contract.
		if strings.HasPrefix(p, "icon=") {
			continue
		}
		if i == 0 || !strings.Contains(p, "=") {
			pos = append(pos, p)
		} else {
			opts = append(opts, p)
		}
	}
	sort.Strings(opts)
	return strings.Join(append(pos, opts...), ", ")
}
func TestINIUpstreamNodeComparison(t *testing.T) {
	if os.Getenv("MIGATE_UPSTREAM_TESTS") != "1" {
		t.Skip("requires Node for pinned upstream oracle")
	}
	links := []string{
		"ss://YWVzLTEyOC1nY206cGFzcw@example.com:8388#HK",
		"vmess://eyJ2IjoiMiIsInBzIjoiSEsiLCJhZGQiOiJleGFtcGxlLmNvbSIsInBvcnQiOjQ0MywiaWQiOiIxMTExMTExMS0xMTExLTQxMTEtYTExMS0xMTExMTExMTExMTEiLCJhaWQiOjAsIm5ldCI6IndzIiwidGxzIjoidGxzIiwic25pIjoiZXhhbXBsZS5vcmciLCJwYXRoIjoiL3dzIiwiaG9zdCI6ImNkbi5leGFtcGxlLmNvbSJ9",
		"trojan://secret@example.com:443?sni=example.org#HK",
		"trojan://secret@example.com:443?sni=example.org&type=ws&path=/ws&host=cdn.example.com#HK",
		"anytls://secret@example.com:443?sni=example.org#HK",
		"https://user:secret@example.com:443?sni=example.org#HK",
		"vless://11111111-1111-4111-a111-111111111111@example.com:443?security=reality&pbk=testkey&sid=abcd&fp=chrome&flow=xtls-rprx-vision#HK",
	}
	count := 0
	for _, target := range []string{"surge", "loon", "quanx"} {
		for _, link := range links {
			out, err := iniOutput([]string{link}, target)
			if err != nil {
				continue
			}
			clash, err := clashOutput([]string{link})
			if err != nil {
				t.Fatal(err)
			}
			var cfg map[string]any
			yaml.Unmarshal(clash, &cfg)
			proxy := cfg["proxies"].([]any)[0].(map[string]any)
			if proxy["type"] == "http" && proxy["tls"] == true {
				proxy["type"] = "https"
			}
			input, _ := json.Marshal(map[string]any{"target": target, "proxy": proxy})
			cmd := exec.Command("node", filepath.Join("..", "..", "third_party", "misub", "reference", "compare-ini.mjs"))
			cmd.Stdin = bytes.NewReader(input)
			result, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("upstream oracle failed: %s", result)
			}
			var line string
			if err = json.Unmarshal(result, &line); err != nil {
				t.Fatal(err)
			}
			actual := ""
			for _, candidate := range strings.Split(string(out), "\n") {
				if strings.HasPrefix(candidate, "HK = ") || strings.Contains(candidate, "tag=HK") {
					actual = candidate
					break
				}
			}
			if normalizeINILine(actual) != normalizeINILine(line) {
				t.Fatalf("%s fields differ\nGo: %s\nJS: %s", target, actual, line)
			}
			count++
		}
	}
	if count < 15 {
		t.Fatalf("unexpected supported matrix size: %d", count)
	}
	t.Logf("%d node conversions compared with pinned upstream", count)
}

func TestINIUnsafeAndUnsupportedFieldsFail(t *testing.T) {
	for _, target := range []string{"surge", "loon", "quanx"} {
		for _, link := range []string{"trojan://secret%0A%5BRule%5D@example.com:443", "trojan://secret@example.com:443?type=grpc&serviceName=a", "hysteria2://secret@example.com:443?obfs=salamander&obfs-password=x", "trojan://secret@example.com:443?alpn=h2,http/1.1"} {
			if _, err := iniOutput([]string{link}, target); err == nil {
				t.Fatalf("%s lost field or injected line", target)
			}
		}
		out, err := iniOutput([]string{"trojan://secret@example.com:443#PROXY", "trojan://secret@example.com:444#bad%0A%5BRule%5D"}, target)
		if err != nil || bytes.Count(out, []byte("[Rule]")) > 1 || bytes.Contains(out, []byte("tag=PROXY\n")) || bytes.Contains(out, []byte("\nPROXY = trojan")) {
			t.Fatal("unsafe or conflicting name survived", err)
		}
	}
}
