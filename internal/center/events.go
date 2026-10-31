package center

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// 控制台事件流（SSE）：每个标签页一条连接，推后台任务的步骤、日志与结束，VPS 配对进度，以及新草稿，
// 代替逐个轮询。普通用户只收到自己发起的任务；配对、VPS 与草稿的提醒只给管理员。
// 提醒只说「什么变了」，页面收到后照常经接口重新读取，权限仍由接口判断。
// 事件编号是「启动编号.任务版本.提醒序号」：浏览器重连时带上它，只补发之后的变化；中心重启过或提醒已经滚出缓冲时，
// hello 里 reset 为真，页面重新读取全部。一次连接最多 2 分钟，每 15 秒一次 ping，与链路页的实时流相同。
// 连不上事件流时页面退回轮询，并写明「实时推送不可用」。

const eventsKeep = 100

type consoleNudge struct {
	seq  uint64
	Kind string `json:"kind"` // pairing（键为凭证摘要）/ gate（键为 VPS 编号）/ drafts
	Key  string `json:"key,omitempty"`
}

type eventHub struct {
	mu     sync.Mutex
	boot   string
	seq    uint64
	recent []consoleNudge
	notify chan struct{}
	stop   chan struct{}
	once   sync.Once
}

func (h *eventHub) bootID() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.boot == "" {
		h.boot = randomHex(4)
	}
	return h.boot
}

// publish 记下一条提醒并唤醒所有连接。
func (h *eventHub) publish(kind, key string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.seq++
	h.recent = append(h.recent, consoleNudge{seq: h.seq, Kind: kind, Key: key})
	if len(h.recent) > eventsKeep {
		h.recent = h.recent[len(h.recent)-eventsKeep:]
	}
	if h.notify != nil {
		close(h.notify)
		h.notify = nil
	}
}

func (h *eventHub) wait() <-chan struct{} {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.notify == nil {
		h.notify = make(chan struct{})
	}
	return h.notify
}

// since 返回 seq 之后的提醒与当前序号；complete 为假表示中间有提醒已经滚出缓冲。
func (h *eventHub) since(seq uint64) (out []consoleNudge, last uint64, complete bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	complete = seq >= h.seq || (len(h.recent) > 0 && h.recent[0].seq <= seq+1)
	for _, n := range h.recent {
		if n.seq > seq {
			out = append(out, n)
		}
	}
	return out, h.seq, complete
}

// stopped 在中心停止时关闭：长连接立即结束，不拖住关机。
func (h *eventHub) stopped() <-chan struct{} { return h.stopCh() }

func (h *eventHub) stopCh() chan struct{} {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.stop == nil {
		h.stop = make(chan struct{})
	}
	return h.stop
}

func (h *eventHub) close() {
	ch := h.stopCh()
	h.once.Do(func() { close(ch) })
}

// StopStreams 结束所有长连接（事件流、链路页实时流）；在 HTTP 服务开始关闭时调用。
func (s *Server) StopStreams() { s.events.close() }

func (s *Server) notifyConsole(kind, key string) { s.events.publish(kind, key) }

// parseEventID 解析「启动编号.任务版本.提醒序号」；不是这次启动的编号时返回 false。
func parseEventID(id, boot string) (rev, seq uint64, ok bool) {
	parts := strings.Split(id, ".")
	if len(parts) != 3 || parts[0] != boot {
		return 0, 0, false
	}
	rev, err1 := strconv.ParseUint(parts[1], 10, 64)
	seq, err2 := strconv.ParseUint(parts[2], 10, 64)
	return rev, seq, err1 == nil && err2 == nil
}

func (s *Server) consoleEvents(c *gin.Context) {
	ctx := c.Request.Context()
	v := viewerOf(c)
	boot := s.events.bootID()
	last := firstNonEmpty(c.GetHeader("Last-Event-ID"), c.Query("since"))
	rev, seq, resume := parseEventID(last, boot)
	var nudges []consoleNudge
	if resume {
		var complete bool
		if nudges, seq, complete = s.events.since(seq); !complete {
			resume = false
		}
	}
	if !resume {
		rev, nudges = 0, nil
		_, seq, _ = s.events.since(^uint64(0))
	}
	w := c.Writer
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(200)
	rc := http.NewResponseController(w)
	var buf bytes.Buffer
	frame := func(event string, v any) {
		b, _ := json.Marshal(v)
		fmt.Fprintf(&buf, "event: %s\ndata: %s\n\n", event, b)
	}
	// 一批事件一起写出，最后一条带编号：中途断开时浏览器从上一批之后重来，不会漏掉这一批里的任务
	flush := func() bool {
		fmt.Fprintf(&buf, "id: %s.%d.%d\nevent: sync\ndata: {}\n\n", boot, rev, seq)
		_ = rc.SetWriteDeadline(time.Now().Add(15 * time.Second))
		_, err := w.Write(buf.Bytes())
		buf.Reset()
		return err == nil && rc.Flush() == nil
	}
	jobs := func() {
		var list []Job
		list, rev = s.jobs.changedSince(v, rev)
		for _, j := range list {
			frame("job", j)
		}
	}
	nudge := func(list []consoleNudge) {
		for _, n := range list {
			if v.admin {
				frame("nudge", n)
			}
		}
	}
	// 先拿到「下一次变化」的通道再读当前状态：读完之后的变化一定会唤醒这里，不会漏掉
	jobWait, eventWait, stop := s.jobs.wait(), s.events.wait(), s.events.stopped()
	frame("hello", gin.H{"boot": boot, "reset": !resume, "admin": v.admin, "uid": v.uid})
	jobs()
	nudge(nudges)
	if !flush() {
		return
	}
	end := time.NewTimer(2 * time.Minute)
	defer end.Stop()
	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()
	var sent time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-stop:
			return
		case <-end.C:
			return
		case <-ping.C:
			frame("ping", gin.H{})
			if !flush() {
				return
			}
		case <-jobWait:
			// 下载进度每秒变化多次：任务的变化最多每秒推一次，结束等状态变化也在 1 秒内到达
			if d := time.Second - time.Since(sent); d > 0 {
				t := time.NewTimer(d)
				select {
				case <-ctx.Done():
					t.Stop()
					return
				case <-stop:
					t.Stop()
					return
				case <-t.C:
				}
			}
			jobWait = s.jobs.wait()
			jobs()
			sent = time.Now()
			if !flush() {
				return
			}
		case <-eventWait:
			eventWait = s.events.wait()
			list, n, _ := s.events.since(seq)
			seq = n
			nudge(list)
			if !flush() {
				return
			}
		}
	}
}
