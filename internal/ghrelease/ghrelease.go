// Package ghrelease 读取 GitHub Release：版本、下载包的官方摘要与下载量，以及经镜像加速的下载地址。
// 摘要只取自 GitHub API；镜像只用来加速，内容由调用方按摘要校验。
package ghrelease

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Asset 是发布里的一个文件。
type Asset struct {
	Name      string `json:"name"`
	URL       string `json:"browser_download_url"`
	Digest    string `json:"digest"` // "sha256:<hex>"；旧发布可能没有
	Size      int64  `json:"size"`
	Downloads int64  `json:"download_count"`
}

// SHA256 返回官方摘要；没有时为空。
func (a Asset) SHA256() string {
	if d := strings.TrimPrefix(a.Digest, "sha256:"); len(d) == 64 {
		return d
	}
	return ""
}

// Release 是一个发布。
type Release struct {
	Tag        string    `json:"tag_name"`
	Name       string    `json:"name"`
	Draft      bool      `json:"draft"`
	Prerelease bool      `json:"prerelease"`
	Published  time.Time `json:"published_at"`
	Body       string    `json:"body"`
	HTMLURL    string    `json:"html_url"`
	Assets     []Asset   `json:"assets"`
}

// Asset 按名称找文件。
func (r Release) Asset(name string) (Asset, bool) {
	for _, a := range r.Assets {
		if a.Name == name {
			return a, true
		}
	}
	return Asset{}, false
}

// Client 访问 GitHub API。API 默认 https://api.github.com；Host 是允许的下载主机，默认 github.com。
type Client struct {
	HTTP    *http.Client
	API     string
	Host    string
	Mirrors []string // 下载加速前缀，如 https://gh-proxy.com
}

func (c Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (c Client) api() string {
	if c.API != "" {
		return strings.TrimRight(c.API, "/")
	}
	return "https://api.github.com"
}

// List 列出仓库最近的发布（新到旧，最多 perPage 个），含草稿与预发布，由调用方筛选。
func (c Client) List(ctx context.Context, repo string, perPage int) ([]Release, error) {
	if perPage <= 0 || perPage > 100 {
		perPage = 50
	}
	var out []Release
	err := c.getJSON(ctx, fmt.Sprintf("%s/repos/%s/releases?per_page=%d", c.api(), repo, perPage), &out)
	return out, err
}

// Tag 读取指定标签的发布。
func (c Client) Tag(ctx context.Context, repo, tag string) (Release, error) {
	var out Release
	err := c.getJSON(ctx, fmt.Sprintf("%s/repos/%s/releases/tags/%s", c.api(), repo, url.PathEscape(tag)), &out)
	return out, err
}

// ErrNotFound 表示仓库或标签不存在。
var ErrNotFound = errors.New("GitHub 上没有这个发布")

func (c Client) getJSON(ctx context.Context, u string, out any) error {
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Weir")
	resp, err := c.http().Do(req)
	if err != nil {
		return fmt.Errorf("无法连接 GitHub API：%w", err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case 200:
	case 404:
		return ErrNotFound
	case 403, 429:
		return fmt.Errorf("GitHub API 限流（HTTP %d），稍后再试", resp.StatusCode)
	default:
		return fmt.Errorf("GitHub API 返回 HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(out)
}

// URLs 返回下载候选地址：先直连，再经各镜像。只接受允许主机的 https 地址。
func (c Client) URLs(raw string) ([]string, error) {
	host := c.Host
	if host == "" {
		host = "github.com"
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != host {
		return nil, errors.New("下载地址不是 " + host + " 的 https 地址，拒绝")
	}
	out := []string{raw}
	for _, m := range c.Mirrors {
		out = append(out, strings.TrimRight(m, "/")+"/"+raw)
	}
	return out, nil
}

// Mirrors 返回默认镜像：WEIR_GITHUB_MIRRORS=off 关闭，或写逗号分隔的主机名替换。与 install-online.sh 一致。
func Mirrors() []string {
	raw := strings.TrimSpace(os.Getenv("WEIR_GITHUB_MIRRORS"))
	if raw == "off" {
		return nil
	}
	hosts := []string{"gh-proxy.com", "ghfast.top", "ghproxy.net", "gh.llkk.cc"}
	if raw != "" {
		hosts = strings.Split(raw, ",")
	}
	out := make([]string, 0, len(hosts))
	for _, h := range hosts {
		if h = strings.TrimSpace(h); validHost(h) {
			out = append(out, "https://"+h)
		}
	}
	return out
}

func validHost(h string) bool {
	if h == "" || len(h) > 253 || strings.HasPrefix(h, "-") {
		return false
	}
	for _, r := range h {
		if !(r == '.' || r == '-' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return strings.Contains(h, ".")
}
