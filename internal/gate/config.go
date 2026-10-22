// Package gate 是 Gate 执行端：以 migate-gate 身份运行的 Agent，以及以 root 运行、
// 只提供白名单操作的辅助程序（内核安装、配置自检与切换、进程托管、端口观测）。
package gate

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/imzyb/MiGate/packages/contracts"
)

// Paths 是 Gate 在本机使用的固定位置；测试与容器里可整体替换。
type Paths struct {
	Config     string // 设备身份与中心地址
	Data       string // Agent 状态（已生效配置、计量累计）
	Socket     string // 辅助程序的本机套接字
	CoresDir   string // 内核配置文件
	CertsDir   string // 证书文件
	KernelsDir string // 内核程序
	Root       string // 本项目程序目录（releases/、current）
	RunAs      string // 内核降权运行的账号
}

func DefaultPaths() Paths {
	return Paths{
		Config: "/etc/migate/gate/agent.json", Data: "/var/lib/migate/gate", Socket: "/run/migate/helper.sock",
		CoresDir: "/etc/migate/gate/cores", CertsDir: "/etc/migate/gate/certs", KernelsDir: "/opt/migate/cores",
		Root: "/opt/migate", RunAs: "migate-gate",
	}
}

// Config 是设备身份：中心地址、设备 ID 与 Ed25519 私钥。CAFile 可选，用于信任自建证书的中心。
type Config struct {
	Center     string `json:"center"`
	GateID     int64  `json:"gate_id"`
	PrivateKey string `json:"private_key"`
	CAFile     string `json:"ca_file,omitempty"`
}

// validCenter：远程中心必须是 HTTPS；只有本机回环地址允许明文 HTTP（单机角色）。
func validCenter(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" && u.Path != "/" {
		return errors.New("center must be an origin like https://example.com")
	}
	if u.Scheme == "https" {
		return nil
	}
	ip := net.ParseIP(u.Hostname())
	if u.Scheme == "http" && (strings.EqualFold(u.Hostname(), "localhost") || ip != nil && ip.IsLoopback()) {
		return nil
	}
	return errors.New("remote center requires https")
}

func LoadConfig(path string) (Config, error) {
	var c Config
	b, err := os.ReadFile(path)
	if err != nil {
		return c, err
	}
	if err = json.Unmarshal(b, &c); err != nil {
		return c, err
	}
	if err = validCenter(c.Center); err != nil {
		return c, err
	}
	if c.GateID <= 0 || c.PrivateKey == "" {
		return c, errors.New("device configuration incomplete; pair again")
	}
	return c, nil
}

// saveConfig 原子写入；以 root 运行时把属组设为 migate-gate 并设 0640，让 Agent 可读、其他账号不可读。
func saveConfig(path string, c Config, group string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(c, "", "  ")
	tmp := path + ".new"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	if os.Geteuid() == 0 && group != "" {
		if g, err := user.LookupGroup(group); err == nil {
			gid, _ := strconv.Atoi(g.Gid)
			if err = os.Chown(tmp, 0, gid); err != nil {
				return err
			}
			if err = os.Chmod(tmp, 0640); err != nil {
				return err
			}
		}
	}
	return os.Rename(tmp, path)
}

func httpClient(c Config, timeout time.Duration) (*http.Client, error) {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if c.CAFile != "" {
		pem, err := os.ReadFile(c.CAFile)
		if err != nil {
			return nil, err
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, errors.New("ca_file has no certificates")
		}
		tr.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: pool}
	}
	return &http.Client{Transport: tr, Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, nil
}

// Pair 用一次性凭证向中心配对，保存设备身份。已存在设备配置时直接返回（重复执行无副作用）。
func Pair(ctx context.Context, p Paths, center, token, name, caFile string) error {
	center = strings.TrimRight(center, "/")
	if err := validCenter(center); err != nil {
		return err
	}
	if _, err := os.Stat(p.Config); err == nil {
		return nil
	}
	if name == "" {
		name, _ = os.Hostname()
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	cfg := Config{Center: center, PrivateKey: base64.StdEncoding.EncodeToString(priv), CAFile: caFile}
	client, err := httpClient(cfg, 15*time.Second)
	if err != nil {
		return err
	}
	body, _ := json.Marshal(contracts.PairRequest{Name: name, PublicKey: base64.StdEncoding.EncodeToString(pub), Token: token})
	req, err := http.NewRequestWithContext(ctx, "POST", center+"/api/v1/agent/pair", strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 201 {
		return statusError{resp.StatusCode}
	}
	var res contracts.PairResult
	if err = json.NewDecoder(resp.Body).Decode(&res); err != nil || res.GateID <= 0 {
		return errors.New("invalid pair response")
	}
	cfg.GateID = res.GateID
	return saveConfig(p.Config, cfg, p.RunAs)
}

// LocalPair 是单机角色：等本机中心初始化完成后用安装器写入的一次性凭证配对，成功后删除凭证文件。
func LocalPair(ctx context.Context, p Paths, center, tokenFile string) error {
	raw, err := os.ReadFile(tokenFile)
	if err != nil {
		if _, statErr := os.Stat(p.Config); statErr == nil {
			return nil
		}
		return errors.New("local pair token unavailable")
	}
	token := strings.TrimSpace(string(raw))
	for {
		err = Pair(ctx, p, center, token, "", "")
		if err == nil {
			_ = os.Remove(tokenFile)
			return nil
		}
		var se statusError
		if errors.As(err, &se) && se.code != 503 && se.code != 429 && se.code < 500 {
			return fmt.Errorf("local pair refused: %w", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}

type statusError struct{ code int }

func (e statusError) Error() string { return fmt.Sprintf("center HTTP %d", e.code) }
