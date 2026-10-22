package ai

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// secretKeys 是节点参数里的机密字段：发给模型前替换为占位符，模型的草稿也不允许修改它们。
var secretKeys = map[string]bool{
	"reality_private_key": true, "ss_password": true, "hy2_obfs_password": true, "shadowtls_password": true,
	"wg_private_key": true, "wg_preshared_key": true, "password": true, "uuid": true,
}

// IsSecret 判断字段是否机密（按名字，含 password / private_key / token / uuid 的都算）。
func IsSecret(key string) bool {
	k := strings.ToLower(key)
	return secretKeys[k] || strings.Contains(k, "password") || strings.Contains(k, "private_key") || strings.Contains(k, "token") || strings.Contains(k, "secret") || strings.HasSuffix(k, "uuid")
}

const placeholder = "<redacted>"

// Redact 返回去掉机密值的副本（发给模型、展示差异都用它）。
func Redact(settings map[string]any) map[string]any {
	out := make(map[string]any, len(settings))
	for k, v := range settings {
		if IsSecret(k) {
			out[k] = placeholder
		} else {
			out[k] = v
		}
	}
	return out
}

var fence = regexp.MustCompile("(?s)```(?:json)?\\s*(\\{.*?\\})\\s*```")

// Patch 是模型给出的草稿：要修改的字段（null 表示删除）与一句说明。
type Patch struct {
	Set     map[string]any `json:"set"`
	Explain string         `json:"explain"`
}

// ParsePatch 从模型回答中取出 JSON 草稿；拒绝修改机密字段、写入占位符或空草稿。
func ParsePatch(answer string) (Patch, error) {
	text := strings.TrimSpace(answer)
	if m := fence.FindStringSubmatch(text); m != nil {
		text = m[1]
	} else if i, j := strings.Index(text, "{"), strings.LastIndex(text, "}"); i >= 0 && j > i {
		text = text[i : j+1]
	}
	var p Patch
	if err := json.Unmarshal([]byte(text), &p); err != nil {
		return p, errors.New("草稿不是有效的 JSON")
	}
	if len(p.Set) == 0 {
		return p, errors.New("草稿没有任何修改")
	}
	for k, v := range p.Set {
		if IsSecret(k) {
			return p, fmt.Errorf("草稿试图修改机密字段 %s", k)
		}
		if s, ok := v.(string); ok && strings.Contains(s, placeholder) {
			return p, fmt.Errorf("草稿字段 %s 含占位符", k)
		}
	}
	return p, nil
}

// Apply 把草稿合并进原参数（机密字段保持原值）。
func Apply(settings map[string]any, p Patch) map[string]any {
	out := make(map[string]any, len(settings)+len(p.Set))
	for k, v := range settings {
		out[k] = v
	}
	for k, v := range p.Set {
		if v == nil {
			delete(out, k)
		} else {
			out[k] = v
		}
	}
	return out
}

// Change 是差异中的一项（值已去机密）。
type Change struct {
	Key    string `json:"key"`
	Before any    `json:"before"`
	After  any    `json:"after"`
}

// Diff 列出新旧参数的差异，按字段名排序。
func Diff(before, after map[string]any) []Change {
	keys := map[string]bool{}
	for k := range before {
		keys[k] = true
	}
	for k := range after {
		keys[k] = true
	}
	list := make([]string, 0, len(keys))
	for k := range keys {
		list = append(list, k)
	}
	sort.Strings(list)
	rb, ra := Redact(before), Redact(after)
	var out []Change
	for _, k := range list {
		b1, _ := json.Marshal(before[k])
		a1, _ := json.Marshal(after[k])
		if string(b1) != string(a1) {
			out = append(out, Change{Key: k, Before: rb[k], After: ra[k]})
		}
	}
	return out
}

// SystemPrompt 约束模型：只输出 JSON 草稿，只用给定字段。
func SystemPrompt(fields []string) string {
	return "你是代理服务器配置助手，为 Xray / sing-box 入站调整参数。只输出一个 JSON 对象，格式为 " +
		`{"set": {"字段": 值}, "explain": "一句中文说明"}` +
		"；值为 null 表示删除该字段。只能使用这些字段：" + strings.Join(fields, ", ") +
		"。标为 <redacted> 的机密字段由系统管理，不要输出它们。不要输出其他文字。"
}
