package center

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/shusfun/weir/internal/buildinfo"
	"github.com/shusfun/weir/internal/fetch"
	"github.com/shusfun/weir/internal/ghrelease"
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
	return ghrelease.Client{HTTP: githubHTTP()}
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
	s.jobs.Start(id, "gate-release", "Weir "+buildinfo.Version+"（"+arch+"）", func(bg context.Context, run JobRun) (map[string]any, error) {
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
	name := releaseAssetName(version, arch)
	archive := filepath.Join(s.dataDir(), "releases", ".downloads", name)
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
	if err = os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, f := range []string{"weir", "install.sh"} {
		if err = os.WriteFile(filepath.Join(dir, f+".new"), files[f], 0o755); err == nil {
			err = os.Rename(filepath.Join(dir, f+".new"), filepath.Join(dir, f))
		}
		if err != nil {
			return err
		}
	}
	sum := sha256.Sum256(files["weir"])
	if err = os.WriteFile(filepath.Join(dir, "weir.sha256"), []byte(hex.EncodeToString(sum[:])), 0o644); err != nil {
		return err
	}
	_ = os.Remove(archive)
	run.Log("已校验并缓存 " + name)
	return nil
}

// fetchWeirPackage 按「GitHub 加速」的来源顺序（含经 VPS 中转）下载 Weir 某版本某架构的发布包到 dest，
// 按 GitHub 的官方摘要校验。Gate 安装包（其他架构）与中心自更新的预下载共用。
func (s *Server) fetchWeirPackage(ctx context.Context, run JobRun, version, arch, dest string) error {
	gh, sources, err := s.downloadGitHub(ctx)
	if err != nil {
		return err
	}
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
