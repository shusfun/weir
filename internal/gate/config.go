// Package gate 是 Gate 执行端：以 weir-gate 身份运行的 Agent，以及以 root 运行、
// 只提供白名单操作的辅助程序（内核安装、配置自检与切换、进程托管、端口观测）。
package gate

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/shusfun/weir/packages/contracts"
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
		Config: "/etc/weir/gate/agent.json", Data: "/var/lib/weir/gate", Socket: "/run/weir/helper.sock",
		CoresDir: "/etc/weir/gate/cores", CertsDir: "/etc/weir/gate/certs", KernelsDir: "/opt/weir/cores",
		Root: "/opt/weir", RunAs: "weir-gate",
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
	if _, err := os.Stat(path + ".pairing"); err == nil {
		return Config{}, errors.New("device pairing is incomplete; retry pairing before starting the agent")
	} else if !errors.Is(err, os.ErrNotExist) {
		return Config{}, err
	}
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

// saveConfig 原子写入；以 root 运行时把属组设为 weir-gate 并设 0640，让 Agent 可读、其他账号不可读。
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

// Pair 默认保留已有身份；显式重新接入时，只有中心接受新凭证后才替换身份并清除旧中心的配置与计量状态。
// 调用方替换身份前必须停止 Agent，成功后重新启动，避免旧进程写回旧状态。
func Pair(ctx context.Context, p Paths, center, token, name, caFile string, replace bool) error {
	center = strings.TrimRight(center, "/")
	if err := validCenter(center); err != nil {
		return err
	}
	marker := p.Config + ".pairing"
	if _, err := os.Stat(marker); err == nil && !replace {
		return errors.New("device pairing is incomplete; retry pairing before starting the agent")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if _, err := os.Stat(p.Config); err == nil && !replace {
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
	var trace [24]byte
	if _, err = rand.Read(trace[:]); err != nil {
		return err
	}
	req.Header.Set("traceparent", "00-"+hex.EncodeToString(trace[:16])+"-"+hex.EncodeToString(trace[16:])+"-01")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	log.Printf("pair response: status=%d request=%q trace=%q", resp.StatusCode, resp.Header.Get("X-Request-Id"), resp.Header.Get("X-Trace-Id"))
	if resp.StatusCode != 201 {
		var problem struct {
			Error struct{ Code string } `json:"error"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&problem)
		code := problem.Error.Code
		// Only fixed error codes are safe to echo into the installer log, never raw response text.
		if len(code) > 80 || strings.IndexFunc(code, func(r rune) bool { return r != '_' && (r < 'a' || r > 'z') && (r < '0' || r > '9') }) >= 0 {
			code = ""
		}
		return fmt.Errorf("pair rejected: %w code=%q request=%q trace=%q", statusError{resp.StatusCode}, code, resp.Header.Get("X-Request-Id"), resp.Header.Get("X-Trace-Id"))
	}
	var res contracts.PairResult
	if err = json.NewDecoder(resp.Body).Decode(&res); err != nil || res.GateID <= 0 {
		return errors.New("invalid pair response")
	}
	cfg.GateID = res.GateID
	if replace {
		if err := os.MkdirAll(filepath.Dir(p.Config), 0750); err != nil {
			return fmt.Errorf("cannot prepare device identity replacement: %w", err)
		}
		// A crash or local error between the two writes must never start a new identity with old state.
		if err := os.WriteFile(marker, nil, 0600); err != nil {
			return fmt.Errorf("cannot protect device identity replacement: %w", err)
		}
	}
	if err := saveConfig(p.Config, cfg, p.RunAs); err != nil {
		return err
	}
	if replace {
		if err := os.Remove(filepath.Join(p.Data, "state.json")); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("new identity saved but old agent state could not be cleared: %w", err)
		}
		if err := os.Remove(marker); err != nil {
			return fmt.Errorf("new identity saved but replacement barrier could not be cleared: %w", err)
		}
	}
	return nil
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
		err = Pair(ctx, p, center, token, "", "", false)
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
