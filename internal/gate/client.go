package gate

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"
)

// centerClient 封装与中心的通信：用设备私钥签名一次性挑战换取 15 分钟访问令牌，过期或 401 时自动重新认证。
type centerClient struct {
	cfg   Config
	http  *http.Client
	long  *http.Client // 长轮询与下载使用更长超时
	mu    sync.Mutex
	token string
	until time.Time
}

func newCenterClient(cfg Config) (*centerClient, error) {
	short, err := httpClient(cfg, 30*time.Second)
	if err != nil {
		return nil, err
	}
	long, err := httpClient(cfg, 10*time.Minute)
	if err != nil {
		return nil, err
	}
	return &centerClient{cfg: cfg, http: short, long: long}, nil
}

func (c *centerClient) raw(ctx context.Context, client *http.Client, method, path string, body any, auth bool) (*http.Response, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.cfg.Center+"/api/v1"+path, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if auth {
		token, err := c.bearer(ctx)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == 401 && auth {
		c.mu.Lock()
		c.until = time.Time{}
		c.mu.Unlock()
	}
	return resp, nil
}

// call 发送请求并把 2xx 响应解码到 out；204 视为成功但无内容。
func (c *centerClient) call(ctx context.Context, method, path string, body, out any) (int, error) {
	client := c.http
	if path == "/agent/config" || len(path) > 13 && path[:14] == "/agent/config?" || path == "/agent/actions" {
		client = c.long
	}
	resp, err := c.raw(ctx, client, method, path, body, true)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var e struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&e)
		if e.Error.Message != "" {
			return resp.StatusCode, errors.New(e.Error.Message)
		}
		return resp.StatusCode, statusError{resp.StatusCode}
	}
	if out != nil && resp.StatusCode != 204 {
		return resp.StatusCode, json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(out)
	}
	return resp.StatusCode, nil
}

// download 下载内核或程序，返回内容与中心给出的摘要头。大小上限 256 MiB。
func (c *centerClient) download(ctx context.Context, path, sumHeader string) ([]byte, string, error) {
	resp, err := c.raw(ctx, c.long, "GET", path, nil, true)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, "", statusError{resp.StatusCode}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 256<<20+1))
	if err != nil || len(data) > 256<<20 {
		return nil, "", errors.New("download too large or interrupted")
	}
	return data, resp.Header.Get(sumHeader), nil
}

func (c *centerClient) bearer(ctx context.Context) (string, error) {
	c.mu.Lock()
	if time.Now().Before(c.until) {
		t := c.token
		c.mu.Unlock()
		return t, nil
	}
	c.mu.Unlock()
	priv, err := base64.StdEncoding.DecodeString(c.cfg.PrivateKey)
	if err != nil || len(priv) != ed25519.PrivateKeySize {
		return "", errors.New("invalid device key")
	}
	var ch struct {
		Challenge string `json:"challenge"`
	}
	if err = c.plain(ctx, "/agent/challenge", map[string]int64{"gate_id": c.cfg.GateID}, &ch); err != nil {
		return "", err
	}
	sig := base64.StdEncoding.EncodeToString(ed25519.Sign(ed25519.PrivateKey(priv), []byte(ch.Challenge)))
	var res struct {
		Token string `json:"token"`
	}
	if err = c.plain(ctx, "/agent/auth", map[string]any{"gate_id": c.cfg.GateID, "challenge": ch.Challenge, "signature": sig}, &res); err != nil {
		return "", err
	}
	c.mu.Lock()
	c.token, c.until = res.Token, time.Now().Add(14*time.Minute)
	c.mu.Unlock()
	return res.Token, nil
}

func (c *centerClient) plain(ctx context.Context, path string, body, out any) error {
	resp, err := c.raw(ctx, c.http, "POST", path, body, false)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return statusError{resp.StatusCode}
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(out)
}
