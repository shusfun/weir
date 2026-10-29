package kernels

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/shusfun/weir/internal/fetch"
	"github.com/shusfun/weir/internal/ghrelease"
)

const maxDownload = 200 << 20

// source 描述一个内核在 GitHub 上的发布方式。
type source struct {
	repo      string
	tagPrefix string
	asset     func(version, arch string) string
	extract   func(archive []byte, version, arch string) ([]byte, error)
}

func xrayAsset(_, arch string) string {
	if arch == "arm64" {
		return "Xray-linux-arm64-v8a.zip"
	}
	return "Xray-linux-64.zip"
}

var sources = map[string]source{
	"xray":   {repo: "XTLS/Xray-core", tagPrefix: "v", asset: xrayAsset, extract: func(a []byte, _, _ string) ([]byte, error) { return fromZip(a, "xray") }},
	"mihomo": {repo: "MetaCubeX/mihomo", tagPrefix: "v", asset: func(v, arch string) string { return "mihomo-linux-" + arch + "-v" + v + ".gz" }, extract: func(a []byte, _, _ string) ([]byte, error) { return fromGzip(a) }},
	"sing-box": {repo: "shusfun/weir", tagPrefix: "sing-box-v", asset: func(v, arch string) string { return "sing-box-" + v + "-linux-" + arch + ".tar.gz" }, extract: func(a []byte, v, arch string) ([]byte, error) {
		return fromTarGz(a, "sing-box-"+v+"-linux-"+arch+"/sing-box")
	}},
}

// Release 是上游的一个正式版本。Downloads 是各架构下载包的下载量；sing-box 的下载量与发布时间取上游
// SagerNet/sing-box 同一标签（本项目 CI 重新编译的时间与下载量没有参考价值）。
type Release struct {
	Version     string           `json:"version"`
	PublishedAt time.Time        `json:"published_at"`
	Notes       string           `json:"notes"`
	Downloads   map[string]int64 `json:"downloads,omitempty"`
}

// statsRepo 是取下载量的仓库：sing-box 由本项目 CI 编译，自己仓库的下载量没有参考价值。
var statsRepo = map[string]struct{ repo, asset string }{
	"sing-box": {"SagerNet/sing-box", "sing-box-%s-linux-%s.tar.gz"},
}

// Fetcher 访问 GitHub API 列版本、下载并校验内核。Mirrors 为下载加速前缀（如 https://gh-proxy.com）；
// 摘要始终取自 GitHub API，镜像返回的内容与摘要不一致即丢弃。下载经 internal/fetch：可续传、可取消。
type Fetcher struct {
	HTTP         *http.Client
	API          string // 默认 https://api.github.com
	DownloadHost string // 默认 github.com；只接受该主机的 https 下载地址
	Mirrors      []string
	Cache        Cache
	// Progress / Log 接收下载进度与日志（可为空）。
	Progress func(fetch.Progress)
	Log      func(string)
}

func (f Fetcher) gh() ghrelease.Client {
	return ghrelease.Client{HTTP: f.HTTP, API: f.API, Host: f.DownloadHost, Mirrors: f.Mirrors}
}

func (f Fetcher) releases(ctx context.Context, kernel string) ([]ghrelease.Release, error) {
	src, ok := sources[kernel]
	if !ok {
		return nil, fmt.Errorf("unknown kernel %q", kernel)
	}
	all, err := f.gh().List(ctx, src.repo, 50)
	if err != nil {
		return nil, err
	}
	var out []ghrelease.Release
	for _, r := range all {
		// sing-box 与 Weir 程序发布在同一仓库，只认 sing-box-v 前缀；其余仓库只认正式版。
		if r.Draft || !strings.HasPrefix(r.Tag, src.tagPrefix) || src.tagPrefix == "v" && r.Prerelease {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

// Releases 列出上游正式版本（新到旧），附各架构下载量。
func (f Fetcher) Releases(ctx context.Context, kernel string) ([]Release, error) {
	rs, err := f.releases(ctx, kernel)
	if err != nil {
		return nil, err
	}
	src := sources[kernel]
	var stats map[string]ghrelease.Release
	if sr, ok := statsRepo[kernel]; ok {
		stats = map[string]ghrelease.Release{}
		if list, err := f.gh().List(ctx, sr.repo, 50); err == nil {
			for _, r := range list {
				stats[strings.TrimPrefix(r.Tag, "v")] = r
			}
		}
	}
	out := make([]Release, 0, len(rs))
	for _, r := range rs {
		v := strings.TrimPrefix(r.Tag, src.tagPrefix)
		rel := Release{Version: v, PublishedAt: r.Published, Notes: r.Body, Downloads: map[string]int64{}}
		if s, ok := stats[v]; ok && !s.Published.IsZero() {
			rel.PublishedAt = s.Published
		}
		for _, arch := range []string{"amd64", "arm64"} {
			if stats != nil {
				if s, ok := stats[v]; ok {
					if a, ok := s.Asset(fmt.Sprintf(statsRepo[kernel].asset, v, arch)); ok {
						rel.Downloads[arch] = a.Downloads
					}
				}
				continue
			}
			if a, ok := r.Asset(src.asset(v, arch)); ok {
				rel.Downloads[arch] = a.Downloads
			}
		}
		out = append(out, rel)
	}
	return out, nil
}

// Fetch 下载指定版本、按 GitHub 摘要校验、解出程序并存入缓存，返回程序摘要。
// 下载到一半的文件放在缓存目录的 .downloads 下，再次调用时续传。
func (f Fetcher) Fetch(ctx context.Context, kernel, version, arch string) (string, error) {
	src, ok := sources[kernel]
	if !ok {
		return "", fmt.Errorf("unknown kernel %q", kernel)
	}
	if !safeVersion.MatchString(version) || !safeArch.MatchString(arch) {
		return "", fmt.Errorf("invalid kernel reference %s %s %s", kernel, version, arch)
	}
	rs, err := f.releases(ctx, kernel)
	if err != nil {
		return "", err
	}
	name := src.asset(version, arch)
	var asset *ghrelease.Asset
	for _, r := range rs {
		if r.Tag == src.tagPrefix+version {
			if a, ok := r.Asset(name); ok {
				asset = &a
			}
		}
	}
	if asset == nil {
		return "", fmt.Errorf("%s %s 没有 %s", kernel, version, name)
	}
	want := asset.SHA256()
	if want == "" {
		return "", fmt.Errorf("%s %s 的发布没有提供摘要，拒绝下载", kernel, version)
	}
	urls, err := f.gh().URLs(asset.URL)
	if err != nil {
		return "", err
	}
	dest := filepath.Join(f.Cache.Dir, ".downloads", name)
	err = fetch.Download(ctx, fetch.Request{URLs: urls, Dest: dest, SHA256: want, MaxSize: maxDownload, HTTP: f.HTTP, OnProgress: f.Progress, Log: f.Log})
	if err != nil {
		return "", err
	}
	archive, err := os.ReadFile(dest)
	if err != nil {
		return "", err
	}
	binary, err := src.extract(archive, version, arch)
	if err != nil {
		return "", err
	}
	sum, err := f.Cache.Put(kernel, version, arch, binary)
	if err == nil {
		_ = os.Remove(dest)
	}
	return sum, err
}

func fromZip(archive []byte, name string) ([]byte, error) {
	r, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, err
	}
	for _, f := range r.File {
		if path.Base(f.Name) == name && !f.FileInfo().IsDir() {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(io.LimitReader(rc, maxDownload))
		}
	}
	return nil, fmt.Errorf("%s not found in archive", name)
}

func fromGzip(archive []byte) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(io.LimitReader(r, maxDownload))
}

func fromTarGz(archive []byte, member string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("%s not found in archive", member)
		}
		if err != nil {
			return nil, err
		}
		if strings.TrimPrefix(h.Name, "./") == member && h.Typeflag == tar.TypeReg {
			return io.ReadAll(io.LimitReader(tr, maxDownload))
		}
	}
}
