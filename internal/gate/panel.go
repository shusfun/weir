package gate

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/shusfun/weir/packages/contracts"
)

// 1Panel：VPS 上装有 1Panel（≥ v2.0.16）且管理员填了 API 密钥时，Gate 只经 127.0.0.1 调用它申请本机 IP 证书，
// 由 1Panel 的 OpenResty 应答验证并自动续签。1Panel 的 API 密钥不能限定权限（等同这台机器的 root），
// 所以只由 root 辅助程序保存与使用（panel.json，0600），Agent 读不到，也从不上报中心。
// Gate 只调用证书接口，只新增和续签自己申请的证书，不改 1Panel 的网站配置。

// PanelConfig 是保存在 Gate 本机的 1Panel 连接信息。
type PanelConfig struct {
	Port   int    `json:"port"`
	HTTPS  bool   `json:"https"`
	APIKey string `json:"api_key"`
}

func (c PanelConfig) base() string {
	scheme := "http"
	if c.HTTPS {
		scheme = "https"
	}
	return scheme + "://127.0.0.1:" + strconv.Itoa(c.Port)
}

// panelDetected 判断这台机器上有没有 1Panel（看程序与服务单元，不看端口）。
var panelDetected = func() bool {
	for _, p := range []string{"/usr/local/bin/1panel", "/usr/bin/1panel", "/etc/systemd/system/1panel.service", "/etc/systemd/system/1panel-core.service"} {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

type panelClient struct {
	cfg  PanelConfig
	http *http.Client
}

func newPanelClient(cfg PanelConfig) *panelClient {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	// 只连本机回环；1Panel 默认用自签证书提供 HTTPS，回环上不校验证书。
	tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec
	tr.Proxy = nil
	return &panelClient{cfg: cfg, http: &http.Client{Transport: tr, Timeout: 30 * time.Second}}
}

// do 按 1Panel API 的签名方式调用：1Panel-Token = md5("1panel" + 密钥 + 时间戳)。
func (p *panelClient) do(ctx context.Context, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, p.cfg.base()+path, rd)
	if err != nil {
		return err
	}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sum := md5.Sum([]byte("1panel" + p.cfg.APIKey + ts))
	req.Header.Set("1Panel-Token", hex.EncodeToString(sum[:]))
	req.Header.Set("1Panel-Timestamp", ts)
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.http.Do(req)
	if err != nil {
		return fmt.Errorf("无法连接本机 1Panel：%w", err)
	}
	defer resp.Body.Close()
	var env struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&env); err != nil {
		return fmt.Errorf("1Panel 返回了无法识别的内容（HTTP %d）", resp.StatusCode)
	}
	if resp.StatusCode == 401 || resp.StatusCode == 403 || env.Code == 401 || env.Code == 403 {
		return errors.New("1Panel 拒绝了 API 密钥：请确认密钥正确，且 API 接口白名单包含 127.0.0.1")
	}
	if resp.StatusCode != 200 || env.Code != 200 && env.Code != 0 {
		return fmt.Errorf("1Panel 返回错误：%s", strings.TrimSpace(env.Message))
	}
	if out != nil && len(env.Data) > 0 {
		return json.Unmarshal(env.Data, out)
	}
	return nil
}

// version 用于测试连接；返回 1Panel 版本号。
func (p *panelClient) version(ctx context.Context) (string, error) {
	var v struct {
		SystemVersion string `json:"systemVersion"`
	}
	if err := p.do(ctx, "POST", "/api/v2/settings/search", map[string]any{}, &v); err != nil {
		return "", err
	}
	return v.SystemVersion, nil
}

type panelSSL struct {
	ID            int    `json:"id"`
	PrimaryDomain string `json:"primaryDomain"`
	Status        string `json:"status"`
	Pem           string `json:"pem"`
	PrivateKey    string `json:"privateKey"`
	Message       string `json:"message"`
}

func (p *panelClient) findSSL(ctx context.Context, ip string) (*panelSSL, error) {
	var page struct {
		Items []panelSSL `json:"items"`
	}
	if err := p.do(ctx, "POST", "/api/v2/websites/ssl/search", map[string]any{"page": 1, "pageSize": 100, "domain": ip}, &page); err != nil {
		return nil, err
	}
	for i := range page.Items {
		if page.Items[i].PrimaryDomain == ip {
			return &page.Items[i], nil
		}
	}
	return nil, nil
}

func (p *panelClient) sslDetail(ctx context.Context, id int) (*panelSSL, error) {
	var v panelSSL
	if err := p.do(ctx, "GET", "/api/v2/websites/ssl/"+strconv.Itoa(id), nil, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// acmeAccount 返回 1Panel 里的第一个 ACME 账户；没有时创建一个（Let's Encrypt，邮箱可为空）。
func (p *panelClient) acmeAccount(ctx context.Context) (int, error) {
	var page struct {
		Items []struct {
			ID int `json:"id"`
		} `json:"items"`
	}
	if err := p.do(ctx, "POST", "/api/v2/websites/acme/search", map[string]any{"page": 1, "pageSize": 10}, &page); err != nil {
		return 0, err
	}
	if len(page.Items) > 0 {
		return page.Items[0].ID, nil
	}
	var created struct {
		ID int `json:"id"`
	}
	if err := p.do(ctx, "POST", "/api/v2/websites/acme", map[string]any{"type": "letsencrypt", "keyType": "P256"}, &created); err != nil {
		return 0, err
	}
	return created.ID, nil
}

// ensureIPCert 找到或申请本机 IP 证书，等到签发完成（最多约 2 分钟），返回证书与私钥。
func (p *panelClient) ensureIPCert(ctx context.Context, ip string) (string, string, error) {
	ssl, err := p.findSSL(ctx, ip)
	if err != nil {
		return "", "", err
	}
	if ssl == nil {
		acct, err := p.acmeAccount(ctx)
		if err != nil {
			return "", "", err
		}
		req := map[string]any{"primaryDomain": ip, "otherDomains": "", "provider": "http", "acmeAccountId": acct, "autoRenew": true, "keyType": "P256",
			"description": "Weir 节点证书（由 Weir 申请，请勿删除）", "apply": true}
		if err = p.do(ctx, "POST", "/api/v2/websites/ssl", req, nil); err != nil {
			return "", "", err
		}
	}
	deadline := time.Now().Add(2 * time.Minute)
	for {
		if ssl, err = p.findSSL(ctx, ip); err != nil {
			return "", "", err
		}
		if ssl != nil {
			if d, err := p.sslDetail(ctx, ssl.ID); err == nil && strings.Contains(d.Pem, "BEGIN CERTIFICATE") && strings.Contains(d.PrivateKey, "PRIVATE KEY") {
				return d.Pem, d.PrivateKey, nil
			} else if d != nil && (d.Status == "applyError" || d.Status == "error") {
				return "", "", fmt.Errorf("1Panel 申请证书失败：%s", d.Message)
			}
		}
		if time.Now().After(deadline) {
			return "", "", errors.New("1Panel 在 2 分钟内没有签发证书，请在 1Panel 的证书页面查看原因")
		}
		select {
		case <-ctx.Done():
			return "", "", ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}

// —— 辅助程序（root）一侧 ——

func (h *Helper) panelPath() string { return filepath.Join(filepath.Dir(h.P.Config), "panel.json") }

func (h *Helper) loadPanel() (PanelConfig, bool) {
	b, err := os.ReadFile(h.panelPath())
	var c PanelConfig
	if err != nil || json.Unmarshal(b, &c) != nil || c.APIKey == "" || c.Port <= 0 {
		return PanelConfig{}, false
	}
	return c, true
}

// panelStatus 返回 1Panel 状态；已配置时顺便测一次连接（结果缓存 10 分钟）。
func (h *Helper) panelStatus(ctx context.Context) contracts.PanelStatus {
	st := contracts.PanelStatus{Detected: panelDetected()}
	cfg, ok := h.loadPanel()
	st.Configured = ok
	if !ok {
		return st
	}
	h.panelMu.Lock()
	defer h.panelMu.Unlock()
	if time.Since(h.panelChecked) < 10*time.Minute {
		st.Version, st.Error = h.panelVersion, h.panelError
		return st
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	v, err := newPanelClient(cfg).version(ctx)
	h.panelChecked, h.panelVersion, h.panelError = time.Now(), v, ""
	if err != nil {
		h.panelError = err.Error()
	}
	st.Version, st.Error = h.panelVersion, h.panelError
	return st
}

// setPanel 保存 1Panel 连接信息前先测试连接；不通时不保存。
func (h *Helper) setPanel(w http.ResponseWriter, r *http.Request) {
	var cfg PanelConfig
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&cfg); err != nil || cfg.Port <= 0 || cfg.Port > 65535 || len(cfg.APIKey) < 8 || len(cfg.APIKey) > 256 {
		writeJSON(w, 400, ApplyResult{Error: "invalid 1Panel settings"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	v, err := newPanelClient(cfg).version(ctx)
	if err != nil {
		writeJSON(w, 422, ApplyResult{Error: err.Error()})
		return
	}
	b, _ := json.Marshal(cfg)
	path := h.panelPath()
	if err = os.WriteFile(path+".new", b, 0600); err == nil {
		err = os.Rename(path+".new", path)
	}
	if err != nil {
		writeJSON(w, 500, ApplyResult{Error: err.Error()})
		return
	}
	h.panelMu.Lock()
	h.panelChecked, h.panelVersion, h.panelError = time.Now(), v, ""
	h.panelMu.Unlock()
	writeJSON(w, 200, map[string]any{"ok": true, "version": v})
}

func (h *Helper) removePanel(w http.ResponseWriter, _ *http.Request) {
	if err := os.Remove(h.panelPath()); err != nil && !os.IsNotExist(err) {
		writeJSON(w, 500, ApplyResult{Error: err.Error()})
		return
	}
	h.panelMu.Lock()
	h.panelChecked = time.Time{}
	h.panelMu.Unlock()
	writeJSON(w, 200, ApplyResult{OK: true})
}

// panelCert 经 1Panel 取得本机 IP 证书并安装为 <id>.crt / <id>.key，返回证书正文、摘要与到期。
func (h *Helper) panelCert(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.URL.Query().Get("id"))
	ip := net.ParseIP(r.URL.Query().Get("ip"))
	if err != nil || id <= 0 || ip == nil {
		writeJSON(w, 400, ApplyResult{Error: "invalid request"})
		return
	}
	cfg, ok := h.loadPanel()
	if !ok {
		writeJSON(w, 409, ApplyResult{Error: "这台 VPS 还没有填写 1Panel API 密钥"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 150*time.Second)
	defer cancel()
	certPEM, keyPEM, err := newPanelClient(cfg).ensureIPCert(ctx, ip.String())
	if err == nil {
		err = h.installCertFiles(id, certPEM, keyPEM)
	}
	if err != nil {
		writeJSON(w, 502, ApplyResult{Error: err.Error()})
		return
	}
	writeJSON(w, 200, panelCertOf(certPEM, keyPEM))
}

func panelCertOf(certPEM, keyPEM string) contracts.PanelCert {
	pc := contracts.PanelCert{CertPEM: certPEM, SHA256: contracts.CertSum(certPEM, keyPEM)}
	if block, _ := pem.Decode([]byte(certPEM)); block != nil {
		if x, err := x509.ParseCertificate(block.Bytes); err == nil {
			pc.NotAfter = x.NotAfter
		}
	}
	return pc
}

// refreshPanelCerts 每 6 小时经 1Panel 重新读取已安装的本机 IP 证书：1Panel 续签后换上新文件。
// 内核会按文件重新加载证书（Xray 定时重读、sing-box 监视文件），不需要重启。
func (h *Helper) refreshPanelCerts(ctx context.Context, certs map[int]string) {
	cfg, ok := h.loadPanel()
	if !ok {
		return
	}
	pc := newPanelClient(cfg)
	for id, ip := range certs {
		ssl, err := pc.findSSL(ctx, ip)
		if err != nil || ssl == nil {
			continue
		}
		d, err := pc.sslDetail(ctx, ssl.ID)
		if err != nil || !strings.Contains(d.Pem, "BEGIN CERTIFICATE") {
			continue
		}
		if h.certs()[strconv.Itoa(id)] != contracts.CertSum(d.Pem, d.PrivateKey) {
			_ = h.installCertFiles(id, d.Pem, d.PrivateKey)
		}
	}
}

// panelCertsReport 是心跳里的本机 1Panel 证书状态（不含私钥）。
func (h *Helper) panelCertsReport(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Certs map[int]string `json:"certs"` // 证书 ID → IP
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req)
	h.panelMu.Lock()
	due := time.Since(h.panelRefreshed) > 6*time.Hour
	if due {
		h.panelRefreshed = time.Now()
	}
	h.panelMu.Unlock()
	if due && len(req.Certs) > 0 {
		ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
		h.refreshPanelCerts(ctx, req.Certs)
		cancel()
	}
	out := map[string]contracts.PanelCert{}
	for id := range req.Certs {
		path := filepath.Join(h.P.CertsDir, strconv.Itoa(id))
		cert, err1 := os.ReadFile(path + ".crt")
		key, err2 := os.ReadFile(path + ".key")
		if err1 == nil && err2 == nil {
			pc := panelCertOf(string(cert), string(key))
			pc.CertPEM = ""
			out[strconv.Itoa(id)] = pc
		}
	}
	st := h.panelStatus(r.Context())
	writeJSON(w, 200, map[string]any{"status": st, "certs": out})
}

// —— ACME HTTP-01：Gate 临时在 80 端口应答 ——

var acmeToken = func(s string) bool {
	if len(s) < 16 || len(s) > 128 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// acmePort 是 HTTP-01 应答端口（测试中替换）。
var acmePort = 80

// acmeHTTP01 在 80 端口只回应 /.well-known/acme-challenge/<token>，最多开 seconds 秒（上限 180）。
// 80 端口被其他程序占用时直接失败，不抢占。
func (h *Helper) acmeHTTP01(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token   string `json:"token"`
		KeyAuth string `json:"key_auth"`
		Seconds int    `json:"seconds"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil || !acmeToken(req.Token) || !strings.HasPrefix(req.KeyAuth, req.Token+".") || len(req.KeyAuth) > 256 {
		writeJSON(w, 400, ApplyResult{Error: "invalid challenge"})
		return
	}
	if req.Seconds <= 0 || req.Seconds > 180 {
		req.Seconds = 120
	}
	ln, err := net.Listen("tcp", ":"+strconv.Itoa(acmePort))
	if err != nil {
		writeJSON(w, 409, ApplyResult{Error: "80 端口被其他程序占用，无法应答验证"})
		return
	}
	path := "/.well-known/acme-challenge/" + req.Token
	srv := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != path {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, req.KeyAuth)
	})}
	go func() { _ = srv.Serve(ln) }()
	go func() {
		time.Sleep(time.Duration(req.Seconds) * time.Second)
		_ = srv.Close()
	}()
	writeJSON(w, 200, ApplyResult{OK: true})
}

// installCertFiles 原子写入 <id>.crt / <id>.key，属组设为内核运行账号。
func (h *Helper) installCertFiles(id int, certPEM, keyPEM string) error {
	if err := os.MkdirAll(h.P.CertsDir, 0750); err != nil {
		return err
	}
	for ext, data := range map[string]string{"crt": certPEM, "key": keyPEM} {
		path := filepath.Join(h.P.CertsDir, fmt.Sprintf("%d.%s", id, ext))
		if err := os.WriteFile(path+".new", []byte(data), 0640); err != nil {
			return err
		}
		h.shareWithAgent(path + ".new")
		if err := os.Rename(path+".new", path); err != nil {
			return err
		}
	}
	return nil
}

// validPanelIP 只接受公网 IP（1Panel 证书是这台 VPS 公网地址的证书）。
func validPanelIP(raw string) bool {
	ip := net.ParseIP(raw)
	return ip != nil && publicIP(ip)
}
