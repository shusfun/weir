//go:build e2e

package e2e

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/gate"
	"github.com/shusfun/weir/internal/store/ent/audit"
	entgate "github.com/shusfun/weir/internal/store/ent/gate"
	"github.com/shusfun/weir/packages/contracts"
)

func TestStabilityRealKernels(t *testing.T) {
	e := setup(t)
	sb := e.seedSingbox()
	xv, _ := exec.Command(e.xray, "version").CombinedOutput()
	sv, _ := exec.Command(sb, "version").CombinedOutput()
	note(t, "versions", map[string]any{"xray": strings.TrimSpace(string(xv)), "singbox": strings.TrimSpace(string(sv)), "xray_registered": e.xrayVersion, "singbox_registered": e.singboxVersion, "xray_digest": e.xrayDigest, "singbox_digest": e.singboxDigest})
	caFile, centerPort, _ := e.httpsCenter("probe.e2e.test")
	origin := fmt.Sprintf("https://probe.e2e.test:%d", centerPort)
	_, stop := e.pairHTTPS("stability", origin, caFile)
	t.Cleanup(stop)
	e.paths = gate.Paths{Socket: filepath.Join("/tmp", "weir-stability.sock"), CoresDir: filepath.Join(e.dir, "stability", "cores"), CertsDir: filepath.Join(e.dir, "stability", "certs")}
	eventually(t, 15*time.Second, "paired gate", func() bool {
		g, err := e.s.Store.Ent.Gate.Query().Where(entgate.NameEQ("stability")).Only(e.ctx)
		if err != nil || g.LastSeenAt == nil {
			return false
		}
		e.gateID = g.ID
		return g.CenterURL != "" && !strings.Contains(g.CenterURL, "127.0.0.1") && !strings.Contains(g.CenterURL, "localhost")
	})
	e.s.Store.Ent.Gate.UpdateOneID(e.gateID).SetPublicHost("127.0.0.1").ExecX(e.ctx)
	_, stopX := e.pairHTTPS("xray-proof", origin, caFile)
	t.Cleanup(stopX)
	var xGateID int
	eventually(t, 15*time.Second, "paired xray gate", func() bool {
		g, err := e.s.Store.Ent.Gate.Query().Where(entgate.NameEQ("xray-proof")).Only(e.ctx)
		if err != nil || g.LastSeenAt == nil {
			return false
		}
		xGateID = g.ID
		return true
	})
	e.s.Store.Ent.Gate.UpdateOneID(xGateID).SetPublicHost("127.0.0.1").ExecX(e.ctx)
	probe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	t.Cleanup(probe.Close)
	oldURL, oldEvery := contracts.GroupProbeURL, contracts.GroupProbeEvery
	contracts.GroupProbeURL, contracts.GroupProbeEvery = probe.URL, "2s"
	t.Cleanup(func() { contracts.GroupProbeURL, contracts.GroupProbeEvery = oldURL, oldEvery })

	dest := localTLS(t)
	xnode := e.post("/api/v1/admin/nodes", map[string]any{"gate_id": xGateID, "preset": "vless-reality", "name": "stable-x", "skip_probe": true, "settings": map[string]any{"reality_dest": dest, "reality_server_names": "e2e.test"}}, nil)
	node := e.post("/api/v1/admin/nodes", map[string]any{"gate_id": e.gateID, "preset": "ss2022", "name": "stable", "skip_probe": true}, nil)
	user := e.post("/api/v1/admin/users", map[string]any{"email": "keep@e2e.test", "password": "E2eUser1", "unlimited": true}, nil)
	eventually(t, 90*time.Second, "initial node", func() bool {
		ok := true
		for _, id := range []int{e.gateID, xGateID} {
			g, err := e.s.Store.Ent.Gate.Get(e.ctx, id)
			if err == nil && g.LastError != "" {
				t.Logf("gate %d error: %s", id, g.LastError)
			}
			ok = ok && err == nil && g.AppliedRevision == g.ConfigRevision && g.AppliedRevision > 0
		}
		return ok
	})
	uid, nid := int(user["id"].(float64)), int(node["id"].(float64))
	xid := int(xnode["id"].(float64))
	known := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("weir-stable-" + r.URL.Path))
	}))
	t.Cleanup(known.Close)
	prove := func(label string, nodeID int, marker string) int {
		t.Helper()
		ob := e.subOutbound(uid, nodeID)
		t.Logf("%s outbound server=%v port=%v type=%v", label, ob["server"], ob["server_port"], ob["type"])
		port := e.runSingboxClient(sb, ob)
		var last error
		eventually(t, 20*time.Second, label+" transfer", func() bool {
			last = fetchVia(port, known.URL+"/"+marker, len("weir-stable-/"+marker))
			if last != nil {
				t.Logf("%s: %v", label, last)
			}
			return last == nil
		})
		note(t, label+"-transfer", map[string]any{"result": "passed", "server": ob["server"], "port": ob["server_port"], "type": ob["type"], "bytes": len("weir-stable-/" + marker)})
		return port
	}
	_ = prove("xray", xid, "xray")
	port := prove("singbox", nid, "singbox")
	drip := dripServer(t)
	live := hold(t, port, drip.URL)
	eventually(t, 10*time.Second, "long connection", func() bool { return live.got.Load() > 0 })

	t.Run("external-probe", func(t *testing.T) {
		rev, pid := e.revision(), kernelPID(t, e.paths.Socket)
		before := live.got.Load()
		for i := 1; i <= 2; i++ {
			minute := (i - 1) * 10
			eventually(t, 60*time.Second, "reach cycle", func() bool {
				e.s.CheckReachabilityAll(e.ctx, time.Date(2026, 1, 1, 12, minute, 0, 0, time.Local))
				n, _ := e.s.Store.Ent.Audit.Query().Where(audit.ActionEQ("node.reach"), audit.TargetEQ("node:"+strconv.Itoa(nid))).Count(e.ctx)
				return n >= i
			})
		}
		time.Sleep(1200 * time.Millisecond)
		reach := nodeReach(t, e, nid)
		if reach["status"] != "ok" {
			t.Fatalf("external probe did not pass: %v", reach)
		}
		if e.revision() != rev || kernelPID(t, e.paths.Socket) != pid || live.errString() != "" || live.got.Load() <= before {
			t.Fatalf("probe changed revision %d→%d pid %d→%d bytes %d→%d err %s", rev, e.revision(), pid, kernelPID(t, e.paths.Socket), before, live.got.Load(), live.errString())
		}
		note(t, "external-probe", map[string]any{"result": "passed", "revision": rev, "applied": e.appliedRevision(), "pid": pid, "bytes": live.got.Load(), "reach": reach})
	})

	t.Run("accounting", func(t *testing.T) {
		rev, hash, pid := e.revision(), kernelHash(t, e.paths.CoresDir), kernelPID(t, e.paths.Socket)
		if err := e.s.Rebalance(e.ctx, uid); err != nil {
			t.Fatal(err)
		}
		e.post(fmt.Sprintf("/api/v1/admin/users/%d/reset-usage", uid), map[string]any{}, nil)
		if err := e.s.SyncUser(e.ctx, uid); err != nil {
			t.Fatal(err)
		}
		if err := e.s.ReclaimStaleBudgets(e.ctx, time.Now()); err != nil {
			t.Fatal(err)
		}
		time.Sleep(2 * time.Second)
		if e.revision() != rev || kernelHash(t, e.paths.CoresDir) != hash || kernelPID(t, e.paths.Socket) != pid {
			t.Fatalf("accounting changed rev %d→%d hash %s→%s pid %v→%v", rev, e.revision(), hash, kernelHash(t, e.paths.CoresDir), pid, helperRunning(t, e.paths.Socket))
		}
		note(t, "accounting", map[string]any{"result": "passed", "revision": rev, "applied": e.appliedRevision(), "config_hash": hash, "pid": pid})
	})

	t.Run("xray-hot-users", func(t *testing.T) {
		pid := kernelPID(t, e.paths.Socket)
		other := e.post("/api/v1/admin/users", map[string]any{"email": "other@e2e.test", "password": "E2eUser1", "unlimited": true}, nil)
		e.applied("hot add")
		if kernelPID(t, e.paths.Socket) == pid || live.errString() == "" {
			t.Fatalf("sing-box user add should restart and drop the old connection pid %d→%d err %s", pid, kernelPID(t, e.paths.Socket), live.errString())
		}
		if waitEvent(t, e, "hot_users") {
			t.Fatal("sing-box user change was recorded as hot_users")
		}
		oid := int(other["id"].(float64))
		oldPort := e.runSingboxClient(sb, e.subOutbound(oid, nid))
		userURL, userLen := known.URL+"/user", len("weir-stable-/user")
		eventually(t, 20*time.Second, "added user", func() bool {
			err := fetchVia(oldPort, userURL, userLen)
			if err != nil {
				t.Logf("added user: %v", err)
			}
			return err == nil
		})
		e.post(fmt.Sprintf("/api/v1/admin/users/%d/links/%s/rotate", oid, linkID(t, e, oid)), map[string]any{"credentials": true}, nil)
		e.applied("same-email credential change")
		if kernelPID(t, e.paths.Socket) == pid {
			t.Fatal("credential change restarted xray")
		}
		eventually(t, 15*time.Second, "old credential rejected", func() bool { return fetchVia(oldPort, userURL, userLen) != nil })
		fresh := e.runSingboxClient(sb, e.subOutbound(oid, nid))
		eventually(t, 20*time.Second, "new credential", func() bool {
			err := fetchVia(fresh, userURL, userLen)
			if err != nil {
				t.Logf("new credential: %v", err)
			}
			return err == nil
		})
		req, _ := http.NewRequest("DELETE", e.url+"/api/v1/admin/users/"+strconv.Itoa(oid), nil)
		resp, err := e.admin.Do(req)
		if err != nil || resp.StatusCode >= 300 {
			t.Fatalf("delete user: %v %v", err, resp)
		}
		resp.Body.Close()
		e.applied("hot delete")
		if kernelPID(t, e.paths.Socket) == 0 {
			t.Fatal("kernel did not come back after user delete")
		}
		if fetchVia(fresh, userURL, userLen) == nil {
			t.Fatal("deleted user can still connect")
		}
		note(t, "singbox-user-change", map[string]any{"result": "passed", "old_pid": pid, "new_pid": kernelPID(t, e.paths.Socket), "mode": lastApplyMode(t, e)})
	})

	t.Run("cpu-cgroup", func(t *testing.T) {
		_, sysErr := exec.LookPath("systemctl")
		raw, _ := os.ReadFile("/proc/self/cgroup")
		note(t, "cpu-cgroup", map[string]any{"result": "uncovered", "systemd": sysErr == nil, "cgroup": strings.TrimSpace(string(raw)), "why": "容器没有正式 weir-gate.service 与 systemd Delegate，不能把 root 容器权限写成限额通过"})
	})
}

func runXrayReality(t *testing.T, bin, dir string, ob map[string]any) int {
	t.Helper()
	tlsMap, _ := ob["tls"].(map[string]any)
	reality, _ := tlsMap["reality"].(map[string]any)
	utls, _ := tlsMap["utls"].(map[string]any)
	if reality["public_key"] == nil || ob["uuid"] == nil {
		t.Fatalf("subscription outbound missing reality material: %#v", ob)
	}
	port := freePort(t)
	client := map[string]any{
		"log":      map[string]any{"loglevel": "warning"},
		"inbounds": []any{map[string]any{"listen": "127.0.0.1", "port": port, "protocol": "socks", "settings": map[string]any{"udp": false}}},
		"outbounds": []any{map[string]any{"protocol": "vless", "settings": map[string]any{"vnext": []any{map[string]any{"address": ob["server"], "port": int(ob["server_port"].(float64)), "users": []any{map[string]any{"id": ob["uuid"], "encryption": "none", "flow": ob["flow"]}}}}},
			"streamSettings": map[string]any{"network": "tcp", "security": "reality", "realitySettings": map[string]any{"serverName": tlsMap["server_name"], "publicKey": reality["public_key"], "shortId": reality["short_id"], "fingerprint": utls["fingerprint"]}}}},
	}
	b, _ := json.Marshal(client)
	path := filepath.Join(dir, fmt.Sprintf("xray-client-%d.json", port))
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "run", "-config", path)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	return port
}

func kernelPID(t *testing.T, socket string) int {
	t.Helper()
	running := helperRunning(t, socket)
	if running["sing-box"] != 0 {
		return running["sing-box"]
	}
	return running["xray"]
}

func note(t *testing.T, scenario string, fields map[string]any) {
	t.Helper()
	fields["scenario"], fields["at"] = scenario, time.Now().UTC().Format(time.RFC3339Nano)
	b, _ := json.Marshal(fields)
	t.Log(string(b))
	f, err := os.OpenFile("/t/stability-evidence.jsonl", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err == nil {
		_, _ = f.Write(append(b, '\n'))
		_ = f.Close()
	}
}

func nonLoopbackIP(t *testing.T) string {
	t.Helper()
	ifaces, err := net.Interfaces()
	if err != nil {
		t.Fatal(err)
	}
	for _, iface := range ifaces {
		addrs, _ := iface.Addrs()
		for _, a := range addrs {
			ip, _, _ := net.ParseCIDR(a.String())
			if ip != nil && ip.To4() != nil && !ip.IsLoopback() {
				return ip.String()
			}
		}
	}
	t.Fatal("no non-loopback address")
	return ""
}

func (e *env) revision() int64 {
	g, err := e.s.Store.Ent.Gate.Get(e.ctx, e.gateID)
	if err != nil {
		e.t.Fatal(err)
	}
	return g.ConfigRevision
}

func (e *env) appliedRevision() int64 {
	return e.s.Store.Ent.Gate.GetX(e.ctx, e.gateID).AppliedRevision
}

func kernelHash(t *testing.T, dir string) string {
	t.Helper()
	h := sha256.New()
	for _, name := range []string{"xray.json", "sing-box.json"} {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		_, _ = h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func localTLS(t *testing.T) string {
	t.Helper()
	s := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("dest")) }))
	s.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	s.StartTLS()
	t.Cleanup(s.Close)
	return s.Listener.Addr().String()
}

func dripServer(t *testing.T) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f, _ := w.(http.Flusher)
		tick := time.NewTicker(100 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-tick.C:
				_, _ = w.Write([]byte("x"))
				if f != nil {
					f.Flush()
				}
			}
		}
	}))
	t.Cleanup(s.Close)
	return s
}

type heldConn struct {
	got    atomic.Int64
	failed atomic.Value
}

func (h *heldConn) fail(err error) {
	if err != nil {
		h.failed.Store(err.Error())
	}
}

func (h *heldConn) errString() string {
	v := h.failed.Load()
	s, _ := v.(string)
	return s
}

func hold(t *testing.T, port int, target string) *heldConn {
	t.Helper()
	h := &heldConn{}
	proxy, _ := url.Parse(fmt.Sprintf("socks5://127.0.0.1:%d", port))
	hc := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxy)}}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	req, _ := http.NewRequestWithContext(ctx, "GET", target, nil)
	go func() {
		var resp *http.Response
		var err error
		for ctx.Err() == nil {
			resp, err = hc.Do(req.Clone(ctx))
			if err == nil {
				break
			}
			h.fail(err)
			time.Sleep(300 * time.Millisecond)
		}
		if err != nil || resp == nil {
			return
		}
		defer resp.Body.Close()
		buf := make([]byte, 32)
		for {
			n, err := resp.Body.Read(buf)
			if n > 0 {
				h.got.Add(int64(n))
				h.failed.Store("")
			}
			if err != nil {
				if ctx.Err() == nil {
					h.fail(err)
				}
				return
			}
		}
	}()
	return h
}

func nodeReach(t *testing.T, e *env, id int) map[string]any {
	t.Helper()
	b, _ := e.get("/api/v1/admin/nodes/"+strconv.Itoa(id), nil)
	var view map[string]any
	_ = json.Unmarshal(b, &view)
	reach, _ := view["reach"].(map[string]any)
	return reach
}

func waitEvent(t *testing.T, e *env, kind string) bool {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := e.admin.Get(e.url + "/api/v1/admin/gates/" + strconv.Itoa(e.gateID))
		if err == nil {
			var view struct {
				Events []map[string]any `json:"kernel_events"`
			}
			_ = json.NewDecoder(resp.Body).Decode(&view)
			resp.Body.Close()
			for _, ev := range view.Events {
				if ev["kind"] == kind {
					if kind == "hot_users" && ev["disconnects"] != nil {
						t.Fatalf("hot_users stored a disconnect count: %v", ev)
					}
					return true
				}
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	return false
}

func linkID(t *testing.T, e *env, uid int) string {
	t.Helper()
	b, _ := e.get("/api/v1/admin/users/"+strconv.Itoa(uid), nil)
	var detail map[string]any
	_ = json.Unmarshal(b, &detail)
	return strconv.Itoa(int(detail["link_items"].([]any)[0].(map[string]any)["id"].(float64)))
}

func lastApplyMode(t *testing.T, e *env) string {
	t.Helper()
	rows, err := e.s.Store.Ent.Audit.Query().Where(audit.ActionEQ("gate.config.apply"), audit.TargetEQ(strconv.Itoa(e.gateID))).All(e.ctx)
	if err != nil || len(rows) == 0 {
		t.Fatal(err)
	}
	mode, _ := rows[len(rows)-1].Detail["mode"].(string)
	return mode
}

func addLoopIP(ip string) error {
	parsed := net.ParseIP(ip).To4()
	if parsed == nil {
		return fmt.Errorf("bad ip")
	}
	iface, err := net.InterfaceByName("lo")
	if err != nil {
		return err
	}
	fd, err := syscall.Socket(syscall.AF_NETLINK, syscall.SOCK_RAW, syscall.NETLINK_ROUTE)
	if err != nil {
		return err
	}
	defer syscall.Close(fd)
	ifa := make([]byte, 8)
	ifa[0], ifa[1], ifa[3] = syscall.AF_INET, 32, 254
	binary.LittleEndian.PutUint32(ifa[4:], uint32(iface.Index))
	attr := func(typ uint16, data []byte) []byte {
		n := (4 + len(data) + 3) &^ 3
		b := make([]byte, n)
		binary.LittleEndian.PutUint16(b[0:], uint16(len(b)))
		binary.LittleEndian.PutUint16(b[2:], typ)
		copy(b[4:], data)
		return b
	}
	body := append(append(ifa, attr(1, parsed)...), attr(2, parsed)...)
	hdr := make([]byte, 16)
	binary.LittleEndian.PutUint32(hdr[0:], uint32(16+len(body)))
	binary.LittleEndian.PutUint16(hdr[4:], 20)
	binary.LittleEndian.PutUint16(hdr[6:], 1|4|0x400)
	binary.LittleEndian.PutUint32(hdr[8:], 1)
	if err = syscall.Sendto(fd, append(hdr, body...), 0, &syscall.SockaddrNetlink{Family: syscall.AF_NETLINK}); err != nil {
		return err
	}
	buf := make([]byte, 256)
	n, _, err := syscall.Recvfrom(fd, buf, 0)
	if err != nil || n < 20 {
		return err
	}
	if binary.LittleEndian.Uint16(buf[4:6]) == 2 {
		errno := int32(binary.LittleEndian.Uint32(buf[16:20]))
		if errno != 0 {
			return syscall.Errno(-errno)
		}
	}
	return nil
}
