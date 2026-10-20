package subscription

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNamePlusUpstreamComparison(t *testing.T) {
	if os.Getenv("MIGATE_UPSTREAM_TESTS") != "1" {
		t.Skip("requires local Node fixed-source oracle")
	}
	for _, link := range []string{
		"trojan://synthetic@example.com:443#HK+A%2BB",
		"vless://12345678-1234-1234-1234-123456789abc@example.com:443#HK+A%2BB",
		"ss://YWVzLTEyOC1nY206c3ludGhldGlj@example.com:8388#HK+A%2BB",
		"hysteria2://synthetic@example.com:443#HK+A%2BB",
	} {
		for _, enabled := range []bool{false, true} {
			input, _ := json.Marshal(map[string]any{"link": link, "enabled": enabled})
			cmd := exec.Command("node", filepath.Join("..", "..", "third_party", "misub", "reference", "compare-name-plus.mjs"))
			cmd.Stdin = bytes.NewReader(input)
			raw, err := cmd.Output()
			if err != nil {
				t.Fatal("upstream oracle failed", err)
			}
			var reference string
			if err = json.Unmarshal(raw, &reference); err != nil {
				t.Fatal(err)
			}
			want, err := ParseLink(reference)
			if err != nil {
				t.Fatal("oracle output invalid", err)
			}
			links, err := DecodeInventoryWithNameOptions([]byte(link), enabled)
			if err != nil {
				t.Fatal(err)
			}
			got, err := ParseLink(links[0])
			if err != nil || got.Remark != want.Remark {
				t.Fatal("name differs from fixed upstream", err)
			}
		}
	}
}

func TestInventoryNamePlusDoesNotChangeConnection(t *testing.T) {
	uri := "trojan://p+a%2B%2525@example.com:443?security=tls&type=ws&path=%2Fa%2Bb%252B&host=example.com#HK+%2B+%252B"
	for _, encoded := range []bool{false, true} {
		body := uri
		if encoded {
			body = base64.StdEncoding.EncodeToString([]byte(body))
		}
		for _, enabled := range []bool{false, true} {
			links, err := DecodeInventoryWithNameOptions([]byte(body), enabled)
			if err != nil || len(links) != 1 {
				t.Fatal("decode failed", err)
			}
			n, err := ParseLink(links[0])
			if err != nil {
				t.Fatal(err)
			}
			want := "HK+++%2B"
			if enabled {
				want = "HK + %2B"
			}
			if n.Remark != want {
				t.Fatalf("name %q != %q", n.Remark, want)
			}
			if strings.Split(links[0], "#")[0] != strings.Split(uri, "#")[0] || n.Password != "p+a+%25" {
				t.Fatal("name option changed connection credentials/parameters")
			}
		}
	}
}

func TestInventoryNamePlusPreservesStructuredNames(t *testing.T) {
	vmess := "vmess://" + base64.StdEncoding.EncodeToString([]byte(`{"v":"2","ps":"HK+A","add":"example.com","port":"443","id":"12345678-1234-1234-1234-123456789abc","aid":"0","net":"tcp","type":"none","tls":"tls"}`))
	for _, body := range []string{
		`{"outbounds":[{"type":"http","tag":"HK+A","server":"example.com","server_port":8080}]}`,
		"proxies:\n - {name: HK+A, type: ss, server: example.com, port: 8388, cipher: aes-128-gcm, password: synthetic}",
		"[Proxy]\nHK+A = ss, example.com, 8388, encrypt-method=aes-128-gcm, password=synthetic",
		vmess,
	} {
		links, err := DecodeInventoryWithNameOptions([]byte(body), true)
		if err != nil || len(links) != 1 {
			t.Fatal("structured decode failed", err)
		}
		n, err := ParseLink(links[0])
		if err != nil || n.Remark != "HK+A" {
			t.Fatal("structured name changed", err)
		}
	}
}
