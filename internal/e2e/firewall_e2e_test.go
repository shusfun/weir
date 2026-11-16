//go:build e2e

package e2e

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/gate"
	"github.com/shusfun/weir/internal/releasefiles"
	"github.com/shusfun/weir/packages/contracts"
)

// The runner sets up a disposable network namespace with NET_ADMIN. These
// commands must never run as part of host-side Go tests.
func TestManagedFirewallNative(t *testing.T) {
	tool := os.Getenv("WEIR_E2E_FIREWALL")
	if tool == "" {
		t.Skip("requires dedicated firewall container")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	command := func(name string, args ...string) string {
		t.Helper()
		out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
		if err != nil {
			t.Fatalf("%s %v: %v: %s", name, args, err, out)
		}
		return string(out)
	}
	if tool == "firewalld" {
		command("dbus-daemon", "--system", "--fork")
		log, err := os.CreateTemp(t.TempDir(), "firewalld-*.log")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_ = log.Close()
			if t.Failed() {
				body, _ := os.ReadFile(log.Name())
				if len(body) > 8192 {
					body = body[len(body)-8192:]
				}
				t.Logf("firewalld startup: %s", body)
			}
		})
		cmd := exec.CommandContext(ctx, "firewalld", "--nofork", "--nopid", "--log-target=console")
		cmd.Stdout, cmd.Stderr = log, log
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
		eventually(t, 45*time.Second, "firewalld start", func() bool {
			return exec.CommandContext(ctx, "firewall-cmd", "--state").Run() == nil
		})
		command("firewall-cmd", "--add-port=22/tcp")
	} else if tool == "ufw" {
		command("ufw", "allow", "22/tcp", "comment", "fixture-external")
	} else {
		command("iptables", "-A", "INPUT", "-p", "tcp", "--dport", "22", "-j", "ACCEPT")
	}
	rules := func() string {
		switch tool {
		case "ufw":
			return command("ufw", "show", "added")
		case "firewalld":
			// Rich rules are an unordered set; re-insertion during rollback can
			// change their presentation order without changing either store.
			normalize := func(value string) string {
				lines := strings.Split(strings.TrimSpace(value), "\n")
				slices.Sort(lines)
				return strings.Join(lines, "\n") + "\n"
			}
			return normalize(command("firewall-cmd", "--list-rich-rules")) + normalize(command("firewall-cmd", "--permanent", "--list-rich-rules"))
		default:
			return command("iptables", "-S", "INPUT") + command("ip6tables", "-S", "INPUT")
		}
	}
	dir := t.TempDir()
	p := gate.Paths{CoresDir: filepath.Join(dir, "cores"), KernelsDir: filepath.Join(dir, "kernels")}
	if err := os.MkdirAll(p.KernelsDir, 0755); err != nil {
		t.Fatal(err)
	}
	h := gate.NewHelper(p)
	ctx = releasefiles.WithTrace(ctx, "00-1234567890abcdef1234567890abcdef-1234567890abcdef-01")
	apply := func(ports ...contracts.FirewallPort) {
		t.Helper()
		res := h.Apply(ctx, gate.ApplyRequest{Firewall: &contracts.FirewallPlan{Version: 1, Ports: ports}})
		if !res.OK {
			t.Fatal(res.Error)
		}
	}
	port := contracts.FirewallPort{Port: 443, Protocol: "tcp"}
	apply(port, port)
	before := rules()
	if !strings.Contains(before, "weir:tcp:443") {
		t.Fatalf("missing owned rule: %s", before)
	}
	if tool == "ufw" {
		for _, file := range []string{"/etc/ufw/user.rules", "/etc/ufw/user6.rules"} {
			body, err := os.ReadFile(file)
			if err != nil || !strings.Contains(string(body), "comment="+hex.EncodeToString([]byte("weir:tcp:443"))) || !strings.Contains(string(body), "--dport 443 -j ACCEPT") {
				t.Fatalf("missing persistent address-family rule %s: %v", file, err)
			}
		}
	} else if tool == "firewalld" {
		for _, family := range []string{"ipv4", "ipv6"} {
			for _, args := range [][]string{{"--list-rich-rules"}, {"--permanent", "--list-rich-rules"}} {
				if out := command("firewall-cmd", args...); !strings.Contains(out, `family="`+family+`"`) {
					t.Fatalf("missing %s rule: %s", family, out)
				}
			}
		}
		for _, name := range []string{"iptables", "ip6tables"} {
			if out := command(name, "-S"); !strings.Contains(out, "--dport 443") || !strings.Contains(out, "weir:tcp:443") {
				t.Fatalf("missing effective %s rule: %s", name, out)
			}
		}
	} else if out := command("ip6tables", "-S", "INPUT"); !strings.Contains(out, "weir:tcp:443") {
		t.Fatalf("missing IPv6 rule: %s", out)
	}
	apply(port)
	if after := rules(); after != before {
		t.Fatalf("shared port repeated: before=%s after=%s", before, after)
	}
	// A new helper must reconcile the saved plan without duplicating any rule.
	gate.NewHelper(p).Boot()
	if after := rules(); after != before {
		t.Fatalf("restart changed rules: %s", after)
	}
	// A service configuration failure after opening the new ports must restore
	// both rule families and leave the last committed desired plan untouched.
	planFile := filepath.Join(p.CoresDir, "firewall.json")
	committed, err := os.ReadFile(planFile)
	if err != nil {
		t.Fatal(err)
	}
	failed := h.Apply(ctx, gate.ApplyRequest{
		Firewall:    &contracts.FirewallPlan{Version: 1, Ports: []contracts.FirewallPort{{Port: 9443, Protocol: "tcp"}}},
		BrowserTest: &contracts.BrowserTestPlan{GateID: 1, Port: 9443, RTCPort: 9444, CertID: 1, Host: "127.0.0.1", Origin: "http://127.0.0.1:22342", PublicKey: base64.RawURLEncoding.EncodeToString(make([]byte, 32))},
	})
	if failed.OK || !strings.Contains(failed.Error, "测试证书不可读") || rules() != before {
		t.Fatalf("service failure did not roll back firewall: %+v rules=%s", failed, rules())
	}
	if body, err := os.ReadFile(planFile); err != nil || string(body) != string(committed) {
		t.Fatalf("failed transaction changed saved plan: %v", err)
	}
	apply(contracts.FirewallPort{Port: 8443, Protocol: "udp"})
	if out := rules(); strings.Contains(out, "weir:tcp:443") || !strings.Contains(out, "weir:udp:8443") {
		t.Fatalf("port change did not clean old rule: %s", out)
	}
	apply()
	if out := rules(); strings.Contains(out, "weir:") {
		t.Fatalf("delete left owned rules: %s", out)
	}
	if tool == "ufw" {
		if !strings.Contains(rules(), "fixture-external") {
			t.Fatal("removed external rule")
		}
		command("ufw", "allow", "from", "192.0.2.1", "to", "any", "port", "443", "proto", "tcp", "comment", "weir:tcp:443")
	} else if tool == "firewalld" {
		if !strings.Contains(command("firewall-cmd", "--list-ports"), "22/tcp") {
			t.Fatal("removed external rule")
		}
		command("firewall-cmd", `--add-rich-rule=rule family="ipv4" source address="192.0.2.1" port port="443" protocol="tcp" log prefix="weir:tcp:443" accept`)
	} else {
		if !strings.Contains(rules(), "--dport 22") {
			t.Fatal("removed external rule")
		}
		command("iptables", "-N", "fixture-foreign")
		command("iptables", "-A", "INPUT", "-j", "fixture-foreign")
	}
	unknown := rules()
	res := h.Apply(ctx, gate.ApplyRequest{Firewall: &contracts.FirewallPlan{Version: 1, Ports: []contracts.FirewallPort{port}}})
	if res.OK || rules() != unknown {
		t.Fatalf("unknown layout accepted or modified: %+v", res)
	}
	t.Logf("firewall native passed tool=%s trace=%s", tool, releasefiles.Trace(ctx))
}
