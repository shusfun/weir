package gate

import (
	"context"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shusfun/weir/packages/contracts"
)

const procTCP = `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 00000000:4E21 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 1001 1 0000000000000000 100 0 0 10 0
   1: 0100007F:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 1002 1 0000000000000000 100 0 0 10 0
   2: 0100007F:D431 0100007F:1F90 01 00000000:00000000 00:00000000 00000000     0        0 1003 1 0000000000000000 100 0 0 10 0
`

const procUDP = `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode ref pointer drops
  10: 00000000:4E22 00000000:0000 07 00000000:00000000 00:00000000 00000000     0        0 2001 2 0000000000000000 0
`

func TestParseProcNet(t *testing.T) {
	tcp := parseProcNet(procTCP, "tcp")
	if len(tcp) != 2 || tcp[0].port != 20001 || tcp[1].port != 8080 || tcp[0].inode != "1001" {
		t.Fatalf("tcp = %+v (established socket must be skipped)", tcp)
	}
	udp := parseProcNet(procUDP, "udp")
	if len(udp) != 1 || udp[0].port != 20002 {
		t.Fatalf("udp = %+v", udp)
	}
	if got := conflicts([]int{8080, 20001}, map[int][]int{8080: {42}, 20001: {7}}, map[int]bool{7: true}); got != 8080 {
		t.Fatalf("conflict = %d, want 8080 (pid 42 is not ours)", got)
	}
	if got := conflicts([]int{20001}, map[int][]int{20001: {7}}, map[int]bool{7: true}); got != 0 {
		t.Fatalf("own kernel port reported as conflict: %d", got)
	}
}

func TestAbsorbHandlesKernelRestart(t *testing.T) {
	st := loadState(t.TempDir() + "/state.json")
	st.absorb(map[string]Counter{"a": {Up: 100, Down: 200}}, false)
	st.absorb(map[string]Counter{"a": {Up: 150, Down: 260}}, false)
	if got := st.Totals["a"]; got.Up != 150 || got.Down != 260 {
		t.Fatalf("totals = %+v", got)
	}
	// 内核重启：读数从零重新开始，新读数整份计入，累计不回退。
	st.absorb(map[string]Counter{"a": {Up: 10, Down: 5}}, false)
	if got := st.Totals["a"]; got.Up != 160 || got.Down != 265 {
		t.Fatalf("after counter reset = %+v", got)
	}
	st.absorb(map[string]Counter{"a": {Up: 20, Down: 5}}, true)
	if got := st.Totals["a"]; got.Up != 180 || got.Down != 270 {
		t.Fatalf("after pid change = %+v", got)
	}
}

func TestOfflineQuotaEnforcement(t *testing.T) {
	st := loadState(t.TempDir() + "/state.json")
	cfg := contracts.GateConfig{
		Credentials: []contracts.Credential{{StatsKey: "a1", UserKey: "u1"}, {StatsKey: "a2", UserKey: "u1"}, {StatsKey: "b1", UserKey: "u2"}, {StatsKey: "x", UserKey: "internal", Internal: true}},
		Budgets:     map[string]contracts.GateBudget{"u1": {Allocated: 1000, Used: 400}, "u2": {Unlimited: true}},
	}
	st.absorb(map[string]Counter{"a1": {Up: 100}}, false)
	st.resetBaseline(cfg)
	st.absorb(map[string]Counter{"a1": {Up: 400}, "a2": {Down: 250}}, false)
	if ex := st.exhausted(cfg); ex["u1"] {
		t.Fatalf("u1 used 400+550 < 1000 but marked exhausted")
	}
	before := signature(cfg, time.Now(), st.exhausted(cfg))
	st.absorb(map[string]Counter{"a1": {Up: 500}, "b1": {Up: 1 << 40}}, false)
	ex := st.exhausted(cfg)
	if !ex["u1"] || ex["u2"] {
		t.Fatalf("exhausted = %v", ex)
	}
	after := signature(cfg, time.Now(), ex)
	if before == after || strings.Contains(after, "a1") || !strings.Contains(after, "x") || !strings.Contains(after, "b1") {
		t.Fatalf("signature before=%q after=%q", before, after)
	}
	past := time.Now().Add(-time.Second)
	cfg.Credentials[2].ExpiresAt = &past
	if strings.Contains(signature(cfg, time.Now(), ex), "b1") {
		t.Fatal("expired credential still in signature")
	}
}

// fakeSocks 是只支持用户名密码认证与 CONNECT 的最小 SOCKS5 服务。
func fakeSocks(t *testing.T, user, pass string) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				head := make([]byte, 2)
				io.ReadFull(c, head)
				io.ReadFull(c, make([]byte, head[1]))
				c.Write([]byte{5, 2})
				ver := make([]byte, 2)
				io.ReadFull(c, ver)
				u := make([]byte, ver[1])
				io.ReadFull(c, u)
				pl := make([]byte, 1)
				io.ReadFull(c, pl)
				p := make([]byte, pl[0])
				io.ReadFull(c, p)
				if string(u) != user || string(p) != pass {
					c.Write([]byte{1, 1})
					return
				}
				c.Write([]byte{1, 0})
				req := make([]byte, 5)
				io.ReadFull(c, req)
				io.ReadFull(c, make([]byte, int(req[4])+2))
				c.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0})
			}(c)
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port
}

func TestChainProbeSocks5(t *testing.T) {
	port := fakeSocks(t, "home", "secret")
	ctx := context.Background()
	base := map[string]any{"kind": "socks5", "address": "127.0.0.1", "port": float64(port), "username": "home", "password": "secret", "target": "example.com:443"}
	if _, err := chainProbe(ctx, base); err != nil {
		t.Fatalf("admin probe: %v", err)
	}
	bad := map[string]any{}
	for k, v := range base {
		bad[k] = v
	}
	bad["password"] = "wrong"
	if _, err := chainProbe(ctx, bad); err == nil || !strings.Contains(err.Error(), "认证失败") {
		t.Fatalf("wrong password: %v", err)
	}
	// 用户自建链式代理：回环 / 内网地址被拒，不会去连接。
	base["public_only"] = true
	if _, err := chainProbe(ctx, base); err == nil || !strings.Contains(err.Error(), "公网") {
		t.Fatalf("public_only loopback: %v", err)
	}
	for _, ip := range []string{"10.0.0.1", "192.168.1.1", "169.254.169.254", "fd00::1", "100.100.100.200"} {
		if publicIP(net.ParseIP(ip)) {
			t.Fatalf("%s treated as public", ip)
		}
	}
	if !publicIP(net.ParseIP("203.0.113.9")) {
		t.Fatal("documentation address treated as private")
	}
	if _, err := chainProbe(ctx, map[string]any{"kind": "node", "address": "127.0.0.1", "port": float64(1)}); err == nil {
		t.Fatal("unreachable node exit accepted")
	}
	_ = strconv.Itoa
}

func TestRunActionRejectsUnknownAndExpired(t *testing.T) {
	a := &Agent{}
	if r := a.runAction(context.Background(), contracts.Action{Kind: "shell", ExpiresAt: time.Now().Add(time.Minute)}); r.OK || r.Error != errUnknownAction.Error() {
		t.Fatalf("unknown action = %+v", r)
	}
	if r := a.runAction(context.Background(), contracts.Action{Kind: contracts.ActionCoreRestart, ExpiresAt: time.Now().Add(-time.Second)}); r.OK {
		t.Fatal("expired action executed")
	}
}
