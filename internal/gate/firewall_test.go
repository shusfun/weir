package gate

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func fakeFirewall(outputs map[string]string, fail map[string]bool) func(context.Context, string, ...string) (string, error) {
	return func(_ context.Context, name string, args ...string) (string, error) {
		key := name
		if len(args) > 0 {
			key += " " + args[len(args)-1]
		}
		out, ok := outputs[key]
		if !ok {
			if _, installed := outputs[name]; !installed {
				return "", errNotInstalled
			}
		}
		if fail[key] {
			return out, errors.New("exit status 1")
		}
		return out, nil
	}
}

func TestDetectFirewall(t *testing.T) {
	ctx := context.Background()
	ufw := `Status: active

To                         Action      From
--                         ------      ----
22/tcp                     ALLOW       Anywhere
443                        ALLOW       Anywhere
20000:20100/udp            ALLOW       Anywhere
OpenSSH                    ALLOW       Anywhere
8443/tcp                   DENY        Anywhere
443 (v6)                   ALLOW       Anywhere (v6)
`
	fs := detectFirewall(ctx, fakeFirewall(map[string]string{"ufw": "", "ufw status": ufw}, nil))
	if fs.Tool != "ufw" || !fs.Active || !reflect.DeepEqual(fs.Rules, []string{"22/tcp", "443", "20000:20100/udp"}) {
		t.Fatalf("ufw = %+v", fs)
	}
	if fs = detectFirewall(ctx, fakeFirewall(map[string]string{"ufw": "", "ufw status": "Status: inactive\n"}, nil)); fs.Active {
		t.Fatalf("inactive ufw = %+v", fs)
	}
	fs = detectFirewall(ctx, fakeFirewall(map[string]string{"firewall-cmd": "", "firewall-cmd --state": "running\n", "firewall-cmd --list-ports": "8443/tcp 20000-20100/udp\n", "firewall-cmd --list-services": "ssh https dhcpv6-client\n"}, nil))
	if fs.Tool != "firewalld" || !fs.Active || !reflect.DeepEqual(fs.Rules, []string{"8443/tcp", "20000:20100/udp", "22/tcp", "443/tcp"}) {
		t.Fatalf("firewalld = %+v", fs)
	}
	ipt := "-P INPUT DROP\n-A INPUT -i lo -j ACCEPT\n-A INPUT -p tcp -m tcp --dport 22 -j ACCEPT\n-A INPUT -p udp -m udp --dport 8443 -j ACCEPT\n-A INPUT -s 10.0.0.0/8 -p tcp --dport 9000 -j ACCEPT\n"
	fs = detectFirewall(ctx, fakeFirewall(map[string]string{"iptables": "", "iptables INPUT": ipt}, nil))
	if fs.Tool != "iptables" || !fs.Active || !reflect.DeepEqual(fs.Rules, []string{"22/tcp", "8443/udp"}) {
		t.Fatalf("iptables = %+v", fs)
	}
	if fs = detectFirewall(ctx, fakeFirewall(map[string]string{"iptables": "", "iptables INPUT": "-P INPUT ACCEPT\n"}, nil)); fs.Active || fs.Error != "" {
		t.Fatalf("open iptables = %+v", fs)
	}
	// 跳到自定义链：看不懂就如实说读取失败，不猜放行。
	if fs = detectFirewall(ctx, fakeFirewall(map[string]string{"iptables": "", "iptables INPUT": "-P INPUT ACCEPT\n-A INPUT -j f2b-sshd\n"}, nil)); fs.Error == "" {
		t.Fatalf("custom chain = %+v", fs)
	}
	if fs = detectFirewall(ctx, fakeFirewall(map[string]string{}, nil)); fs.Tool != "none" {
		t.Fatalf("no firewall = %+v", fs)
	}
}

// 内核绑定端口失败（被出站连接的本地端口占着）时按端口冲突上报，中心据此换端口。
func TestBoundPortFromKernelLog(t *testing.T) {
	logs := []string{"starting", "FATAL start service: post-start v2ray server: listen tcp 127.0.0.1:41640: bind: address already in use"}
	if p := boundPort(logs, []int{443, 41640}); p != 41640 {
		t.Fatalf("stats port = %d", p)
	}
	if p := boundPort([]string{"listen tcp4 0.0.0.0:33522: bind: address already in use"}, []int{33522}); p != 33522 {
		t.Fatalf("inbound port = %d", p)
	}
	if p := boundPort([]string{"listen tcp 127.0.0.1:9: bind: address already in use"}, []int{443}); p != 0 {
		t.Fatalf("unrelated port = %d", p)
	}
}
