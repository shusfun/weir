package center

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// 页面感知：dsh 随时知道管理员此刻在控制台看什么。上下文由各页面主动声明、控制台报给中心，dsh 经 get_console_context 读取；
// 控制台与 dsh 的 iframe 之间不直接传消息。上下文属于「dsh 会话」：绑定打开 dsh 窗口的管理员会话和那一个浏览器标签，
// 多个标签各自独立、互不覆盖。只存在短期存储里（关闭 dsh 窗口超过 30 分钟或退出登录即失效），不写审计、不进备份。

const (
	ctxTTL       = 30 * time.Minute
	ctxStale     = 2 * time.Minute
	navTTL       = 2 * time.Minute
	ctxKeyPrefix = "dsh-ctx:"
	ctxActiveKey = "dsh-ctx-active"
)

var drawerIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,64}$`)

// ConsoleContext 是一个 dsh 会话的最新上下文。
type ConsoleContext struct {
	Drawer     string         `json:"-"`
	UserID     int            `json:"-"`
	SessionKey string         `json:"-"`
	At         time.Time      `json:"at"`
	Visible    bool           `json:"visible"`
	DrawerOpen bool           `json:"drawer_open"`
	Page       string         `json:"page"`
	Tab        string         `json:"tab,omitempty"`
	Declared   bool           `json:"declared"`
	Object     *ctxObject     `json:"object,omitempty"`
	Dialog     *ctxDialog     `json:"dialog,omitempty"`
	List       *ctxList       `json:"list,omitempty"`
	Form       []ctxFormField `json:"form,omitempty"`
	Notices    []string       `json:"notices,omitempty"`
	LastError  *ctxError      `json:"last_error,omitempty"`
}

// ctxObject 是页面上正在看的对象：数字编号的对象填 id，驱动、内核版本、公开页面这类按名称的填 key。
type ctxObject struct {
	Type string `json:"type"`
	Name string `json:"name"`
	ID   int    `json:"id,omitempty"`
	Key  string `json:"key,omitempty"`
}

type ctxDialog struct {
	Title string `json:"title"`
	Step  string `json:"step,omitempty"`
}

type ctxList struct {
	Filters  map[string]string `json:"filters,omitempty"`
	Total    int               `json:"total,omitempty"`    // 列表里一共有几项（Items 只给前 20 项）
	Selected int               `json:"selected,omitempty"` // 真正勾选了几项（列表支持勾选时才有）
	Items    []ctxObject       `json:"items,omitempty"` // 选中较多时只给前 20 项
}

type ctxFormField struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Value  string `json:"value,omitempty"`
	Secret bool   `json:"secret,omitempty"`
	Filled bool   `json:"filled"`
}

type ctxError struct {
	Code    string    `json:"code"`
	Message string    `json:"message"`
	At      time.Time `json:"at"`
}

var secretKey = regexp.MustCompile(`(?i)pass|secret|token|private|code|uuid|key|totp|otp|link`)

// redact 按能力目录的规则脱敏：密码、密钥、验证码类字段一律不传，只标已填 / 未填；文本限长。
func (cc *ConsoleContext) redact() {
	clip := func(s string, n int) string {
		if r := []rune(strings.TrimSpace(s)); len(r) > n {
			return string(r[:n]) + "…"
		}
		return strings.TrimSpace(s)
	}
	cc.Page, cc.Tab = clip(cc.Page, 60), clip(cc.Tab, 60)
	if len(cc.Form) > 40 {
		cc.Form = cc.Form[:40]
	}
	for i := range cc.Form {
		f := &cc.Form[i]
		f.Key, f.Label = clip(f.Key, 60), clip(f.Label, 60)
		if f.Secret || secretKey.MatchString(f.Key) {
			f.Secret, f.Filled, f.Value = true, f.Filled || f.Value != "", ""
			continue
		}
		f.Filled = f.Filled || f.Value != ""
		f.Value = clip(f.Value, 300)
	}
	if cc.List != nil && len(cc.List.Items) > 20 {
		cc.List.Items = cc.List.Items[:20]
	}
	if len(cc.Notices) > 10 {
		cc.Notices = cc.Notices[:10]
	}
	for i := range cc.Notices {
		cc.Notices[i] = clip(cc.Notices[i], 200)
	}
	if cc.LastError != nil {
		cc.LastError.Code, cc.LastError.Message = clip(cc.LastError.Code, 60), clip(cc.LastError.Message, 300)
	}
}

func (s *Server) contextRoutes(admin *gin.RouterGroup) {
	admin.POST("/dsh/context", s.reportContext)
	admin.GET("/dsh/navigation", s.pollNavigation)
}

func ctxKey(uid int, drawer string) string { return ctxKeyPrefix + itoa(uid) + ":" + drawer }

// reportContext 保存一个 dsh 会话的上下文；在前台且 dsh 窗口开着（含缩小）的标签成为 dsh 当前读取的会话。
func (s *Server) reportContext(c *gin.Context) {
	var req struct {
		Drawer string `json:"drawer"`
		ConsoleContext
	}
	if !bind(c, &req) {
		return
	}
	if !drawerIDPattern.MatchString(req.Drawer) {
		failure(c, 400, "invalid_drawer")
		return
	}
	cc := req.ConsoleContext
	cc.redact()
	cc.At, cc.UserID, cc.SessionKey = time.Now().UTC(), c.GetInt("user_id"), c.GetString("session_key")
	b, _ := json.Marshal(struct {
		ConsoleContext
		UserID     int    `json:"user_id"`
		SessionKey string `json:"session_key"`
	}{cc, cc.UserID, cc.SessionKey})
	ctx, key := c.Request.Context(), ctxKey(cc.UserID, req.Drawer)
	if err := s.Store.KV.Set(ctx, key, string(b), ctxTTL); err != nil {
		failInternal(c, 503, "context_unavailable", err)
		return
	}
	if cc.Visible && cc.DrawerOpen {
		_ = s.Store.KV.Set(ctx, ctxActiveKey, key, ctxTTL)
	}
	c.Status(204)
}

// activeContext 是 dsh 当前读取的 dsh 会话：最近一个在前台、dsh 窗口开着的标签。
func (s *Server) activeContext(ctx context.Context) (*ConsoleContext, string, string) {
	key, ok, err := s.Store.KV.Get(ctx, ctxActiveKey)
	if err != nil || !ok {
		return nil, "", "还没有打开过 dsh 窗口的控制台标签（或已关闭超过 30 分钟）"
	}
	raw, ok, err := s.Store.KV.Get(ctx, key)
	if err != nil || !ok {
		return nil, "", "dsh 会话已结束"
	}
	var v struct {
		ConsoleContext
		UserID     int    `json:"user_id"`
		SessionKey string `json:"session_key"`
	}
	if json.Unmarshal([]byte(raw), &v) != nil {
		return nil, "", "上下文无法读取"
	}
	// 管理员退出登录后上下文立即失效。
	if _, alive, _ := s.Store.KV.Get(ctx, v.SessionKey); !alive || v.SessionKey == "" {
		_ = s.Store.KV.Del(ctx, key, ctxActiveKey)
		return nil, "", "管理员已退出登录"
	}
	cc := v.ConsoleContext
	cc.UserID, cc.SessionKey = v.UserID, v.SessionKey
	return &cc, key, ""
}

// consoleContextForModel 是 get_console_context 的结果；缺失或过期时返回「未知」并说明原因，不拿旧上下文冒充当前。
func (s *Server) consoleContextForModel(ctx context.Context) map[string]any {
	cc, _, why := s.activeContext(ctx)
	if cc == nil {
		return map[string]any{"page": "未知", "reason": why}
	}
	out := map[string]any{"page": cc.Page, "tab": cc.Tab, "at": cc.At, "object": cc.Object, "dialog": cc.Dialog, "list": cc.List, "form": cc.Form, "notices": cc.Notices, "last_error": cc.LastError}
	switch {
	case time.Since(cc.At) > ctxStale:
		out["status"] = "过期"
		out["note"] = "超过 2 分钟没有收到这个标签的上下文，以下内容可能已经不是当前页面"
	case !cc.Visible:
		out["status"] = "不在前台"
		out["note"] = "管理员已切到别的标签或窗口，以下是离开前的页面"
	default:
		out["status"] = "当前"
	}
	if !cc.Declared {
		note, _ := out["note"].(string)
		out["note"] = strings.TrimPrefix(note+"；此页未提供详细信息", "；")
	}
	return out
}

// navPages 是 dsh 可以让控制台打开的页面（只换页面，不改任何数据）。
var navPages = map[string]string{"gates": "/gates", "users": "/users", "linkmap": "/linkmap", "kernels": "/kernels", "subscriptions": "/subscriptions", "pages": "/pages", "audit": "/ops/audit", "system": "/system"}

func (s *Server) queueNavigation(ctx context.Context, args map[string]any) error {
	page, _ := args["page"].(string)
	path, ok := navPages[page]
	if !ok && !(page == "" && args["object_type"] != nil) {
		return errors.New("不认识的页面 " + page)
	}
	tab, _ := args["tab"].(string)
	if len(tab) > 40 || strings.ContainsAny(tab, "&?#/ ") {
		return errors.New("标签名无效")
	}
	cc, key, why := s.activeContext(ctx)
	if cc == nil {
		return errors.New(why)
	}
	nav := map[string]any{"path": path, "tab": tab, "at": time.Now().UTC()}
	typ, _ := args["object_type"].(string)
	okey, _ := args["object_key"].(string)
	if id := intArg(args, "object_id"); id > 0 && okey == "" {
		okey = itoa(id)
	}
	// 旧写法：只给 page 与 object_id 时按页面推断类型（VPS 与节点页是 VPS，用户页是用户）
	if typ == "" && okey != "" {
		typ = map[string]string{"gates": "gate", "users": "user"}[page]
	}
	if typ != "" {
		ref, ok := s.resolveObject(ctx, typ, okey)
		if !ok {
			return errors.New("找不到要打开的对象 " + typ + ":" + okey)
		}
		nav["object"] = ref
	}
	b, _ := json.Marshal(nav)
	return s.Store.KV.Set(ctx, "dsh-nav:"+strings.TrimPrefix(key, ctxKeyPrefix), string(b), navTTL)
}

// pollNavigation 由打开 dsh 窗口的那个标签轮询：只作用于它自己（按 dsh 会话），取一次即删。
func (s *Server) pollNavigation(c *gin.Context) {
	drawer := c.Query("drawer")
	if !drawerIDPattern.MatchString(drawer) {
		failure(c, 400, "invalid_drawer")
		return
	}
	raw, ok, err := s.Store.KV.GetDel(c.Request.Context(), "dsh-nav:"+itoa(c.GetInt("user_id"))+":"+drawer)
	if err != nil || !ok {
		c.Status(204)
		return
	}
	c.Data(200, "application/json", []byte(raw))
}

var consoleTools = []mcpTool{
	{"get_console_context", "管理员此刻在 Weir 控制台里看什么：页面与标签、正在看的对象、打开的弹窗与步骤、列表筛选与选中项、未保存表单（密码类只标已填 / 未填）、最近的失败提示与页面告警。每轮回答前先读一次；缺失或过期时会说明原因。", obj(map[string]any{})},
	{"navigate_console", "让控制台打开某个页面、标签或对象（如带管理员去某台 VPS 的详情、某个驱动、某条审计）。只换页面，不改任何数据，不需要确认；不能借此填表或点按钮。给对象时可以不给 page，控制台按对象类型打开它所在的页面。", obj(map[string]any{
		"page":        map[string]any{"type": "string", "enum": []string{"gates", "users", "linkmap", "kernels", "subscriptions", "pages", "audit", "system"}, "description": "gates=VPS 与节点；users=用户；linkmap=链路（地图与实时流量）；kernels=内核；subscriptions=订阅管理；pages=公开页面；audit=操作审计；system=系统设置"},
		"tab":         map[string]any{"type": "string", "description": "可选：页面里的标签。system：update=版本与更新、ai=AI 与 dsh、defense=安全防御、login=登录与令牌、notify=通知（含邮件 SMTP）、certs=节点证书、backup=备份迁移、distribution=分发；kernels：versions=版本、drivers=驱动、checks=订阅检查、dsh=dsh 版本；subscriptions：sources=订阅源、nodes=手动节点、profiles=订阅组、templates=规则模板、settings=默认设置"},
		"object_type": map[string]any{"type": "string", "enum": objectTypes, "description": "可选：要打开的对象类型。gate=VPS、node=节点、user=用户、link=订阅链接、chain=链式代理、driver=内核驱动、kernel=内核版本、source=订阅源、profile=订阅组、page=公开页面、cert=证书、audit=审计记录"},
		"object_id":   map[string]any{"type": "number", "description": "可选：数字编号的对象（VPS、节点、用户、链接、链式代理、订阅源、订阅组、证书、审计记录）"},
		"object_key":  map[string]any{"type": "string", "description": "可选：按名称的对象：驱动名（如 xray-26.3）、内核版本（如 xray@26.3.27）、公开页面的地址名"},
	})},
}
