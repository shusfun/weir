package center

import (
	"log/slog"
	"sort"
	"sync"
	"time"
)

// taskLog 记录周期任务的失败：同一项任务原因不变时只记一次日志，原因变了或恢复了再记一次，每分钟运行也不刷屏。
// 正在失败的任务同时进管理员通知（写原因与从什么时候开始失败），恢复后通知自动消失。
// 订阅源刷新、自动备份、证书续签的结果记在各自的对象上并显示，不重复进通知。
type taskLog struct {
	mu      sync.Mutex
	failing map[string]taskFailure
}

type taskFailure struct {
	msg   string
	since time.Time
}

// taskLabels 是要进通知的周期任务（没有别的地方显示它们的结果）。
var taskLabels = map[string]string{
	"reset-usage":      "按月清零用量",
	"check-alerts":     "离线与额度提醒",
	"observe-security": "整理安全事件",
	"reclaim-budgets":  "回收离线 VPS 的额度",
	"prune-samples":    "清理过期的观测记录",
	"maintenance-lock": "获取周期任务锁",
}

func (t *taskLog) note(task string, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.failing == nil {
		t.failing = map[string]taskFailure{}
	}
	prev, failing := t.failing[task]
	switch {
	case err == nil && failing:
		delete(t.failing, task)
		slog.Info("task recovered", "task", task)
	case err != nil:
		msg := redact(err.Error())
		if !failing {
			prev.since = time.Now()
		}
		if msg != prev.msg {
			slog.Error("task failed", "task", task, "err", msg)
		}
		t.failing[task] = taskFailure{msg: msg, since: prev.since}
	}
}

// notices 返回正在失败的周期任务。通知 id 带开始失败的时间：恢复后再失败会重新提示。
func (t *taskLog) notices() []Notice {
	t.mu.Lock()
	defer t.mu.Unlock()
	var out []Notice
	for task, f := range t.failing {
		label, ok := taskLabels[task]
		if !ok {
			continue
		}
		out = append(out, Notice{
			ID: "task-failed:" + task + ":" + f.since.UTC().Format(time.RFC3339), Level: "warn",
			Title:  "周期任务「" + label + "」失败",
			Detail: f.msg,
			Extra:  map[string]any{"reason": "从 " + f.since.Local().Format("01-02 15:04") + " 开始失败，每分钟重试一次；恢复后这条通知自动消失。", "since": f.since.UTC()},
		})
	}
	sort.Slice(out, func(i, k int) bool { return out[i].ID < out[k].ID })
	return out
}
