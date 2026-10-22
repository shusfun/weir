// Package kernels 负责中心侧的内核：版本档案选择、从上游拉取与校验、本地缓存。
// 内核只作为独立程序调用，不引入它们的 Go 包，内核更新不需要 MiGate 发版。
package kernels

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/imzyb/MiGate/internal/kernelconf"
)

//go:embed profiles/*.yaml
var builtin embed.FS

// CompareVersions 按数字段比较版本号（忽略前缀 v 与 "-" 之后的后缀）：a<b 返回负数。
func CompareVersions(a, b string) int {
	parse := func(v string) []int {
		v = strings.TrimPrefix(strings.TrimSpace(v), "v")
		if i := strings.IndexAny(v, "-+"); i >= 0 {
			v = v[:i]
		}
		var out []int
		for _, part := range strings.Split(v, ".") {
			n, _ := strconv.Atoi(part)
			out = append(out, n)
		}
		return out
	}
	x, y := parse(a), parse(b)
	for i := 0; i < len(x) || i < len(y); i++ {
		var p, q int
		if i < len(x) {
			p = x[i]
		}
		if i < len(y) {
			q = y[i]
		}
		if p != q {
			return p - q
		}
	}
	return 0
}

// Profiles 读取内置档案，再用覆盖目录中同名文件替换（AI 修改与管理员调整写在覆盖目录）。
type Profiles struct {
	OverrideDir string
}

type entry struct {
	text    string
	profile kernelconf.Profile
}

func (p Profiles) load() (map[string]entry, error) {
	out := map[string]entry{}
	files, err := builtin.ReadDir("profiles")
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		b, err := builtin.ReadFile("profiles/" + f.Name())
		if err != nil {
			return nil, err
		}
		out[f.Name()] = entry{text: string(b)}
	}
	if p.OverrideDir != "" {
		matches, _ := filepath.Glob(filepath.Join(p.OverrideDir, "*.yaml"))
		for _, path := range matches {
			b, err := os.ReadFile(path)
			if err != nil {
				return nil, err
			}
			out[filepath.Base(path)] = entry{text: string(b)}
		}
	}
	for name, e := range out {
		prof, err := kernelconf.ParseProfile(e.text)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		e.profile = prof
		out[name] = e
	}
	return out, nil
}

// Select 返回适用于该内核版本的档案原文：min_version ≤ 版本 ≤ max_version 中 min_version 最高者。
func (p Profiles) Select(kernel, version string) (string, error) {
	all, err := p.load()
	if err != nil {
		return "", err
	}
	var best *entry
	names := make([]string, 0, len(all))
	for name := range all {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		e := all[name]
		pr := e.profile
		if pr.Kernel != kernel || CompareVersions(version, pr.MinVersion) < 0 || pr.MaxVersion != "" && CompareVersions(version, pr.MaxVersion) > 0 {
			continue
		}
		if best == nil || CompareVersions(pr.MinVersion, best.profile.MinVersion) > 0 {
			copy := e
			best = &copy
		}
	}
	if best == nil {
		return "", fmt.Errorf("没有适用于 %s %s 的版本档案", kernel, version)
	}
	return best.text, nil
}

// List 列出全部档案（文件名 → 原文），供管理页面与 AI 助手查看。
func (p Profiles) List() (map[string]string, error) {
	all, err := p.load()
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(all))
	for name, e := range all {
		out[name] = e.text
	}
	return out, nil
}
