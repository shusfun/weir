//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/gate"
)

func TestKernelBatchRollbackNative(t *testing.T) {
	dir := t.TempDir()
	p := gate.Paths{CoresDir: filepath.Join(dir, "cores"), KernelsDir: filepath.Join(dir, "kernels")}
	if err := os.MkdirAll(p.KernelsDir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, spec := range []struct{ name, env string }{{"xray", "WEIR_E2E_XRAY"}, {"sing-box", "WEIR_E2E_SINGBOX"}} {
		b, err := os.ReadFile(os.Getenv(spec.env))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(p.KernelsDir, spec.name+"-fixture"), b, 0755); err != nil {
			t.Fatal(err)
		}
	}
	h := gate.NewHelper(p)
	t.Cleanup(func() { h.Sup.Stop("xray"); h.Sup.Stop("sing-box") })
	oldPort, newPort, missingPort := freePort(t), freePort(t), freePort(t)
	config := func(port int) []byte {
		return []byte(fmt.Sprintf(`{"log":{"loglevel":"warning"},"inbounds":[{"listen":"127.0.0.1","port":%d,"protocol":"socks","settings":{"auth":"noauth"}}],"outbounds":[{"protocol":"freedom"}]}`, port))
	}
	old := config(oldPort)
	req := gate.ApplyRequest{Kernels: map[string]string{"xray": "fixture"}, Configs: map[string][]byte{"xray": old}, StatsPorts: map[string]int{"xray": oldPort}, Ports: []int{oldPort}}
	if res := h.Apply(t.Context(), req); !res.OK {
		t.Fatal(res.Error)
	}
	req.Configs["xray"] = config(newPort)
	req.Kernels["sing-box"] = "fixture"
	req.Configs["sing-box"] = []byte(`{"log":{"level":"warn"},"outbounds":[{"type":"direct","tag":"direct"}]}`)
	req.StatsPorts = map[string]int{"xray": newPort, "sing-box": missingPort}
	req.Ports = []int{newPort}
	res := h.Apply(t.Context(), req)
	if res.OK || !strings.Contains(res.Error, "已恢复本批") {
		t.Fatalf("missing batch rollback: %+v", res)
	}
	got, err := os.ReadFile(filepath.Join(p.CoresDir, "xray.json"))
	if err != nil || !bytes.Equal(got, old) {
		t.Fatalf("earlier kernel changed: %s %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(p.CoresDir, "sing-box.json")); !os.IsNotExist(err) {
		t.Fatalf("failed kernel left config: %v", err)
	}
	eventually(t, 10*time.Second, "restored listener", func() bool {
		c, e := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", oldPort), time.Second)
		if e != nil {
			return false
		}
		c.Close()
		return true
	})
	if c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", newPort), time.Second); err == nil {
		c.Close()
		t.Fatal("new config still serving")
	}
	if _, running := h.Sup.Running()["sing-box"]; running {
		t.Fatal("failed kernel kept running")
	}
	encoded, _ := json.Marshal(res)
	t.Logf("whole batch restored: %s", encoded)
}
