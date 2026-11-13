// Package selfupdate 是 Weir 从 GitHub Release 自更新的 root 一侧。
//
// 中心以普通用户运行、不能替换程序，只把请求写进 Dir/request.json；systemd 的 weir-update.path 发现后
// 以 root 运行 `weir self-update --apply`：自己从 GitHub API 取官方摘要、用包里的 install.sh 安装
// （install.sh 失败时自动切回上一版本）。中心只能指定版本号与下载来源的顺序（直连与镜像）。
// 中心可以先按「GitHub 加速」（含经 VPS 中转）把发布包预下载到 Dir：root 先把它复制到只有 root 能写的 WorkDir
// 再按官方摘要校验（防止中心在校验后替换文件），一致就用，不一致或没有就按来源顺序自己下载。
// 进度写进 Dir/status.json，中心读取后显示在页面上。中心写请求前把预下载的记录写进 Dir/prefetch.json，
// 中心重启后页面还能看到这段；root 不读这个文件。
package selfupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/shusfun/weir/internal/buildinfo"
	"github.com/shusfun/weir/internal/fetch"
	"github.com/shusfun/weir/internal/ghrelease"
	"github.com/shusfun/weir/internal/lockfile"
	"github.com/shusfun/weir/internal/releasefiles"
)

// MinVersion 是能自更新到的最低版本。Weir 已从 0.1.1 重新开始发版；更早的历史版本不是 Weir 安装包。
const MinVersion = "0.1.1"

// 固定位置。测试可替换。
var (
	Dir       = "/var/lib/weir/update"
	WorkDir   = "/var/lib/weir-update-work" // 只有 root 能写，解包在这里
	Base      = "/opt/weir"
	RoleFile  = "/etc/weir/role"
	CenterCfg = "/etc/weir/center/config.json"
	Repo      = "shusfun/weir"
	GitHub    = func() ghrelease.Client {
		tr := http.DefaultTransport.(*http.Transport).Clone()
		tr.ResponseHeaderTimeout = 30 * time.Second
		return ghrelease.Client{HTTP: &http.Client{Transport: tr}, Sources: ghrelease.DefaultSources()}
	}
	runInstaller = func(ctx context.Context, dir string, args []string, log func(string)) error {
		cmd := exec.CommandContext(ctx, "sh", append([]string{filepath.Join(dir, "install.sh")}, args...)...)
		cmd.Dir = dir
		if id, ok := ctx.Value(updateIDKey{}).(string); ok {
			cmd.Env = append(os.Environ(), "WEIR_UPDATE_ID="+id)
		}
		return runLogged(cmd, log)
	}
)

type updateIDKey struct{}

var safeVersion = regexp.MustCompile(`^[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,4}$`)

// Request 是中心写入的请求：Action 为 update（到 Version）或 rollback（切回本机保留的上一版本）。
// Sources 是下载来源的顺序（只有直连与镜像）；旧版中心不写时用 ghrelease.DefaultSources()。
type Request struct {
	ID      string             `json:"id"`
	Action  string             `json:"action"`
	Version string             `json:"version,omitempty"`
	Sources []ghrelease.Source `json:"sources,omitempty"`
	TraceID string             `json:"trace_id,omitempty"`
}

// maxSources 与中心「GitHub 加速」的上限一致。
const maxSources = 12

// PrefetchName 是中心预下载的发布包在 Dir 里的文件名。
func PrefetchName(version, arch string) string {
	return fmt.Sprintf("weir-%s-linux-%s.tar.gz", version, arch)
}

// Status 是 root 写回的进度。
type Status struct {
	Stage    string          `json:"stage,omitempty"`
	TraceID  string          `json:"trace_id,omitempty"`
	ID       string          `json:"id"`
	Action   string          `json:"action"`
	From     string          `json:"from"`
	Version  string          `json:"version"`
	State    string          `json:"state"` // downloading / installing / done / failed
	Progress *fetch.Progress `json:"progress,omitempty"`
	Logs     []string        `json:"logs"`
	Error    string          `json:"error,omitempty"`
	Started  time.Time       `json:"started_at"`
	Finished *time.Time      `json:"finished_at,omitempty"`
}

// Valid 检查请求：中心不可信，只接受固定的动作与版本号格式。
func (r Request) Valid() error {
	if r.TraceID != "" {
		b, e := hex.DecodeString(r.TraceID)
		if e != nil || len(b) != 16 {
			return errors.New("关联编号无效")
		}
	}
	if len(r.ID) < 8 || len(r.ID) > 64 || strings.ContainsAny(r.ID, "/\\ \n") {
		return errors.New("请求编号无效")
	}
	switch r.Action {
	case "enable-local-gate", "prepare-laya":
		if r.Version != "" || len(r.Sources) > 0 {
			return errors.New("固定宿主动作不接受版本与来源")
		}
	case "update":
		if !safeVersion.MatchString(r.Version) {
			return errors.New("版本号无效")
		}
		if Compare(r.Version, MinVersion) < 0 {
			return fmt.Errorf("只能更新到 %s 及以后的版本", MinVersion)
		}
	case "rollback":
		if r.Version != "" || len(r.Sources) > 0 {
			return errors.New("切回上一版本不带版本号与下载来源")
		}
	default:
		return errors.New("未知动作")
	}
	if len(r.Sources) > maxSources {
		return errors.New("下载来源过多")
	}
	direct, seen := false, map[string]bool{}
	for _, src := range r.Sources {
		switch {
		case src.Kind == ghrelease.SourceDirect && !direct && src.Prefix == "" && src.GateID == 0:
			direct = true
		case src.Kind == ghrelease.SourceMirror && ghrelease.ValidPrefix(src.Prefix) && !seen[src.Prefix] && src.GateID == 0:
			seen[src.Prefix] = true
		default:
			return errors.New("下载来源无效（只接受直连与 https 镜像前缀）")
		}
	}
	return nil
}

// Compare 按数字比较两个版本号。
func Compare(a, b string) int {
	x, okA := buildinfo.ParseScheme(a)
	y, okB := buildinfo.ParseScheme(b)
	if !okA || !okB {
		return strings.Compare(a, b)
	}
	return x.Compare(y)
}

// Previous 返回本机保留的上一版本（没有时为空）。
func Previous() string {
	link, err := os.Readlink(filepath.Join(Base, "previous"))
	if err != nil {
		return ""
	}
	v := filepath.Base(link)
	if !safeVersion.MatchString(v) || Compare(v, MinVersion) < 0 {
		return ""
	}
	if _, err = os.Stat(filepath.Join(Base, "releases", v, "weir")); err != nil {
		return ""
	}
	return v
}

// Apply 读取并执行请求（以 root 运行）。执行结束后消费请求；结果保留在操作回执中。
func Apply(ctx context.Context) error {
	path := filepath.Join(Dir, "request.json")
	st, err := os.Lstat(path)
	if err != nil {
		return nil // 没有请求
	}
	if !st.Mode().IsRegular() || st.Size() > 16<<10 {
		_ = os.Remove(path)
		return errors.New("请求文件无效")
	}
	unlock, err := lockfile.TryAcquire(WorkDir + ".lock")
	if err != nil {
		return err
	}
	defer unlock()
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	defer os.Remove(path)
	var req Request
	if err = json.Unmarshal(data, &req); err != nil {
		return errors.New("请求格式无效")
	}
	// 切回用本机保留的上一版本，不下载
	state := "downloading"
	if req.Action == "rollback" {
		state = "installing"
	}
	w := &writer{st: Status{ID: req.ID, Action: req.Action, TraceID: req.TraceID, From: buildinfo.Version, Version: req.Version, State: state, Started: time.Now().UTC(), Logs: []string{}}}
	if err = req.Valid(); err != nil {
		w.fail(err)
		return err
	}
	w.set(func(st *Status) {})
	ctx = context.WithValue(ctx, updateIDKey{}, req.ID)
	if req.TraceID != "" {
		ctx = releasefiles.WithTrace(ctx, "00-"+req.TraceID+"-"+randomSpan()+"-01")
	}
	w.log("开始固定操作 " + req.Action + " operation=" + req.ID + " trace=" + req.TraceID)
	switch req.Action {
	case "enable-local-gate":
		err = w.enableLocalGate(ctx)
	case "prepare-laya":
		err = w.prepareLaya(ctx)
	case "update":
		err = w.update(ctx, req.Version, req.Sources)
	case "rollback":
		err = w.rollback(ctx)
	}
	if err != nil {
		w.fail(err)
		return err
	}
	w.set(func(s *Status) { s.State = "done" })
	w.log("完成")
	w.finish()
	return nil
}

type writer struct {
	mu sync.Mutex
	st Status
}

func (w *writer) set(f func(*Status)) {
	w.mu.Lock()
	defer w.mu.Unlock()
	f(&w.st)
	b, _ := json.Marshal(w.st)
	_ = writeStatus(b)
	// Keep the receipt addressable after another root operation starts.
	if w.st.ID != "" && !strings.ContainsAny(w.st.ID, "/\\ \n") {
		_ = writeOperation(w.st.ID, b)
	}
}

func (w *writer) log(s string) {
	w.set(func(st *Status) {
		// 写完整的 UTC 时间，由控制台换成浏览器本地时间显示
		st.Logs = append(st.Logs, time.Now().UTC().Format(time.RFC3339)+" "+s)
		if len(st.Logs) > 300 {
			st.Logs = st.Logs[len(st.Logs)-300:]
		}
	})
}

func (w *writer) fail(err error) {
	w.log(err.Error())
	w.set(func(s *Status) { s.State, s.Error = "failed", err.Error() })
	w.finish()
}

func (w *writer) finish() {
	now := time.Now().UTC()
	w.set(func(s *Status) { s.Finished, s.Progress = &now, nil })
}

// writeStatus 原子写入 status.json（随机临时名 + O_EXCL，不跟随中心可能放置的符号链接），组内可读。
func writeStatus(b []byte) error {
	var rnd [8]byte
	_, _ = rand.Read(rnd[:])
	tmp := filepath.Join(Dir, ".status-"+hex.EncodeToString(rnd[:]))
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return err
	}
	if _, err = f.Write(b); err == nil {
		err = f.Close()
	} else {
		_ = f.Close()
	}
	if err == nil {
		err = os.Rename(tmp, filepath.Join(Dir, "status.json"))
	}
	if err != nil {
		_ = os.Remove(tmp)
	}
	return err
}

// ReadStatus 读取 status.json（中心调用）。
func ReadStatus() (Status, bool) {
	var st Status
	b, err := os.ReadFile(filepath.Join(Dir, "status.json"))
	valid := err == nil && json.Unmarshal(b, &st) == nil
	var req Request
	pending, err := os.ReadFile(filepath.Join(Dir, "request.json"))
	if err == nil && json.Unmarshal(pending, &req) == nil && req.Valid() == nil && (!valid || st.ID != req.ID) {
		return Status{ID: req.ID, Action: req.Action, TraceID: req.TraceID, State: "queued", Logs: []string{}}, true
	}
	return st, valid
}

func writeOperation(id string, b []byte) error {
	f, err := os.CreateTemp(Dir, ".operation-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0640); err == nil {
		_, err = f.Write(b)
	}
	e := f.Close()
	if err == nil {
		err = e
	}
	if err == nil {
		err = os.Rename(f.Name(), filepath.Join(Dir, "operation-"+id+".json"))
	}
	return err
}

func ReadOperation(id string) (Status, bool) {
	if (Request{ID: id, Action: "prepare-laya"}).Valid() != nil {
		return Status{}, false
	}
	var st Status
	b, e := os.ReadFile(filepath.Join(Dir, "operation-"+id+".json"))
	if e == nil && json.Unmarshal(b, &st) == nil && st.ID == id {
		return st, true
	}
	b, e = os.ReadFile(filepath.Join(Dir, "request.json"))
	var req Request
	if e == nil && json.Unmarshal(b, &req) == nil && req.ID == id {
		return Status{ID: id, Action: req.Action, TraceID: req.TraceID, State: "queued", Logs: []string{}}, true
	}
	return Status{}, false
}

// WriteRequest 写入请求（中心调用）；已有未处理的请求时拒绝。
func WriteRequest(r Request) error {
	if err := r.Valid(); err != nil {
		return err
	}
	unlock, err := lockfile.TryAcquire(filepath.Join(Dir, "request.lock"))
	if err != nil {
		return err
	}
	defer unlock()
	if st, ok := ReadStatus(); ok && st.Finished == nil && st.State != "failed" && st.State != "done" {
		return errors.New("已有宿主操作正在执行")
	}
	path := filepath.Join(Dir, "request.json")
	if _, err := os.Lstat(path); err == nil {
		return errors.New("已有一个更新请求在等待执行")
	}
	b, _ := json.Marshal(r)
	tmp := path + ".new"
	_ = os.Remove(tmp)
	if err := os.WriteFile(tmp, b, 0o640); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func randomSpan() string { var b [8]byte; _, _ = rand.Read(b[:]); return hex.EncodeToString(b[:]) }

// Handoff 是中心交给 root 之前留下的记录：哪次更新、预下载的日志与实际来源。只有中心读写。
type Handoff struct {
	ID      string       `json:"id"`
	Action  string       `json:"action"`
	From    string       `json:"from"`
	Version string       `json:"version,omitempty"`
	Source  string       `json:"source,omitempty"`
	Logs    []HandoffLog `json:"logs,omitempty"`
	Started time.Time    `json:"started_at"`
	At      time.Time    `json:"at"`
}

// HandoffLog 是预下载的一行日志。
type HandoffLog struct {
	At   time.Time `json:"at"`
	Text string    `json:"text"`
}

const handoffFile = "prefetch.json"

// WriteHandoff 写入交接记录（中心调用）。
func WriteHandoff(h Handoff) error {
	b, err := json.Marshal(h)
	if err != nil {
		return err
	}
	tmp := filepath.Join(Dir, handoffFile+".new")
	_ = os.Remove(tmp)
	if err = os.WriteFile(tmp, b, 0o640); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(Dir, handoffFile))
}

// ReadHandoff 读取交接记录（中心调用）。
func ReadHandoff() (Handoff, bool) {
	var h Handoff
	b, err := os.ReadFile(filepath.Join(Dir, handoffFile))
	if err != nil || json.Unmarshal(b, &h) != nil || h.ID == "" {
		return Handoff{}, false
	}
	return h, true
}

// RequestPending 报告请求文件还在（root 还没接手）。
func RequestPending() bool {
	_, err := os.Lstat(filepath.Join(Dir, "request.json"))
	return err == nil
}

// ClearRequest 删除还没被接手的请求（更新服务一直没接手时，让管理员能重新发起）。
func ClearRequest() error {
	err := os.Remove(filepath.Join(Dir, "request.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// CleanPrefetch 删除别的版本预下载留下的包与 .part；keep 这个文件名（及其 .part）保留，空表示全删。
func CleanPrefetch(keep string) {
	list, _ := filepath.Glob(filepath.Join(Dir, "weir-*-linux-*.tar.gz*"))
	for _, f := range list {
		if b := filepath.Base(f); keep == "" || b != keep && b != keep+".part" {
			_ = os.Remove(f)
		}
	}
}

func role() (string, error) {
	b, err := os.ReadFile(RoleFile)
	r := strings.TrimSpace(string(b))
	if err != nil || r != "standalone" && r != "center" {
		return "", errors.New("只有装了中心的机器能在面板里自更新")
	}
	return r, nil
}

// installArgs 是重新运行 install.sh 的参数：角色与原来的监听地址（来自中心配置）。
func installArgs() ([]string, error) {
	r, err := role()
	if err != nil {
		return nil, err
	}
	var cfg struct {
		Listen string `json:"listen"`
	}
	if b, err := os.ReadFile(CenterCfg); err == nil {
		_ = json.Unmarshal(b, &cfg)
	}
	if cfg.Listen == "" {
		cfg.Listen = "127.0.0.1:19810"
	}
	return []string{r, cfg.Listen}, nil
}

func (w *writer) update(ctx context.Context, version string, sources []ghrelease.Source) error {
	args, err := installArgs()
	if err != nil {
		return err
	}
	if version == buildinfo.Version {
		return errors.New("已经是这个版本")
	}
	arch := runtime.GOARCH
	gh := GitHub()
	// 旧版中心的请求没有来源：用 GitHub() 的（默认来源）
	if len(sources) > 0 {
		gh.Sources = sources
	}
	w.log("读取 GitHub 发布 v" + version)
	rel, err := gh.Tag(ctx, Repo, "v"+version)
	if err != nil {
		return err
	}
	if rel.Draft || rel.Prerelease {
		return errors.New("只能更新到正式版本")
	}
	name := fmt.Sprintf("weir-%s-linux-%s.tar.gz", version, arch)
	asset, ok := rel.Asset(name)
	if !ok {
		return fmt.Errorf("v%s 没有 %s 的发布包", version, arch)
	}
	want := asset.SHA256()
	if want == "" {
		return errors.New("发布包没有官方摘要，拒绝下载")
	}
	urls, err := gh.URLs(asset.URL)
	if err != nil {
		return err
	}
	if err = releasefiles.DownloadDir(WorkDir, name); err != nil {
		return err
	}
	archive := filepath.Join(WorkDir, name)
	w.cleanup(WorkDir, map[string]bool{name: true, name + ".part": true}, workArtifact)
	w.adopt(name, archive, want)
	err = fetch.Download(ctx, fetch.Request{URLs: urls, Dest: archive, SHA256: want, MaxSize: 256 << 20, HTTP: gh.HTTP,
		OnProgress: func(p fetch.Progress) { w.set(func(s *Status) { s.Progress = &p }) }, Log: w.log})
	if err != nil {
		return err
	}
	return w.install(ctx, archive, version, args)
}

// adopt 采用中心预下载的发布包：先复制到 WorkDir（只有 root 能写），再由 fetch.Download 按官方摘要校验，
// 一致就直接用，不一致就丢弃、按来源顺序重新下载。中心放的文件只读不信：不跟随符号链接，限制大小。
func (w *writer) adopt(name, archive, want string) {
	src := filepath.Join(Dir, name)
	defer os.Remove(src)
	st, err := os.Lstat(src)
	if err != nil {
		return
	}
	if !st.Mode().IsRegular() || st.Size() > 256<<20 {
		w.log("中心预下载的文件无效，改为自己下载")
		return
	}
	if err = copyNoFollow(src, archive); err != nil {
		_ = os.Remove(archive)
		w.log("读取中心预下载的文件失败（" + err.Error() + "），改为自己下载")
		return
	}
	if ok, _ := sameDigest(archive, want); !ok {
		_ = os.Remove(archive)
		w.log("中心预下载的文件与官方摘要不一致，已丢弃，改为自己下载")
		return
	}
	w.log("使用中心预下载的发布包（已按官方摘要校验）")
}

func sameDigest(path, want string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return false, err
	}
	return hex.EncodeToString(h.Sum(nil)) == strings.ToLower(want), nil
}

func (w *writer) install(ctx context.Context, archive, version string, args []string) error {
	w.set(func(s *Status) { s.State, s.Progress = "installing", nil })
	dir := filepath.Join(WorkDir, "v"+version)
	w.cleanup(WorkDir, nil, func(n string) bool { return n == "v"+version })
	defer w.cleanup(WorkDir, nil, func(n string) bool { return n == "v"+version })
	if err := extract(archive, dir); err != nil {
		return err
	}
	w.log("安装 v" + version + "（服务会重启，控制台短暂不可用）")
	if err := runInstaller(ctx, dir, args, w.log); err != nil {
		return fmt.Errorf("安装失败，已保持或切回原版本：%w", err)
	}
	w.cleanup(WorkDir, nil, func(n string) bool { return n == filepath.Base(archive) || n == filepath.Base(archive)+".part" })
	return nil
}

// rollback 用本机保留的上一版本重新安装（install.sh 会把当前版本留作新的上一版本）。
func (w *writer) rollback(ctx context.Context) error {
	args, err := installArgs()
	if err != nil {
		return err
	}
	prev := Previous()
	if prev == "" {
		return errors.New("本机没有可切回的上一版本")
	}
	w.set(func(s *Status) { s.Version = prev })
	w.log("切回本机保留的 v" + prev)
	src := filepath.Join(Base, "releases", prev)
	dir := filepath.Join(WorkDir, "rollback-"+prev)
	w.cleanup(WorkDir, nil, workArtifact)
	root, err := releasefiles.Ensure(dir, 0o700)
	if err != nil {
		return err
	}
	defer root.Close()
	defer w.cleanup(WorkDir, nil, func(n string) bool { return n == "rollback-"+prev })
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			return err
		}
		f, err := root.OpenFile(e.Name(), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o755)
		if err != nil {
			return err
		}
		_, err = f.Write(b)
		err = errors.Join(err, f.Close())
		if err != nil {
			return err
		}
	}
	w.set(func(s *Status) { s.State = "installing" })
	if err = runInstaller(ctx, dir, args, w.log); err != nil {
		return fmt.Errorf("切回失败：%w", err)
	}
	return nil
}

func workArtifact(name string) bool {
	if strings.HasPrefix(name, "rollback-") {
		return releasefiles.Version(strings.TrimPrefix(name, "rollback-"))
	}
	if strings.HasPrefix(name, "v") {
		return releasefiles.Version(strings.TrimPrefix(name, "v"))
	}
	return strings.HasPrefix(name, "weir-") && (strings.HasSuffix(name, ".tar.gz") || strings.HasSuffix(name, ".tar.gz.part"))
}

func (w *writer) cleanup(dir string, keep map[string]bool, owned func(string) bool) {
	stats, err := releasefiles.Prune(dir, keep, owned, nil)
	message := fmt.Sprintf("release cleanup update=%s version=%s %s", w.st.ID, w.st.Version, stats)
	if err != nil {
		message = fmt.Sprintf("WARN %s error=%q", message, err)
	}
	log.Print(message)
	w.log(message)
}

// extract 解开发布包到 dir：只取顶层普通文件，拒绝路径穿越。
func extract(archive, dir string) error {
	data, err := os.ReadFile(archive)
	if err != nil {
		return err
	}
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer gz.Close()
	parent, err := releasefiles.Open(filepath.Dir(dir))
	if err != nil {
		return err
	}
	defer parent.Close()
	name := filepath.Base(dir)
	if st, e := parent.Lstat(name); e == nil && (!st.IsDir() || st.Mode()&os.ModeSymlink != 0) {
		return errors.New("更新解压目录不安全")
	}
	if err = parent.MkdirAll(name, 0o700); err != nil {
		return err
	}
	root, err := parent.OpenRoot(name)
	if err != nil {
		return err
	}
	defer root.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		name := strings.TrimPrefix(h.Name, "./")
		if h.Typeflag != tar.TypeReg || name == "" || strings.Contains(name, "/") || strings.Contains(name, "..") {
			continue
		}
		f, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o755)
		if err != nil {
			return err
		}
		_, err = io.Copy(f, io.LimitReader(tr, 256<<20))
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
	}
	for _, need := range []string{"weir", "install.sh", "SHA256SUMS", "VERSION"} {
		if _, err := os.Stat(filepath.Join(dir, need)); err != nil {
			return fmt.Errorf("发布包缺少 %s", need)
		}
	}
	return nil
}

// runLogged 运行命令并逐行写进日志。
func runLogged(cmd *exec.Cmd, log func(string)) error {
	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw
	done := make(chan struct{})
	go func() {
		buf := make([]byte, 4096)
		var line []byte
		for {
			n, err := pr.Read(buf)
			for _, b := range buf[:n] {
				if b == '\n' {
					if s := strings.TrimSpace(string(line)); s != "" {
						log(s)
					}
					line = line[:0]
				} else if len(line) < 2000 {
					line = append(line, b)
				}
			}
			if err != nil {
				if s := strings.TrimSpace(string(line)); s != "" {
					log(s)
				}
				close(done)
				return
			}
		}
	}()
	err := cmd.Run()
	_ = pw.Close()
	<-done
	return err
}

// copyTo 把已打开的普通文件复制到 dst（新建，只有 root 可读写）。
func copyTo(in *os.File, dst string) error {
	if st, err := in.Stat(); err != nil || !st.Mode().IsRegular() {
		return os.ErrInvalid
	}
	_ = os.Remove(dst)
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, io.LimitReader(in, 256<<20))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return err
}
