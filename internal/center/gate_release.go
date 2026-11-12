package center

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/shusfun/weir/internal/buildinfo"
	"github.com/shusfun/weir/internal/fetch"
	"github.com/shusfun/weir/internal/ghrelease"
	"github.com/shusfun/weir/internal/releasefiles"
	"github.com/shusfun/weir/internal/selfupdate"
)

// weirRepo 是 Weir 程序的发布仓库；WEIR_GITHUB_REPO 只用于测试。
func weirRepo() string {
	if r := strings.TrimSpace(os.Getenv("WEIR_GITHUB_REPO")); r != "" {
		return r
	}
	return "shusfun/weir"
}

// weirGitHub 是读取 Weir 发布信息用的客户端（只访问 GitHub API；测试时用 Server.GitHub）。
// 下载用 downloadGitHub：按「GitHub 加速」的来源顺序。
func (s *Server) weirGitHub() ghrelease.Client {
	if s.GitHub != nil {
		return *s.GitHub
	}
	return ghrelease.Client{Query: s.queryReleases}
}

// releaseAssetName 是某版本某架构的发布包文件名。
func releaseAssetName(version, arch string) string {
	return fmt.Sprintf("weir-%s-linux-%s.tar.gz", version, arch)
}

// gateProgram 返回给某架构 Gate 用的、与中心同版本的程序路径与摘要。
// 同架构直接用中心自己；不同架构从 GitHub Release 取对应包，按官方摘要与包内 SHA256SUMS 校验后缓存。
// 开发构建没有对应发布，只能给同架构的 Gate。
func (s *Server) gateProgram(ctx context.Context, arch string) (string, string, error) {
	if arch != "amd64" && arch != "arm64" {
		return "", "", errors.New("不支持的架构")
	}
	if runtime.GOOS == "linux" && runtime.GOARCH == arch {
		return selfSHA256()
	}
	if !buildinfo.Release() {
		return "", "", errors.New("开发构建只能提供与中心同架构的程序")
	}
	dir := filepath.Join(s.dataDir(), "releases", buildinfo.Version, arch)
	bin := filepath.Join(dir, "weir")
	if sum, err := os.ReadFile(bin + ".sha256"); err == nil {
		if got, _ := fileSHA256(bin); got == strings.TrimSpace(string(sum)) {
			return bin, got, nil
		}
	}
	id := "gate-release:" + buildinfo.Version + ":" + arch
	trace := requestID()
	if t, ok := ctx.Value(httpTraceKey{}).(httpTrace); ok {
		trace = t.ID
	}
	s.jobs.StartWith(jobSpec{ID: id, Kind: "gate-release", Title: "Weir " + buildinfo.Version + "（" + arch + "）", Trace: trace}, func(bg context.Context, run JobRun) (map[string]any, error) {
		return nil, s.downloadRelease(bg, run, buildinfo.Version, arch, dir)
	})
	wctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	j, _ := s.jobs.Wait(wctx, id)
	if j.Status != jobDone {
		if j.Error != "" {
			return "", "", errors.New(j.Error)
		}
		return "", "", errors.New("程序仍在下载，稍后重试")
	}
	sum, err := fileSHA256(bin)
	return bin, sum, err
}

// downloadRelease 下载并校验一个 Weir 发布包，把其中的 weir 与 install.sh 放进 dir。
func (s *Server) downloadRelease(ctx context.Context, run JobRun, version, arch, dir string) error {
	s.releaseFilesMu.Lock()
	defer s.releaseFilesMu.Unlock()
	s.pruneReleaseCache(ctx, &run)
	name := releaseAssetName(version, arch)
	archive := filepath.Join(s.dataDir(), "releases", ".downloads", name)
	if err := releasefiles.DownloadDir(filepath.Dir(archive), name); err != nil {
		return err
	}
	if err := s.fetchWeirPackage(ctx, run, version, arch, archive); err != nil {
		return err
	}
	data, err := os.ReadFile(archive)
	if err != nil {
		return err
	}
	files, err := verifiedPackage(data)
	if err != nil {
		return err
	}
	root, err := releasefiles.Ensure(dir, 0o755)
	if err != nil {
		return err
	}
	defer root.Close()
	for _, f := range []string{"weir", "install.sh"} {
		if err = writeCachedFile(root, f, files[f], 0o755); err != nil {
			return err
		}
	}
	sum := sha256.Sum256(files["weir"])
	if err = writeCachedFile(root, "weir.sha256", []byte(hex.EncodeToString(sum[:])), 0o644); err != nil {
		return err
	}
	if err = os.Remove(archive); err != nil {
		run.Log("WARN release download cleanup: " + err.Error())
	}
	s.pruneReleaseCache(ctx, &run)
	run.Log("已校验并缓存 " + name)
	return nil
}

func writeCachedFile(root *os.Root, name string, data []byte, mode os.FileMode) error {
	if err := root.Remove(name + ".new"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	f, err := root.OpenFile(name+".new", os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	return root.Rename(name+".new", name)
}

func (s *Server) pruneReleaseCache(ctx context.Context, run *JobRun) {
	keep := map[string]bool{buildinfo.Version: true}
	if previous := selfupdate.Previous(); previous != "" {
		keep[previous] = true
	}
	dir := filepath.Join(s.dataDir(), "releases")
	stats, err := releasefiles.PruneVersionDirs(dir, keep, nil)
	archives := map[string]bool{}
	for version := range keep {
		for _, arch := range []string{"amd64", "arm64"} {
			name := releaseAssetName(version, arch)
			archives[name], archives[name+".part"] = true, true
		}
	}
	downloads, downloadErr := releasefiles.Prune(filepath.Join(dir, ".downloads"), archives, func(name string) bool {
		return strings.HasPrefix(name, "weir-") && (strings.HasSuffix(name, ".tar.gz") || strings.HasSuffix(name, ".tar.gz.part"))
	}, nil)
	stats.Removed += downloads.Removed
	stats.Bytes += downloads.Bytes
	err = errors.Join(err, downloadErr)
	trace := ""
	if t, ok := ctx.Value(httpTraceKey{}).(httpTrace); ok {
		trace = t.ID
	}
	if trace == "" && run != nil && run.e != nil {
		run.m.mu.Lock()
		trace = run.e.job.Trace
		run.m.mu.Unlock()
	}
	if trace == "" {
		trace = requestID()
	}
	message := fmt.Sprintf("release cache cleanup trace=%s version=%s %s", trace, buildinfo.Version, stats)
	if err != nil {
		message = fmt.Sprintf("WARN %s error=%q", message, err)
	}
	log.Print(message)
	if run != nil {
		run.Log(message)
	}
}

// fetchWeirPackage 按「GitHub 加速」的来源顺序（含经 VPS 中转）下载 Weir 某版本某架构的发布包到 dest，
// 按 GitHub 的官方摘要校验。Gate 安装包（其他架构）与中心自更新的预下载共用。
func (s *Server) fetchWeirPackage(ctx context.Context, run JobRun, version, arch, dest string) error {
	gh, sources, err := s.downloadGitHub(ctx)
	if err != nil {
		return err
	}
	run.Stage("读取 GitHub 发布 v" + version)
	rel, err := gh.Tag(ctx, weirRepo(), "v"+version)
	if err != nil {
		return err
	}
	name := releaseAssetName(version, arch)
	asset, ok := rel.Asset(name)
	if !ok {
		return fmt.Errorf("v%s 没有 %s 的发布包", version, arch)
	}
	want := asset.SHA256()
	if want == "" {
		return fmt.Errorf("v%s 的发布包没有官方摘要，拒绝下载", version)
	}
	urls, err := gh.URLs(asset.URL)
	if err != nil {
		return err
	}
	const max = 256 << 20
	relay := s.relaySources(gh, sources, asset.URL, want, asset.Size, max)
	return run.Download(ctx, fetch.Request{URLs: urls, Sources: relay, Dest: dest, SHA256: want, MaxSize: max, HTTP: s.githubClientHTTP()})
}

// verifiedPackage 见 ghrelease.VerifiedPackage。
func verifiedPackage(data []byte) (map[string][]byte, error) { return ghrelease.VerifiedPackage(data) }

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
