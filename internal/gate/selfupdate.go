package gate

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/shusfun/weir/internal/buildinfo"
	"github.com/shusfun/weir/internal/fetch"
	"github.com/shusfun/weir/internal/ghrelease"
	"github.com/shusfun/weir/internal/publicnet"
	"github.com/shusfun/weir/internal/releasefiles"
	"github.com/shusfun/weir/packages/contracts"
)

// updateRetry 是自动更新失败后的冷却时间：期间心跳不再触发，手动更新（release.replace）不受限。
const updateRetry = 10 * time.Minute

// releaseMaxSize 与辅助程序 /release 接收程序的上限一致。
const releaseMaxSize = 256 << 20

// updater 保证同一时间只跑一个自更新；状态随每次心跳上报。
type updater struct {
	pushed   time.Time // 最近一次单独上报进度的时间
	mu       sync.Mutex
	status   *contracts.SelfUpdateStatus
	run      *updateRun
	failedAt time.Time
}

type updateRun struct {
	version string
	done    chan struct{}
	err     error
}

// updateStatus 返回当前自更新状态的副本，供心跳上报。
func (a *Agent) updateStatus() *contracts.SelfUpdateStatus {
	a.up.mu.Lock()
	defer a.up.mu.Unlock()
	if a.up.status == nil {
		return nil
	}
	st := *a.up.status
	return &st
}

// updatePushEvery 是下载期间单独上报进度的最短间隔（心跳 30 秒一次，页面看到的进度会一跳一跳）。
const updatePushEvery = 3 * time.Second

func (a *Agent) setUpdateStatus(st *contracts.SelfUpdateStatus) {
	a.up.mu.Lock()
	a.up.status = st
	push := st != nil && a.center != nil && (st.State != "downloading" || time.Since(a.up.pushed) >= updatePushEvery)
	if push {
		a.up.pushed = time.Now()
	}
	a.up.mu.Unlock()
	if push {
		// 只用于页面显示：后台发送、失败忽略（下一次心跳还会带上）
		cp := *st
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_, _ = a.center.call(ctx, "POST", "/agent/self-update", cp, nil)
		}()
	}
}

// followCenter 处理心跳回复里的目标版本：与本机不同就在后台更新，不阻塞心跳；没有目标版本表示已与中心同版本，清掉旧状态。
func (a *Agent) followCenter(ctx context.Context, rel *contracts.ReleaseTarget) {
	if rel == nil || rel.Version == "" || rel.Version == buildinfo.Version {
		a.up.mu.Lock()
		if a.up.run == nil {
			a.up.status = nil
		}
		a.up.mu.Unlock()
		return
	}
	a.startUpdate(ctx, *rel, false)
}

// startUpdate 启动一次更新并返回它；已有更新在跑时返回那一个（版本不同时返回 nil 与错误）。
// manual 为假（心跳触发）时，上次失败后 updateRetry 内不再启动，返回 nil。
func (a *Agent) startUpdate(ctx context.Context, rel contracts.ReleaseTarget, manual bool) (*updateRun, error) {
	a.up.mu.Lock()
	defer a.up.mu.Unlock()
	if r := a.up.run; r != nil {
		if r.version != rel.Version {
			return nil, errors.New("正在更新到 " + r.version + "，请等它结束后再试")
		}
		return r, nil
	}
	if !manual && time.Since(a.up.failedAt) < updateRetry {
		return nil, nil
	}
	r := &updateRun{version: rel.Version, done: make(chan struct{})}
	a.up.run = r
	go func() {
		r.err = a.selfUpdate(ctx, rel)
		a.up.mu.Lock()
		a.up.run = nil
		if r.err != nil {
			a.up.failedAt = time.Now()
		}
		a.up.mu.Unlock()
		close(r.done)
	}()
	return r, nil
}

// manualUpdate 是 release.replace：不受失败冷却限制，等更新结束再回报结果。
func (a *Agent) manualUpdate(ctx context.Context, rel contracts.ReleaseTarget) error {
	r, err := a.startUpdate(ctx, rel, true)
	if err != nil {
		return err
	}
	select {
	case <-r.done:
		return r.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// selfUpdate 先按中心给的地址从 GitHub（或镜像）下载发布包，校验后取出 weir；没有地址或都失败时改从中心下载，
// 再交给辅助程序替换（辅助程序随后重启本服务）。失败原因写进状态随心跳上报，VPS 卡片上能看到，不只留在本机日志。
func (a *Agent) selfUpdate(ctx context.Context, rel contracts.ReleaseTarget) error {
	release, err := a.downloads.acquire(ctx, false)
	if err != nil {
		return err
	}
	defer release()
	if releasefiles.Trace(ctx) == "" {
		ctx = releasefiles.WithTrace(ctx, "")
	}
	version := rel.Version
	log.Printf("self update trace=%s version=%s stage=start", releasefiles.Trace(ctx), version)
	fail := func(msg string) error {
		log.Printf("self update trace=%s version=%s failed: %s", releasefiles.Trace(ctx), version, msg)
		a.setUpdateStatus(&contracts.SelfUpdateStatus{Version: version, State: "failed", Error: msg, At: time.Now().UTC()})
		return errors.New(msg)
	}
	if !safeVer.MatchString(version) {
		return fail("版本号无效")
	}
	a.setUpdateStatus(&contracts.SelfUpdateStatus{Version: version, State: "downloading", At: time.Now().UTC()})
	var reasons []string
	var data []byte
	if len(rel.URLs) > 0 && len(rel.SHA256) == 64 {
		var err error
		if data, err = a.releaseFromGitHub(ctx, rel); err != nil {
			if ctx.Err() != nil {
				return fail("已取消")
			}
			reasons = append(reasons, "从 GitHub 下载失败："+err.Error())
		}
	}
	if data == nil {
		a.setUpdateStatus(&contracts.SelfUpdateStatus{Version: version, State: "downloading", Source: "中心", At: time.Now().UTC()})
		got, sum, err := a.center.download(ctx, "/agent/release/"+version, "X-Release-SHA256")
		switch {
		case err != nil:
			reasons = append(reasons, "从中心下载程序失败："+err.Error())
		case sumOf(got) != sum:
			reasons = append(reasons, "从中心下载的程序与中心给的摘要不一致")
		default:
			data = got
		}
	}
	if data == nil {
		return fail(strings.Join(reasons, "；"))
	}
	a.setUpdateStatus(&contracts.SelfUpdateStatus{Version: version, State: "replacing", At: time.Now().UTC()})
	started := time.Now()
	if err := a.replace(ctx, version, sumOf(data), data); err != nil {
		return fail("替换程序失败：" + err.Error())
	}
	if filepath.Clean(a.P.Root) == "/opt/weir" {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			var st struct {
				Version string    `json:"version"`
				State   string    `json:"state"`
				Error   string    `json:"error"`
				Started time.Time `json:"started_at"`
			}
			body, _ := os.ReadFile("/var/lib/weir/update/status.json")
			if json.Unmarshal(body, &st) == nil && st.Version == version && !st.Started.Before(started.Add(-time.Second)) {
				if st.State == "failed" {
					return fail("宿主升级失败：" + st.Error)
				}
				if st.State == "done" {
					break
				}
			}
			select {
			case <-ctx.Done():
				return fail("等待宿主升级结果中断")
			case <-ticker.C:
			}
		}
	}
	a.cleanupUpdateDownloads(ctx, version, nil)
	log.Printf("self update trace=%s version=%s stage=done", releasefiles.Trace(ctx), version)
	if filepath.Clean(a.P.Root) != "/opt/weir" {
		a.setUpdateStatus(nil)
	}
	return nil
}

// replace 交给辅助程序替换程序（测试中替换）。
func (a *Agent) replace(ctx context.Context, version, sum string, data []byte) error {
	if err := a.checkExecutionLease(ctx); err != nil {
		return err
	}
	if a.replaceFn != nil {
		return a.replaceFn(ctx, version, sum, data)
	}
	return a.helper.Release(ctx, version, sum, data)
}

// releaseFromGitHub 下载发布包到 <数据目录>/update/（断点保留，下次续传），按官方摘要与包内 SHA256SUMS 校验，返回 weir。
func (a *Agent) releaseFromGitHub(ctx context.Context, rel contracts.ReleaseTarget) ([]byte, error) {
	urls, err := a.githubURLs(rel.URLs)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(a.P.Data, "update")
	name := "weir-" + rel.Version + ".tar.gz"
	if err := releasefiles.DownloadDir(dir, name); err != nil {
		return nil, err
	}
	a.cleanupUpdateDownloads(ctx, rel.Version, map[string]bool{name: true, name + ".part": true})
	dest := filepath.Join(dir, name)
	err = fetch.Download(ctx, fetch.Request{URLs: urls, Dest: dest, SHA256: rel.SHA256, MaxSize: releaseMaxSize, HTTP: a.externalHTTP(),
		OnProgress: func(p fetch.Progress) {
			a.setUpdateStatus(&contracts.SelfUpdateStatus{Version: rel.Version, State: "downloading", Done: p.Done, Total: p.Total, Speed: p.Speed, Source: p.Source, At: time.Now().UTC()})
		},
		Log: func(s string) { log.Printf("self update %s: %s", rel.Version, s) }})
	if err != nil {
		return nil, err
	}
	pkg, err := os.ReadFile(dest)
	if removeErr := os.Remove(dest); removeErr != nil {
		log.Printf("WARN self update cleanup trace=%s version=%s error=%q", releasefiles.Trace(ctx), rel.Version, removeErr)
	}
	if err != nil {
		return nil, err
	}
	files, err := ghrelease.VerifiedPackage(pkg)
	if err != nil {
		return nil, err
	}
	return files["weir"], nil
}

func (a *Agent) cleanupUpdateDownloads(ctx context.Context, version string, keep map[string]bool) {
	stats, err := releasefiles.Prune(filepath.Join(a.P.Data, "update"), keep, func(n string) bool {
		return strings.HasPrefix(n, "weir-") && (strings.HasSuffix(n, ".tar.gz") || strings.HasSuffix(n, ".tar.gz.part"))
	}, nil)
	if err != nil {
		log.Printf("WARN self update cleanup trace=%s version=%s %s error=%q", releasefiles.Trace(ctx), version, stats, err)
	} else {
		log.Printf("self update cleanup trace=%s version=%s %s", releasefiles.Trace(ctx), version, stats)
	}
}

// externalHTTP 是访问 GitHub 与镜像的客户端：系统根证书、跟随跳转（发布文件会跳到 objects.githubusercontent.com）、
// 不设整体超时（由 fetch 的无数据超时控制）。
func (a *Agent) externalHTTP() *http.Client {
	if a.extHTTP != nil {
		return a.extHTTP
	}
	return a.workerHTTP(publicnet.Client(0))
}
