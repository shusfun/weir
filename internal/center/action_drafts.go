package center

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// 草稿：dsh 提交 → 后端用手动操作的同一套校验试运行（不改数据）→ 不通过把原因原样交回 dsh，不打扰管理员；
// 通过则保存，控制台弹出 Weir 自己的通用确认窗。一份草稿可以有多步，后面的步骤用 "$名字" 引用前面新建的对象；
// 一次确认、整体生效，任一步失败已执行的步骤倒序撤回。草稿记下所依据的数据版本，1 小时未处理自动作废。

var refNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,23}$`)

// submitActionDraft 校验并试运行一份草稿；不通过返回原因，通过后保存等管理员确认。
func (s *Server) submitActionDraft(ctx context.Context, source, explain string, steps []DraftStep) (*Draft, error) {
	explain = strings.TrimSpace(explain)
	if explain == "" || len(explain) > 2000 {
		return nil, errors.New("explain 必须是一句不超过 2000 字的说明")
	}
	if len(steps) == 0 || len(steps) > 10 {
		return nil, errors.New("一份草稿要有 1 到 10 步")
	}
	d := &Draft{ID: randomToken(12), Kind: "actions", Source: source, Explain: explain, CreatedAt: time.Now().UTC(), Steps: steps}
	if err := s.planDraft(ctx, d); err != nil {
		return nil, err
	}
	d.Status = "passed"
	if err := s.saveDraft(ctx, d); err != nil {
		return nil, err
	}
	for _, st := range d.Steps {
		if st.Action == "defense.block" {
			s.markEvent(ctx, intArg(st.Args, "event_id"), "drafted", "")
		}
	}
	_, _ = s.Store.Ent.Audit.Create().SetAction("ai.draft").SetTarget(source + " " + d.ID).SetResult("passed").Save(ctx)
	return d, nil
}

// planDraft 逐步校验并试运行，填好确认窗的字段与预演，记下所依据的数据版本（d.Versions）。
func (s *Server) planDraft(ctx context.Context, d *Draft) error {
	p := newPlanCtx(d.Source)
	d.Danger = false
	for i := range d.Steps {
		st := &d.Steps[i]
		c, ok := findCap(d.Source, st.Action)
		if !ok || c.Kind != "draft" {
			return fmt.Errorf("第 %d 步：没有能力 %q（用 list_actions 查看可以提交的能力）", i+1, st.Action)
		}
		if c.Last && i != len(d.Steps)-1 {
			return fmt.Errorf("第 %d 步：%s 不能撤回，只能是最后一步", i+1, c.Title)
		}
		if st.Args == nil {
			st.Args = map[string]any{}
		}
		if err := s.checkArgs(ctx, p, c.Params, st.Args); err != nil {
			return fmt.Errorf("第 %d 步（%s）：%v", i+1, c.Title, err)
		}
		if st.As != "" {
			if !c.Creates {
				return fmt.Errorf("第 %d 步：%s 不新建对象，不能用 as", i+1, c.Title)
			}
			if !refNamePattern.MatchString(st.As) || p.refs[st.As] != "" {
				return fmt.Errorf("第 %d 步：as 必须是小写字母开头、不重复的名字", i+1)
			}
		}
		st.Risk, st.Preview, st.Fields = c.Risk, nil, s.fields(ctx, p, c.Params, st.Args)
		if err := c.plan(ctx, s, p, st); err != nil {
			return fmt.Errorf("第 %d 步（%s）：%v", i+1, c.Title, err)
		}
		if st.Title == "" {
			st.Title = c.Title
		}
		if st.As != "" {
			p.refs[st.As] = c.Object
			email, _ := st.Args["email"].(string)
			name, _ := st.Args["name"].(string)
			p.names[st.As] = firstNonEmpty(email, name, c.Title)
		}
		d.Danger = d.Danger || c.Risk == "danger"
	}
	d.Versions = p.versions
	return nil
}

// applyActionDraft 以点确认的管理员身份执行。管理员可以在确认窗里改字段（edits: 步骤序号 → 字段 → 新值），
// 改过的字段做标记并重新试运行；相关数据自起草后被改动则拒绝；有危险步骤时要求当前密码与二次验证码。
func (s *Server) applyActionDraft(c *gin.Context, d *Draft) {
	var req struct {
		Edits    map[string]map[string]any `json:"edits"`
		Password string                    `json:"password"`
		Code     string                    `json:"code"`
	}
	_ = c.ShouldBindJSON(&req)
	ctx := c.Request.Context()
	before := d.Versions
	// 先只读一遍引用对象的版本：数据已被改动就直接拒绝，不在旧数据上再跑一次实测。
	quick := newPlanCtx(d.Source)
	for _, st := range d.Steps {
		if capb, ok := findCap(d.Source, st.Action); ok {
			_ = s.checkArgs(ctx, quick, capb.Params, st.Args)
		}
		if st.As != "" {
			if capb, ok := findCap(d.Source, st.Action); ok {
				quick.refs[st.As] = capb.Object
			}
		}
	}
	for k := range before {
		if name, ok := strings.CutPrefix(k, "driver:"); ok {
			quick.versions[k] = driverVersion(ctx, s, name)
		}
	}
	for k, v := range before {
		if quick.versions[k] != v {
			failDetail(c, 409, "draft_stale", "草稿依据的数据已被改动，请让 dsh 重新生成", nil)
			return
		}
	}
	for idx, fields := range req.Edits {
		i, err := strconv.Atoi(idx)
		if err != nil || i < 0 || i >= len(d.Steps) {
			failure(c, 400, "invalid_edit")
			return
		}
		st := &d.Steps[i]
		for k, v := range fields {
			if key, ok := strings.CutPrefix(k, "set."); ok {
				set, _ := st.Args["set"].(map[string]any)
				if set == nil {
					set = map[string]any{}
				}
				set[key] = v
				st.Args["set"] = set
			} else {
				st.Args[k] = v
			}
			st.Edited = append(st.Edited, k)
		}
	}
	if err := s.planDraft(ctx, d); err != nil {
		failDetail(c, 422, "draft_invalid", err.Error(), nil)
		return
	}
	for k, v := range before {
		if d.Versions[k] != v {
			failDetail(c, 409, "draft_stale", "草稿依据的数据已被改动，请让 dsh 重新生成", nil)
			return
		}
	}
	if d.Danger && !s.reauth(c, req.Password, req.Code) {
		return
	}
	x := &execCtx{actor: c, created: map[string]int{}}
	var undo []func(context.Context) error
	for i := range d.Steps {
		st := &d.Steps[i]
		capb, _ := findCap(d.Source, st.Action)
		u, err := capb.exec(ctx, s, x, st)
		if err != nil {
			var failed []string
			for j := len(undo) - 1; j >= 0; j-- {
				if undo[j] != nil {
					if e := undo[j](ctx); e != nil {
						failed = append(failed, e.Error())
					}
				}
			}
			msg := fmt.Sprintf("第 %d 步（%s）失败：%v。前面的步骤已撤回，草稿没有生效。", i+1, st.Title, err)
			if len(failed) > 0 {
				msg += "撤回时出错：" + strings.Join(failed, "；")
			}
			failDetail(c, 422, "draft_failed", msg, nil)
			return
		}
		undo = append(undo, u)
	}
	_ = s.Store.KV.Del(ctx, draftPrefix+d.ID)
	steps := make([]map[string]any, 0, len(d.Steps))
	for _, st := range d.Steps {
		steps = append(steps, map[string]any{"action": st.Action, "title": st.Title, "args": st.Args, "edited": st.Edited})
	}
	_, _ = s.Store.Ent.Audit.Create().SetActor(c.GetInt("user_id")).SetAction("ai.apply").SetTarget("dsh 草稿 · 管理员确认 " + d.ID).SetResult("ok").SetIP(c.ClientIP()).
		SetDetail(map[string]any{"source": d.Source, "explain": d.Explain, "steps": steps}).Save(ctx)
	c.JSON(200, gin.H{"applied": len(d.Steps), "outputs": x.outputs})
}
