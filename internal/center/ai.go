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
	"strconv"
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
	draftApplying  = "ai-draft-applying:"
	maxDraftsLimit = 50
)

func (s *Server) aiRoutes(admin *gin.RouterGroup) {
	admin.GET("/ai/drafts", s.listDrafts)
	admin.GET("/ai/drafts/:id", s.getDraft)
	admin.POST("/ai/drafts/:id/apply", s.asJob("确认 dsh 草稿", "已取消，草稿没有执行", s.applyDraft))
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

// 首次接入时需要先下载两个内核；连接实测本身仍由 Gate 限时执行。
const nodeTestTimeout = 5 * time.Minute

// testNode 让节点所在 Gate 用给定参数实测：内核自检 + 本机回环真实连接下载。
func (s *Server) testNode(ctx context.Context, n *ent.Node, settings map[string]any) (map[string]any, error) {
	return s.testNodeWith(ctx, n, settings, nil)
}

// testNodeWith 同 testNode，drivers 非空时用其中的驱动（展开后的全文）代替当前驱动——保存驱动前实测用。
func (s *Server) testNodeWith(ctx context.Context, n *ent.Node, settings map[string]any, drivers map[string]string) (map[string]any, error) {
	g, err := s.Store.Ent.Gate.Get(ctx, n.GateID)
	if err != nil {
		return nil, err
	}
	supported, err := s.gateSupportsTask(ctx, g, contracts.ActionNodeTest, 3)
	if err != nil {
		return nil, err
	}
	if !supported {
		return nil, errors.New("这台 VPS 尚不支持完整客户端实测，请先升级 Gate 并等待心跳")
	}
	kernels, profiles, err := s.gateKernels(ctx, g, map[string]bool{n.Kernel: true, "mihomo": true})
	if err != nil {
		return nil, err
	}
	for k, text := range drivers {
		profiles[k] = text
	}
	id, password := newSecret(n.Protocol)
	in := contracts.Inbound{ID: 1, Tag: "test", Kernel: n.Kernel, Protocol: n.Protocol, Network: n.Network, Security: n.Security, Settings: settings}
	cfg := contracts.GateConfig{Kernels: kernels, Drivers: profiles, Inbounds: []contracts.Inbound{in},
		Credentials: []contracts.Credential{{ID: 1, InboundID: 1, StatsKey: "test", UserKey: "test", UUID: id, Password: password, Internal: true}}, Budgets: map[string]contracts.GateBudget{"test": {Unlimited: true}}}
	var cert *ent.Certificate
	if n.CertID != nil {
		if cert, err = s.Store.Ent.Certificate.Get(ctx, *n.CertID); err != nil {
			return nil, err
		}
		cfg.Inbounds[0].CertID = int64(cert.ID)
		cfg.Certs = []contracts.CertRef{{ID: int64(cert.ID), SHA256: cert.Sha256}}
		if cert.Source == "panel" {
			cfg.Certs[0].SHA256, cfg.Certs[0].Panel = "", cert.Domain
		}
		// 节点可能尚未保存（建节点前实测）：短时允许该 Gate 下载这张证书。
		_ = s.Store.KV.Set(ctx, fmt.Sprintf("cert-grant:%d:%d", g.ID, cert.ID), "1", nodeTestTimeout+time.Minute)
	}
	probe := *n
	probe.Settings, probe.Port = settings, 1
	link, err := shareLink(&probe, "127.0.0.1", &ent.Credential{ID: 1, UUID: id, Password: password}, "test", cert)
	if err != nil {
		return nil, err
	}
	proxy, err := subout.ClashProxy(link)
	if err != nil {
		return nil, fmt.Errorf("客户端配置无法生成：%w", err)
	}
	doc := contracts.NodeTestClient{Proxies: []map[string]any{proxy}, Target: proxy["name"].(string)}
	var payload map[string]any
	raw, _ := json.Marshal(map[string]any{"config": cfg, "client": doc})
	_ = json.Unmarshal(raw, &payload)
	return s.gateProbe(ctx, g, contracts.ActionNodeTest, payload, nodeTestTimeout)
}

func (s *Server) testNodeHandler(c *gin.Context) {
	s.startDeployedNodeTest(c)
}

// Draft 是一份待确认的草稿（保存在 KV，1 小时后过期）：能力目录里的多步草稿（见 action_drafts.go），
// 在通用确认窗里按 Steps 的参数说明逐步展示；驱动草稿另附逐行差异。
type Draft struct {
	ID        string            `json:"id"`
	Kind      string            `json:"kind"`
	Source    string            `json:"source"` // dsh（窗口里的对话）/ analysis（后台分析）
	Status    string            `json:"status"` // passed
	Explain   string            `json:"explain"`
	CreatedAt time.Time         `json:"created_at"`
	Steps     []DraftStep       `json:"steps"`
	Versions  map[string]string `json:"versions,omitempty"` // 所依据的数据版本
	Danger    bool              `json:"danger,omitempty"`   // 有危险步骤：确认窗里标出
	// Drawer 是提交时的 dsh 会话（浏览器标签）：只有同一个会话能撤回。
	Drawer string `json:"drawer,omitempty"`
	// 运维草稿：按状态和 VPS 筛选。执行前复判不通过时 OpsStatus 为 blocked，没有强制执行入口。
	OpsStatus      string  `json:"ops_status,omitempty"`
	GateID         int     `json:"gate_id,omitempty"`
	Confidence     float64 `json:"confidence,omitempty"`
	ConfigRevision int64   `json:"config_revision,omitempty"`
	TraceID        string  `json:"trace_id,omitempty"`
	// OpsBefore 是确认后、写入前重新采集的当前状态。执行后复测不能拿旧说明代替它。
	OpsBefore string `json:"ops_before,omitempty"`
	// OpsRecord 是系统自动起草时的触发记录（见 ops_auto.go）；确认、放弃、拦下、回退都回写到这条记录。
	OpsRecord string `json:"ops_record,omitempty"`
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
	if err := s.Store.KV.Set(ctx, draftPrefix+d.ID, string(b), draftTTL); err != nil {
		return err
	}
	s.notifyConsole("drafts", "")
	return nil
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
		d, err := s.loadDraft(ctx, strings.TrimPrefix(k, draftPrefix))
		if err != nil || d.Status != "passed" {
			continue
		}
		// 正在执行的草稿不再弹确认窗
		if _, applying, _ := s.Store.KV.Get(ctx, draftApplying+d.ID); !applying {
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
		failInternal(c, 503, "draft_unavailable", err)
		return
	}
	if list == nil {
		list = []*Draft{}
	}
	status, gateID := c.Query("ops_status"), 0
	if raw := c.Query("gate_id"); raw != "" {
		gateID, _ = strconv.Atoi(raw)
	}
	if status != "" || gateID > 0 {
		filtered := make([]*Draft, 0, len(list))
		for _, d := range list {
			if status != "" && d.OpsStatus != status {
				continue
			}
			if gateID > 0 && d.GateID != gateID {
				continue
			}
			filtered = append(filtered, d)
		}
		list = filtered
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
	s.notifyConsole("drafts", "")
	s.markOpsRecord(ctx, d, "discarded", "管理员放弃了草稿")
	for _, st := range d.Steps {
		if st.Action == "defense.block" {
			s.markEvent(ctx, intArg(st.Args, "event_id"), "dismissed", "")
		}
	}
	s.audit(c, "ai.discard", d.Kind+":"+d.ID, "ok")
	s.recordDraft(ctx, d, draftDiscarded, "", nil, nil)
	c.Status(204)
}

// applyDraft 只接受已通过试运行的草稿，按步骤执行。
func (s *Server) applyDraft(c *gin.Context) {
	d, err := s.loadDraft(c.Request.Context(), c.Param("id"))
	if err != nil {
		failure(c, 404, "draft_not_found")
		return
	}
	if d.Status != "passed" {
		failure(c, 409, "draft_not_passed")
		return
	}
	if d.Source == "analysis" || d.OpsStatus != "" {
		reason, blocked := s.rejudgeOpsDraft(c.Request.Context(), d)
		if blocked {
			d.OpsStatus = "blocked"
			_ = s.saveDraft(c.Request.Context(), d)
			s.markOpsRecord(c.Request.Context(), d, "blocked", reason)
			failDetail(c, 409, "draft_blocked", reason, gin.H{"ops_status": "blocked"})
			return
		}
		d.OpsStatus = "executing"
		_ = s.saveDraft(c.Request.Context(), d)
	}
	// 确认在后台任务里执行，可能要几分钟：期间同一份草稿不能再确认一次（如刷新页面后确认窗又弹出来）
	key := draftApplying + d.ID
	if ok, err := s.Store.KV.SetNX(c.Request.Context(), key, "1", 30*time.Minute); err != nil || !ok {
		failDetail(c, 409, "draft_applying", "这份草稿正在执行，结束后会提示结果", nil)
		return
	}
	defer s.Store.KV.Del(context.WithoutCancel(c.Request.Context()), key)
	s.notifyConsole("drafts", "")
	s.applyActionDraft(c, d)
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
