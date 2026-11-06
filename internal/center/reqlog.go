package center

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// failInternal 返回固定错误码给页面，真实原因交给请求日志（页面上不显示内部错误）。
func failInternal(c *gin.Context, status int, code string, err error) {
	if err != nil {
		_ = c.Error(err)
	}
	failure(c, status, code)
}

// requestLog 给每个请求一个 X-Request-Id，并只记录出问题的请求：5xx 带真实原因记 ERROR，
// 页面需要排查的 4xx 只记错误码。路径记路由模板而不是原始地址，订阅令牌与查询参数不进日志。
func requestLog(c *gin.Context) {
	id := requestID()
	trace := newHTTPTrace(c.GetHeader("traceparent"))
	c.Set("request_id", id)
	c.Set("trace_id", trace.ID)
	c.Set("span_id", trace.Span)
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), httpTraceKey{}, trace))
	c.Header("X-Request-Id", id)
	c.Header("X-Trace-Id", trace.ID)
	c.Header("traceparent", trace.parent())
	start := time.Now()
	c.Next()
	status, code := c.Writer.Status(), c.GetString("error_code")
	if !logged(status, code) && !customTraceRoute(c.FullPath()) {
		return
	}
	route := c.FullPath()
	if route == "" {
		route = "(未匹配)"
	}
	attrs := []any{"id", id, "trace", trace.ID, "span", trace.Span, "method", c.Request.Method, "route", route, "status", status, "ms", time.Since(start).Milliseconds()}
	if observation := c.GetString("observation_id"); observation != "" {
		attrs = append(attrs, "observation", observation)
	}
	if code != "" {
		attrs = append(attrs, "code", code)
	}
	if uid := c.GetInt("user_id"); uid != 0 {
		attrs = append(attrs, "user", uid)
	}
	if status >= 500 {
		if len(c.Errors) > 0 {
			attrs = append(attrs, "err", redact(strings.Join(c.Errors.Errors(), "; ")))
		}
		slog.Error("request", attrs...)
		return
	}
	if status >= 400 {
		slog.Warn("request", attrs...)
	} else {
		slog.Info("request", attrs...)
	}
}

type httpTraceKey struct{}
type httpTrace struct{ ID, Span, Flags string }

func (t httpTrace) parent() string { return "00-" + t.ID + "-" + t.Span + "-" + t.Flags }

// Only version 00 is accepted here; malformed or zero identifiers start a new trace.
func newHTTPTrace(parent string) httpTrace {
	t := httpTrace{ID: randomHex(16), Span: randomHex(8), Flags: "01"}
	p := strings.Split(parent, "-")
	if len(p) == 4 && p[0] == "00" && validTraceHex(p[1], 32) && validTraceHex(p[2], 16) && len(p[3]) == 2 {
		if _, err := hex.DecodeString(p[3]); err == nil && strings.ToLower(parent) == parent && validHTTPTraceFlags(p[3]) {
			t.ID, t.Flags = p[1], p[3]
		}
	}
	return t
}

func validHTTPTraceFlags(s string) bool {
	b, _ := hex.DecodeString(s)
	return len(b) == 1 && b[0]&0xfe == 0
}

func validTraceHex(s string, n int) bool {
	if len(s) != n || s == strings.Repeat("0", n) {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

// Successful records are limited to this batch's operations; routes never contain tokens.
func customTraceRoute(route string) bool {
	return strings.Contains(route, "/center/resources") || route == "/api/v1/admin/settings/logo" || route == "/api/v1/agent/heartbeat" || strings.Contains(route, "/proxy/") || strings.Contains(route, "/exits") || strings.Contains(route, "/nodes") || strings.Contains(route, "/chains") || strings.Contains(route, "/routes") || strings.Contains(route, "/users") || route == "/api/v1/setup/install" || route == "/api/v1/auth/reset" || route == "/api/v1/auth/register" || route == "/api/v1/auth/verify" || route == "/api/v1/security/password" || route == "/sub/:token/rules" || route == "/sub/:token" || route == "/api/v1/agent/pair"
}

// logged 排除正常流程里大量出现的状态：未登录（401）、找不到（404）、被封禁来源的重复访问、未初始化。
func logged(status int, code string) bool {
	switch {
	case code == "setup_required" || code == "source_blocked":
		return false
	case status >= 500:
		return true
	case status == 400, status == 403, status == 409, status == 422, status == 429:
		return true
	}
	return false
}

func requestID() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// urlRe 匹配错误里的地址：订阅源、WebDAV、代理地址的路径、查询参数和账号密码里可能带着令牌。
var urlRe = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://)(?:[^@/\s"']*@)?([^/\s"'?#]+)[^\s"']*`)

// redact 只保留地址的协议和主机，用于写日志的错误。
func redact(msg string) string {
	return urlRe.ReplaceAllString(msg, "$1$2/…")
}
