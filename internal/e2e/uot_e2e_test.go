//go:build e2e

package e2e

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/gate"
	"github.com/shusfun/weir/internal/kernelconf"
	"github.com/shusfun/weir/internal/kernels"
	"github.com/shusfun/weir/packages/contracts"
)

func TestNativeUoTEncryptedTCPAndFailClosed(t *testing.T) {
	bin := os.Getenv("WEIR_E2E_SINGBOX")
	if bin == "" {
		t.Fatal("sing-box fixture required")
	}
	dir := t.TempDir()
	p := gate.Paths{CoresDir: filepath.Join(dir, "cores"), KernelsDir: filepath.Join(dir, "kernels")}
	if err := os.MkdirAll(p.KernelsDir, 0755); err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(p.KernelsDir, "sing-box-fixture"), binary, 0755); err != nil {
		t.Fatal(err)
	}
	builtins := kernels.Builtin()
	_, driver, err := kernelconf.Resolve(builtins["sing-box-1.12"], func(key string) (string, bool) { v, ok := builtins[key]; return v, ok })
	if err != nil {
		t.Fatal(err)
	}
	port := freePort(t)
	// Occupy and discard outer UDP. A native UDP Shadowsocks path cannot pass.
	drop, err := net.ListenPacket("udp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		t.Fatal(err)
	}
	defer drop.Close()
	var datagrams atomic.Int64
	go func() {
		buf := make([]byte, 65536)
		for {
			if _, _, err := drop.ReadFrom(buf); err != nil {
				return
			}
			datagrams.Add(1)
		}
	}()
	cfg := contracts.GateConfig{
		Drivers: map[string]string{"sing-box": driver}, Ports: contracts.PortPlan{SingboxStats: freePort(t)},
		Inbounds:    []contracts.Inbound{{ID: 1, Tag: "receiver", Kernel: "sing-box", Protocol: "shadowsocks", Network: "tcp", Port: port, Settings: map[string]any{"ss_method": "2022-blake3-aes-128-gcm", "ss_password": "AAAAAAAAAAAAAAAAAAAAAA==", "tcp_only": true}}},
		Credentials: []contracts.Credential{{ID: 1, InboundID: 1, StatsKey: "internal", Internal: true, UserKey: "internal", Password: "BBBBBBBBBBBBBBBBBBBBBA=="}},
	}
	out, err := kernelconf.Build(cfg, kernelconf.Options{})
	if err != nil {
		t.Fatal(err)
	}
	helper := gate.NewHelper(p)
	defer helper.Sup.Stop("sing-box")
	if result := helper.Apply(t.Context(), gate.ApplyRequest{Kernels: map[string]string{"sing-box": "fixture"}, Configs: map[string][]byte{"sing-box": out.Singbox}, Args: out.Args, StatsPorts: map[string]int{"sing-box": cfg.Ports.SingboxStats}, Ports: []int{cfg.Ports.SingboxStats}}); !result.OK {
		t.Fatal(result.Error)
	}
	target := contracts.Outbound{ID: 2, Tag: "uot", Protocol: "shadowsocks", Address: "127.0.0.1", Port: port, Username: "2022-blake3-aes-128-gcm", Password: "AAAAAAAAAAAAAAAAAAAAAA==:BBBBBBBBBBBBBBBBBBBBBA==", Settings: map[string]any{"udp_over_tcp": true}}
	cfg.Inbounds = nil
	cfg.Credentials = nil
	cfg.Outbounds = []contracts.Outbound{target}
	cfg.Bridges = []contracts.OutboundBridge{{FromKernel: "xray", Kernel: "sing-box", OutboundTag: target.Tag, Port: freePort(t), Username: "probe-user", Password: "probe-password-at-least-24"}}
	generated, err := kernelconf.Build(cfg, kernelconf.Options{})
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Outbounds []map[string]any `json:"outbounds"`
	}
	if err = json.Unmarshal(generated.Singbox, &doc); err != nil {
		t.Fatal(err)
	}
	var outbound map[string]any
	for _, o := range doc.Outbounds {
		if o["type"] == "shadowsocks" {
			outbound = o
		}
	}
	if outbound == nil {
		t.Fatal("native outbound missing")
	}
	e := &env{t: t, dir: dir}
	client := e.runSingboxClient(bin, outbound)
	echo := udpEcho(t)
	payload := []byte("native-uot-through-encrypted-tcp")
	data, _, err := socks5UDP(client, echo.addr, payload, 3*time.Second)
	if err != nil || string(data) != string(payload) {
		t.Fatal("UDP over TCP failed", err)
	}
	if datagrams.Load() != 0 {
		t.Fatal("used outer UDP")
	}
	helper.Sup.Stop("sing-box")
	before := echo.count.Load()
	if _, _, err = socks5UDP(client, echo.addr, payload, time.Second); err == nil {
		t.Fatal("receiver stopped but UDP passed")
	}
	if echo.count.Load() != before {
		t.Fatal("failed transport leaked to direct")
	}
}

func TestNativeUoTUpdateRestartDeleteAndPortConflict(t *testing.T) {
	bin := os.Getenv("WEIR_E2E_SINGBOX")
	if bin == "" {
		t.Fatal("sing-box fixture required")
	}
	dir := t.TempDir()
	p := gate.Paths{CoresDir: filepath.Join(dir, "cores"), KernelsDir: filepath.Join(dir, "kernels")}
	if err := os.MkdirAll(p.KernelsDir, 0755); err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(p.KernelsDir, "sing-box-fixture"), binary, 0755); err != nil {
		t.Fatal(err)
	}
	builtins := kernels.Builtin()
	_, driver, err := kernelconf.Resolve(builtins["sing-box-1.12"], func(key string) (string, bool) { v, ok := builtins[key]; return v, ok })
	if err != nil {
		t.Fatal(err)
	}
	port := freePort(t)
	drop, err := net.ListenPacket("udp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		t.Fatal(err)
	}
	defer drop.Close()
	var datagrams atomic.Int64
	go func() {
		buf := make([]byte, 65536)
		for {
			if _, _, err := drop.ReadFrom(buf); err != nil {
				return
			}
			datagrams.Add(1)
		}
	}()
	occupied := freePort(t)
	held, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(occupied)))
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	conflict := contracts.GateConfig{
		Drivers: map[string]string{"sing-box": driver}, Ports: contracts.PortPlan{SingboxStats: freePort(t)},
		Inbounds:    []contracts.Inbound{{ID: 1, Tag: "receiver", Kernel: "sing-box", Protocol: "shadowsocks", Network: "tcp", Port: occupied, Settings: map[string]any{"ss_method": "2022-blake3-aes-128-gcm", "ss_password": "AAAAAAAAAAAAAAAAAAAAAA==", "tcp_only": true}}},
		Credentials: []contracts.Credential{{ID: 1, InboundID: 1, StatsKey: "internal", Internal: true, UserKey: "internal", Password: "BBBBBBBBBBBBBBBBBBBBBA=="}},
	}
	blocked, err := kernelconf.Build(conflict, kernelconf.Options{})
	if err != nil {
		t.Fatal(err)
	}
	helper := gate.NewHelper(p)
	defer helper.Sup.Stop("sing-box")
	if result := helper.Apply(t.Context(), gate.ApplyRequest{Kernels: map[string]string{"sing-box": "fixture"}, Configs: map[string][]byte{"sing-box": blocked.Singbox}, Args: blocked.Args, StatsPorts: map[string]int{"sing-box": conflict.Ports.SingboxStats}, Ports: []int{occupied, conflict.Ports.SingboxStats}}); result.OK {
		t.Fatal("occupied port was accepted")
	}
	cfg := contracts.GateConfig{
		Drivers: map[string]string{"sing-box": driver}, Ports: contracts.PortPlan{SingboxStats: freePort(t)},
		Inbounds:    []contracts.Inbound{{ID: 1, Tag: "receiver", Kernel: "sing-box", Protocol: "shadowsocks", Network: "tcp", Port: port, Settings: map[string]any{"ss_method": "2022-blake3-aes-128-gcm", "ss_password": "AAAAAAAAAAAAAAAAAAAAAA==", "tcp_only": true}}},
		Credentials: []contracts.Credential{{ID: 1, InboundID: 1, StatsKey: "internal", Internal: true, UserKey: "internal", Password: "BBBBBBBBBBBBBBBBBBBBBA=="}},
	}
	apply := func(password string) {
		t.Helper()
		cfg.Inbounds[0].Settings["ss_password"] = password
		out, err := kernelconf.Build(cfg, kernelconf.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if result := helper.Apply(t.Context(), gate.ApplyRequest{Kernels: map[string]string{"sing-box": "fixture"}, Configs: map[string][]byte{"sing-box": out.Singbox}, Args: out.Args, StatsPorts: map[string]int{"sing-box": cfg.Ports.SingboxStats}, Ports: []int{cfg.Ports.SingboxStats}}); !result.OK {
			t.Fatal(result.Error)
		}
	}
	clientFor := func(password string) int {
		t.Helper()
		target := contracts.Outbound{ID: 2, Tag: "uot", Protocol: "shadowsocks", Address: "127.0.0.1", Port: port, Username: "2022-blake3-aes-128-gcm", Password: password, Settings: map[string]any{"udp_over_tcp": true}}
		view := cfg
		view.Inbounds, view.Credentials = nil, nil
		view.Outbounds = []contracts.Outbound{target}
		view.Bridges = []contracts.OutboundBridge{{FromKernel: "xray", Kernel: "sing-box", OutboundTag: target.Tag, Port: freePort(t), Username: "probe-user", Password: "probe-password-at-least-24"}}
		generated, err := kernelconf.Build(view, kernelconf.Options{})
		if err != nil {
			t.Fatal(err)
		}
		var doc struct {
			Outbounds []map[string]any `json:"outbounds"`
		}
		if err = json.Unmarshal(generated.Singbox, &doc); err != nil {
			t.Fatal(err)
		}
		for _, o := range doc.Outbounds {
			if o["type"] == "shadowsocks" {
				return (&env{t: t, dir: dir}).runSingboxClient(bin, o)
			}
		}
		t.Fatal("native outbound missing")
		return 0
	}
	apply("AAAAAAAAAAAAAAAAAAAAAA==")
	echo := udpEcho(t)
	payload := []byte("native-uot-update")
	first := clientFor("AAAAAAAAAAAAAAAAAAAAAA==:BBBBBBBBBBBBBBBBBBBBBA==")
	if data, _, err := socks5UDP(first, echo.addr, payload, 3*time.Second); err != nil || string(data) != string(payload) || datagrams.Load() != 0 {
		t.Fatal("initial transport failed", err)
	}
	apply("CCCCCCCCCCCCCCCCCCCCCC==")
	before := echo.count.Load()
	if _, _, err = socks5UDP(first, echo.addr, payload, time.Second); err == nil || echo.count.Load() != before || datagrams.Load() != 0 {
		t.Fatal("old password remained open", err)
	}
	updated := clientFor("CCCCCCCCCCCCCCCCCCCCCC==:BBBBBBBBBBBBBBBBBBBBBA==")
	if data, _, err := socks5UDP(updated, echo.addr, payload, 3*time.Second); err != nil || string(data) != string(payload) || datagrams.Load() != 0 {
		t.Fatal("updated transport failed", err)
	}
	helper.Sup.Stop("sing-box")
	apply("CCCCCCCCCCCCCCCCCCCCCC==")
	restarted := clientFor("CCCCCCCCCCCCCCCCCCCCCC==:BBBBBBBBBBBBBBBBBBBBBA==")
	if data, _, err := socks5UDP(restarted, echo.addr, payload, 3*time.Second); err != nil || string(data) != string(payload) || datagrams.Load() != 0 {
		t.Fatal("restarted transport failed", err)
	}
	helper.Sup.Stop("sing-box")
	before = echo.count.Load()
	if _, _, err = socks5UDP(restarted, echo.addr, payload, time.Second); err == nil || echo.count.Load() != before || datagrams.Load() != 0 {
		t.Fatal("deleted receiver leaked to direct", err)
	}
}
