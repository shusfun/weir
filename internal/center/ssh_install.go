package center

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/ssh"
)

// 用 SSH 一次装好 Gate：管理员填 VPS 的地址与登录方式，先确认主机指纹，再由中心登录上去执行与「复制命令」相同的
// 一行安装命令，日志实时显示，装完断开。登录密码与私钥只在这次任务的内存里，不存库、不写日志与审计、不给 dsh；
// 中心只执行这一条固定命令，不提供任意 Shell。VPS 仍主动连中心配对，之后与手动安装的 Gate 完全一样。

type sshTarget struct {
	Host       string `json:"host"`
	Port       int    `json:"port"`
	User       string `json:"user"`
	Password   string `json:"password"`
	PrivateKey string `json:"private_key"`
	Passphrase string `json:"passphrase"`
}

const (
	sshDialTimeout    = 15 * time.Second
	sshInstallTimeout = 15 * time.Minute
)

// sshDial 连接 VPS 的 SSH 端口：只连公网地址（测试中替换）。
var sshDial = func(ctx context.Context, host string, port int) (net.Conn, error) {
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil || len(ips) == 0 {
		return nil, errors.New("ssh_unresolved")
	}
	for _, ip := range ips {
		if !publicAddr(ip) {
			return nil, errors.New("ssh_not_public")
		}
	}
	return (&net.Dialer{Timeout: sshDialTimeout}).DialContext(ctx, "tcp", net.JoinHostPort(ips[0].String(), strconv.Itoa(port)))
}

func (s *Server) sshInstallRoutes(admin *gin.RouterGroup) {
	admin.POST("/gates/ssh/probe", s.sshProbe)
	admin.POST("/gates/ssh/install", s.sshInstall)
}

func (t *sshTarget) normalize() error {
	t.Host = strings.Trim(strings.TrimSpace(t.Host), "[]")
	t.User = strings.TrimSpace(t.User)
	if t.Port == 0 {
		t.Port = 22
	}
	switch {
	case t.Host == "" || len(t.Host) > 255 || strings.ContainsAny(t.Host, " /@"):
		return errors.New("invalid_ssh_host")
	case t.Port < 1 || t.Port > 65535:
		return errors.New("invalid_ssh_port")
	case t.User == "" || len(t.User) > 64 || strings.ContainsAny(t.User, " :@/"):
		return errors.New("invalid_ssh_user")
	}
	if ip := net.ParseIP(t.Host); ip != nil && !publicAddr(ip) {
		return errors.New("ssh_not_public")
	}
	return nil
}

func (t *sshTarget) auth() ([]ssh.AuthMethod, error) {
	if strings.TrimSpace(t.PrivateKey) != "" {
		// 与控制台的文件限制一致：私钥文件不会超过 16 KB
		if len(t.PrivateKey) > 16*1024 {
			return nil, errors.New("invalid_ssh_key")
		}
		var signer ssh.Signer
		var err error
		if t.Passphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(t.PrivateKey), []byte(t.Passphrase))
		} else {
			signer, err = ssh.ParsePrivateKey([]byte(t.PrivateKey))
		}
		var missing *ssh.PassphraseMissingError
		switch {
		case errors.As(err, &missing):
			return nil, errors.New("ssh_key_passphrase_required")
		case err != nil:
			return nil, errors.New("invalid_ssh_key")
		}
		return []ssh.AuthMethod{ssh.PublicKeys(signer)}, nil
	}
	if t.Password == "" {
		return nil, errors.New("ssh_auth_required")
	}
	pw := t.Password
	return []ssh.AuthMethod{ssh.Password(pw), ssh.KeyboardInteractive(func(_, _ string, qs []string, _ []bool) ([]string, error) {
		out := make([]string, len(qs))
		for i := range qs {
			out[i] = pw
		}
		return out, nil
	})}, nil
}

func fingerprint(k ssh.PublicKey) string {
	h := sha256.Sum256(k.Marshal())
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(h[:])
}

var errProbeDone = errors.New("probe done")

// sshProbe 只做到握手、拿到主机公钥就断开（不发送密码），给管理员核对指纹。
func (s *Server) sshProbe(c *gin.Context) {
	if !s.rate(c, "ssh-probe", 30) {
		return
	}
	var t sshTarget
	if !bind(c, &t) {
		return
	}
	if err := t.normalize(); err != nil {
		failure(c, 400, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), sshDialTimeout)
	defer cancel()
	conn, err := sshDial(ctx, t.Host, t.Port)
	if err != nil {
		failSSH(c, err)
		return
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(sshDialTimeout))
	var key ssh.PublicKey
	cfg := &ssh.ClientConfig{User: t.User, Timeout: sshDialTimeout, HostKeyCallback: func(_ string, _ net.Addr, k ssh.PublicKey) error {
		key = k
		return errProbeDone
	}}
	_, _, _, err = ssh.NewClientConn(conn, net.JoinHostPort(t.Host, strconv.Itoa(t.Port)), cfg)
	if key == nil {
		failDetail(c, 422, "ssh_handshake_failed", sshReason(err), nil)
		return
	}
	c.JSON(200, gin.H{"fingerprint": fingerprint(key), "key_type": key.Type()})
}

func failSSH(c *gin.Context, err error) {
	switch err.Error() {
	case "ssh_unresolved", "ssh_not_public":
		failure(c, 400, err.Error())
	default:
		failDetail(c, 422, "ssh_unreachable", sshReason(err), nil)
	}
}

// sshReason 把连接错误说成人话；不含任何凭据（错误来自网络层与握手，里面没有密码）。
func sshReason(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "i/o timeout") || strings.Contains(msg, "deadline exceeded"):
		// ping 走 ICMP，安全组与防火墙对它和 TCP 端口分开放行：ping 得通不说明 SSH 端口连得上
		return "TCP 连接 15 秒没有建立：安全组、防火墙没有放行这个端口，或中心所在网络到这台 VPS 不通。ping 得通不代表 SSH 端口连得上"
	case strings.Contains(msg, "connection refused"):
		return "端口没有在监听：检查 SSH 端口是否正确"
	case strings.Contains(msg, "unable to authenticate"):
		return "登录失败：用户名、密码或私钥不对，或这台 VPS 不允许这种登录方式"
	}
	return msg
}

// sshInstall 启动安装任务。fingerprint 必须是管理员在上一步核对过的指纹，连接时不一致就中止。
func (s *Server) sshInstall(c *gin.Context) {
	if !s.rate(c, "ssh-install", 10) {
		return
	}
	var req struct {
		sshTarget
		Name        string `json:"name"`
		Fingerprint string `json:"fingerprint"`
	}
	if !bind(c, &req) {
		return
	}
	t := req.sshTarget
	if err := t.normalize(); err != nil {
		failure(c, 400, err.Error())
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || len(name) > 100 {
		failure(c, 400, "invalid_name")
		return
	}
	if !strings.HasPrefix(req.Fingerprint, "SHA256:") {
		failure(c, 400, "ssh_fingerprint_required")
		return
	}
	auth, err := t.auth()
	if err != nil {
		failure(c, 400, err.Error())
		return
	}
	// VPS 要能用 HTTPS 连回中心：必须已记录中心地址（见「中心地址与 HTTPS」），不能是本机回环
	origin := s.centerOrigin(c.Request.Context())
	if u, err := url.Parse(origin); origin == "" || err != nil || u.Scheme != "https" || loopbackURL(origin) {
		failure(c, 409, "center_origin_required")
		return
	}
	token, err := s.newInvite(c.Request.Context(), name)
	if err != nil {
		failInternal(c, 503, "invite_unavailable", err)
		return
	}
	s.audit(c, "gate.ssh_install", name+" @ "+t.Host, "started")
	// 安装命令与「复制命令」相同；凭证只作为参数传入。WEIR_NONINTERACTIVE 让安装脚本不提问。
	script := "curl -fsSL " + shellQuote(origin+"/install/gate.sh") + " | WEIR_NONINTERACTIVE=1 sh -s -- --token " + shellQuote(token)
	host, port, user, password, keyed := t.Host, t.Port, t.User, t.Password, strings.TrimSpace(t.PrivateKey) != ""
	expected := req.Fingerprint
	job := s.jobs.StartWith(jobSpec{ID: "gate-ssh:" + randomHex(6), Kind: "gate-ssh", Title: "安装 Gate「" + name + "」（" + host + "）", Trace: c.GetString("request_id"), CancelMsg: "已取消安装"}, func(bg context.Context, run JobRun) (map[string]any, error) {
		ctx, cancel := context.WithTimeout(bg, sshInstallTimeout)
		defer cancel()
		mask := func(line string) string {
			line = strings.ReplaceAll(line, token, "••••")
			if password != "" {
				line = strings.ReplaceAll(line, password, "••••")
			}
			return line
		}
		run.Step("连接 " + net.JoinHostPort(host, strconv.Itoa(port)))
		conn, err := sshDial(ctx, host, port)
		if err != nil {
			return nil, errors.New(sshReason(err))
		}
		defer conn.Close()
		cfg := &ssh.ClientConfig{User: user, Auth: auth, Timeout: sshDialTimeout, HostKeyCallback: func(_ string, _ net.Addr, k ssh.PublicKey) error {
			if fingerprint(k) != expected {
				return errors.New("主机指纹与确认时不一致（" + fingerprint(k) + "），已中止：可能连到了别的机器")
			}
			return nil
		}}
		_ = conn.SetDeadline(time.Now().Add(sshDialTimeout))
		cc, chans, reqs, err := ssh.NewClientConn(conn, net.JoinHostPort(host, strconv.Itoa(port)), cfg)
		if err != nil {
			if strings.Contains(err.Error(), "主机指纹") {
				return nil, err
			}
			return nil, errors.New(sshReason(err))
		}
		_ = conn.SetDeadline(time.Time{})
		client := ssh.NewClient(cc, chans, reqs)
		defer client.Close()
		run.Step("登录成功，开始安装")
		sess, err := client.NewSession()
		if err != nil {
			return nil, err
		}
		defer sess.Close()
		cmd := "sh -c " + shellQuote(script)
		var stdin io.Reader
		if user != "root" {
			if keyed {
				cmd = "sudo -n " + cmd
			} else {
				cmd, stdin = "sudo -S -p '' "+cmd, strings.NewReader(password+"\n")
			}
		}
		sess.Stdin = stdin
		out, w := io.Pipe()
		sess.Stdout, sess.Stderr = w, w
		done := make(chan struct{})
		go func() {
			defer close(done)
			sc := bufio.NewScanner(out)
			sc.Buffer(make([]byte, 64*1024), 64*1024)
			for sc.Scan() {
				if line := strings.TrimSpace(mask(sc.Text())); line != "" {
					run.Log(line)
				}
			}
		}()
		go func() {
			<-ctx.Done()
			_ = sess.Close()
		}()
		err = sess.Run(cmd)
		w.Close()
		<-done
		if err != nil {
			if ctx.Err() != nil {
				return nil, errors.New("安装超过 15 分钟没有结束，已中止")
			}
			var exit *ssh.ExitError
			if errors.As(err, &exit) {
				hint := ""
				if user != "root" && keyed && exit.ExitStatus() == 1 {
					hint = "；用户不是 root 时需要免密 sudo，或改用 root / 密码登录"
				}
				return nil, fmt.Errorf("安装命令退出码 %d，见上方日志%s", exit.ExitStatus(), hint)
			}
			return nil, errors.New(mask(err.Error()))
		}
		run.Step("安装完成，等待 VPS 连上中心")
		gid, err := s.waitPaired(ctx, token)
		if err != nil {
			return nil, err
		}
		return map[string]any{"gate_id": gid}, nil
	})
	c.JSON(202, gin.H{"job": job})
}

// waitPaired 等安装出的 Gate 配对并上线：先等配对记录（最多 60 秒），再等第一次心跳（最多 2 分钟）。
func (s *Server) waitPaired(ctx context.Context, token string) (int, error) {
	gid := 0
	for i := 0; i < 60 && gid == 0; i++ {
		if raw, ok, _ := s.Store.KV.Get(ctx, "paired:"+digest(token)); ok {
			gid, _ = strconv.Atoi(raw)
			break
		}
		if !sleepCtx(ctx, time.Second) {
			return 0, ctx.Err()
		}
	}
	if gid == 0 {
		return 0, errors.New("安装命令已结束，但 60 秒内没有收到这台 VPS 的配对：检查日志里的错误，或 VPS 能否用 HTTPS 访问中心地址")
	}
	for i := 0; i < 120; i++ {
		if g, err := s.Store.Ent.Gate.Get(ctx, gid); err == nil && g.LastSeenAt != nil && time.Since(*g.LastSeenAt) < 90*time.Second {
			return gid, nil
		}
		if !sleepCtx(ctx, time.Second) {
			return gid, ctx.Err()
		}
	}
	return gid, errors.New("已配对，但 2 分钟内没有收到心跳：在 VPS 上查看 weir-gate 服务状态")
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// newInvite 生成一次性配对凭证（1 小时）。
func (s *Server) newInvite(ctx context.Context, name string) (string, error) {
	token := randomToken(32)
	b, _ := json.Marshal(pairInvite{Name: name})
	return token, s.Store.KV.Set(ctx, "pair:"+digest(token), string(b), time.Hour)
}
