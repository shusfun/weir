package control

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"github.com/imzyb/MiGate/internal/subscription"
	"gopkg.in/yaml.v3"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestExtendedPinnedUpstreamComparison(t *testing.T) {
	if os.Getenv("MIGATE_UPSTREAM_TESTS") != "1" {
		t.Skip("pinned Node oracle is local only")
	}
	for _, link := range []string{"snell://p%3Ass@example.com:443?version=4&reuse=false&tfo=true&obfs=tls&obfs-host=cdn.example#Snell", wireGuardFixture()} {
		input, _ := json.Marshal(map[string]any{"link": link})
		cmd := exec.Command("node", filepath.Join("..", "..", "third_party", "misub", "reference", "compare-extended.mjs"))
		cmd.Stdin = bytes.NewReader(input)
		result, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("extended upstream oracle: %s", result)
		}
		var upstream map[string]any
		if json.Unmarshal(result, &upstream) != nil || upstream == nil {
			t.Fatal("invalid upstream fixture")
		}
		body, err := clashOutput([]string{link})
		if err != nil {
			t.Fatal(err)
		}
		var cfg map[string]any
		if yaml.Unmarshal(body, &cfg) != nil {
			t.Fatal("invalid Clash")
		}
		actual := cfg["proxies"].([]any)[0].(map[string]any)
		// Existing converters include a default UDP capability. Compare every
		// explicit upstream connection field, not unrelated default metadata.
		for key, value := range upstream {
			a, _ := json.Marshal(actual[key])
			b, _ := json.Marshal(value)
			if !bytes.Equal(a, b) {
				t.Fatalf("upstream %s field %s differs", upstream["type"], key)
			}
		}
	}
}

func wireGuardFixture() string {
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	return "wireguard://" + url.PathEscape(key) + "@example.com:51820?" + url.Values{"publickey": {key}, "presharedkey": {key}, "address": {"10.2.0.2/32,fd00::2/128"}, "allowedips": {"0.0.0.0/0,::/0"}, "reserved": {"1,2,3"}, "mtu": {"1280"}, "keepalive": {"25"}}.Encode() + "#WireGuard"
}
func TestExtendedProtocolFieldRoundtrips(t *testing.T) {
	for _, link := range []string{"snell://p%3Ass@example.com:443?version=4&reuse=false&tfo=true&obfs=tls&obfs-host=cdn.example#Snell", wireGuardFixture()} {
		before, err := subscription.ParseLink(link)
		if err != nil {
			t.Fatal(err)
		}
		clash, err := clashOutput([]string{link})
		if err != nil {
			t.Fatal(err)
		}
		formats := map[string][]byte{"clash": clash}
		if before.Protocol == "wireguard" {
			formats["surge"], err = iniOutput([]string{link}, "surge")
			if err != nil {
				t.Fatal(err)
			}
			formats["singbox"], err = singboxOutput([]string{link})
			if err != nil {
				t.Fatal(err)
			}
		} else {
			for _, target := range []string{"surge", "loon"} {
				formats[target], err = iniOutput([]string{link}, target)
				if err != nil {
					t.Fatal(err)
				}
			}
		}
		for target, body := range formats {
			links, err := subscription.DecodeInventory(body)
			if err != nil || len(links) != 1 {
				t.Fatalf("%s extended import: %v", target, err)
			}
			after, err := subscription.ParseLink(links[0])
			if err != nil {
				t.Fatal(err)
			}
			var x, y subscription.Settings
			json.Unmarshal([]byte(before.SettingsJSON), &x)
			json.Unmarshal([]byte(after.SettingsJSON), &y)
			if before.Password != after.Password || before.Address != after.Address || before.Port != after.Port || !reflect.DeepEqual(x, y) {
				t.Fatalf("%s %s lost connection fields", target, before.Protocol)
			}
			if target == "singbox" && os.Getenv("MIGATE_SINGBOX_BINARY") != "" {
				file := filepath.Join(t.TempDir(), "client.json")
				os.WriteFile(file, body, 0600)
				if out, e := exec.Command(os.Getenv("MIGATE_SINGBOX_BINARY"), "check", "-c", file).CombinedOutput(); e != nil {
					t.Fatalf("actual modern WireGuard schema rejected: %s", out)
				}
			}
		}
	}
}

func TestWireGuardLoonLocalGrammarAndRefusal(t *testing.T) {
	u, _ := url.Parse(wireGuardFixture())
	q := u.Query()
	for _, key := range []string{"presharedkey", "allowedips", "keepalive"} {
		q.Del(key)
	}
	q.Set("address", "10.2.0.2/32")
	u.RawQuery = q.Encode()
	body, err := iniOutput([]string{u.String()}, "loon")
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range [][]byte{body, []byte(strings.Split(strings.Split(string(body), "[Proxy]\n")[1], "\n\n")[0])} {
		links, err := subscription.DecodeInventory(input)
		if err != nil || len(links) != 1 {
			t.Fatalf("Loon import: %v", err)
		}
		before, _ := subscription.ParseLink(u.String())
		after, _ := subscription.ParseLink(links[0])
		if before.Password != after.Password || before.SettingsJSON != after.SettingsJSON {
			t.Fatal("Loon fields changed")
		}
	}
	if _, err := iniOutput([]string{wireGuardFixture()}, "loon"); err == nil {
		t.Fatal("dropped unsupported Loon fields")
	}
	for _, input := range []string{
		"[Proxy]\nw = wireguard, section-name = a\n[WireGuard a]\nprivate-key=\nprivate-key=ignored",
		"[Proxy]\nw = wireguard, section-name = missing",
		"[Proxy]\nw = wireguard, section-name = a\n[WireGuard a]\nunknown=ignored",
	} {
		if _, err := subscription.DecodeInventory([]byte(input)); err == nil {
			t.Fatal("malformed WireGuard INI accepted")
		}
	}
}

func TestWireGuardLoonModernTemplateRoundtrip(t *testing.T) {
	u, _ := url.Parse(wireGuardFixture())
	q := u.Query()
	q.Del("keepalive") // No corresponding field in the fixed Loon renderer.
	q.Set("dns", "1.1.1.1,2606:4700:4700::1111")
	u.Host = "[2001:db8::1]:51820"
	u.RawQuery = q.Encode()
	body, err := iniOutput([]string{u.String()}, "loon")
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"interface-ip=10.2.0.2/32", "interface-ipV6=fd00::2/128", "dns=1.1.1.1", "dnsV6=2606:4700:4700::1111", "preshared-key=", "reserved=[1,2,3]", "endpoint=[2001:db8::1]:51820"} {
		if !bytes.Contains(body, []byte(field)) {
			t.Fatalf("modern Loon missing %s", field)
		}
	}
	links, err := subscription.DecodeInventory(body)
	if err != nil || len(links) != 1 {
		t.Fatalf("modern Loon import: %v", err)
	}
	before, _ := subscription.ParseLink(u.String())
	after, _ := subscription.ParseLink(links[0])
	if before.Password != after.Password || before.Address != after.Address || before.SettingsJSON != after.SettingsJSON {
		t.Fatal("modern Loon lost connection fields")
	}
	if os.Getenv("MIGATE_UPSTREAM_TESTS") == "1" {
		clash, err := clashOutput([]string{u.String()})
		if err != nil {
			t.Fatal(err)
		}
		var config map[string]any
		yaml.Unmarshal(clash, &config)
		proxy := config["proxies"].([]any)[0].(map[string]any)
		// Fixed renderer expects separate ip / ipv6, unlike Clash inventory.
		proxy["ip"], proxy["ipv6"] = "10.2.0.2/32", "fd00::2/128"
		input, _ := json.Marshal(proxy)
		cmd := exec.Command("node", filepath.Join("..", "..", "third_party", "misub", "reference", "compare-loon-wireguard.mjs"))
		cmd.Stdin = bytes.NewReader(input)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("fixed Loon renderer: %s", out)
		}
		var original string
		if json.Unmarshal(out, &original) != nil {
			t.Fatal("invalid oracle output")
		}
		upstreamLinks, err := subscription.DecodeInventory([]byte(original))
		if err != nil || len(upstreamLinks) != 1 {
			t.Fatalf("fixed modern Loon import: %v", err)
		}
		upstream, _ := subscription.ParseLink(upstreamLinks[0])
		if upstream.SettingsJSON != before.SettingsJSON || upstream.Password != before.Password {
			t.Fatal("fixed template connection fields differ")
		}
	}
	line := strings.Split(strings.Split(string(body), "[Proxy]\n")[1], "\n\n")[0]
	for _, invalid := range []string{
		strings.Replace(line, "mtu=1280", "mtu=1280,mtu=1300", 1),
		strings.Replace(line, "reserved=[1,2,3]", "reserved=[1,2,999]", 1),
		strings.Replace(line, "interface-ipV6=fd00::2/128", "interface-ipV6=10.0.0.1/32", 1),
		strings.Replace(line, "peers=[{", "peers=[{script=evil,", 1),
		strings.Replace(line, "}]", "},{endpoint=other:443}]", 1),
		line + ",unknown=true",
		strings.TrimSuffix(line, "]"),
	} {
		if _, err := subscription.DecodeInventory([]byte(invalid)); err == nil {
			t.Fatal("ambiguous or unsupported modern Loon accepted")
		}
	}
	q.Set("dns", "1.1.1.1,8.8.8.8")
	u.RawQuery = q.Encode()
	if _, err := iniOutput([]string{u.String()}, "loon"); err == nil {
		t.Fatal("silently discarded second Loon DNS address")
	}
}
func TestExtendedProtocolsRefuseLossAndUnsafeInput(t *testing.T) {
	for _, link := range []string{"snell://p@example.com:443?version=6", "snell://p@example.com:443?reuse=maybe", "snell://p@example.com:443?psk=q", "snell://p@example.com:443?unknown=discarded", "snell://p@example.com:443?obfs-host=discarded", "wireguard://invalid@example.com:443?publickey=invalid&address=10.1.1.1/32"} {
		if _, e := subscription.ParseLink(link); e == nil {
			t.Fatal("invalid extended input accepted")
		}
	}
	if _, e := iniOutput([]string{"snell://p@example.com:443?ecn=true"}, "surge"); e == nil {
		t.Fatal("dropped Snell ECN")
	}
	if _, e := singboxOutput([]string{"snell://p@example.com:443"}); e == nil {
		t.Fatal("invented Snell sing-box support")
	}
	if _, e := singboxOutput([]string{wireGuardFixture() + ""}); e != nil {
		t.Fatal(e)
	}
}

func TestWireGuardTemplateKeepsEndpointOrderAndNames(t *testing.T) {
	template := templateFixture("singbox")
	tm, e := decodeOutputTemplate(template)
	if e != nil {
		t.Fatal(e)
	}
	body, e := renderOutputTemplate([]string{wireGuardFixture(), "http://u:p@example.com:80#HK"}, tm)
	if e != nil {
		t.Fatal(e)
	}
	var cfg map[string]any
	if json.Unmarshal(body, &cfg) != nil {
		t.Fatal("invalid output")
	}
	endpoints := cfg["endpoints"].([]any)
	if len(endpoints) != 1 {
		t.Fatal("template dropped endpoint")
	}
	tag := endpoints[0].(map[string]any)["tag"].(string)
	found := false
	for _, raw := range cfg["outbounds"].([]any) {
		p := raw.(map[string]any)
		if p["type"] == "wireguard" {
			t.Fatal("endpoint emitted as legacy outbound")
		}
		if p["type"] == "selector" {
			for _, member := range p["outbounds"].([]any) {
				found = found || member == tag
			}
		}
	}
	if !found {
		t.Fatal("template lost WireGuard selector membership")
	}
}
