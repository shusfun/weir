package kernels

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
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
	"sing-box": {repo: "shusfun/MiSub", tagPrefix: "sing-box-v", asset: func(v, arch string) string { return "sing-box-" + v + "-linux-" + arch + ".tar.gz" }, extract: func(a []byte, v, arch string) ([]byte, error) {
		return fromTarGz(a, "sing-box-"+v+"-linux-"+arch+"/sing-box")
	}},
}

// Release 是上游的一个正式版本。
type Release struct {
	Version     string    `json:"version"`
	PublishedAt time.Time `json:"published_at"`
	Notes       string    `json:"notes"`
}

// Fetcher 访问 GitHub API 列版本、下载并校验内核。Mirrors 为下载加速前缀（如 https://gh-proxy.com/），
// 摘要始终取自 GitHub API，镜像返回的内容与摘要不一致即丢弃。
type Fetcher struct {
	HTTP         *http.Client
	API          string // 默认 https://api.github.com
	DownloadHost string // 默认 github.com；只接受该主机的 https 下载地址
	Mirrors      []string
	Cache        Cache
}

type ghAsset struct {
	Name   string `json:"name"`
	URL    string `json:"browser_download_url"`
	Digest string `json:"digest"`
}

type ghRelease struct {
	Tag        string    `json:"tag_name"`
	Draft      bool      `json:"draft"`
	Prerelease bool      `json:"prerelease"`
	Published  time.Time `json:"published_at"`
	Body       string    `json:"body"`
	Assets     []ghAsset `json:"assets"`
}

func (f Fetcher) client() *http.Client {
	if f.HTTP != nil {
		return f.HTTP
	}
	return &http.Client{Timeout: 10 * time.Minute}
}

func (f Fetcher) api() string {
	if f.API != "" {
		return strings.TrimRight(f.API, "/")
	}
	return "https://api.github.com"
}

func (f Fetcher) getJSON(ctx context.Context, u string, out any) error {
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := f.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("github api HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(out)
}

func (f Fetcher) releases(ctx context.Context, kernel string) ([]ghRelease, error) {
	src, ok := sources[kernel]
	if !ok {
		return nil, fmt.Errorf("unknown kernel %q", kernel)
	}
	var all []ghRelease
	if err := f.getJSON(ctx, f.api()+"/repos/"+src.repo+"/releases?per_page=50", &all); err != nil {
		return nil, err
	}
	var out []ghRelease
	for _, r := range all {
		// sing-box 与 MiGate 程序发布在同一仓库，只认 sing-box-v 前缀；其余仓库只认正式版。
		if r.Draft || !strings.HasPrefix(r.Tag, src.tagPrefix) || src.tagPrefix == "v" && r.Prerelease {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

// Releases 列出上游正式版本（新到旧）。
func (f Fetcher) Releases(ctx context.Context, kernel string) ([]Release, error) {
	rs, err := f.releases(ctx, kernel)
	if err != nil {
		return nil, err
	}
	src := sources[kernel]
	out := make([]Release, 0, len(rs))
	for _, r := range rs {
		out = append(out, Release{Version: strings.TrimPrefix(r.Tag, src.tagPrefix), PublishedAt: r.Published, Notes: r.Body})
	}
	return out, nil
}

// Fetch 下载指定版本、按 GitHub 摘要校验、解出程序并存入缓存，返回程序摘要。
func (f Fetcher) Fetch(ctx context.Context, kernel, version, arch string) (string, error) {
	src, ok := sources[kernel]
	if !ok {
		return "", fmt.Errorf("unknown kernel %q", kernel)
	}
	rs, err := f.releases(ctx, kernel)
	if err != nil {
		return "", err
	}
	name := src.asset(version, arch)
	var asset *ghAsset
	for _, r := range rs {
		if r.Tag != src.tagPrefix+version {
			continue
		}
		for i := range r.Assets {
			if r.Assets[i].Name == name {
				asset = &r.Assets[i]
			}
		}
	}
	if asset == nil {
		return "", fmt.Errorf("%s %s 没有 %s", kernel, version, name)
	}
	want := strings.TrimPrefix(asset.Digest, "sha256:")
	if len(want) != 64 {
		return "", fmt.Errorf("%s %s 的发布没有提供摘要，拒绝下载", kernel, version)
	}
	archive, err := f.download(ctx, asset.URL, want)
	if err != nil {
		return "", err
	}
	binary, err := src.extract(archive, version, arch)
	if err != nil {
		return "", err
	}
	return f.Cache.Put(kernel, version, arch, binary)
}

func (f Fetcher) download(ctx context.Context, raw, want string) ([]byte, error) {
	host := f.DownloadHost
	if host == "" {
		host = "github.com"
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != host {
		return nil, errors.New("download URL refused")
	}
	candidates := append([]string{raw}, nil...)
	for _, m := range f.Mirrors {
		candidates = append(candidates, strings.TrimRight(m, "/")+"/"+raw)
	}
	var last error
	for _, c := range candidates {
		data, err := f.get(ctx, c)
		if err != nil {
			last = err
			continue
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != want {
			last = errors.New("archive digest mismatch")
			continue
		}
		return data, nil
	}
	return nil, last
}

func (f Fetcher) get(ctx context.Context, u string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := f.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("download HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxDownload+1))
	if err != nil || len(data) > maxDownload {
		return nil, errors.New("download too large or interrupted")
	}
	return data, nil
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
