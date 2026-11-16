package gate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/shusfun/weir/packages/contracts"
)

func TestManagedFirewallDeltaRollbackAndPersistence(t *testing.T) {
	for _, tool := range []string{"ufw", "firewalld", "iptables"} {
		t.Run(tool, func(t *testing.T) {
			old := contracts.FirewallPort{Port: 8443, Protocol: "tcp"}
			oldRule := func(family string) string {
				switch tool {
				case "ufw":
					return "ufw allow 8443/tcp comment 'weir:tcp:8443'\nufw allow 22/tcp\n"
				case "firewalld":
					return strings.TrimPrefix(firewallCommands(tool, managedPort{family: family, port: old}, true)[0].args[0], "--add-rich-rule=") + "\n"
				default:
					return fmt.Sprintf("-P INPUT DROP\n-A INPUT -p tcp -m tcp --dport 8443 -m comment --comment weir:tcp:8443 -j ACCEPT\n-A INPUT -p tcp --dport 22 -j ACCEPT\n")
				}
			}
			var commands []string
			failAt := 0
			run := func(_ context.Context, name string, args ...string) (string, error) {
				joined := strings.Join(args, " ")
				if name == "ufw" {
					if tool != "ufw" {
						return "", errNotInstalled
					}
					if joined == "status" {
						return "Status: inactive\n", nil
					}
					if joined == "show added" {
						return oldRule("ipv4"), nil
					}
				}
				if name == "firewall-cmd" {
					if tool != "firewalld" {
						return "", errNotInstalled
					}
					switch joined {
					case "--state":
						return "running", nil
					case "--list-ports", "--list-services":
						return "", nil
					case "--get-default-zone":
						return "public", nil
					case "--get-active-zones":
						return "public\n  interfaces: eth0", nil
					case "--list-rich-rules", "--permanent --list-rich-rules":
						return oldRule("ipv4") + oldRule("ipv6"), nil
					}
				}
				if name == "iptables" || name == "ip6tables" {
					if tool != "iptables" {
						return "", errNotInstalled
					}
					if joined == "-S INPUT" {
						return oldRule(""), nil
					}
				}
				commands = append(commands, name+" "+joined)
				if failAt > 0 && len(commands) == failAt {
					return "", errors.New("injected write failure")
				}
				return "", nil
			}
			dir := t.TempDir()
			h := NewHelper(Paths{CoresDir: dir})
			h.firewallExec = run
			plan := &contracts.FirewallPlan{Version: 1, Ports: []contracts.FirewallPort{{Port: 9443, Protocol: "udp"}, {Port: 9443, Protocol: "udp"}}}
			undo, err := h.prepareFirewall(t.Context(), plan)
			if err != nil {
				t.Fatal(err)
			}
			count := len(commands)
			want := 2
			if tool == "iptables" {
				want = 4
			}
			if tool == "firewalld" {
				want = 8
			}
			if count != want {
				t.Fatalf("duplicate/shared rules: %v", commands)
			}
			if err := h.persistFirewall(plan); err != nil {
				t.Fatal(err)
			}
			if _, err := os.ReadFile(h.firewallPlanPath()); err != nil {
				t.Fatal(err)
			}
			if err := undo(); err != nil || len(commands) != count*2 {
				t.Fatalf("rollback %v %v", err, commands)
			}
			for _, cmd := range commands {
				if strings.Contains(cmd, " 22") || strings.Contains(cmd, "flush") || strings.Contains(cmd, "--reload") {
					t.Fatalf("changed external state: %s", cmd)
				}
			}
			commands = nil
			failAt = 2
			if _, err := h.prepareFirewall(t.Context(), plan); err == nil || len(commands) != 3 {
				t.Fatalf("partial write rollback: %v %v", err, commands)
			}
		})
	}
}

func TestManagedFirewallRejectsUnknownLayoutBeforeWrite(t *testing.T) {
	for _, line := range []string{"-A INPUT -j foreign", "-A INPUT -s 10.0.0.0/8 -p tcp --dport 8443 -m comment --comment weir:tcp:8443 -j ACCEPT", "-A INPUT -p tcp --dport 8443 -m comment --comment weir:malformed -j ACCEPT"} {
		writes := 0
		h := NewHelper(Paths{})
		h.firewallExec = func(_ context.Context, name string, args ...string) (string, error) {
			if name != "iptables" && name != "ip6tables" {
				return "", errNotInstalled
			}
			if slices.Equal(args, []string{"-S", "INPUT"}) {
				return "-P INPUT DROP\n" + line, nil
			}
			writes++
			return "", nil
		}
		if _, err := h.prepareFirewall(t.Context(), &contracts.FirewallPlan{Version: 1}); err == nil || writes != 0 {
			t.Fatalf("unknown rules changed: %s err=%v writes=%d", line, err, writes)
		}
	}
}

func TestKernelBatchSnapshotRestoresAllFiles(t *testing.T) {
	dir := t.TempDir()
	p := Paths{CoresDir: filepath.Join(dir, "cores"), KernelsDir: filepath.Join(dir, "kernels")}
	for _, d := range []string{p.CoresDir, p.KernelsDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}
	h := NewHelper(p)
	for _, name := range helperKernels {
		if err := os.WriteFile(h.Sup.config(name), []byte("old-"+name), 0640); err != nil {
			t.Fatal(err)
		}
	}
	before, err := h.snapshotKernels()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range helperKernels {
		_ = os.WriteFile(h.Sup.config(name), []byte("new"), 0640)
		_ = h.setPointer(name, "new-version")
	}
	if err := h.restoreKernels(before, map[string]bool{"mihomo": true, "sing-box": true, "xray": true}); err != nil {
		t.Fatal(err)
	}
	for _, name := range helperKernels {
		body, err := os.ReadFile(h.Sup.config(name))
		if err != nil || string(body) != "old-"+name || h.pointer(name) != "" {
			t.Fatalf("%s not restored: %s %v", name, body, err)
		}
	}
}
