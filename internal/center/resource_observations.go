package center

import (
	"context"
	"os"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shusfun/weir/internal/hoststats"
	"github.com/shusfun/weir/internal/store/ent/sample"
	"github.com/shusfun/weir/packages/contracts"
)

type resourceState struct {
	sync.RWMutex
	host *contracts.HostSample
}

func (s *Server) observeResources(ctx context.Context) {
	var sampler hoststats.Sampler
	sampler.Run(ctx, s.dataDir(), func() map[string]int { return map[string]int{"weir-center": os.Getpid()} }, func(h *contracts.HostSample) {
		h.ObservationID = randomHex(16)
		s.resources.Lock()
		s.resources.host = h
		s.resources.Unlock()
		s.state.RLock()
		defer s.state.RUnlock()
		if !s.Config.Installed || s.Store == nil {
			return
		}
		runCtx := context.WithValue(ctx, httpTraceKey{}, httpTrace{ID: h.ObservationID, Span: randomHex(8), Flags: "01"})
		s.tasks.note("resource-samples", s.recordResourceHost(runCtx, sample.ScopeCenter, 1, h, time.Now()))
	})
}

func (s *Server) centerResources(c *gin.Context) {
	s.resources.RLock()
	h := s.resources.host
	s.resources.RUnlock()
	if h != nil {
		c.Set("observation_id", h.ObservationID)
	}
	c.JSON(200, gin.H{"host": h})
}

func (s *Server) centerHistory(c *gin.Context) {
	points, err := s.history(c.Request.Context(), sample.ScopeCenter, 1, atoiOr(c.DefaultQuery("hours", "1")))
	if err != nil {
		failInternal(c, 503, "query_failed", err)
		return
	}
	c.JSON(200, gin.H{"points": points})
}
