package gate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/shusfun/weir/internal/hoststats"
)

// hostRepairKeys 是允许修改的系统参数。值也在白名单里。
// 执行前备份，写完复测，没有变成目标值就回退。不重启代理内核，不改面板配置。
var hostRepairKeys = map[string]map[string]bool{
	"net.ipv4.tcp_congestion_control": {"bbr": true, "cubic": true},
	"net.core.default_qdisc":          {"fq": true, "fq_codel": true},
	"net.core.rmem_max":               {"16777216": true, "8388608": true},
	"net.core.wmem_max":               {"16777216": true, "8388608": true},
	"dns.resolvers":                   {"public": true},
	"dns.recheck":                     {"read": true},
}

func (h *Helper) repairHost(ctx context.Context, key, value string) (string, error) {
	allowed := hostRepairKeys[key]
	if allowed == nil || !allowed[value] {
		return "", fmt.Errorf("不允许修改 %s", key)
	}
	if strings.Contains(key, "..") || strings.ContainsAny(key, "/\\") {
		return "", fmt.Errorf("不允许修改 %s", key)
	}
	if key == "dns.recheck" {
		status, ok := hoststats.CompareDNS(ctx)
		if !ok {
			return "", fmt.Errorf("复测结果未知")
		}
		return status, nil
	}
	if key == "dns.resolvers" {
		return h.repairDNS(ctx, value)
	}
	current, err := os.ReadFile(sysctlPath(key))
	if err != nil {
		return "", fmt.Errorf("读不到当前值：%w", err)
	}
	backup := filepath.Join(h.P.Data, "sysctl-backup-"+strings.ReplaceAll(key, ".", "-"))
	if err = os.MkdirAll(h.P.Data, 0750); err != nil {
		return "", err
	}
	if err = os.WriteFile(backup, current, 0600); err != nil {
		return "", err
	}
	if err = os.WriteFile(sysctlPath(key), []byte(value+"\n"), 0644); err != nil {
		_ = os.WriteFile(sysctlPath(key), current, 0644)
		return "", err
	}
	time.Sleep(50 * time.Millisecond)
	got, err := os.ReadFile(sysctlPath(key))
	if err != nil || strings.TrimSpace(string(got)) != value {
		_ = os.WriteFile(sysctlPath(key), current, 0644)
		if err == nil {
			err = fmt.Errorf("复测仍是 %s", strings.TrimSpace(string(got)))
		}
		return "", fmt.Errorf("没有改善，已回退：%w", err)
	}
	_ = ctx
	return strings.TrimSpace(string(current)), nil
}

func (h *Helper) repairDNS(ctx context.Context, value string) (string, error) {
	if value != "public" {
		return "", fmt.Errorf("不允许修改 dns.resolvers")
	}
	before, beforeOK := hoststats.CompareDNS(ctx)
	current, err := os.ReadFile("/etc/resolv.conf")
	if err != nil {
		return "", fmt.Errorf("读不到当前解析配置：%w", err)
	}
	backup := filepath.Join(h.P.Data, "resolv.conf.backup")
	if err = os.MkdirAll(h.P.Data, 0750); err != nil {
		return "", err
	}
	if err = os.WriteFile(backup, current, 0600); err != nil {
		return "", err
	}
	next := []byte("# weir host repair\nnameserver 1.1.1.1\nnameserver 8.8.8.8\n")
	if err = os.WriteFile("/etc/resolv.conf", next, 0644); err != nil {
		return "", err
	}
	_ = h.repairResolvedDropIn()
	status, ok := hoststats.CompareDNS(ctx)
	if !ok || status != "clean" || (beforeOK && before == "clean" && status != "clean") {
		_ = os.WriteFile("/etc/resolv.conf", current, 0644)
		if !ok {
			return "", fmt.Errorf("复测结果未知，已回退")
		}
		return "", fmt.Errorf("复测仍是 %s，已回退", status)
	}
	return "resolv.conf", nil
}

func (h *Helper) repairResolvedDropIn() error {
	dir := "/etc/systemd/resolved.conf.d"
	if _, err := os.Stat("/usr/bin/resolvectl"); err != nil {
		if _, err = os.Stat("/bin/resolvectl"); err != nil {
			return nil
		}
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	body := []byte("[Resolve]\nDNS=1.1.1.1 8.8.8.8\nFallbackDNS=8.8.8.8\n")
	return os.WriteFile(filepath.Join(dir, "weir.conf"), body, 0644)
}

func sysctlPath(key string) string {
	return filepath.Join("/proc/sys", strings.ReplaceAll(key, ".", "/"))
}
