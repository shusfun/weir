package contracts

import (
	"encoding/json"
	"strings"
	"testing"
)

func xhttpTestClient() NodeTestClient {
	return NodeTestClient{Target: "node-test", XHTTP: &XHTTPProbeClient{Server: "proxy.example.test", Port: 443, UUID: "12345678-1234-1234-1234-123456789abc", SNI: "tls.example.test", Host: "http.example.test", Path: "/transport", Mode: "auto", Fingerprint: "chrome", ALPN: []string{"h2"}}}
}

func TestXHTTPProbeContract(t *testing.T) {
	c := xhttpTestClient()
	req := NodeProbeTask{Client: c, Credentials: FetchSecrets{URL: "https://center.example.test/api/v1/node-test/download", Authorization: "test"}, Kernels: map[string]Kernel{"xray": {Version: "26.3.27", SHA256: strings.Repeat("a", 64)}}}
	validate := func(kind string, req NodeProbeTask) error {
		b, _ := json.Marshal(req)
		return ValidateTaskPayload(kind, TaskVersion, b)
	}
	if err := validate(TaskNodeProbeXray, req); err != nil {
		t.Fatal(err)
	}
	if validate(TaskNodeProbe, req) == nil {
		t.Fatal("legacy task accepted XHTTP")
	}
	req.Kernels["mihomo"] = req.Kernels["xray"]
	if validate(TaskNodeProbeXray, req) == nil {
		t.Fatal("extra kernel accepted")
	}
	delete(req.Kernels, "xray")
	if validate(TaskNodeProbeXray, req) == nil {
		t.Fatal("wrong kernel accepted")
	}
	for name, change := range map[string]func(*NodeTestClient){
		"uuid":        func(c *NodeTestClient) { c.XHTTP.UUID = "not-a-uuid" },
		"mode":        func(c *NodeTestClient) { c.XHTTP.Mode = "unknown" },
		"path":        func(c *NodeTestClient) { c.XHTTP.Path = "/test\nheader" },
		"host":        func(c *NodeTestClient) { c.XHTTP.Host = "test/path" },
		"fingerprint": func(c *NodeTestClient) { c.XHTTP.Fingerprint = "unsupported" },
		"alpn":        func(c *NodeTestClient) { c.XHTTP.ALPN = []string{"h3"} },
		"extra-proxy": func(c *NodeTestClient) { c.Proxies = []map[string]any{{"name": "direct"}} },
	} {
		t.Run(name, func(t *testing.T) {
			c := xhttpTestClient()
			change(&c)
			if c.Validate() == nil {
				t.Fatal("invalid client accepted")
			}
		})
	}
	if err := c.Loopback(12345); err != nil {
		t.Fatal(err)
	}
	if c.XHTTP.Server != "127.0.0.1" || c.XHTTP.SNI != "tls.example.test" || c.XHTTP.Host != "http.example.test" {
		t.Fatal("redirect changed TLS or HTTP identity")
	}
	cfg := c.Config(12346)
	outs := cfg["outbounds"].([]any)
	if len(outs) != 1 || outs[0].(map[string]any)["protocol"] != "vless" {
		t.Fatal("client has a fallback")
	}
	if cfg["inbounds"].([]any)[0].(map[string]any)["listen"] != "127.0.0.1" {
		t.Fatal("public listener")
	}
	b, _ := json.Marshal(req)
	b = []byte(strings.Replace(string(b), `"xhttp":{`, `"xhttp":{"outbounds":[],`, 1))
	if ValidateTaskPayload(TaskNodeProbeXray, TaskVersion, b) == nil {
		t.Fatal("arbitrary configuration accepted")
	}
}
