package center

import (
	"encoding/json"
	"strings"

	"github.com/shusfun/weir/packages/contracts"
)

// 旧字段只在读取升级前数据时解析，不进入新结构或订阅运行路径。
func migratePolicyJSON(p contracts.ProxyPolicy, raw map[string]any) contracts.ProxyPolicy {
	if p.Version >= 3 {
		return p
	}
	var old struct {
		Rules  map[string]string `json:"rules"`
		Custom struct {
			Mode       string   `json:"mode"`
			SourceMode string   `json:"source_mode"`
			Lines      []string `json:"lines"`
			Source     string   `json:"source"`
		} `json:"custom"`
		Groups []map[string]json.RawMessage `json:"groups"`
	}
	b, _ := json.Marshal(raw)
	_ = json.Unmarshal(b, &old)
	convert := func(lines []string, mode string) []contracts.CustomEntry {
		out := []contracts.CustomEntry{}
		for _, r := range parseCustomEntries(strings.Join(lines, "\n")).Rules {
			target := "direct"
			if mode == "only" {
				target = "proxy"
			}
			out = append(out, contracts.CustomEntry{ID: "legacy-" + digest(r.Kind + ":" + r.Value)[:20], Kind: r.Kind, Value: r.Value, Target: target, Enabled: mode != "" && mode != "off"})
		}
		return out
	}
	if p.Custom.Version < 2 && p.Custom.Entries == nil {
		lines := old.Custom.Lines
		if old.Custom.Mode == "" && (p.Custom.Check == nil || p.Custom.Check.Status != "failed") {
			lines = customLines(parseCustomEntries(old.Custom.Source).Rules)
		}
		p.Custom.Entries = convert(lines, old.Custom.Mode)
		if p.Custom.Check != nil && p.Custom.Check.Status == "failed" {
			p.Custom.Pending = &contracts.CustomCandidate{Entries: convert(customLines(parseCustomEntries(old.Custom.Source).Rules), old.Custom.SourceMode), Source: old.Custom.Source}
		}
	}
	p.Custom.Version = 2
	for i := range p.Groups {
		if i < len(old.Groups) && old.Groups[i]["tolerance_ms"] == nil {
			p.Groups[i].ToleranceMS = 50
		}
	}
	p = migrateProxyTargets(p)
	if len(old.Rules) > 0 && len(p.TargetMigration) == 0 {
		p.TargetMigration = []string{"旧类别策略与速度选路已移除；旧类别网站可从快捷预设添加到清单"}
	}
	return p
}
