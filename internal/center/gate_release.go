package center

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
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

// weirGitHub 是读取 Weir 发布用的客户端（测试时用 Server.GitHub）。
func (s *Server) weirGitHub() ghrelease.Client {
	if s.GitHub != nil {
		return *s.GitHub
	}
	return ghrelease.Client{HTTP: githubHTTP(), Mirrors: ghrelease.Mirrors()}
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
	gh := s.weirGitHub()
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
	archive := filepath.Join(s.dataDir(), "releases", ".downloads", name)
	if err = run.Download(ctx, fetch.Request{URLs: urls, Dest: archive, SHA256: want, MaxSize: 256 << 20, HTTP: githubHTTP()}); err != nil {
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

// verifiedPackage 解开发布包，按包内 SHA256SUMS 校验每个文件，返回 weir 与 install.sh 的内容。
func verifiedPackage(data []byte) (map[string][]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	files := map[string][]byte{}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		name := strings.TrimPrefix(h.Name, "./")
		if h.Typeflag != tar.TypeReg || strings.Contains(name, "..") || strings.Contains(name, "/") {
			continue
		}
		if name == "weir" || name == "install.sh" || name == "SHA256SUMS" || name == "VERSION" {
			if files[name], err = io.ReadAll(io.LimitReader(tr, 256<<20)); err != nil {
				return nil, err
			}
		}
	}
	want := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(files["SHA256SUMS"]))
	for sc.Scan() {
		if f := strings.Fields(sc.Text()); len(f) == 2 {
			want[f[1]] = f[0]
		}
	}
	for _, name := range []string{"weir", "install.sh"} {
		got := sha256.Sum256(files[name])
		if len(files[name]) == 0 || want[name] != hex.EncodeToString(got[:]) {
			return nil, fmt.Errorf("发布包里的 %s 与 SHA256SUMS 不一致", name)
		}
	}
	return files, nil
}

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
