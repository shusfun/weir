package control

import (
	"encoding/base64"
	"encoding/json"
	"gopkg.in/yaml.v3"
	"strings"
	"testing"
)

func TestVMessGRPCPreservesServiceAndTLS(t *testing.T) {
	b, _ := json.Marshal(map[string]any{"v": "2", "add": "example.com", "port": 443, "id": "11111111-1111-4111-a111-111111111111", "net": "grpc", "path": "grpc-service", "tls": "tls", "sni": "example.org", "alpn": "h2", "fp": "chrome"})
	link := "vmess://" + base64.StdEncoding.EncodeToString(b)
	for _, convert := range []func([]string) ([]byte, error){clashOutput, singboxOutput} {
		out, err := convert([]string{link})
		if err != nil {
			t.Fatal(err)
		}
		for _, field := range []string{"grpc-service", "example.org", "h2", "chrome"} {
			if !strings.Contains(string(out), field) {
				t.Fatal("VMess gRPC field lost", field)
			}
		}
	}
}

func TestConvertedRealityAndGRPCFields(t *testing.T) {
	for _, network := range []string{"tcp", "grpc"} {
		link := "vless://11111111-1111-4111-a111-111111111111@example.com:443?security=reality&pbk=public-key&sid=abcd&type=" + network + "&sni=example.org&fp=chrome&alpn=h2&flow=xtls-rprx-vision#HK"
		if network == "grpc" {
			link = strings.Replace(link, "&flow=xtls-rprx-vision", "", 1)
			link = strings.Replace(link, "#HK", "&serviceName=my-service#HK", 1)
		}
		clash, e := clashOutput([]string{link})
		if e != nil {
			t.Fatal(e)
		}
		sing, e := singboxOutput([]string{link})
		if e != nil {
			t.Fatal(e)
		}
		for _, b := range [][]byte{clash, sing} {
			for _, field := range []string{"public-key", "abcd", "example.org", "chrome", "h2"} {
				if !strings.Contains(string(b), field) {
					t.Fatalf("missing %s in %s", field, b)
				}
			}
			if network == "grpc" && !strings.Contains(string(b), "my-service") {
				t.Fatal("gRPC service lost")
			}
		}
		var cm map[string]any
		if e = yaml.Unmarshal(clash, &cm); e != nil {
			t.Fatal(e)
		}
		var sm map[string]any
		if e = json.Unmarshal(sing, &sm); e != nil {
			t.Fatal(e)
		}
	}
}
func TestUnknownTransportAndNodesFailExplicitly(t *testing.T) {
	for _, link := range []string{"vless://11111111-1111-4111-a111-111111111111@example.com:443?type=xhttp#unsupported", "unknown://credentials@example.com:80"} {
		if _, e := clashOutput([]string{link}); e == nil {
			t.Fatalf("unsupported link silently converted: %s", link)
		}
		if _, e := singboxOutput([]string{link}); e == nil {
			t.Fatal("unsupported singbox link silently converted")
		}
	}
}

func TestShadowsocksOutputs(t *testing.T) {
	links := []string{"ss://YWVzLTEyOC1nY206cGFzcw@example.com:8388#HK"}
	clash, e := clashOutput(links)
	if e != nil {
		t.Fatal(e)
	}
	var cm map[string]any
	if e = yaml.Unmarshal(clash, &cm); e != nil {
		t.Fatal(e)
	}
	p := cm["proxies"].([]any)[0].(map[string]any)
	if p["type"] != "ss" || p["cipher"] != "aes-128-gcm" || p["password"] != "pass" {
		t.Fatal("SS fields lost")
	}
	sing, e := singboxOutput(links)
	if e != nil {
		t.Fatal(e)
	}
	var sm map[string]any
	json.Unmarshal(sing, &sm)
	p = sm["outbounds"].([]any)[1].(map[string]any)
	if p["type"] != "shadowsocks" || p["method"] != "aes-128-gcm" || p["password"] != "pass" {
		t.Fatal("SS fields lost")
	}
}
