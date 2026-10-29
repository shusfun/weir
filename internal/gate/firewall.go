package gate

import (
	"context"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/shusfun/weir/packages/contracts"
)

// 本机防火墙（只读）：辅助程序以 root 运行固定的查询命令，判断节点端口有没有放行，在控制台提示；不改防火墙规则。
// 依次看 ufw、firewalld、iptables；都没有时为 none。读不懂的规则（跳到自定义链等）不猜，标为读取失败。

var (
	ufwRule    = regexp.MustCompile(`^(\d{1,5})(?::(\d{1,5}))?(?:/(tcp|udp))?(?:\s+\(v6\))?\s+ALLOW(?:\s+IN)?\s+Anywhere`)
	portToken  = regexp.MustCompile(`^\d{1,5}(?:[:-]\d{1,5})?(?:/(?:tcp|udp))?$`)
	iptAccept  = regexp.MustCompile(`-p (tcp|udp)\b.*--dports? (\d{1,5}(?::\d{1,5})?)\b.*-j ACCEPT`)
	knownPorts = map[string]string{"ssh": "22/tcp", "http": "80/tcp", "https": "443/tcp"}
)

const firewallTTL = time.Minute

// firewall 返回缓存的防火墙状态（一分钟内不重复查询）。
func (h *Helper) firewall() *contracts.FirewallStatus {
	h.fwMu.Lock()
	defer h.fwMu.Unlock()
	if h.fw != nil && time.Since(h.fwAt) < firewallTTL {
		return h.fw
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	h.fw, h.fwAt = detectFirewall(ctx, runFirewallCmd), time.Now()
	return h.fw
}

func runFirewallCmd(ctx context.Context, name string, args ...string) (string, error) {
	if _, err := exec.LookPath(name); err != nil {
		return "", errNotInstalled
	}
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	return string(out), err
}

type notInstalled struct{}

func (notInstalled) Error() string { return "not installed" }

var errNotInstalled error = notInstalled{}

// detectFirewall 按 ufw → firewalld → iptables 的顺序读取；run 便于测试替换。
func detectFirewall(ctx context.Context, run func(context.Context, string, ...string) (string, error)) *contracts.FirewallStatus {
	if out, err := run(ctx, "ufw", "status"); err != errNotInstalled {
		fs := &contracts.FirewallStatus{Tool: "ufw"}
		if err != nil {
			fs.Error = strings.TrimSpace(out)
			return fs
		}
		fs.Active = strings.Contains(out, "Status: active")
		for _, line := range strings.Split(out, "\n") {
			if m := ufwRule.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
				r := m[1]
				if m[2] != "" {
					r += ":" + m[2]
				}
				if m[3] != "" {
					r += "/" + m[3]
				}
				fs.Rules = appendUnique(fs.Rules, r)
			}
		}
		return fs
	}
	if out, err := run(ctx, "firewall-cmd", "--state"); err != errNotInstalled {
		fs := &contracts.FirewallStatus{Tool: "firewalld", Active: err == nil && strings.TrimSpace(out) == "running"}
		if !fs.Active {
			return fs
		}
		ports, err := run(ctx, "firewall-cmd", "--list-ports")
		if err != nil {
			fs.Error = strings.TrimSpace(ports)
			return fs
		}
		for _, p := range strings.Fields(ports) {
			if portToken.MatchString(p) {
				fs.Rules = appendUnique(fs.Rules, strings.ReplaceAll(p, "-", ":"))
			}
		}
		services, _ := run(ctx, "firewall-cmd", "--list-services")
		for _, name := range strings.Fields(services) {
			if p, ok := knownPorts[name]; ok {
				fs.Rules = appendUnique(fs.Rules, p)
			}
		}
		return fs
	}
	if out, err := run(ctx, "iptables", "-S", "INPUT"); err != errNotInstalled {
		fs := &contracts.FirewallStatus{Tool: "iptables"}
		if err != nil {
			fs.Error = strings.TrimSpace(out)
			return fs
		}
		blocking := false
		for _, line := range strings.Split(out, "\n") {
			line = strings.TrimSpace(line)
			switch {
			case line == "-P INPUT DROP" || line == "-P INPUT REJECT":
				blocking = true
			case strings.HasPrefix(line, "-A INPUT") && (strings.HasSuffix(line, "-j DROP") || strings.HasSuffix(line, "-j REJECT") || strings.Contains(line, "-j REJECT ")):
				if !strings.Contains(line, "--dport") && !strings.Contains(line, "-s ") {
					blocking = true
				}
			case strings.HasPrefix(line, "-A INPUT") && strings.Contains(line, " -j ") && !strings.HasSuffix(line, "-j ACCEPT") && !strings.HasSuffix(line, "-j DROP") && !strings.Contains(line, "-j REJECT"):
				fs.Error = "INPUT 链跳到了自定义链，无法判断"
			}
			if m := iptAccept.FindStringSubmatch(line); m != nil && !strings.Contains(line, "-s ") {
				fs.Rules = appendUnique(fs.Rules, m[2]+"/"+m[1])
			}
		}
		fs.Active = blocking
		if fs.Error != "" {
			fs.Active = true
		}
		return fs
	}
	return &contracts.FirewallStatus{Tool: "none"}
}

func appendUnique(list []string, v string) []string {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}
