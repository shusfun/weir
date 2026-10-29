package kernels

import (
	"embed"
	"fmt"
	"sort"
	"strings"

	"github.com/shusfun/weir/internal/kernelconf"
)

//go:embed drivers/*.yaml
var builtinFS embed.FS

// Builtin 返回内置驱动：名称 → 原文。
func Builtin() map[string]string {
	out := map[string]string{}
	files, _ := builtinFS.ReadDir("drivers")
	for _, f := range files {
		b, err := builtinFS.ReadFile("drivers/" + f.Name())
		if err == nil {
			out[strings.TrimSuffix(f.Name(), ".yaml")] = string(b)
		}
	}
	return out
}

// Saved 是中心保存的一个驱动版本：覆盖同名内置驱动，或新增。Disabled 表示停用；Text 为空表示恢复内置。
type Saved struct {
	Name     string
	Text     string
	Disabled bool
}

// Drivers 合并内置与保存的驱动，按名称解析 extends，按内核版本选用。
type Drivers struct {
	Saved map[string]Saved // 每个名称最新的保存版本
}

// Entry 是管理页上的一个驱动。
type Entry struct {
	Name       string `json:"name"`
	Kernel     string `json:"kernel"`
	MinVersion string `json:"min_version"`
	MaxVersion string `json:"max_version,omitempty"`
	Extends    string `json:"extends,omitempty"`
	Source     string `json:"source"` // builtin / custom / modified
	Disabled   bool   `json:"disabled"`
	Text       string `json:"text"`
	Error      string `json:"error,omitempty"`
}

// source 返回某名称当前生效的原文（停用的也能被继承，只是不参与选用）。
func (d Drivers) source(name string) (string, bool) {
	if s, ok := d.Saved[name]; ok && s.Text != "" {
		return s.Text, true
	}
	t, ok := Builtin()[name]
	return t, ok
}

// Resolve 解析某名称的驱动，返回展开后的驱动与全文。
func (d Drivers) Resolve(name string) (kernelconf.Driver, string, error) {
	text, ok := d.source(name)
	if !ok {
		return kernelconf.Driver{}, "", fmt.Errorf("没有驱动 %s", name)
	}
	drv, full, err := kernelconf.Resolve(text, d.source)
	if err != nil {
		return drv, "", fmt.Errorf("%s：%w", name, err)
	}
	if drv.Name != name {
		return drv, "", fmt.Errorf("驱动 %s 里写的名称是 %s", name, drv.Name)
	}
	return drv, full, nil
}

func (d Drivers) names() []string {
	set := map[string]bool{}
	for n := range Builtin() {
		set[n] = true
	}
	for n, s := range d.Saved {
		if s.Text != "" || set[n] {
			set[n] = true
		}
	}
	out := make([]string, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func (d Drivers) disabled(name string) bool {
	s, ok := d.Saved[name]
	return ok && s.Disabled
}

// Select 返回适用于该内核版本的驱动：版本区间覆盖它、没有停用、min_version 最高者。
func (d Drivers) Select(kernel, version string) (kernelconf.Driver, string, error) {
	var best *kernelconf.Driver
	var bestText string
	for _, name := range d.names() {
		if d.disabled(name) {
			continue
		}
		drv, full, err := d.Resolve(name)
		if err != nil {
			// 同一内核的驱动坏了：可能它本该被选中，不悄悄换成别的。
			if text, _ := d.source(name); kernelconf.RawField(text, "kernel") == kernel {
				return kernelconf.Driver{}, "", err
			}
			continue
		}
		if drv.Kernel != kernel || !drv.Covers(version) {
			continue
		}
		if best == nil || kernelconf.CompareVersions(drv.MinVersion, best.MinVersion) > 0 {
			c := drv
			best, bestText = &c, full
		}
	}
	if best == nil {
		return kernelconf.Driver{}, "", fmt.Errorf("没有适用于 %s %s 的驱动", kernel, version)
	}
	return *best, bestText, nil
}

// Newest 返回某内核 min_version 最高的可用驱动（还没有任何已下载版本时，用它的方案建节点）。
func (d Drivers) Newest(kernel string) (kernelconf.Driver, bool) {
	var best *kernelconf.Driver
	for _, name := range d.names() {
		if d.disabled(name) {
			continue
		}
		drv, _, err := d.Resolve(name)
		if err != nil || drv.Kernel != kernel {
			continue
		}
		if best == nil || kernelconf.CompareVersions(drv.MinVersion, best.MinVersion) > 0 {
			c := drv
			best = &c
		}
	}
	if best == nil {
		return kernelconf.Driver{}, false
	}
	return *best, true
}

// List 列出全部驱动（管理页与 dsh 查看用）。
func (d Drivers) List() []Entry {
	builtin := Builtin()
	out := []Entry{}
	for _, name := range d.names() {
		text, _ := d.source(name)
		e := Entry{Name: name, Text: text, Disabled: d.disabled(name), Source: "builtin"}
		if s, ok := d.Saved[name]; ok && s.Text != "" {
			e.Source = "custom"
			if _, isBuiltin := builtin[name]; isBuiltin {
				e.Source = "modified"
			}
		}
		if drv, _, err := d.Resolve(name); err != nil {
			e.Error = err.Error()
		} else {
			e.Kernel, e.MinVersion, e.MaxVersion = drv.Kernel, drv.MinVersion, drv.MaxVersion
		}
		e.Extends = kernelconf.RawField(text, "extends")
		if e.Kernel == "" {
			e.Kernel = kernelconf.RawField(text, "kernel")
		}
		out = append(out, e)
	}
	return out
}

// CompareVersions 按数字段比较版本号（忽略前缀 v 与 "-" 之后的后缀）：a<b 返回负数。
func CompareVersions(a, b string) int { return kernelconf.CompareVersions(a, b) }
