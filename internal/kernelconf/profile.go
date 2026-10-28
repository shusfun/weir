package kernelconf

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Profile 是某内核某个版本区间的「版本档案」：允许的协议 / 传输 / 安全组合，
// 以及生成后需要删除的字段（该版本已移除的选项）。档案是数据文件，可单独更新，也可由 AI 出草稿。
type Profile struct {
	Kernel     string   `yaml:"kernel"`
	MinVersion string   `yaml:"min_version"`
	MaxVersion string   `yaml:"max_version,omitempty"`
	Protocols  []string `yaml:"protocols"`
	Networks   []string `yaml:"networks"`
	Securities []string `yaml:"securities"`
	Strip      []string `yaml:"strip,omitempty"` // 点分路径，* 匹配数组任意元素或对象任意键
	// Features 是生成配置要用到的内核特性（如 Xray 的 balancer、observatory，sing-box 的 urltest）。
	// 没写或为空表示一项都不支持——和协议列表「为空即不限制」的含义相反。
	Features []string `yaml:"features,omitempty"`
	Notes    string   `yaml:"notes,omitempty"`
}

// Has 判断档案是否声明了某项特性；没有声明就是不支持。
func (p Profile) Has(feature string) bool {
	for _, f := range p.Features {
		if strings.EqualFold(f, feature) {
			return true
		}
	}
	return false
}

func ParseProfile(text string) (Profile, error) {
	var p Profile
	dec := yaml.NewDecoder(strings.NewReader(text))
	dec.KnownFields(true)
	if err := dec.Decode(&p); err != nil {
		return p, fmt.Errorf("profile: %w", err)
	}
	if p.Kernel != "xray" && p.Kernel != "sing-box" {
		return p, fmt.Errorf("profile: unknown kernel %q", p.Kernel)
	}
	if p.MinVersion == "" || len(p.Protocols) == 0 {
		return p, fmt.Errorf("profile: min_version and protocols are required")
	}
	return p, nil
}

func contains(list []string, v string) bool {
	if len(list) == 0 {
		return true
	}
	for _, item := range list {
		if strings.EqualFold(item, v) {
			return true
		}
	}
	return false
}

// Allows 判断入站组合是否被该版本支持；不支持时返回明确原因，不静默降级。
func (p Profile) Allows(protocol, network, security string) error {
	switch {
	case !contains(p.Protocols, protocol):
		return fmt.Errorf("%s %s 不支持协议 %s", p.Kernel, p.MinVersion, protocol)
	case network != "" && !contains(p.Networks, network):
		return fmt.Errorf("%s %s 不支持传输 %s", p.Kernel, p.MinVersion, network)
	case security != "" && security != "none" && !contains(p.Securities, security):
		return fmt.Errorf("%s %s 不支持安全层 %s", p.Kernel, p.MinVersion, security)
	}
	return nil
}

// strip 按点分路径删除字段；路径不存在时忽略。
func strip(doc any, path []string) {
	if len(path) == 0 {
		return
	}
	switch node := doc.(type) {
	case map[string]any:
		if path[0] == "*" {
			for k := range node {
				if len(path) == 1 {
					delete(node, k)
				} else {
					strip(node[k], path[1:])
				}
			}
			return
		}
		if len(path) == 1 {
			delete(node, path[0])
			return
		}
		strip(node[path[0]], path[1:])
	case []any:
		if path[0] == "*" {
			for _, item := range node {
				strip(item, path[1:])
			}
		}
	}
}
