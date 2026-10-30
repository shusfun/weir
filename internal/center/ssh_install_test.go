package center

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net"
	"regexp"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/shusfun/weir/packages/contracts"
)

// fakeSSH 是进程内的 SSH 服务器：只认一个密码，收到 exec 时交给 onExec，按它的返回值作退出码。
type fakeSSH struct {
	signer ssh.Signer
	cmds   chan string
	stdin  chan string
}

func newFakeSSH(t *testing.T, password string, onExec func(cmd string, out ssh.Channel) int) *fakeSSH {
	t.Helper()
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeSSH{signer: signer, cmds: make(chan string, 4), stdin: make(chan string, 4)}
	cfg := &ssh.ServerConfig{PasswordCallback: func(_ ssh.ConnMetadata, pw []byte) (*ssh.Permissions, error) {
		if string(pw) == password {
			return nil, nil
		}
		return nil, errors.New("denied")
	}}
	cfg.AddHostKey(signer)
	// 真的 TCP 监听（net.Pipe 没有缓冲，双方同时发版本行会互相卡住）
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			server, err := ln.Accept()
			if err != nil {
				return
			}
			go f.serve(server, cfg, onExec)
		}
	}()
	old := sshDial
	sshDial = func(ctx context.Context, host string, _ int) (net.Conn, error) {
		if ip := net.ParseIP(host); ip != nil && !publicAddr(ip) {
			return nil, errors.New("ssh_not_public")
		}
		return (&net.Dialer{}).DialContext(ctx, "tcp", ln.Addr().String())
	}
	t.Cleanup(func() { sshDial = old })
	return f
}

func (f *fakeSSH) serve(server net.Conn, cfg *ssh.ServerConfig, onExec func(cmd string, out ssh.Channel) int) {
	_, chans, reqs, err := ssh.NewServerConn(server, cfg)
	if err != nil {
		return
	}
	go ssh.DiscardRequests(reqs)
	for nc := range chans {
		ch, creqs, err := nc.Accept()
		if err != nil {
			continue
		}
		go func() {
			for req := range creqs {
				if req.Type != "exec" {
					_ = req.Reply(false, nil)
					continue
				}
				var p struct{ Command string }
				_ = ssh.Unmarshal(req.Payload, &p)
				_ = req.Reply(true, nil)
				f.cmds <- p.Command
				if strings.HasPrefix(p.Command, "sudo -S") {
					line, _ := bufio.NewReader(ch).ReadString('\n')
					f.stdin <- line
				}
				code := onExec(p.Command, ch)
				_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ C uint32 }{uint32(code)}))
				_ = ch.Close()
				return
			}
		}()
	}
}

var tokenArg = regexp.MustCompile(`--token '\\''([^']+)'\\''`)

func TestSSHInstallGate(t *testing.T) {
	s, admin := newInstalledServer(t)
	const password = "vps-root-Pa55"
	// 「VPS」收到安装命令后：回显一行带凭证的输出（应被遮盖），再用这个凭证真的配对并发一次心跳。
	f := newFakeSSH(t, password, func(cmd string, out ssh.Channel) int {
		m := tokenArg.FindStringSubmatch(cmd)
		if m == nil {
			_, _ = out.Write([]byte("no token\n"))
			return 2
		}
		_, _ = out.Write([]byte("Downloading Weir…\nusing token " + m[1] + " and " + password + "\n"))
		pub, priv, _ := ed25519.GenerateKey(rand.Reader)
		g := &fakeGate{t: t, c: &testClient{t: t, router: admin.router}, priv: priv}
		w := g.c.do("POST", "/api/v1/agent/pair", contracts.PairRequest{Name: "x", PublicKey: base64.StdEncoding.EncodeToString(pub), Token: m[1]}, nil)
		var res contracts.PairResult
		if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &res) != nil {
			return 1
		}
		g.id = res.GateID
		g.auth()
		g.call("POST", "/api/v1/agent/heartbeat", contracts.Heartbeat{Version: "test", Hostname: "vps", Arch: "amd64"})
		return 0
	})
	target := map[string]any{"host": "203.0.113.20", "port": 22, "user": "root", "password": password}

	// 内网地址不连
	mustJSON(t, admin, "POST", "/api/v1/admin/gates/ssh/probe", map[string]any{"host": "10.0.0.5", "user": "root"}, 400)
	probe := mustJSON(t, admin, "POST", "/api/v1/admin/gates/ssh/probe", target, 200)
	fp := probe["fingerprint"].(string)
	if fp != fingerprint(f.signer.PublicKey()) {
		t.Fatalf("fingerprint = %v", probe)
	}
	install := func(fp string) Job {
		t.Helper()
		body := map[string]any{"name": "东京-1", "fingerprint": fp}
		for k, v := range target {
			body[k] = v
		}
		res := mustJSON(t, admin, "POST", "/api/v1/admin/gates/ssh/install", body, 202)
		id := res["job"].(map[string]any)["id"].(string)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		j, _ := s.jobs.Wait(ctx, id)
		return j
	}
	// 中心地址还没记录时不能装（VPS 不知道连哪里）
	body := map[string]any{"name": "东京-1", "fingerprint": fp}
	for k, v := range target {
		body[k] = v
	}
	mustJSON(t, admin, "POST", "/api/v1/admin/gates/ssh/install", body, 409)
	s.origin.Lock()
	s.origin.value, s.origin.loaded = "https://center.example.test", true
	s.origin.Unlock()

	// 指纹与确认时不一致：不登录、不执行
	if j := install("SHA256:not-the-same"); j.Status != jobFailed || !strings.Contains(j.Error, "主机指纹") {
		t.Fatalf("mismatch job = %+v", j)
	}
	j := install(fp)
	if j.Status != jobDone || j.Result["gate_id"] == nil {
		t.Fatalf("install job = %+v", j)
	}
	cmd := <-f.cmds
	if !strings.HasPrefix(cmd, "sh -c ") || !strings.Contains(cmd, "https://center.example.test/install/gate.sh") || !strings.Contains(cmd, "WEIR_NONINTERACTIVE=1") {
		t.Fatalf("command = %s", cmd)
	}
	logs, _ := json.Marshal(j.Logs)
	if strings.Contains(string(logs), password) || tokenArg.FindString(cmd) != "" && strings.Contains(string(logs), tokenArg.FindStringSubmatch(cmd)[1]) || !strings.Contains(string(logs), "••••") {
		t.Fatalf("logs must mask token and password: %s", logs)
	}
	g, err := s.Store.Ent.Gate.Get(context.Background(), int(j.Result["gate_id"].(int)))
	if err != nil || g.Name != "东京-1" {
		t.Fatalf("gate = %v %v", g, err)
	}

	// 非 root 用户：经 sudo 执行，密码从标准输入给 sudo，不出现在命令里
	target["user"] = "ubuntu"
	install(fp)
	if cmd := <-f.cmds; !strings.HasPrefix(cmd, "sudo -S -p '' sh -c ") || strings.Contains(cmd, password) {
		t.Fatalf("sudo command = %s", cmd)
	}
	if in := <-f.stdin; in != password+"\n" {
		t.Fatalf("sudo stdin = %q", in)
	}
	// 密码错误：说明是登录失败
	target["user"], target["password"] = "root", "wrong"
	if j := install(fp); j.Status != jobFailed || !strings.Contains(j.Error, "登录失败") {
		t.Fatalf("bad password job = %+v", j)
	}
}

// 私钥：OpenSSH、RSA（PKCS#1）、EC、PKCS#8 都能用；带密码的要填密码；PuTTY .ppk 与超长内容拒绝。
func TestSSHKeyFormats(t *testing.T) {
	rsaKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	ecKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	_, edKey, _ := ed25519.GenerateKey(rand.Reader)
	pkcs8, _ := x509.MarshalPKCS8PrivateKey(rsaKey)
	ecDER, _ := x509.MarshalECPrivateKey(ecKey)
	openssh, _ := ssh.MarshalPrivateKey(edKey, "")
	locked, _ := ssh.MarshalPrivateKeyWithPassphrase(edKey, "", []byte("pp"))
	for name, pemText := range map[string]string{
		"rsa":     string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(rsaKey)})),
		"pkcs8":   string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8})),
		"ec":      string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: ecDER})),
		"openssh": string(pem.EncodeToMemory(openssh)),
	} {
		if _, err := (&sshTarget{PrivateKey: pemText}).auth(); err != nil {
			t.Fatalf("%s key: %v", name, err)
		}
	}
	lockedText := string(pem.EncodeToMemory(locked))
	if _, err := (&sshTarget{PrivateKey: lockedText}).auth(); err == nil || err.Error() != "ssh_key_passphrase_required" {
		t.Fatalf("locked key without passphrase = %v", err)
	}
	if _, err := (&sshTarget{PrivateKey: lockedText, Passphrase: "pp"}).auth(); err != nil {
		t.Fatalf("locked key with passphrase = %v", err)
	}
	for _, bad := range []string{"PuTTY-User-Key-File-3: ssh-ed25519\nEncryption: none\n", strings.Repeat("A", 17*1024)} {
		if _, err := (&sshTarget{PrivateKey: bad}).auth(); err == nil || err.Error() != "invalid_ssh_key" {
			t.Fatalf("bad key = %v", err)
		}
	}
}
