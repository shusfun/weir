// Package selfupdate 是 Weir 从 GitHub Release 自更新的 root 一侧。
//
// 中心以普通用户运行、不能替换程序，只把请求写进 Dir/request.json；systemd 的 weir-update.path 发现后
// 以 root 运行 `weir self-update --apply`：自己从 GitHub 取发布、按官方摘要校验、用包里的 install.sh 安装
// （install.sh 失败时自动切回上一版本）。中心只能指定版本号，root 不使用中心下载的任何文件。
// 进度写进 Dir/status.json，中心读取后显示在页面上。
package selfupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
)

// MinVersion 是能自更新到的最低版本：更早的版本还叫 migate，目录与服务名都不同。
const MinVersion = "0.1.13"

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
		return ghrelease.Client{HTTP: &http.Client{Transport: tr}, Mirrors: ghrelease.Mirrors()}
	}
	runInstaller = func(ctx context.Context, dir string, args []string, log func(string)) error {
		cmd := exec.CommandContext(ctx, "sh", append([]string{filepath.Join(dir, "install.sh")}, args...)...)
		cmd.Dir = dir
		return runLogged(cmd, log)
	}
)

var safeVersion = regexp.MustCompile(`^[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,4}$`)

// Request 是中心写入的请求：Action 为 update（到 Version）或 rollback（切回本机保留的上一版本）。
type Request struct {
	ID      string `json:"id"`
	Action  string `json:"action"`
	Version string `json:"version,omitempty"`
}

// Status 是 root 写回的进度。
type Status struct {
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
	if len(r.ID) < 8 || len(r.ID) > 64 || strings.ContainsAny(r.ID, "/\\ \n") {
		return errors.New("请求编号无效")
	}
	switch r.Action {
	case "update":
		if !safeVersion.MatchString(r.Version) {
			return errors.New("版本号无效")
		}
		if Compare(r.Version, MinVersion) < 0 {
			return fmt.Errorf("只能更新到 %s 及以后的版本", MinVersion)
		}
	case "rollback":
		if r.Version != "" {
			return errors.New("切回上一版本不带版本号")
		}
	default:
		return errors.New("未知动作")
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

// Apply 读取并执行请求（以 root 运行）。请求文件读后即删；结果写进 status.json。
func Apply(ctx context.Context) error {
	path := filepath.Join(Dir, "request.json")
	st, err := os.Lstat(path)
	if err != nil {
		return nil // 没有请求
	}
	if !st.Mode().IsRegular() || st.Size() > 4096 {
		_ = os.Remove(path)
		return errors.New("请求文件无效")
	}
	data, err := os.ReadFile(path)
	_ = os.Remove(path)
	if err != nil {
		return err
	}
	var req Request
	if err = json.Unmarshal(data, &req); err != nil {
		return errors.New("请求格式无效")
	}
	w := &writer{st: Status{ID: req.ID, Action: req.Action, From: buildinfo.Version, Version: req.Version, State: "downloading", Started: time.Now().UTC(), Logs: []string{}}}
	if err = req.Valid(); err != nil {
		w.fail(err)
		return err
	}
	switch req.Action {
	case "update":
		err = w.update(ctx, req.Version)
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
	f(&w.st)
	b, _ := json.Marshal(w.st)
	w.mu.Unlock()
	_ = writeStatus(b)
}

func (w *writer) log(s string) {
	w.set(func(st *Status) {
		st.Logs = append(st.Logs, time.Now().UTC().Format("15:04:05")+" "+s)
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
	if err != nil || json.Unmarshal(b, &st) != nil {
		return Status{}, false
	}
	return st, true
}

// WriteRequest 写入请求（中心调用）；已有未处理的请求时拒绝。
func WriteRequest(r Request) error {
	if err := r.Valid(); err != nil {
		return err
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

func (w *writer) update(ctx context.Context, version string) error {
	args, err := installArgs()
	if err != nil {
		return err
	}
	if version == buildinfo.Version {
		return errors.New("已经是这个版本")
	}
	arch := runtime.GOARCH
	gh := GitHub()
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
	if err = os.MkdirAll(WorkDir, 0o700); err != nil {
		return err
	}
	archive := filepath.Join(WorkDir, name)
	err = fetch.Download(ctx, fetch.Request{URLs: urls, Dest: archive, SHA256: want, MaxSize: 256 << 20, HTTP: gh.HTTP,
		OnProgress: func(p fetch.Progress) { w.set(func(s *Status) { s.Progress = &p }) }, Log: w.log})
	if err != nil {
		return err
	}
	return w.install(ctx, archive, version, args)
}

func (w *writer) install(ctx context.Context, archive, version string, args []string) error {
	w.set(func(s *Status) { s.State, s.Progress = "installing", nil })
	dir := filepath.Join(WorkDir, "v"+version)
	_ = os.RemoveAll(dir)
	if err := extract(archive, dir); err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	w.log("安装 v" + version + "（服务会重启，控制台短暂不可用）")
	if err := runInstaller(ctx, dir, args, w.log); err != nil {
		return fmt.Errorf("安装失败，已保持或切回原版本：%w", err)
	}
	_ = os.Remove(archive)
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
	_ = os.RemoveAll(dir)
	if err = os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	defer os.RemoveAll(dir)
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
		if err = os.WriteFile(filepath.Join(dir, e.Name()), b, 0o755); err != nil {
			return err
		}
	}
	w.set(func(s *Status) { s.State = "installing" })
	if err = runInstaller(ctx, dir, args, w.log); err != nil {
		return fmt.Errorf("切回失败：%w", err)
	}
	return nil
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
	if err = os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
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
		f, err := os.OpenFile(filepath.Join(dir, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o755)
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
