package agent

import (
	"context"
	"github.com/imzyb/MiGate/packages/contracts"
	"net/http"
	"path/filepath"
	"testing"
	"time"
)

func TestTLSNameCannotChangeIdentityOrDisableVerification(t *testing.T) {
	if _, err := centerClient(Config{Center: "https://center.example", CenterDialAddress: "0.0.0.0:443"}, time.Second); err == nil {
		t.Fatal("nonloopback tunnel accepted")
	}
	if _, err := centerClient(Config{Center: "https://center.example", TLSServerName: "other.example"}, time.Second); err == nil {
		t.Fatal("different certificate identity accepted")
	}
	c, err := centerClient(Config{Center: "https://center.example", TLSServerName: "CENTER.EXAMPLE"}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	config := c.Transport.(*http.Transport).TLSClientConfig
	if config.InsecureSkipVerify || config.ServerName != "CENTER.EXAMPLE" {
		t.Fatal("TLS identity verification changed")
	}
}

func TestTaskBoundariesAndPersistence(t *testing.T) {
	a, e := New(Config{DeviceID: 8, DataDir: filepath.Join(t.TempDir(), "data")})
	if e != nil {
		t.Fatal(e)
	}
	defer a.DB.Close()
	task := contracts.Task{ID: 1, DeviceID: 9, Operation: "inbound.upsert", Revision: 1, ExpiresAt: time.Now().Add(time.Minute), Payload: map[string]any{"id": 1, "port": 24445, "protocol": "vless"}}
	if a.execute(context.Background(), task).Status != "failed" || len(a.State.Inbounds) != 0 {
		t.Fatal("wrong target accepted")
	}
	task.DeviceID = 8
	task.ExpiresAt = time.Now().Add(-time.Minute)
	if a.execute(context.Background(), task).Status != "failed" {
		t.Fatal("expired task accepted")
	}
	for _, port := range []int{80, 443, 8443, 0, 65536} {
		task.Payload["port"] = port
		if a.mutate(task) == nil {
			t.Fatalf("reserved/invalid port %d accepted", port)
		}
	}
	task.Payload["port"] = 24445
	if e = a.mutate(task); e != nil {
		t.Fatal(e)
	}
	if e = a.mutate(task); e != nil || len(a.State.Inbounds) != 1 {
		t.Fatal("duplicate inbound")
	}
	if e = a.persist(); e != nil {
		t.Fatal(e)
	}
	if err := a.mutate(contracts.Task{Operation: "core.stop", Payload: map[string]any{"core": "xray"}}); err != nil {
		t.Fatal(err)
	}
	if err := a.persist(); err != nil {
		t.Fatal(err)
	}
	b, e := New(a.Config)
	if e != nil {
		t.Fatal(e)
	}
	defer b.DB.Close()
	if len(b.State.Inbounds) != 1 {
		t.Fatal("state lost on restart")
	}
	if !b.State.CoreDisabled["xray"] {
		t.Fatal("desired stopped core lost on restart")
	}
	if b.mutate(contracts.Task{Operation: "core.stop", Payload: map[string]any{"core": "x-ui"}}) == nil {
		t.Fatal("unowned core accepted")
	}
	if err := b.mutate(contracts.Task{Operation: "core.start", Payload: map[string]any{"core": "xray"}}); err != nil || b.State.CoreDisabled["xray"] {
		t.Fatal("start did not clear stopped desired state")
	}
	for _, op := range []string{"shell", "exec", "unknown", "certificate.import"} {
		task.Operation = op
		if a.mutate(task) == nil {
			t.Fatalf("unsupported operation accepted %s", op)
		}
	}
}
func TestStaleGrantAndLocalExpiry(t *testing.T) {
	a := &Agent{}
	task := contracts.Task{Operation: "grant.upsert", Revision: 5, Payload: map[string]any{"grant_id": 1, "inbound_id": 1, "allocated": 32 << 20, "enabled": true, "expires": time.Now().Add(-time.Minute).Unix(), "credential": map[string]any{"uuid": "example"}}}
	if e := a.mutate(task); e != nil {
		t.Fatal(e)
	}
	if a.State.Grants[0].Enabled {
		t.Fatal("expired grant enabled")
	}
	task.Revision = 4
	if a.mutate(task) == nil {
		t.Fatal("stale version accepted")
	}
}
