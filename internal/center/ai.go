package center

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/shusfun/weir/internal/ai"
	"github.com/shusfun/weir/internal/db"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/setting"
	"github.com/shusfun/weir/internal/subout"
	"github.com/shusfun/weir/packages/contracts"
)

// AI 草稿：托管的 dsh 通过 Weir 的 MCP 工具提交草稿 → 系统按内核版本试生成并在 Gate 本机实测 →
// 实测结果交回 dsh（未通过时它可以修改后再交）→ 控制台弹出 Weir 自己的确认窗，管理员确认后才写入。
// dsh 拿到的节点参数不含任何密码、UUID、密钥与订阅令牌；dsh 的令牌只能提交草稿，不能确认。

const (
	draftTTL       = time.Hour
	draftPrefix    = "ai-draft:"
	maxDraftsLimit = 50
)

func (s *Server) aiRoutes(admin *gin.RouterGroup) {
	admin.GET("/ai/drafts", s.listDrafts)
	admin.GET("/ai/drafts/:id", s.getDraft)
	admin.POST("/ai/drafts/:id/apply", s.applyDraft)
	admin.DELETE("/ai/drafts/:id", s.discardDraft)
	admin.POST("/nodes/:id/test", s.testNodeHandler)
}

// editableFields 是草稿可以修改的入站字段：构建器认识的字段，去掉系统管理与机密字段。
var editableFields = func() []string {
	var out []string
	t := reflect.TypeOf(db.Inbound{})
	for i := 0; i < t.NumField(); i++ {
		name := strings.Split(t.Field(i).Tag.Get("json"), ",")[0]
		if name == "" || name == "-" || lockedSettings[name] || ai.IsSecret(name) {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}()

func editable(field string) bool {
	i := sort.SearchStrings(editableFields, field)
	return i < len(editableFields) && editableFields[i] == field
}

// testNode 让节点所在 Gate 用给定参数实测：内核自检 + 本机回环真实连接下载。
func (s *Server) testNode(ctx context.Context, n *ent.Node, settings map[string]any) (map[string]any, error) {
	g, err := s.Store.Ent.Gate.Get(ctx, n.GateID)
	if err != nil {
		return nil, err
	}
	kernels, profiles, err := s.gateKernels(ctx, g, map[string]bool{n.Kernel: true, "sing-box": true})
	if err != nil {
		return nil, err
	}
	id, password := newSecret(n.Protocol)
	in := contracts.Inbound{ID: 1, Tag: "test", Kernel: n.Kernel, Protocol: n.Protocol, Network: n.Network, Security: n.Security, Settings: settings}
	cfg := contracts.GateConfig{Kernels: kernels, Profiles: profiles, Inbounds: []contracts.Inbound{in},
		Credentials: []contracts.Credential{{ID: 1, InboundID: 1, StatsKey: "test", UserKey: "test", UUID: id, Password: password}}, Budgets: map[string]contracts.GateBudget{"test": {Unlimited: true}}}
	var cert *ent.Certificate
	if n.CertID != nil {
		if cert, err = s.Store.Ent.Certificate.Get(ctx, *n.CertID); err != nil {
			return nil, err
		}
		cfg.Inbounds[0].CertID = int64(cert.ID)
		cfg.Certs = []contracts.CertRef{{ID: int64(cert.ID), SHA256: cert.Sha256}}
		// 节点可能尚未保存（建节点前实测）：短时允许该 Gate 下载这张证书。
		_ = s.Store.KV.Set(ctx, fmt.Sprintf("cert-grant:%d:%d", g.ID, cert.ID), "1", 2*time.Minute)
	}
	probe := *n
	probe.Settings, probe.Port = settings, 1
	link, err := shareLink(&probe, "127.0.0.1", &ent.Credential{UUID: id, Password: password}, "test", cert)
	if err != nil {
		return nil, err
	}
	body, _, err := subout.Render([]string{link}, "singbox")
	if err != nil {
		return nil, fmt.Errorf("客户端配置无法生成：%w", err)
	}
	var doc struct {
		Outbounds []map[string]any `json:"outbounds"`
	}
	if err = json.Unmarshal(body, &doc); err != nil {
		return nil, err
	}
	var client map[string]any
	for _, o := range doc.Outbounds {
		if o["type"] != "selector" {
			client = o
		}
	}
	if client == nil {
		return nil, errors.New("客户端配置为空")
	}
	var payload map[string]any
	raw, _ := json.Marshal(map[string]any{"config": cfg, "client": client})
	_ = json.Unmarshal(raw, &payload)
	return s.gateProbe(ctx, g, contracts.ActionNodeTest, payload, 50*time.Second)
}

func (s *Server) testNodeHandler(c *gin.Context) {
	n, ok := s.nodeParam(c)
	if !ok {
		return
	}
	res, err := s.testNode(c.Request.Context(), n, n.Settings)
	if err != nil {
		failDetail(c, 422, "node_test_failed", err.Error(), gin.H{"result": res})
		return
	}
	c.JSON(200, res)
}

// Draft 是一份待确认的 AI 草稿（保存在 KV，1 小时后过期）。Kind 为 node（节点参数）或 defense（防御动作）。
// 只有 status=passed 的草稿会弹出确认窗；failed 的草稿只用于把原因交回 dsh。
type Draft struct {
	ID        string         `json:"id"`
	Kind      string         `json:"kind"`
	Source    string         `json:"source"` // dsh（抽屉对话）/ analysis（后台分析）
	Status    string         `json:"status"` // passed / failed
	Error     string         `json:"error,omitempty"`
	Explain   string         `json:"explain"`
	CreatedAt time.Time      `json:"created_at"`
	NodeID    int            `json:"node_id,omitempty"`
	NodeName  string         `json:"node_name,omitempty"`
	Changes   []ai.Change    `json:"changes,omitempty"`
	Result    map[string]any `json:"result,omitempty"` // Gate 实测详情（不含机密）
	PatchRaw  map[string]any `json:"patch,omitempty"`
	BaseHash  string         `json:"base_hash,omitempty"`
	Defense   *DefenseAction `json:"defense,omitempty"`
	Plugin    *pluginDraft   `json:"plugin,omitempty"`
}

func settingsHash(m map[string]any) string {
	b, _ := json.Marshal(m)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func (s *Server) saveDraft(ctx context.Context, d *Draft) error {
	b, err := json.Marshal(d)
	if err != nil {
		return err
	}
	return s.Store.KV.Set(ctx, draftPrefix+d.ID, string(b), draftTTL)
}

func (s *Server) loadDraft(ctx context.Context, id string) (*Draft, error) {
	if id == "" || len(id) > 64 {
		return nil, errors.New("draft_not_found")
	}
	raw, ok, err := s.Store.KV.Get(ctx, draftPrefix+id)
	if err != nil || !ok {
		return nil, errors.New("draft_not_found")
	}
	var d Draft
	if err = json.Unmarshal([]byte(raw), &d); err != nil {
		return nil, err
	}
	return &d, nil
}

// pendingDrafts 返回等待确认的草稿（旧到新），控制台据此依次弹出确认窗。
func (s *Server) pendingDrafts(ctx context.Context) ([]*Draft, error) {
	keys, err := s.Store.KV.Keys(ctx, draftPrefix, 500)
	if err != nil {
		return nil, err
	}
	var out []*Draft
	for _, k := range keys {
		if d, err := s.loadDraft(ctx, strings.TrimPrefix(k, draftPrefix)); err == nil && d.Status == "passed" {
			out = append(out, d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	if len(out) > maxDraftsLimit {
		out = out[:maxDraftsLimit]
	}
	return out, nil
}

func (s *Server) listDrafts(c *gin.Context) {
	list, err := s.pendingDrafts(c.Request.Context())
	if err != nil {
		failure(c, 503, "draft_unavailable")
		return
	}
	if list == nil {
		list = []*Draft{}
	}
	c.JSON(200, gin.H{"drafts": list})
}

func (s *Server) getDraft(c *gin.Context) {
	d, err := s.loadDraft(c.Request.Context(), c.Param("id"))
	if err != nil {
		failure(c, 404, "draft_not_found")
		return
	}
	c.JSON(200, d)
}

func (s *Server) discardDraft(c *gin.Context) {
	ctx := c.Request.Context()
	d, err := s.loadDraft(ctx, c.Param("id"))
	if err != nil {
		failure(c, 404, "draft_not_found")
		return
	}
	_ = s.Store.KV.Del(ctx, draftPrefix+d.ID)
	if d.Kind == "defense" && d.Defense != nil {
		s.markEvent(ctx, d.Defense.EventID, "dismissed", "")
	}
	if d.Kind == "plugin" && d.Plugin != nil && strings.HasPrefix(filepath.Clean(d.Plugin.Dir), filepath.Join(s.dshLayout().Runtime(), "plugin-drafts")+string(filepath.Separator)) {
		_ = os.RemoveAll(d.Plugin.Dir)
	}
	s.audit(c, "ai.discard", d.Kind+":"+d.ID, "ok")
	c.Status(204)
}

// submitNodeDraft 校验并实测 dsh 提交的节点草稿。返回的草稿 status 为 passed（等待管理员确认）或 failed（附原因）。
func (s *Server) submitNodeDraft(ctx context.Context, source string, nodeID int, set map[string]any, explain string) (*Draft, error) {
	n, err := s.Store.Ent.Node.Get(ctx, nodeID)
	if err != nil {
		return nil, errors.New("节点不存在")
	}
	explain = strings.TrimSpace(explain)
	if explain == "" || len(explain) > 2000 {
		return nil, errors.New("explain 必须是一句不超过 2000 字的说明")
	}
	patch := ai.Patch{Set: set, Explain: explain}
	d := &Draft{ID: randomToken(12), Kind: "node", Source: source, Explain: explain, CreatedAt: time.Now().UTC(), NodeID: n.ID, NodeName: n.Name, PatchRaw: set, BaseHash: settingsHash(n.Settings)}
	err = ai.CheckPatch(patch)
	for k := range set {
		if err == nil && !editable(k) {
			err = fmt.Errorf("字段 %s 不允许修改", k)
		}
	}
	if err == nil {
		after := ai.Apply(n.Settings, patch)
		d.Changes = ai.Diff(n.Settings, after)
		if len(d.Changes) == 0 {
			err = errors.New("草稿与当前参数相同")
		} else if g, gerr := s.Store.Ent.Gate.Get(ctx, n.GateID); gerr != nil {
			err = errors.New("节点所在 VPS 不存在")
		} else if err = s.checkSettings(ctx, g, n.Kernel, n.Protocol, n.Network, n.Security, n.Port, after); err == nil {
			d.Result, err = s.testNode(ctx, n, after)
		}
	}
	d.Status = "passed"
	if err != nil {
		d.Status, d.Error = "failed", err.Error()
	}
	if err = s.saveDraft(ctx, d); err != nil {
		return nil, err
	}
	_, _ = s.Store.Ent.Audit.Create().SetAction("ai.draft").SetTarget(source + " node:" + itoa(n.ID)).SetResult(d.Status).Save(ctx)
	return d, nil
}

// applyDraft 只接受已通过实测的草稿；节点草稿还要求节点参数自起草后未被修改。
func (s *Server) applyDraft(c *gin.Context) {
	ctx := c.Request.Context()
	d, err := s.loadDraft(ctx, c.Param("id"))
	if err != nil {
		failure(c, 404, "draft_not_found")
		return
	}
	if d.Status != "passed" {
		failure(c, 409, "draft_not_passed")
		return
	}
	if d.Kind == "defense" {
		s.applyDefenseDraft(c, d)
		return
	}
	if d.Kind == "plugin" {
		s.applyPluginDraft(c, d)
		return
	}
	n, err := s.Store.Ent.Node.Get(ctx, d.NodeID)
	if err != nil {
		failure(c, 404, "node_not_found")
		return
	}
	if settingsHash(n.Settings) != d.BaseHash {
		failure(c, 409, "draft_changed")
		return
	}
	if n, err = n.Update().SetSettings(ai.Apply(n.Settings, ai.Patch{Set: d.PatchRaw})).Save(ctx); err != nil {
		failure(c, 500, "update_failed")
		return
	}
	if err = s.afterNodeChange(ctx, n); err != nil {
		failure(c, 500, "sync_failed")
		return
	}
	_ = s.Store.KV.Del(ctx, draftPrefix+d.ID)
	s.audit(c, "ai.apply", "node:"+itoa(n.ID), "ok")
	c.JSON(200, s.nodeView(ctx, n))
}

// TestNodeSettings 以给定参数实测某节点（不保存）；供端到端测试与内部调用。
func (s *Server) TestNodeSettings(ctx context.Context, nodeID int, settings map[string]any) (map[string]any, error) {
	n, err := s.Store.Ent.Node.Get(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	return s.testNode(ctx, n, settings)
}

// dropLegacyAIProvider 删除旧版的 Claude / GPT 模型服务配置：AI 改由托管的 dsh 提供，Key 只保存在 dsh 里。
func (s *Server) dropLegacyAIProvider(ctx context.Context) {
	n, err := s.Store.Ent.Setting.Delete().Where(setting.KeyEQ("ai-provider")).Exec(ctx)
	if err == nil && n > 0 {
		_, _ = s.Store.Ent.Audit.Create().SetAction("ai.provider.delete").SetTarget("migrated-to-dsh").SetResult("ok").Save(ctx)
	}
}
