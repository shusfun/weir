// Package ai 对接大模型生成配置草稿。模型只出草稿：系统负责校验、实测，管理员看过差异后确认才生效；
// 节点密码、UUID、密钥、订阅令牌在发出前全部替换为占位符，模型故障不影响任何基础功能。
package ai

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
	"strings"
	"time"
)

// Provider 是一个模型服务：协议（anthropic / openai / laya）、Base URL、模型名与 API Key。
type Provider struct {
	Protocol string `json:"protocol"`
	BaseURL  string `json:"base_url"`
	Model    string `json:"model"`
	Key      string `json:"-"`
}

var Protocols = []string{"anthropic", "openai", "laya"}

// Validate 只接受 HTTPS；http 仅限本机回环（本地网关与测试）。
func (p Provider) Validate() error {
	switch p.Protocol {
	case "anthropic", "openai", "laya":
	default:
		return errors.New("unknown_protocol")
	}
	u, err := url.Parse(strings.TrimSpace(p.BaseURL))
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("invalid_base_url")
	}
	if u.Scheme != "https" {
		ip := net.ParseIP(u.Hostname())
		if u.Scheme != "http" || !(u.Hostname() == "localhost" || ip != nil && ip.IsLoopback()) {
			return errors.New("https_required")
		}
	}
	if strings.TrimSpace(p.Model) == "" || len(p.Model) > 200 || strings.ContainsAny(p.Model, "\r\n") {
		return errors.New("invalid_model")
	}
	if p.Key == "" || len(p.Key) > 8192 {
		return errors.New("key_required")
	}
	return nil
}

// endpoint 拼出各协议的接口地址：Base URL 已含版本路径时不重复添加。
func (p Provider) endpoint() string {
	base := strings.TrimRight(strings.TrimSpace(p.BaseURL), "/")
	add := func(version, path string) string {
		if strings.HasSuffix(base, path) {
			return base
		}
		if strings.HasSuffix(base, version) {
			return base + strings.TrimPrefix(path, version)
		}
		return base + path
	}
	switch p.Protocol {
	case "anthropic":
		return add("/v1", "/v1/messages")
	case "openai":
		return add("/v1", "/v1/chat/completions")
	default:
		return add("/v1", "/v1/systemone")
	}
}

// Client 发送请求；HTTP 可替换（测试）。
type Client struct {
	HTTP *http.Client
}

func (c Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 90 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

// Message 是一轮对话。
type Message struct {
	Role    string `json:"role"` // user / assistant
	Content string `json:"content"`
}

// Complete 发送系统提示与对话，返回模型的文本回答。
func (c Client) Complete(ctx context.Context, p Provider, system string, msgs []Message) (string, error) {
	var body any
	header := map[string]string{}
	switch p.Protocol {
	case "anthropic":
		body = map[string]any{"model": p.Model, "max_tokens": 4096, "system": system, "messages": msgs}
		header["x-api-key"], header["anthropic-version"] = p.Key, "2023-06-01"
	case "openai":
		all := append([]Message{{Role: "system", Content: system}}, msgs...)
		body = map[string]any{"model": p.Model, "messages": all}
		header["Authorization"] = "Bearer " + p.Key
	case "laya":
		// Jev 原生协议：状态 + 问题；这里只问一个文本题，答案即草稿。
		var convo strings.Builder
		for _, m := range msgs {
			fmt.Fprintf(&convo, "[%s]\n%s\n\n", m.Role, m.Content)
		}
		body = map[string]any{"model": p.Model, "state": map[string]any{"conversation": convo.String()},
			"questions": map[string]any{"draft": map[string]any{"type": "text", "instructions": system}}}
		header["Authorization"] = "Bearer " + p.Key
	default:
		return "", errors.New("unknown_protocol")
	}
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST", p.endpoint(), bytes.NewReader(b))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return "", errors.New("model_unreachable")
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", errors.New("model_unreachable")
	}
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("model_http_%d", resp.StatusCode)
	}
	return extract(p.Protocol, raw)
}

func extract(protocol string, raw []byte) (string, error) {
	var doc map[string]any
	if json.Unmarshal(raw, &doc) != nil {
		return "", errors.New("model_invalid_response")
	}
	var text string
	switch protocol {
	case "anthropic":
		parts, _ := doc["content"].([]any)
		for _, p := range parts {
			if m, ok := p.(map[string]any); ok && m["type"] == "text" {
				s, _ := m["text"].(string)
				text += s
			}
		}
	case "openai":
		choices, _ := doc["choices"].([]any)
		if len(choices) > 0 {
			if m, ok := choices[0].(map[string]any); ok {
				if msg, ok := m["message"].(map[string]any); ok {
					text, _ = msg["content"].(string)
				}
			}
		}
	case "laya":
		answers, _ := doc["answers"].(map[string]any)
		if d, ok := answers["draft"].(map[string]any); ok {
			text, _ = d["text"].(string)
		}
	}
	if strings.TrimSpace(text) == "" {
		return "", errors.New("model_empty_response")
	}
	return text, nil
}
