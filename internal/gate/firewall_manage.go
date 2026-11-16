package gate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/shusfun/weir/internal/buildinfo"
	"github.com/shusfun/weir/internal/releasefiles"
	"github.com/shusfun/weir/packages/contracts"
)

type firewallRunner func(context.Context, string, ...string) (string, error)
type firewallCommand struct {
	name string
	args []string
}
type firewallEdit struct{ apply, undo firewallCommand }
type managedPort struct {
	family    string
	port      contracts.FirewallPort
	permanent bool
}

var managedMarker = regexp.MustCompile(`weir:(tcp|udp):([0-9]{1,5})`)

func firewallMarker(v contracts.FirewallPort) string {
	return fmt.Sprintf("weir:%s:%d", v.Protocol, v.Port)
}

func (h *Helper) firewallRunner() firewallRunner {
	if h.firewallExec != nil {
		return h.firewallExec
	}
	return runFirewallCmd
}

func (h *Helper) firewallPlanPath() string { return filepath.Join(h.P.CoresDir, "firewall.json") }

// Each command has fixed arguments generated from a validated port. Shells,
// caller-provided rules, chain names, addresses and policies are never accepted.
func firewallCommands(tool string, v managedPort, add bool) []firewallCommand {
	p, proto, marker := strconv.Itoa(v.port.Port), v.port.Protocol, firewallMarker(v.port)
	switch tool {
	case "ufw":
		a := []string{"allow", "proto", proto, "from", "any", "to", "any", "port", p, "comment", marker}
		if !add {
			a = append([]string{"--force", "delete"}, a...)
		}
		return []firewallCommand{{"ufw", a}}
	case "firewalld":
		// The bounded log prefix is the native rich-rule ownership marker.
		rule := fmt.Sprintf(`rule family="%s" port port="%s" protocol="%s" log prefix="%s" limit value="1/m" accept`, v.family, p, proto, marker)
		op := "--add-rich-rule="
		if !add {
			op = "--remove-rich-rule="
		}
		args := []string{op + rule}
		if v.permanent {
			args = append([]string{"--permanent"}, args...)
		}
		return []firewallCommand{{"firewall-cmd", args}}
	case "iptables":
		name := "iptables"
		if v.family == "ipv6" {
			name = "ip6tables"
		}
		a := []string{"-w", "5", "-A", "INPUT", "-p", proto, "--dport", p, "-m", "comment", "--comment", marker, "-j", "ACCEPT"}
		if add {
			a[2] = "-I"
			a = append(a[:4], append([]string{"1"}, a[4:]...)...)
		} else {
			a[2] = "-D"
		}
		return []firewallCommand{{name, a}}
	}
	return nil
}

func validateFirewallZone(ctx context.Context, run firewallRunner) error {
	zone, err := run(ctx, "firewall-cmd", "--get-default-zone")
	if err != nil || !regexp.MustCompile(`^[a-zA-Z0-9_-]+$`).MatchString(strings.TrimSpace(zone)) {
		return errors.New("无法识别 firewalld 默认区域，拒绝修改")
	}
	active, err := run(ctx, "firewall-cmd", "--get-active-zones")
	if err != nil {
		return fmt.Errorf("读取 firewalld 活跃区域失败：%w", err)
	}
	for _, line := range strings.Split(active, "\n") {
		if line == "" || strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			continue
		}
		if strings.TrimSpace(line) != strings.TrimSpace(zone) {
			return errors.New("firewalld 使用非默认区域，无法确认端口生效范围，拒绝修改")
		}
	}
	return nil
}

func readManagedFirewall(ctx context.Context, run firewallRunner, tool string) ([]managedPort, error) {
	var result []managedPort
	if tool == "firewalld" {
		if err := validateFirewallZone(ctx, run); err != nil {
			return nil, err
		}
	}
	// UFW's saved command list includes inactive rules and represents both IP
	// families. Firewalld's runtime and persistent stores are reconciled separately.
	scopes := []managedPort{{family: "ipv4"}, {family: "ipv6"}}
	if tool == "firewalld" {
		scopes = []managedPort{{}, {permanent: true}}
	}
	for _, scope := range scopes {
		family := scope.family
		var out string
		var err error
		switch tool {
		case "ufw":
			if family == "ipv6" {
				continue
			}
			out, err = run(ctx, "ufw", "show", "added")
		case "firewalld":
			args := []string{"--list-rich-rules"}
			if scope.permanent {
				args = append([]string{"--permanent"}, args...)
			}
			out, err = run(ctx, "firewall-cmd", args...)
		case "iptables":
			name := "iptables"
			if family == "ipv6" {
				name = "ip6tables"
			}
			out, err = run(ctx, name, "-S", "INPUT")
		default:
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("读取 %s %s 规则失败：%w", tool, family, err)
		}
		for _, line := range strings.Split(out, "\n") {
			line = strings.TrimSpace(line)
			if tool == "iptables" && strings.Contains(line, " -j ") {
				target := strings.Fields(strings.SplitN(line, " -j ", 2)[1])[0]
				if target != "ACCEPT" && target != "DROP" && target != "REJECT" && target != "RETURN" {
					return nil, errors.New("INPUT 链含未知跳转，拒绝修改防火墙")
				}
			}
			if !strings.Contains(line, "weir:") {
				continue
			}
			m := managedMarker.FindStringSubmatch(line)
			if m == nil {
				return nil, errors.New("无法识别 Weir 防火墙标记，拒绝修改")
			}
			port, _ := strconv.Atoi(m[2])
			v := managedPort{family: family, port: contracts.FirewallPort{Port: port, Protocol: m[1]}, permanent: scope.permanent}
			if tool == "firewalld" {
				switch {
				case strings.Contains(line, `family="ipv4"`):
					v.family = "ipv4"
				case strings.Contains(line, `family="ipv6"`):
					v.family = "ipv6"
				default:
					return nil, errors.New("Weir firewalld 规则缺少地址族，拒绝修改")
				}
			}
			switch tool {
			case "ufw":
				normal := "ufw allow " + m[2] + "/" + m[1] + " comment '" + m[0] + "'"
				full := "ufw allow proto " + m[1] + " from any to any port " + m[2] + " comment '" + m[0] + "'"
				if line != normal && line != full {
					return nil, errors.New("Weir ufw 规则布局已变化，拒绝修改")
				}
			case "firewalld":
				commands := firewallCommands(tool, v, true)[0].args
				expected := strings.TrimPrefix(commands[len(commands)-1], "--add-rich-rule=")
				if line != expected {
					return nil, errors.New("Weir firewalld 规则布局已变化，拒绝修改")
				}
			case "iptables":
				canonical := fmt.Sprintf("-A INPUT -p %s -m %s --dport %d -m comment --comment %s -j ACCEPT", m[1], m[1], port, m[0])
				if strings.ReplaceAll(line, `"`, "") != canonical {
					return nil, errors.New("Weir iptables 规则布局已变化，拒绝修改")
				}
			}
			if port < 1 || port > 65535 || slices.Contains(result, v) {
				return nil, errors.New("重复或非法 Weir 防火墙规则，拒绝修改")
			}
			result = append(result, v)
		}
	}
	return result, nil
}

// prepareFirewall applies only the owned delta, retaining inverse operations
// until all kernels and the persistent desired state have committed.
func (h *Helper) prepareFirewall(ctx context.Context, plan *contracts.FirewallPlan) (func() error, error) {
	noop := func() error { return nil }
	if plan == nil {
		return noop, nil
	}
	if err := plan.Validate(); err != nil {
		return noop, err
	}
	run := h.firewallRunner()
	status := detectFirewall(ctx, run)
	if status.Error != "" {
		return noop, errors.New(status.Error)
	}
	if status.Tool == "none" {
		return noop, nil
	}
	if status.Tool == "firewalld" && !status.Active {
		return noop, errors.New("firewalld 未运行，无法同步端口规则")
	}
	old, err := readManagedFirewall(ctx, run, status.Tool)
	if err != nil {
		return noop, err
	}
	wanted := []managedPort{}
	families := []string{"ipv4", "ipv6"}
	if status.Tool == "ufw" {
		families = families[:1]
	}
	for _, family := range families {
		for _, port := range plan.Ports {
			v := managedPort{family: family, port: port}
			if !slices.Contains(wanted, v) {
				wanted = append(wanted, v)
				if status.Tool == "firewalld" {
					v.permanent = true
					wanted = append(wanted, v)
				}
			}
		}
	}
	var edits []firewallEdit
	for _, add := range []bool{true, false} {
		from, to := wanted, old
		if !add {
			from, to = old, wanted
		}
		for _, v := range from {
			if slices.Contains(to, v) {
				continue
			}
			apply, undo := firewallCommands(status.Tool, v, add), firewallCommands(status.Tool, v, !add)
			for i := range apply {
				edits = append(edits, firewallEdit{apply: apply[i], undo: undo[i]})
			}
		}
	}
	var done []firewallEdit
	rollback := func() error {
		rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		var errs []error
		for i := len(done) - 1; i >= 0; i-- {
			_, e := run(rollbackCtx, done[i].undo.name, done[i].undo.args...)
			log.Printf("firewall operation=firewall.rollback trace=%s tool=%s outcome_ok=%t code.file.path=internal/gate/firewall_manage.go code.function.name=prepareFirewall vcs.ref.head.revision=%s", releasefiles.Trace(ctx), status.Tool, e == nil, buildinfo.Revision)
			if e != nil {
				errs = append(errs, e)
			}
		}
		h.fwMu.Lock()
		h.fw = nil
		h.fwMu.Unlock()
		return errors.Join(errs...)
	}
	for _, edit := range edits {
		started := time.Now()
		_, err := run(ctx, edit.apply.name, edit.apply.args...)
		log.Printf("firewall operation=firewall.apply trace=%s tool=%s outcome_ok=%t ms=%d code.file.path=internal/gate/firewall_manage.go code.function.name=prepareFirewall vcs.ref.head.revision=%s", releasefiles.Trace(ctx), status.Tool, err == nil, time.Since(started).Milliseconds(), buildinfo.Revision)
		if err != nil {
			return noop, errors.Join(fmt.Errorf("%s 端口规则应用失败：%w", status.Tool, err), rollback())
		}
		done = append(done, edit)
	}
	h.fwMu.Lock()
	h.fw = nil
	h.fwMu.Unlock()
	return rollback, nil
}

func (h *Helper) persistFirewall(plan *contracts.FirewallPlan) error {
	if plan == nil {
		return nil
	}
	b, err := json.Marshal(plan)
	if err != nil {
		return err
	}
	return writeAtomic(h.firewallPlanPath(), b)
}

func (h *Helper) bootFirewall() error {
	b, err := os.ReadFile(h.firewallPlanPath())
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var plan contracts.FirewallPlan
	if err := json.Unmarshal(b, &plan); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err = h.prepareFirewall(ctx, &plan)
	return err
}
