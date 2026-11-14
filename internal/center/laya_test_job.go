package center

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
)

type layaTestRevisionKey struct{}
type layaTestProfileKey struct{}

// Jobs are coalesced by configuration revision. Credentials
// never appear in the job identity, and stale results cannot verify new settings.
func (s *Server) testLayaAsync(c *gin.Context) {
	configured, revision, err := s.readLayaSettings(c.Request.Context())
	if err != nil {
		failInternal(c, 503, "laya_configuration_unavailable", err)
		return
	}
	profile, err := s.aiProfile(c.Request.Context())
	if err != nil {
		failInternal(c, 503, "laya_configuration_unavailable", err)
		return
	}
	inner, recorder := detached(c, nil)
	trace := taskTrace(c.Request.Context())
	id := fmt.Sprintf("laya-test:%x", sha256.Sum256([]byte(revision+":"+profile.Revision)))
	deadline := time.Now().Add(time.Duration(layaWarmupSeconds(configured)) * time.Second)
	job := s.jobs.StartWith(jobSpec{ID: id, Kind: "action", Title: "测试 Laya", Owner: 0, Trace: trace, CancelMsg: "测试已取消，规则防线继续运行"}, func(ctx context.Context, run JobRun) (map[string]any, error) {
		ctx, cancel := context.WithDeadline(ctx, deadline)
		defer cancel()
		ctx = context.WithValue(withTrace(ctx, trace), jobCtxKey{}, &run)
		ctx = context.WithValue(ctx, layaTestRevisionKey{}, revision)
		ctx = context.WithValue(ctx, layaTestProfileKey{}, profile.Revision)
		inner.Request = inner.Request.WithContext(ctx)
		s.testLaya(inner)
		inner.Writer.WriteHeaderNow()
		var body map[string]any
		_ = json.Unmarshal(recorder.Body.Bytes(), &body)
		result := map[string]any{"status": recorder.Code, "body": body}
		if recorder.Code >= 400 {
			return result, handlerError(recorder.Code, body)
		}
		return result, nil
	})
	c.JSON(202, gin.H{"job": job})
}
