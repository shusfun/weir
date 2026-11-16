package center

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/nodeprobe"
	"github.com/shusfun/weir/packages/contracts"
)

func TestXHTTPNodeClientPreservesParameters(t *testing.T) {
	base := "vless://12345678-1234-1234-1234-123456789abc@proxy.example.test:443?type=xhttp&security=tls&sni=tls.example.test&host=http.example.test&path=%2Fupload&mode=packet-up&fp=chrome&alpn=h2%2Chttp%2F1.1"
	c, err := nodeClient(base)
	if err != nil {
		t.Fatal(err)
	}
	x := c.XHTTP
	if c.Kernel() != "xray" || x == nil || x.SNI != "tls.example.test" || x.Host != "http.example.test" || x.Path != "/upload" || x.Mode != "packet-up" || x.Fingerprint != "chrome" || len(x.ALPN) != 2 {
		t.Fatalf("parameters lost: %+v", x)
	}
	for _, query := range []string{"allowInsecure=1", "spki=hash", "pinSHA256=hash", "flow=xtls-rprx-vision", "extra=%7B%7D", "serviceName=grpc", "obfs=test"} {
		if _, err := nodeClient(base + "&" + query); err == nil {
			t.Fatalf("silently accepted %s", query)
		}
	}
	u, _ := url.Parse(base)
	q := u.Query()
	q.Set("security", "reality")
	u.RawQuery = q.Encode()
	if _, err := nodeClient(u.String()); err == nil {
		t.Fatal("unsupported security accepted")
	}
}

func TestXHTTPDeployedNodeUsesNativeClient(t *testing.T) {
	s, admin, n := readyDeployedNode(t, "ss2022")
	n = s.Store.Ent.Node.UpdateOne(n).SetKernel("xray").SetProtocol("vless").SetNetwork("xhttp").SetSecurity("tls").SetSettings(map[string]any{"xhttp_path": "/test", "xhttp_mode": "auto", "tls_sni": "proxy.example.test"}).SaveX(t.Context())
	if _, err := s.rotateProbeCredential(t.Context(), n); err != nil {
		t.Fatal(err)
	}
	g := s.Store.Ent.Gate.GetX(t.Context(), n.GateID)
	s.Store.Ent.Gate.UpdateOne(g).SetKernelsRunning(map[string]string{"xray": "26.3.27"}).SetAppliedRevision(g.ConfigRevision).SetLastSeenAt(time.Now()).ExecX(t.Context())
	called := false
	stubDeployedProbe(t, func(ctx context.Context, o nodeprobe.Options) (nodeprobe.Result, error) {
		called = true
		if o.Client.XHTTP == nil || o.Client.Kernel() != "xray" || o.Client.Validate() != nil || !strings.HasPrefix(o.Target, "https://") {
			t.Error("wrong native client")
		}
		return nodeprobe.Result{TCP: nodeprobe.Check{Status: "passed"}, UDP: nodeprobe.Check{Status: "passed"}}, nil
	})
	result := finishNodeTest(t, s, startNodeTest(t, admin, n.ID))
	if !called || result.Status != "passed" {
		t.Fatalf("native test not executed: %+v", result)
	}
	ctx := t.Context()
	caps := contracts.CapabilityDocument{Protocol: contracts.AgentProtocol, Version: "old", Arch: "amd64", Tasks: map[string]int{contracts.TaskNodeProbe: contracts.TaskVersion}}
	if err := s.Store.Tasks.SetCapabilities(ctx, g.ID, caps); err != nil {
		t.Fatal(err)
	}
	w := admin.do("POST", "/api/v1/admin/nodes/"+itoa(n.ID)+"/test", map[string]any{"gate_id": g.ID}, nil)
	if w.Code != 409 || !strings.Contains(w.Body.String(), "agent_upgrade_required") {
		t.Fatalf("old Gate queued native task: %d %s", w.Code, w.Body.String())
	}
}
