package gate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/shusfun/weir/packages/contracts"
)

// helperClient 是 Agent 访问本机辅助程序的客户端（经 Unix 套接字）。
type helperClient struct{ http *http.Client }

func newHelperClient(socket string) *helperClient {
	tr := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	return &helperClient{http: &http.Client{Transport: tr, Timeout: 3 * time.Minute}}
}

func (h *helperClient) do(ctx context.Context, method, path string, body io.Reader, header map[string]string, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, "http://helper"+path, body)
	if err != nil {
		return err
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := h.http.Do(req)
	if err != nil {
		return errors.New("helper unavailable: " + err.Error())
	}
	defer resp.Body.Close()
	if out != nil {
		if err = json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(out); err != nil {
			return err
		}
	}
	if resp.StatusCode >= 300 {
		if r, ok := out.(*ApplyResult); ok && r.Error != "" {
			return errors.New(r.Error)
		}
		return statusError{resp.StatusCode}
	}
	return nil
}

func (h *helperClient) Status(ctx context.Context) (HelperStatus, error) {
	var st HelperStatus
	return st, h.do(ctx, "GET", "/status", nil, nil, &st)
}

// SetCenter 让辅助程序（root）把设备配置里的中心地址改为 center。
func (h *helperClient) SetCenter(ctx context.Context, center string) error {
	body, _ := json.Marshal(map[string]string{"center": center})
	var res ApplyResult
	return h.do(ctx, "POST", "/center", bytes.NewReader(body), nil, &res)
}

func (h *helperClient) InstallCert(ctx context.Context, id int64, certPEM, keyPEM string) error {
	body, _ := json.Marshal(map[string]string{"cert_pem": certPEM, "key_pem": keyPEM})
	var res ApplyResult
	return h.do(ctx, "POST", fmt.Sprintf("/cert?id=%d", id), bytes.NewReader(body), nil, &res)
}

func (h *helperClient) InstallKernel(ctx context.Context, name, version, sum string, data []byte) error {
	var res ApplyResult
	return h.do(ctx, "POST", "/kernel", bytes.NewReader(data), map[string]string{"X-Kernel": name, "X-Version": version, "X-SHA256": sum}, &res)
}

// Apply 返回辅助程序的结果；校验 / 健康检查失败时 OK 为 false 并带原因，不视为通信错误。
func (h *helperClient) Apply(ctx context.Context, req ApplyRequest) (ApplyResult, error) {
	b, err := json.Marshal(req)
	if err != nil {
		return ApplyResult{}, err
	}
	var res ApplyResult
	err = h.do(ctx, "POST", "/apply", bytes.NewReader(b), map[string]string{"Content-Type": "application/json"}, &res)
	if res.Error != "" || res.ConflictPort != 0 {
		return res, nil
	}
	return res, err
}

func (h *helperClient) Restart(ctx context.Context, name string) error {
	var res ApplyResult
	return h.do(ctx, "POST", "/restart?name="+name, nil, nil, &res)
}

func (h *helperClient) Diagnostics(ctx context.Context, name string) (map[string]any, error) {
	var out map[string]any
	return out, h.do(ctx, "GET", "/diagnostics?name="+name, nil, nil, &out)
}

func (h *helperClient) Release(ctx context.Context, version, sum string, data []byte) error {
	var res ApplyResult
	return h.do(ctx, "POST", "/release", bytes.NewReader(data), map[string]string{"X-Version": version, "X-SHA256": sum}, &res)
}

func (h *helperClient) Uninstall(ctx context.Context) error {
	var res ApplyResult
	return h.do(ctx, "POST", "/uninstall", nil, nil, &res)
}

// call 发送 JSON 请求；失败时辅助程序返回 {"error": "..."}，转成错误。
func (h *helperClient) call(ctx context.Context, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	var raw json.RawMessage
	err := h.do(ctx, method, path, rd, map[string]string{"Content-Type": "application/json"}, &raw)
	if err != nil {
		var e ApplyResult
		if json.Unmarshal(raw, &e) == nil && e.Error != "" {
			return errors.New(e.Error)
		}
		return err
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}

// SetPanel 让辅助程序测试并保存 1Panel 连接信息（密钥只存在本机，root 可读）。
func (h *helperClient) SetPanel(ctx context.Context, cfg PanelConfig) (string, error) {
	var out struct {
		Version string `json:"version"`
	}
	return out.Version, h.call(ctx, "POST", "/panel", cfg, &out)
}

func (h *helperClient) RemovePanel(ctx context.Context) error {
	return h.call(ctx, "DELETE", "/panel", nil, nil)
}

// PanelCert 经本机 1Panel 取得 IP 证书并安装为证书 id。
func (h *helperClient) PanelCert(ctx context.Context, id int64, ip string) (contracts.PanelCert, error) {
	var out contracts.PanelCert
	return out, h.call(ctx, "POST", fmt.Sprintf("/panel/cert?id=%d&ip=%s", id, url.QueryEscape(ip)), nil, &out)
}

// PanelReport 返回 1Panel 状态与本机 1Panel 证书的摘要（必要时先经 1Panel 刷新续签后的证书）。
func (h *helperClient) PanelReport(ctx context.Context, certs map[int64]string) (contracts.PanelStatus, map[string]contracts.PanelCert, error) {
	var out struct {
		Status contracts.PanelStatus          `json:"status"`
		Certs  map[string]contracts.PanelCert `json:"certs"`
	}
	err := h.call(ctx, "POST", "/panel/report", map[string]any{"certs": certs}, &out)
	return out.Status, out.Certs, err
}

// ACMEHTTP01 让辅助程序在 80 端口临时应答一次 HTTP-01 验证。
func (h *helperClient) ACMEHTTP01(ctx context.Context, token, keyAuth string, seconds int) error {
	return h.call(ctx, "POST", "/acme/http01", map[string]any{"token": token, "key_auth": keyAuth, "seconds": seconds}, nil)
}

// HelperHealth 供安装器确认辅助程序已在本机套接字上应答。
func HelperHealth(ctx context.Context, p Paths) error {
	_, err := newHelperClient(p.Socket).Status(ctx)
	return err
}
