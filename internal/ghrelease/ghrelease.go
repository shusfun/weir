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
// Sources 是按优先级排好的下载来源；为空时用 Mirrors（直连在前，再经各镜像）。
type Client struct {
	HTTP    *http.Client
	API     string
	Host    string
	Mirrors []string // 下载加速前缀，如 https://gh-proxy.com
	Sources []Source
	MaxBody int64 // API 响应正文上限，默认 16 MiB
}

func (c Client) maxBody() int64 {
	if c.MaxBody > 0 {
		return c.MaxBody
	}
	return 16 << 20
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

// ErrRateLimited 表示 GitHub API 限流（HTTP 403 / 429）。
var ErrRateLimited = errors.New("GitHub API 限流，稍后再试")

// Latest 读取仓库的最新正式发布，并返回实际读到的仓库：仓库在 GitHub 上改过名时，API 会跳转到新地址，
// 调用方据此核对下载地址属于哪个仓库。跳转规则由 HTTP 客户端的 CheckRedirect 决定。
func (c Client) Latest(ctx context.Context, repo string) (Release, *url.URL, error) {
	var out Release
	final, err := c.get(ctx, fmt.Sprintf("%s/repos/%s/releases/latest", c.api(), repo), &out)
	return out, final, err
}

func (c Client) getJSON(ctx context.Context, u string, out any) error {
	_, err := c.get(ctx, u, out)
	return err
}

// get 读取一个 API 地址，返回跟随跳转后的最终地址。正文超过 MaxBody 视为无效。
func (c Client) get(ctx context.Context, u string, out any) (*url.URL, error) {
	limit := c.maxBody()
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Weir")
	resp, err := c.http().Do(req)
	if err != nil {
		return nil, fmt.Errorf("无法连接 GitHub API：%w", err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case 200:
	case 404:
		return nil, ErrNotFound
	case 403, 429:
		return nil, fmt.Errorf("%w（HTTP %d）", ErrRateLimited, resp.StatusCode)
	default:
		return nil, fmt.Errorf("GitHub API 返回 HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("读取 GitHub API 响应失败：%w", err)
	}
	if int64(len(raw)) > limit {
		return nil, errors.New("GitHub API 响应过大")
	}
	if err = json.Unmarshal(raw, out); err != nil {
		return nil, fmt.Errorf("GitHub API 响应格式无效：%w", err)
	}
	final := req.URL
	if resp.Request != nil && resp.Request.URL != nil {
		final = resp.Request.URL
	}
	return final, nil
}

// URLs 返回下载候选地址：按 Sources 的顺序（gate 来源跳过，由调用方另行中转）；没有 Sources 时先直连，
// 再经各镜像。只接受允许主机的 https 地址。
func (c Client) URLs(raw string) ([]string, error) {
	if err := c.Allowed(raw); err != nil {
		return nil, err
	}
	if len(c.Sources) > 0 {
		var out []string
		for _, s := range c.Sources {
			if u := s.URL(raw); u != "" {
				out = append(out, u)
			}
		}
		return out, nil
	}
	out := []string{raw}
	for _, m := range c.Mirrors {
		out = append(out, strings.TrimRight(m, "/")+"/"+raw)
	}
	return out, nil
}

// Allowed 检查 raw 是允许主机的 https 地址。
func (c Client) Allowed(raw string) error {
	host := c.Host
	if host == "" {
		host = "github.com"
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != host || u.User != nil {
		return errors.New("下载地址不是 " + host + " 的 https 地址，拒绝")
	}
	return nil
}

// 来源种类：直连 GitHub、加速镜像（前缀 + 原地址）、经某台 VPS 中转。
const (
	SourceDirect = "direct"
	SourceMirror = "mirror"
	SourceGate   = "gate"
)

// Source 是一个 GitHub 下载来源。整个项目只有一份按优先级排好的来源列表（中心设置「GitHub 加速」），
// 内核、dsh、Gate 安装包、中心与 Gate 的自更新、客户端安装包、一行安装命令都按它的顺序取用。
type Source struct {
	Kind   string `json:"kind"`
	Prefix string `json:"prefix,omitempty"`  // mirror：https://主机/ 或 https://主机/路径/，以 / 结尾
	GateID int64  `json:"gate_id,omitempty"` // gate：哪台 VPS
}

// DefaultSources 是没有设置时的来源：直连在前，再是默认镜像（WEIR_GITHUB_MIRRORS 可改，与安装脚本一致）。
func DefaultSources() []Source {
	out := []Source{{Kind: SourceDirect}}
	for _, m := range Mirrors() {
		out = append(out, Source{Kind: SourceMirror, Prefix: m + "/"})
	}
	return out
}

// ValidPrefix 判断镜像前缀：https、没有账号、查询与片段，以 / 结尾。
func ValidPrefix(v string) bool {
	if len(v) > 200 || !strings.HasSuffix(v, "/") || strings.ContainsAny(v, " \t\r\n") {
		return false
	}
	u, err := url.Parse(v)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && u.RawQuery == "" && u.Fragment == ""
}

// URL 返回经这个来源下载 raw 的地址；gate 来源不是 HTTP 地址，返回空。
func (s Source) URL(raw string) string {
	switch s.Kind {
	case SourceDirect:
		return raw
	case SourceMirror:
		return s.Prefix + raw
	}
	return ""
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
