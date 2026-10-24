// Package dsh 托管 DeepSeek Harness：中心下载 Node.js 运行环境与 dsh 官方 npm 包（镜像回退、官方摘要校验），
// 按版本存放，可切换与回退；dsh 以独立子进程运行，只监听本机回环。
//
// 目录（Dir 下）：node/<Node 版本>/ 运行环境；versions/<dsh 版本>/ npm 安装前缀；home/ 是 DSH_HOME（会话、
// dsh 自己保存的 Key）；work/ 是工作目录；backups/ 是切换版本前的 home 备份。
package dsh

import (
	"archive/tar"
	"bufio"
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
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Package 是 dsh 的官方 npm 包名；只认这个名字，同名的第三方包不会被当成本体。
const Package = "@deepseek-ai/dsh"

// 部分依赖要求 Node ≥ 22.19；运行环境固定在 22.x 中满足要求的最新版本。
const (
	nodeMajor    = 22
	nodeMinMinor = 19
)

var safeVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$`)

// ValidVersion 判断 dsh 版本号是否合法（防止路径注入）。
func ValidVersion(v string) bool { return len(v) <= 64 && safeVersion.MatchString(v) }

// Supported 报告本机能否托管 dsh：只支持 Linux amd64 / arm64。
func Supported() error {
	if runtime.GOOS != "linux" {
		return errors.New("dsh 只能托管在 Linux 中心上")
	}
	if nodeArch() == "" {
		return fmt.Errorf("不支持的架构 %s", runtime.GOARCH)
	}
	return nil
}

func nodeArch() string {
	switch runtime.GOARCH {
	case "amd64":
		return "x64"
	case "arm64":
		return "arm64"
	}
	return ""
}

// Installer 下载并校验 Node.js 与 dsh。官方地址提供摘要，镜像只用来加速下载。
type Installer struct {
	Dir  string
	HTTP *http.Client
	// NodeOfficial 默认 https://nodejs.org/dist；摘要 SHASUMS256.txt 只从这里取。
	NodeOfficial string
	NodeMirrors  []string
	// Registries 的第一个是官方源（摘要以它为准），其余是安装时的回退源。
	Registries []string
}

// DefaultInstaller 使用官方源与国内镜像；MIGATE_NPM_REGISTRIES / MIGATE_NODE_MIRRORS 可用逗号分隔替换回退源。
func DefaultInstaller(dir string) Installer {
	regs := []string{"https://registry.npmjs.org", "https://registry.npmmirror.com"}
	if v := os.Getenv("MIGATE_NPM_REGISTRIES"); v != "" {
		regs = append(regs[:1], splitList(v)...)
	}
	mirrors := []string{"https://npmmirror.com/mirrors/node"}
	if v := os.Getenv("MIGATE_NODE_MIRRORS"); v != "" {
		mirrors = splitList(v)
	}
	return Installer{Dir: dir, HTTP: &http.Client{Timeout: 10 * time.Minute}, NodeOfficial: "https://nodejs.org/dist", NodeMirrors: mirrors, Registries: regs}
}

func splitList(v string) []string {
	var out []string
	for _, s := range strings.Split(v, ",") {
		if s = strings.TrimRight(strings.TrimSpace(s), "/"); strings.HasPrefix(s, "https://") {
			out = append(out, s)
		}
	}
	return out
}

func (in Installer) client() *http.Client {
	if in.HTTP != nil {
		return in.HTTP
	}
	return &http.Client{Timeout: 10 * time.Minute}
}

func (in Installer) get(ctx context.Context, u string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := in.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("%s 返回 HTTP %d", hostOf(u), resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil || int64(len(b)) > limit {
		return nil, fmt.Errorf("%s 下载中断或过大", hostOf(u))
	}
	return b, nil
}

func hostOf(u string) string {
	if p, err := url.Parse(u); err == nil {
		return p.Host
	}
	return u
}

// Release 是 dsh 的一个发布版本。
type Release struct {
	Version     string    `json:"version"`
	PublishedAt time.Time `json:"published_at"`
	Tags        []string  `json:"tags,omitempty"` // latest / next / alpha
}

// Releases 列出最近的版本（新到旧），并标出发布通道。
func (in Installer) Releases(ctx context.Context) ([]Release, error) {
	var doc struct {
		Tags     map[string]string    `json:"dist-tags"`
		Time     map[string]time.Time `json:"time"`
		Versions map[string]any       `json:"versions"`
	}
	var last error
	for _, reg := range in.Registries {
		b, err := in.get(ctx, reg+"/"+url.PathEscape(Package), 64<<20)
		if err == nil {
			if err = json.Unmarshal(b, &doc); err == nil {
				last = nil
				break
			}
		}
		last = err
	}
	if last != nil {
		return nil, last
	}
	tags := map[string][]string{}
	for tag, v := range doc.Tags {
		tags[v] = append(tags[v], tag)
	}
	var out []Release
	for v := range doc.Versions {
		if ValidVersion(v) {
			sort.Strings(tags[v])
			out = append(out, Release{Version: v, PublishedAt: doc.Time[v], Tags: tags[v]})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PublishedAt.After(out[j].PublishedAt) })
	if len(out) > 20 {
		out = out[:20]
	}
	return out, nil
}

// NodeBin 返回已安装的运行环境中 node 程序的路径；没有满足要求的版本时返回空。
func (in Installer) NodeBin() string {
	entries, _ := os.ReadDir(filepath.Join(in.Dir, "node"))
	best, bestBin := [3]int{}, ""
	for _, e := range entries {
		v, ok := parseNodeVersion(e.Name())
		bin := filepath.Join(in.Dir, "node", e.Name(), "bin", "node")
		if _, err := os.Stat(bin); ok && err == nil && newer(v, best) {
			best, bestBin = v, bin
		}
	}
	return bestBin
}

func parseNodeVersion(s string) ([3]int, bool) {
	var v [3]int
	parts := strings.Split(strings.TrimPrefix(s, "v"), ".")
	if len(parts) != 3 {
		return v, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return v, false
		}
		v[i] = n
	}
	return v, v[0] == nodeMajor && v[1] >= nodeMinMinor
}

func newer(a, b [3]int) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

// EnsureNode 确保有满足要求的 Node.js；没有时下载 22.x 最新版：先试镜像，内容必须与 nodejs.org 的 SHASUMS256 一致。
func (in Installer) EnsureNode(ctx context.Context) (string, error) {
	if bin := in.NodeBin(); bin != "" {
		return bin, nil
	}
	arch := nodeArch()
	if arch == "" {
		return "", Supported()
	}
	var index []struct {
		Version string `json:"version"`
	}
	b, err := in.get(ctx, in.NodeOfficial+"/index.json", 8<<20)
	for i := 0; err != nil && i < len(in.NodeMirrors); i++ {
		b, err = in.get(ctx, in.NodeMirrors[i]+"/index.json", 8<<20)
	}
	if err != nil {
		return "", fmt.Errorf("无法获取 Node.js 版本列表：%w", err)
	}
	if err = json.Unmarshal(b, &index); err != nil {
		return "", errors.New("Node.js 版本列表格式无效")
	}
	version, best := "", [3]int{}
	for _, r := range index {
		if v, ok := parseNodeVersion(r.Version); ok && newer(v, best) {
			version, best = strings.TrimPrefix(r.Version, "v"), v
		}
	}
	if version == "" {
		return "", errors.New("没有找到满足要求的 Node.js 22 版本")
	}
	name := fmt.Sprintf("node-v%s-linux-%s.tar.gz", version, arch)
	sums, err := in.get(ctx, fmt.Sprintf("%s/v%s/SHASUMS256.txt", in.NodeOfficial, version), 1<<20)
	if err != nil {
		return "", fmt.Errorf("无法从 nodejs.org 获取官方摘要：%w", err)
	}
	want := ""
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		if f := strings.Fields(sc.Text()); len(f) == 2 && f[1] == name && len(f[0]) == 64 {
			want = f[0]
		}
	}
	if want == "" {
		return "", fmt.Errorf("官方摘要中没有 %s", name)
	}
	// 镜像优先（国内更快），官方地址兜底；任何来源的内容都要与官方摘要一致。
	var archive []byte
	last := errors.New("没有可用的下载地址")
	for _, base := range append(append([]string{}, in.NodeMirrors...), in.NodeOfficial) {
		data, err := in.get(ctx, fmt.Sprintf("%s/v%s/%s", base, version, name), 200<<20)
		if err != nil {
			last = err
			continue
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != want {
			last = fmt.Errorf("%s 提供的 Node.js 与官方摘要不一致", hostOf(base))
			continue
		}
		archive = data
		break
	}
	if archive == nil {
		return "", last
	}
	dest := filepath.Join(in.Dir, "node", "v"+version)
	tmp := dest + ".new"
	_ = os.RemoveAll(tmp)
	if err = extractTarGz(archive, tmp, 1); err != nil {
		_ = os.RemoveAll(tmp)
		return "", err
	}
	_ = os.RemoveAll(dest)
	if err = os.Rename(tmp, dest); err != nil {
		return "", err
	}
	publish(in.Dir)
	return filepath.Join(dest, "bin", "node"), nil
}

// Publish 让中心写入的插件文件对 dsh 管理进程可读。
func Publish(root string) { publish(root) }

// publish 让托管运行环境对 dsh 管理进程只读可用：目录 0755、可执行文件 0755、其他 0644。
// 中心以 UMask=0077 运行，下载与 npm 安装出来的文件默认只有中心自己能读。
func publish(root string) {
	_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		switch {
		case info.IsDir():
			_ = os.Chmod(p, 0o755)
		case info.Mode()&0o111 != 0:
			_ = os.Chmod(p, 0o755)
		default:
			_ = os.Chmod(p, 0o644)
		}
		return nil
	})
}

// extractTarGz 解压到 dest，去掉前 strip 层目录；拒绝越出目标目录的路径与链接。
func extractTarGz(archive []byte, dest string, strip int) error {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	root, _ := filepath.Abs(dest)
	inside := func(p string) bool {
		return p == root || strings.HasPrefix(p, root+string(filepath.Separator))
	}
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		parts := strings.Split(strings.Trim(filepath.ToSlash(h.Name), "/"), "/")
		if len(parts) <= strip {
			continue
		}
		target := filepath.Join(root, filepath.FromSlash(strings.Join(parts[strip:], "/")))
		if !inside(target) {
			return fmt.Errorf("压缩包含非法路径 %s", h.Name)
		}
		switch h.Typeflag {
		case tar.TypeDir:
			err = os.MkdirAll(target, 0o755)
		case tar.TypeReg:
			if err = os.MkdirAll(filepath.Dir(target), 0o755); err == nil {
				var f *os.File
				if f, err = os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(h.Mode)&0o755|0o644); err == nil {
					_, err = io.Copy(f, tr)
					if cerr := f.Close(); err == nil {
						err = cerr
					}
				}
			}
		case tar.TypeSymlink:
			link := filepath.Join(filepath.Dir(target), h.Linkname)
			if filepath.IsAbs(h.Linkname) || !inside(link) {
				return fmt.Errorf("压缩包含非法链接 %s", h.Name)
			}
			if err = os.MkdirAll(filepath.Dir(target), 0o755); err == nil {
				_ = os.Remove(target)
				err = os.Symlink(h.Linkname, target)
			}
		}
		if err != nil {
			return err
		}
	}
}

// Installed 是已安装的一个 dsh 版本。
type Installed struct {
	Version     string    `json:"version"`
	Integrity   string    `json:"integrity"`
	Registry    string    `json:"registry"`
	InstalledAt time.Time `json:"installed_at"`
}

func (in Installer) versionDir(v string) string { return filepath.Join(in.Dir, "versions", v) }

// Bin 返回某个已安装版本的启动脚本。
func (in Installer) Bin(v string) string {
	return filepath.Join(in.versionDir(v), "node_modules", "@deepseek-ai", "dsh", "lib", "bin.js")
}

// List 列出已安装的版本（新到旧）。
func (in Installer) List() []Installed {
	entries, _ := os.ReadDir(filepath.Join(in.Dir, "versions"))
	var out []Installed
	for _, e := range entries {
		var meta Installed
		b, err := os.ReadFile(filepath.Join(in.Dir, "versions", e.Name(), ".migate.json"))
		if err != nil || json.Unmarshal(b, &meta) != nil || meta.Version != e.Name() {
			continue
		}
		out = append(out, meta)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].InstalledAt.After(out[j].InstalledAt) })
	return out
}

// Has 判断某个版本是否已完整安装。
func (in Installer) Has(v string) bool {
	for _, i := range in.List() {
		if i.Version == v {
			return true
		}
	}
	return false
}

// officialIntegrity 只从官方源读取该版本的 sha512 摘要。
func (in Installer) officialIntegrity(ctx context.Context, v string) (string, error) {
	var meta struct {
		Name string `json:"name"`
		Dist struct {
			Integrity string `json:"integrity"`
		} `json:"dist"`
	}
	b, err := in.get(ctx, in.Registries[0]+"/"+url.PathEscape(Package)+"/"+v, 4<<20)
	if err != nil {
		return "", fmt.Errorf("无法从官方源获取摘要：%w", err)
	}
	if json.Unmarshal(b, &meta) != nil || meta.Name != Package || !strings.HasPrefix(meta.Dist.Integrity, "sha512-") {
		return "", errors.New("官方源返回的版本信息无效")
	}
	return meta.Dist.Integrity, nil
}

// Install 安装一个 dsh 版本：用托管的 Node 自带的 npm 安装（--ignore-scripts，服务器上不执行任何安装脚本、
// 不编译），依次尝试各个源；装好后核对锁文件中 dsh 本体的摘要与官方源一致，不一致就丢弃。
func (in Installer) Install(ctx context.Context, v string) (Installed, error) {
	if !ValidVersion(v) {
		return Installed{}, errors.New("版本号无效")
	}
	if err := Supported(); err != nil {
		return Installed{}, err
	}
	want, err := in.officialIntegrity(ctx, v)
	if err != nil {
		return Installed{}, err
	}
	node, err := in.EnsureNode(ctx)
	if err != nil {
		return Installed{}, err
	}
	npm := filepath.Join(filepath.Dir(filepath.Dir(node)), "lib", "node_modules", "npm", "bin", "npm-cli.js")
	dest := in.versionDir(v)
	tmp := dest + ".new"
	last := errors.New("没有可用的 npm 源")
	for _, reg := range in.Registries {
		_ = os.RemoveAll(tmp)
		if err = os.MkdirAll(tmp, 0o755); err != nil {
			return Installed{}, err
		}
		_ = os.WriteFile(filepath.Join(tmp, "package.json"), []byte(`{"name":"migate-dsh","private":true}`), 0o644)
		cmd := exec.CommandContext(ctx, node, npm, "install", "--prefix", tmp, "--ignore-scripts", "--omit=dev", "--no-audit", "--no-fund", "--no-update-notifier", "--registry", reg+"/", Package+"@"+v)
		cmd.Env = []string{"PATH=" + filepath.Dir(node) + ":/usr/local/bin:/usr/bin:/bin", "HOME=" + filepath.Join(in.Dir, "user"), "npm_config_cache=" + filepath.Join(in.Dir, "npm-cache"), "LANG=C.UTF-8"}
		out, runErr := cmd.CombinedOutput()
		if runErr != nil {
			last = fmt.Errorf("从 %s 安装失败：%s", hostOf(reg), tail(string(out), 400))
			continue
		}
		got, lockErr := lockIntegrity(filepath.Join(tmp, "package-lock.json"))
		if lockErr != nil || got != want {
			last = fmt.Errorf("%s 提供的 dsh 与官方摘要不一致", hostOf(reg))
			continue
		}
		meta := Installed{Version: v, Integrity: want, Registry: hostOf(reg), InstalledAt: time.Now().UTC()}
		b, _ := json.Marshal(meta)
		if err = os.WriteFile(filepath.Join(tmp, ".migate.json"), b, 0o644); err != nil {
			return Installed{}, err
		}
		_ = os.RemoveAll(dest)
		if err = os.Rename(tmp, dest); err != nil {
			return Installed{}, err
		}
		publish(in.Dir)
		return meta, nil
	}
	_ = os.RemoveAll(tmp)
	return Installed{}, last
}

func lockIntegrity(path string) (string, error) {
	var lock struct {
		Packages map[string]struct {
			Version   string `json:"version"`
			Integrity string `json:"integrity"`
		} `json:"packages"`
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if err = json.Unmarshal(b, &lock); err != nil {
		return "", err
	}
	return lock.Packages["node_modules/"+Package].Integrity, nil
}

// Remove 删除一个已安装的版本。
func (in Installer) Remove(v string) error {
	if !ValidVersion(v) {
		return errors.New("版本号无效")
	}
	return os.RemoveAll(in.versionDir(v))
}

func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		s = "…" + s[len(s)-n:]
	}
	return s
}
