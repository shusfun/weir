package center

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shusfun/weir/internal/buildinfo"
	"github.com/shusfun/weir/internal/ghrelease"
	"github.com/shusfun/weir/internal/selfupdate"
)

const updateCheckInterval = 15 * time.Minute
const updateCheckKey = "update-check:snapshot"

type updateSnapshot struct {
	Releases    []weirRelease `json:"releases"`
	CheckedAt   time.Time     `json:"checked_at"`
	SucceededAt time.Time     `json:"succeeded_at"`
	NextCheckAt time.Time     `json:"next_check_at"`
	Checking    bool          `json:"checking"`
	Stale       bool          `json:"stale"`
	Error       string        `json:"error,omitempty"`
	Trace       string        `json:"trace,omitempty"`
}

type updateChecker struct {
	sync.Mutex
	once     sync.Once
	ctx      context.Context
	done     chan struct{}
	snapshot updateSnapshot
	cancel   context.CancelFunc
	stopped  chan struct{}
}

func (s *Server) startUpdateChecks(ctx context.Context, interval time.Duration) {
	s.updates.once.Do(func() {
		s.updates.Lock()
		ctx, s.updates.cancel = context.WithCancel(ctx)
		s.updates.ctx = ctx
		s.updates.stopped = make(chan struct{})
		s.updates.Unlock()
		go func() {
			defer close(s.updates.stopped)
			tick := time.NewTicker(interval)
			defer tick.Stop()
			check := func() {
				s.updates.Lock()
				s.updates.snapshot.NextCheckAt = time.Now().UTC().Add(interval)
				s.updates.Unlock()
				_, _ = s.beginUpdateCheck("", "scheduled")
			}
			check()
			for {
				select {
				case <-ctx.Done():
					return
				case <-tick.C:
					check()
				}
			}
		}()
	})
}

// beginUpdateCheck joins the existing operation; HTTP cancellation never owns scheduled work.
func (s *Server) beginUpdateCheck(trace, source string) (<-chan struct{}, error) {
	u := &s.updates
	u.Lock()
	defer u.Unlock()
	if u.ctx == nil || u.ctx.Err() != nil {
		return nil, errors.New("版本检查服务未运行")
	}
	if u.done != nil {
		return u.done, nil
	}
	if trace == "" {
		trace = taskTrace(u.ctx)
	}
	u.done = make(chan struct{})
	u.snapshot.Checking = true
	u.snapshot.CheckedAt = time.Now().UTC()
	u.snapshot.Trace = trace
	done := u.done
	go s.checkUpdates(withTrace(u.ctx, trace), source, done)
	return done, nil
}

func (s *Server) checkUpdates(parent context.Context, source string, done chan struct{}) {
	ctx, cancel := context.WithTimeout(parent, 45*time.Second)
	defer cancel()
	started := time.Now()
	trace := traceOf(ctx)
	slog.Info("update.check started", "trace", trace, "source", source, "code.function.name", "center.checkUpdates")
	s.state.RLock()
	db := s.Store
	s.state.RUnlock()
	var list []weirRelease
	var err error
	finish := operationStage(ctx, "update.check", "source", source)
	defer func() { finish(err) }()
	if db == nil {
		err = errors.New("中心尚未初始化")
	} else {
		// Storage I/O never holds the snapshot mutex used by progress polling.
		if s.updateCheckSnapshot().SucceededAt.IsZero() {
			if raw, ok, readErr := db.KV.Get(ctx, updateCheckKey); readErr == nil && ok {
				var saved updateSnapshot
				if json.Unmarshal([]byte(raw), &saved) == nil {
					s.updates.Lock()
					s.updates.snapshot.Releases = saved.Releases
					s.updates.snapshot.SucceededAt = saved.SucceededAt
					s.updates.snapshot.Error = saved.Error
					s.updates.Unlock()
				}
			}
		}
		list, err = s.queryUpdateReleases(ctx)
	}
	result := s.updateCheckSnapshot()
	result.Checking = false
	if err != nil {
		result.Error = redact(err.Error())
	} else {
		result.Releases = list
		result.SucceededAt = time.Now().UTC()
		result.Error = ""
	}
	result.Stale = err != nil
	if db != nil && ctx.Err() == nil {
		raw, _ := json.Marshal(result)
		if saveErr := db.KV.Set(ctx, updateCheckKey, string(raw), 0); saveErr != nil {
			result.Error = "保存版本检查结果失败：" + redact(saveErr.Error())
			result.Stale = true
		}
	}
	slog.Info("update.check finished", "trace", trace, "source", source, "releases", len(list), "error", result.Error, "ms", time.Since(started).Milliseconds(), "code.function.name", "center.checkUpdates")
	u := &s.updates
	u.Lock()
	result.NextCheckAt = u.snapshot.NextCheckAt
	u.snapshot = result
	u.done = nil
	close(done)
	u.Unlock()
}

func (s *Server) queryUpdateReleases(ctx context.Context) (out []weirRelease, err error) {
	finish := operationStage(ctx, "update.releases.select")
	defer func() { finish(err) }()
	client := s.weirGitHub()
	list, err := client.ListAll(ctx, weirRepo())
	if err != nil {
		return nil, err
	}
	// GitHub's anonymous list index can lag behind its latest-release endpoint.
	// Merge only complete successful queries; latest is not a numeric ordering rule.
	latest, _, latestErr := client.Latest(ctx, weirRepo())
	if latestErr != nil && !errors.Is(latestErr, ghrelease.ErrNotFound) {
		return nil, fmt.Errorf("核对最新发布失败：%w", latestErr)
	}
	if latestErr == nil {
		found := false
		for i := range list {
			if list[i].Tag == latest.Tag {
				list[i] = latest
				found = true
				break
			}
		}
		if !found {
			list = append(list, latest)
			slog.Info("update.releases reconciled", "trace", traceOf(ctx), "tag", latest.Tag, "source", "latest", "code.function.name", "center.queryUpdateReleases")
		}
	}
	out = []weirRelease{}
	for _, r := range list {
		v := strings.TrimPrefix(r.Tag, "v")
		if r.Draft || r.Prerelease || !strings.HasPrefix(r.Tag, "v") {
			continue
		}
		if _, ok := buildinfo.ParseScheme(v); !ok || selfupdate.Compare(v, selfupdate.MinVersion) < 0 {
			continue
		}
		if errors.Is(latestErr, ghrelease.ErrNotFound) {
			return nil, errors.New("发布列表与最新发布查询不一致，请重试")
		}
		item := weirRelease{Version: v, PublishedAt: r.Published, Notes: r.Body, URL: r.HTMLURL}
		if err := selfupdate.ValidateRelease(r, v); err != nil {
			item.Unavailable = err.Error()
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return selfupdate.Compare(out[i].Version, out[j].Version) > 0 })
	return out, nil
}

func (s *Server) updateCheckSnapshot() updateSnapshot {
	s.updates.Lock()
	defer s.updates.Unlock()
	out := s.updates.snapshot
	out.Releases = append([]weirRelease{}, out.Releases...)
	out.Stale = out.Error != "" || out.SucceededAt.IsZero() || time.Since(out.SucceededAt) >= updateCheckInterval
	return out
}

func (s *Server) updateCheckView() gin.H {
	check := s.updateCheckSnapshot()
	var latest *weirRelease
	older, unavailable := []weirRelease{}, []weirRelease{}
	for _, r := range check.Releases {
		if r.Unavailable != "" {
			unavailable = append(unavailable, r)
			continue
		}
		if latest == nil {
			copy := r
			latest = &copy
		}
		if buildinfo.Release() && selfupdate.Compare(r.Version, buildinfo.Version) < 0 && len(older) < 3 {
			older = append(older, r)
		}
	}
	hasUpdate := latest != nil && buildinfo.Release() && selfupdate.Compare(latest.Version, buildinfo.Version) > 0
	status := "unknown"
	switch {
	case check.Error != "":
		status = "failed"
	case check.SucceededAt.IsZero():
	case check.Stale:
		status = "stale"
	case hasUpdate:
		status = "available"
	default:
		if buildinfo.Release() && latest != nil {
			status = "current"
		}
		for _, r := range unavailable {
			if selfupdate.Compare(r.Version, buildinfo.Version) > 0 {
				status = "unavailable"
				break
			}
		}
	}
	return gin.H{"update_status": status, "check": check, "latest": latest, "has_update": latest != nil && buildinfo.Release() && selfupdate.Compare(latest.Version, buildinfo.Version) > 0, "older": older, "unavailable": unavailable}
}

func (s *Server) postUpdateCheck(c *gin.Context) {
	if _, err := s.beginUpdateCheck(traceOf(c.Request.Context()), "manual"); err != nil {
		failDetail(c, 503, "update_check_unavailable", err.Error(), nil)
		return
	}
	c.JSON(202, s.updateCheckView())
}

// stopUpdateChecks joins both the scheduler and in-flight query before Store closes.
func (s *Server) stopUpdateChecks() {
	u := &s.updates
	u.Lock()
	cancel, stopped := u.cancel, u.stopped
	u.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	<-stopped
	u.Lock()
	done := u.done
	u.Unlock()
	if done != nil {
		<-done
	}
}
