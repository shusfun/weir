package center

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/shusfun/weir/internal/kernelconf"
	"github.com/shusfun/weir/internal/kernels"
)

// dsh 的驱动能力：查看驱动、查看内核更新说明（只读），以及提交「新增或修改驱动」草稿。
// 草稿的试运行就是保存前检查（checkDriver）；确认窗显示与当前版本的逐行差异，确认后保存为新版本。

var driverReadCaps = []Capability{
	{Name: "list_drivers", Title: "驱动列表", Purpose: "列出全部内核驱动：名称、内核、覆盖的版本区间、继承自哪个、来源（内置 / 自定义 / 改过的内置）、是否停用，以及用到它的已下载版本与 VPS。", Kind: "read",
		read: func(ctx context.Context, s *Server, _ map[string]any) (any, error) {
			d := s.drivers(ctx)
			usage := s.driverUsage(ctx, d)
			out := []map[string]any{}
			for _, e := range d.List() {
				u := usage[e.Name]
				out = append(out, map[string]any{"name": e.Name, "kernel": e.Kernel, "min_version": e.MinVersion, "max_version": e.MaxVersion, "extends": e.Extends,
					"source": e.Source, "disabled": e.Disabled, "error": e.Error, "versions": nonNil(u.Versions), "gates": nonNil(u.Gates)})
			}
			return out, nil
		}},
	{Name: "get_driver", Title: "驱动全文", Purpose: "读取一个驱动的原文与展开 extends 后的全文。", Kind: "read",
		Params: []Param{{Key: "name", Label: "驱动名称", Kind: "string", Required: true}},
		read: func(ctx context.Context, s *Server, args map[string]any) (any, error) {
			name, _ := args["name"].(string)
			d := s.drivers(ctx)
			text, ok := d.Saved[name]
			raw := text.Text
			if !ok || raw == "" {
				raw, ok = kernels.Builtin()[name]
			}
			if !ok {
				return nil, fmt.Errorf("没有驱动 %s", name)
			}
			out := map[string]any{"name": name, "text": raw}
			if _, full, err := d.Resolve(name); err == nil {
				out["resolved"] = full
			} else {
				out["error"] = err.Error()
			}
			return out, nil
		}},
	{Name: "get_kernel_release_notes", Title: "内核更新说明", Purpose: "读取 Xray / sing-box 在两个版本之间（含 to，不含 from）的官方更新说明，最多 20 个版本。", Kind: "read",
		Params: []Param{{Key: "kernel", Label: "内核", Kind: "enum", Options: [][2]string{{"xray", "Xray"}, {"sing-box", "sing-box"}}, Required: true},
			{Key: "from", Label: "从版本", Kind: "string"}, {Key: "to", Label: "到版本", Kind: "string", Required: true}},
		read: func(ctx context.Context, s *Server, args map[string]any) (any, error) {
			kernel, _ := args["kernel"].(string)
			from, _ := args["from"].(string)
			to, _ := args["to"].(string)
			if kernel != "xray" && kernel != "sing-box" {
				return nil, errors.New("内核只能是 xray 或 sing-box")
			}
			f := s.fetcher()
			list, err := f.Releases(ctx, kernel)
			if err != nil {
				return nil, err
			}
			out := []map[string]any{}
			for _, r := range list {
				if kernelconf.CompareVersions(r.Version, to) > 0 || from != "" && kernelconf.CompareVersions(r.Version, from) <= 0 {
					continue
				}
				notes := r.Notes
				if len(notes) > 12000 {
					notes = notes[:12000] + "\n…（已截断）"
				}
				out = append(out, map[string]any{"version": r.Version, "published_at": r.PublishedAt, "notes": notes})
				if len(out) >= 20 {
					break
				}
			}
			return out, nil
		}},
}

var driverDraftCaps = []Capability{
	{Name: "driver.save", Title: "保存驱动", Purpose: "新增或修改一个内核驱动（写完整原文）。Weir 严格解析、为每个方案生成样例配置并用对应版本内核检查，改的是在用驱动时再在一台 VPS 上实测；通过才弹窗，确认后保存为新版本，旧版本保留可退回。", Kind: "draft", Risk: "normal", Object: "driver",
		Params: []Param{{Key: "name", Label: "驱动名称", Kind: "string", Required: true}, {Key: "text", Label: "驱动原文（YAML）", Kind: "text", Required: true},
			{Key: "note", Label: "说明", Kind: "string", Hint: "这次改了什么、依据哪段更新说明"}},
		plan: planDriverSave, exec: execDriverSave},
}

func planDriverSave(ctx context.Context, s *Server, p *planCtx, st *DraftStep) error {
	name, _ := st.Args["name"].(string)
	text, _ := st.Args["text"].(string)
	if got := kernelconf.RawField(text, "name"); got != name {
		return fmt.Errorf("驱动里写的名称是 %q，与 %s 不一致", got, name)
	}
	check := s.checkDriver(ctx, name, text, s.driverInUse(ctx, name))
	if check.State == "failed" {
		var logs []string
		for _, sm := range check.Samples {
			if sm.State == "failed" {
				logs = append(logs, sm.Preset+"："+sm.Log)
			}
		}
		if check.Live != nil && check.Live.State == "failed" {
			logs = append(logs, "VPS "+check.Live.Gate+" 节点 "+check.Live.Node+"："+check.Live.Error)
		}
		return fmt.Errorf("%s；%s", check.Error, strings.Join(logs, "；"))
	}
	_, isBuiltin := kernels.Builtin()[name]
	_, saved := s.drivers(ctx).Saved[name]
	switch {
	case saved || isBuiltin:
		st.Title = "修改驱动 " + name
	default:
		st.Title = "新增驱动 " + name
	}
	st.Diff = check.Diff
	st.Preview = []string{driverCheckSummary(check)}
	if check.Live != nil {
		st.Preview = append(st.Preview, "VPS 实测："+liveSummary(check.Live))
	}
	st.Result = map[string]any{"check": check}
	p.versions["driver:"+name] = driverVersion(ctx, s, name)
	return nil
}

func driverCheckSummary(c DriverCheck) string {
	passed, unknown := 0, 0
	for _, sm := range c.Samples {
		switch sm.State {
		case "passed":
			passed++
		case "unknown":
			unknown++
		}
	}
	switch c.State {
	case "passed":
		return fmt.Sprintf("%d 个方案的样例配置都通过了 %s %s 的检查", passed, c.Kernel, c.Version)
	case "unknown":
		return fmt.Sprintf("%d 个方案的样例配置都能生成；%s", unknown, c.Error)
	}
	return c.Error
}

func liveSummary(l *DriverLiveCheck) string {
	switch l.State {
	case "passed":
		return "通过（" + l.Gate + " / " + l.Node + "）"
	case "failed":
		return "未通过（" + l.Gate + " / " + l.Node + "）：" + l.Error
	}
	return l.Error
}

// driverVersion 是某驱动当前保存版本的摘要：确认时已有人改过就拒绝，让 dsh 重新生成。
func driverVersion(ctx context.Context, s *Server, name string) string {
	saved := s.drivers(ctx).Saved[name]
	return fmt.Sprintf("%t:%d:%s", saved.Disabled, len(saved.Text), settingsHash(map[string]any{"t": saved.Text}))
}

func execDriverSave(ctx context.Context, s *Server, x *execCtx, st *DraftStep) (func(context.Context) error, error) {
	name, _ := st.Args["name"].(string)
	text, _ := st.Args["text"].(string)
	note, _ := st.Args["note"].(string)
	before := s.drivers(ctx).Saved[name]
	check := s.checkDriver(ctx, name, text, false)
	if check.State == "failed" {
		return nil, errors.New(check.Error)
	}
	rev, err := s.saveDriverVersion(ctx, name, check.Kernel, text, false, "dsh 草稿："+note, "dsh", x.actor.GetInt("user_id"))
	if err != nil {
		return nil, err
	}
	s.audit(x.actor, "driver.save", fmt.Sprintf("%s #%d", name, rev), "ok")
	return func(ctx context.Context) error {
		_, err := s.saveDriverVersion(ctx, name, check.Kernel, before.Text, before.Disabled, "撤回 dsh 草稿", "admin", x.actor.GetInt("user_id"))
		return err
	}, nil
}
