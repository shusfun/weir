package center

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// 可能超过反代时限（nginx 默认 60 秒）的操作在后台任务里执行：立即续签、建节点（签证书、实测伪装站点与候选节点）、
// 链式代理与类型出口的实测、确认多步草稿、1Panel 测试并保存、立即备份。处理函数不变（同一份校验、写入与审计），
// 只是不再跟着请求连接：浏览器断开、反代超时，操作照常做完，不会停在一半。
// 2 秒内做完的照常同步返回（字段校验等），否则返回 202 和任务，页面经事件流跟着看步骤与结果；
// 任务的结果是处理函数原本的响应（状态码与正文），页面按同步返回时的同一套规则显示成功或失败。

// actionInline 是同步返回前最多等多久（测试中调短）。
var actionInline = 2 * time.Second

type jobCtxKey struct{}
type nestedKey struct{}
type stepPrefixKey struct{}

// jobOf 返回 ctx 所在的后台任务；不在任务里时为空，下面的函数都什么也不做。
func jobOf(ctx context.Context) *JobRun {
	r, _ := ctx.Value(jobCtxKey{}).(*JobRun)
	return r
}

// nested：在进程内调用（草稿的步骤、试运行）里，标题与提交由外层操作决定。
func nested(ctx context.Context) bool { return ctx.Value(nestedKey{}) != nil }

// jobStep 报告当前步骤与最多等多久，页面写「正在<步骤> · 已等 N 秒，最多 M 秒」。
func jobStep(ctx context.Context, step string, max time.Duration) {
	if r := jobOf(ctx); r != nil {
		prefix, _ := ctx.Value(stepPrefixKey{}).(string)
		r.StepFor(prefix+step, max)
	}
}

// jobPrefix 给里层报告的步骤加上外层的位置，如「第 2/5 个节点（香港）· 正在经出口实测」。
func jobPrefix(ctx context.Context, prefix string) context.Context {
	if jobOf(ctx) == nil {
		return ctx
	}
	outer, _ := ctx.Value(stepPrefixKey{}).(string)
	return context.WithValue(ctx, stepPrefixKey{}, outer+prefix)
}

// jobTitle 把任务名称换成带对象的说法（如「续签证书 a.example.com」）。
func jobTitle(ctx context.Context, title string) {
	if r := jobOf(ctx); r != nil && !nested(ctx) {
		r.SetTitle(title)
	}
}

// jobCommitCtx 在第一次写入之前调用：之后不再接受取消。返回 false 表示已被取消。
func jobCommitCtx(ctx context.Context) bool {
	r := jobOf(ctx)
	return r == nil || nested(ctx) || r.Commit()
}

// jobCommit 同 jobCommitCtx；已被取消时返回 409，调用方直接返回。
func jobCommit(c *gin.Context) bool {
	if jobCommitCtx(c.Request.Context()) {
		return true
	}
	failDetail(c, 409, "job_cancelled", "已取消，没有保存任何改动", nil)
	return false
}

// asJob 把处理函数包成后台任务。title 是开始时的名称，处理函数可以用 jobTitle 换成带对象的说法；
// cancelMsg 是取消后显示的说明。
func (s *Server) asJob(title, cancelMsg string, h gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20))
		if err != nil {
			failure(c, 400, "invalid_request")
			return
		}
		inner, w := detached(c, body)
		route, trace := c.FullPath(), c.GetString("request_id")
		id := "action:" + randomHex(8)
		job := s.jobs.StartWith(jobSpec{ID: id, Kind: "action", Title: title, Owner: c.GetInt("user_id"), Trace: trace, Hidden: true, CancelMsg: cancelMsg},
			func(ctx context.Context, run JobRun) (map[string]any, error) {
				inner.Request = inner.Request.WithContext(context.WithValue(withTrace(ctx, trace), jobCtxKey{}, &run))
				h(inner)
				inner.Writer.WriteHeaderNow()
				var out map[string]any
				_ = json.Unmarshal(w.Body.Bytes(), &out)
				res := map[string]any{"status": w.Code, "body": out}
				if w.Code >= 500 {
					attrs := []any{"id", trace, "job", id, "route", route, "status", w.Code, "code", inner.GetString("error_code")}
					if len(inner.Errors) > 0 {
						attrs = append(attrs, "err", redact(strings.Join(inner.Errors.Errors(), "; ")))
					}
					slog.Error("job", attrs...)
				}
				if w.Code >= 400 {
					return res, handlerError(w.Code, out)
				}
				return res, nil
			})
		wait, cancel := context.WithTimeout(c.Request.Context(), actionInline)
		j, _ := s.jobs.Wait(wait, job.ID)
		cancel()
		if j.Status != jobRunning {
			// 做完了：像原来一样同步返回，请求日志照常记下错误码与真实原因
			s.jobs.Forget(job.ID)
			if code := inner.GetString("error_code"); code != "" {
				c.Set("error_code", code)
			}
			c.Errors = append(c.Errors, inner.Errors...)
			if j.Result == nil {
				// 处理函数 panic 了（已记在中心日志里）：没有可以转交的响应
				failInternal(c, 500, "internal_error", errors.New(j.Error))
				return
			}
			c.Data(w.Code, w.Header().Get("Content-Type"), w.Body.Bytes())
			return
		}
		j, _ = s.jobs.Publish(job.ID)
		j.Logs = nil
		c.JSON(202, gin.H{"job": j})
	}
}

// detached 复制一份不跟着请求连接的 gin 上下文：身份、路由参数、请求头与来源地址不变，请求体已经读出。
// 在请求处理函数返回前调用（gin 会回收原上下文）。
func detached(c *gin.Context, body []byte) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	inner, eng := gin.CreateTestContext(w)
	_ = eng.SetTrustedProxies([]string{"127.0.0.1", "::1"})
	req := c.Request.Clone(context.Background())
	req.Body = io.NopCloser(bytes.NewReader(body))
	req.ContentLength = int64(len(body))
	inner.Request, inner.Params = req, append(gin.Params(nil), c.Params...)
	for k, v := range c.Keys {
		inner.Set(k, v)
	}
	return inner, w
}
