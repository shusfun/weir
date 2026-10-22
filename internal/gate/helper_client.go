package gate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"time"
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

// HelperHealth 供安装器确认辅助程序已在本机套接字上应答。
func HelperHealth(ctx context.Context, p Paths) error {
	_, err := newHelperClient(p.Socket).Status(ctx)
	return err
}
