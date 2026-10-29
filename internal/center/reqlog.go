package center

import (
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
	c.Set("request_id", id)
	c.Header("X-Request-Id", id)
	start := time.Now()
	c.Next()
	status, code := c.Writer.Status(), c.GetString("error_code")
	if !logged(status, code) {
		return
	}
	route := c.FullPath()
	if route == "" {
		route = "(未匹配)"
	}
	attrs := []any{"id", id, "method", c.Request.Method, "route", route, "status", status, "ms", time.Since(start).Milliseconds()}
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
	slog.Warn("request", attrs...)
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

// taskLog 记录周期任务的失败：同一项任务原因不变时只记第一次，原因变了或恢复了再记一次，每分钟运行也不刷屏。
type taskLog map[string]string

func (t taskLog) note(task string, err error) {
	prev := t[task]
	switch {
	case err == nil && prev != "":
		delete(t, task)
		slog.Info("task recovered", "task", task)
	case err != nil:
		if msg := redact(err.Error()); msg != prev {
			t[task] = msg
			slog.Error("task failed", "task", task, "err", msg)
		}
	}
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
