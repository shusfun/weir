package center

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/imzyb/MiGate/internal/ai"
	"github.com/imzyb/MiGate/internal/db"
	"github.com/imzyb/MiGate/internal/store/ent"
	"github.com/imzyb/MiGate/internal/store/ent/setting"
	"github.com/imzyb/MiGate/internal/subout"
	"github.com/imzyb/MiGate/packages/contracts"
)

// AI 配置助手：模型只出草稿 → 系统按内核版本试生成并在 Gate 本机实测真实连接 → 失败原因回给模型重写
// （最多 3 轮）→ 管理员看差异确认后才写入节点。发给模型的内容不含任何密码、UUID、密钥与订阅令牌。

const (
	aiSettingKey = "ai-provider"
	aiRounds     = 3
	draftTTL     = time.Hour
)

func (s *Server) aiRoutes(admin *gin.RouterGroup) {
	admin.GET("/ai/provider", s.getAIProvider)
	admin.PUT("/ai/provider", s.putAIProvider)
	admin.DELETE("/ai/provider", s.deleteAIProvider)
	admin.POST("/ai/drafts", s.createDraft)
	admin.GET("/ai/drafts/:id", s.getDraft)
	admin.POST("/ai/drafts/:id/apply", s.applyDraft)
	admin.POST("/nodes/:id/test", s.testNodeHandler)
}

type storedProvider struct {
	Protocol string `json:"protocol"`
	BaseURL  string `json:"base_url"`
	Model    string `json:"model"`
	Key      string `json:"key"` // 加密保存
}

func (s *Server) aiProvider(ctx context.Context) (ai.Provider, bool, error) {
	row, err := s.Store.Ent.Setting.Query().Where(setting.KeyEQ(aiSettingKey)).Only(ctx)
	if ent.IsNotFound(err) {
		return ai.Provider{}, false, nil
	}
	if err != nil {
		return ai.Provider{}, false, err
	}
	var sp storedProvider
	b, _ := json.Marshal(row.Value)
	if err = json.Unmarshal(b, &sp); err != nil {
		return ai.Provider{}, false, err
	}
	key, err := s.reveal(sp.Key)
	if err != nil {
		return ai.Provider{}, false, err
	}
	return ai.Provider{Protocol: sp.Protocol, BaseURL: sp.BaseURL, Model: sp.Model, Key: key}, true, nil
}

func (s *Server) getAIProvider(c *gin.Context) {
	p, ok, err := s.aiProvider(c.Request.Context())
	if err != nil {
		failure(c, 503, "ai_provider_unavailable")
		return
	}
	c.JSON(200, gin.H{"configured": ok, "protocol": p.Protocol, "base_url": p.BaseURL, "model": p.Model, "key_set": p.Key != "", "protocols": ai.Protocols})
}

// putAIProvider 保存模型服务；改动需要重新输入密码（与二次验证码）。Key 留空时沿用原值，只写不读。
func (s *Server) putAIProvider(c *gin.Context) {
	var req struct {
		Protocol string `json:"protocol"`
		BaseURL  string `json:"base_url"`
		Model    string `json:"model"`
		Key      string `json:"key"`
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if !bind(c, &req) || !s.reauth(c, req.Password, req.Code) {
		return
	}
	ctx := c.Request.Context()
	p := ai.Provider{Protocol: req.Protocol, BaseURL: strings.TrimSpace(req.BaseURL), Model: strings.TrimSpace(req.Model), Key: strings.TrimSpace(req.Key)}
	if p.Key == "" {
		if old, ok, err := s.aiProvider(ctx); err == nil && ok {
			p.Key = old.Key
		}
	}
	if err := p.Validate(); err != nil {
		failure(c, 400, "ai_"+err.Error())
		return
	}
	sealed, err := s.Crypto.Encrypt(p.Key)
	if err != nil {
		failure(c, 500, "encrypt_failed")
		return
	}
	value := map[string]any{"protocol": p.Protocol, "base_url": p.BaseURL, "model": p.Model, "key": sealed}
	if err = s.Store.Ent.Setting.Create().SetKey(aiSettingKey).SetValue(value).OnConflictColumns(setting.FieldKey).UpdateNewValues().Exec(ctx); err != nil {
		failure(c, 500, "save_failed")
		return
	}
	s.audit(c, "ai.provider", p.Protocol, "ok")
	c.JSON(200, gin.H{"configured": true, "protocol": p.Protocol, "base_url": p.BaseURL, "model": p.Model, "key_set": true, "protocols": ai.Protocols})
}

func (s *Server) deleteAIProvider(c *gin.Context) {
	_, _ = s.Store.Ent.Setting.Delete().Where(setting.KeyEQ(aiSettingKey)).Exec(c.Request.Context())
	s.audit(c, "ai.provider.delete", "", "ok")
	c.Status(204)
}

// editableFields 是模型可以修改的入站字段：构建器认识的字段，去掉系统管理与机密字段。
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

// Draft 是一次 AI 调整：保存在 KV（1 小时）。Patch 只含模型改动的字段，确认时合并到节点当前参数。
type Draft struct {
	ID          string         `json:"id"`
	NodeID      int            `json:"node_id"`
	Instruction string         `json:"instruction"`
	Status      string         `json:"status"` // running / passed / failed / error
	Error       string         `json:"error,omitempty"`
	Explain     string         `json:"explain,omitempty"`
	Changes     []ai.Change    `json:"changes,omitempty"`
	Attempts    []Attempt      `json:"attempts"`
	Patch       ai.Patch       `json:"-"`
	PatchRaw    map[string]any `json:"patch,omitempty"`
	BaseHash    string         `json:"base_hash"`
	CreatedAt   time.Time      `json:"created_at"`
}

// Attempt 是一轮草稿与实测结果（不含任何机密值）。
type Attempt struct {
	Explain string         `json:"explain"`
	Changes []ai.Change    `json:"changes"`
	Error   string         `json:"error,omitempty"`
	Result  map[string]any `json:"result,omitempty"`
}

func settingsHash(m map[string]any) string {
	b, _ := json.Marshal(m)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func draftKey(id string) string { return "ai-draft:" + id }

func (s *Server) saveDraft(ctx context.Context, d *Draft) error {
	d.PatchRaw = d.Patch.Set
	b, err := json.Marshal(d)
	if err != nil {
		return err
	}
	return s.Store.KV.Set(ctx, draftKey(d.ID), string(b), draftTTL)
}

func (s *Server) loadDraft(ctx context.Context, id string) (*Draft, error) {
	raw, ok, err := s.Store.KV.Get(ctx, draftKey(id))
	if err != nil || !ok {
		return nil, errors.New("draft_not_found")
	}
	var d Draft
	if err = json.Unmarshal([]byte(raw), &d); err != nil {
		return nil, err
	}
	d.Patch.Set, d.Patch.Explain = d.PatchRaw, d.Explain
	return &d, nil
}

// createDraft 后台生成草稿并实测；页面轮询 GET /ai/drafts/:id。
func (s *Server) createDraft(c *gin.Context) {
	var req struct {
		NodeID      int    `json:"node_id"`
		Instruction string `json:"instruction"`
	}
	if !bind(c, &req) {
		return
	}
	req.Instruction = strings.TrimSpace(req.Instruction)
	if req.Instruction == "" || len(req.Instruction) > 2000 {
		failure(c, 400, "invalid_instruction")
		return
	}
	ctx := c.Request.Context()
	n, err := s.Store.Ent.Node.Get(ctx, req.NodeID)
	if err != nil {
		failure(c, 404, "node_not_found")
		return
	}
	p, ok, err := s.aiProvider(ctx)
	if err != nil || !ok {
		failure(c, 409, "ai_not_configured")
		return
	}
	if ok, _ := s.Store.KV.SetNX(ctx, fmt.Sprintf("ai-running:%d", n.ID), "1", 5*time.Minute); !ok {
		failure(c, 409, "draft_running")
		return
	}
	d := &Draft{ID: randomToken(12), NodeID: n.ID, Instruction: req.Instruction, Status: "running", Attempts: []Attempt{}, BaseHash: settingsHash(n.Settings), CreatedAt: time.Now()}
	if err = s.saveDraft(ctx, d); err != nil {
		_ = s.Store.KV.Del(ctx, fmt.Sprintf("ai-running:%d", n.ID))
		failure(c, 503, "draft_unavailable")
		return
	}
	s.audit(c, "ai.draft", "node:"+itoa(n.ID), "started")
	go func() {
		bg, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		defer s.Store.KV.Del(bg, fmt.Sprintf("ai-running:%d", n.ID))
		s.runDraft(bg, p, n, d)
	}()
	c.JSON(202, d)
}

// runDraft 是草稿循环：模型故障只让本次草稿失败，节点与内核保持原样。
func (s *Server) runDraft(ctx context.Context, p ai.Provider, n *ent.Node, d *Draft) {
	g, _ := s.Store.Ent.Gate.Get(ctx, n.GateID)
	versions := map[string]string{}
	if g != nil {
		if ks, _, err := s.gateKernels(ctx, g, map[string]bool{n.Kernel: true}); err == nil {
			for k, v := range ks {
				versions[k] = v.Version
			}
		}
	}
	state, _ := json.MarshalIndent(map[string]any{
		"kernel": n.Kernel, "kernel_version": versions[n.Kernel], "protocol": n.Protocol, "network": n.Network, "security": n.Security,
		"settings": ai.Redact(n.Settings), "gate_last_error": func() string {
			if g != nil {
				return g.LastError
			}
			return ""
		}(),
	}, "", "  ")
	msgs := []ai.Message{{Role: "user", Content: "当前节点：\n" + string(state) + "\n\n管理员要求：" + d.Instruction}}
	system := ai.SystemPrompt(editableFields)
	for round := 0; round < aiRounds; round++ {
		answer, err := (ai.Client{}).Complete(ctx, p, system, msgs)
		if err != nil {
			d.Status, d.Error = "error", err.Error()
			_ = s.saveDraft(ctx, d)
			return
		}
		msgs = append(msgs, ai.Message{Role: "assistant", Content: answer})
		att := Attempt{}
		patch, err := ai.ParsePatch(answer)
		if err == nil {
			after := ai.Apply(n.Settings, patch)
			att.Explain, att.Changes = patch.Explain, ai.Diff(n.Settings, after)
			if g == nil {
				err = errors.New("gate_not_found")
			} else if err = s.checkSettings(ctx, g, n.Kernel, n.Protocol, n.Network, n.Security, n.Port, after); err == nil {
				att.Result, err = s.testNode(ctx, n, after)
			}
			if err == nil {
				d.Attempts = append(d.Attempts, att)
				d.Status, d.Explain, d.Changes, d.Patch = "passed", patch.Explain, att.Changes, patch
				_ = s.saveDraft(ctx, d)
				return
			}
		}
		att.Error = err.Error()
		d.Attempts = append(d.Attempts, att)
		_ = s.saveDraft(ctx, d)
		feedback := "上一份草稿未通过：" + att.Error
		if att.Result != nil {
			if b, e := json.Marshal(att.Result); e == nil {
				feedback += "\n实测详情：" + string(b)
			}
		}
		msgs = append(msgs, ai.Message{Role: "user", Content: feedback + "\n请修正后重新输出完整 JSON 草稿。"})
	}
	d.Status, d.Error = "failed", "连续 3 次草稿未通过实测，节点保持原样"
	_ = s.saveDraft(ctx, d)
}

func (s *Server) getDraft(c *gin.Context) {
	d, err := s.loadDraft(c.Request.Context(), c.Param("id"))
	if err != nil {
		failure(c, 404, "draft_not_found")
		return
	}
	c.JSON(200, d)
}

// applyDraft 只接受已通过实测、且节点参数自起草后未被修改的草稿。
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
	n, err := s.Store.Ent.Node.Get(ctx, d.NodeID)
	if err != nil {
		failure(c, 404, "node_not_found")
		return
	}
	if settingsHash(n.Settings) != d.BaseHash {
		failure(c, 409, "draft_changed")
		return
	}
	if n, err = n.Update().SetSettings(ai.Apply(n.Settings, d.Patch)).Save(ctx); err != nil {
		failure(c, 500, "update_failed")
		return
	}
	if err = s.afterNodeChange(ctx, n); err != nil {
		failure(c, 500, "sync_failed")
		return
	}
	_ = s.Store.KV.Del(ctx, draftKey(d.ID))
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
