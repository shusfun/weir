package center

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"github.com/imzyb/MiGate/internal/store/ent"
	"github.com/imzyb/MiGate/internal/store/ent/resource"
)

// 公开页面：匿名可访问的介绍页（/p/<slug>），可展示已分享的订阅组卡片与留言板。
// 匿名接口只投影白名单字段；卡片只指向「已分享且未过期」的订阅组，不暴露任何令牌。

const (
	kindPage      = "public-page"
	kindGuestbook = "guestbook"
)

var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,47}$`)

type pageCard struct {
	ProfileID int    `json:"profile_id"`
	Target    string `json:"target"`
	Title     string `json:"title"`
}

type guestbookConfig struct {
	Enabled        bool `json:"enabled"`
	AllowAnonymous bool `json:"allow_anonymous"`
	RequireAudit   bool `json:"require_audit"`
}

type publicPageData struct {
	Enabled     bool            `json:"enabled"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Notice      string          `json:"notice"`
	NoticeTitle string          `json:"notice_title"`
	NoticeType  string          `json:"notice_type"`
	NoticeClose bool            `json:"notice_dismissible"`
	ShowLogin   bool            `json:"show_login"`
	Theme       string          `json:"theme"`
	ShowShare   bool            `json:"show_share"`
	Cards       []pageCard      `json:"cards"`
	Guestbook   guestbookConfig `json:"guestbook"`
}

func decodeStrict(m map[string]any, out any) error {
	raw, _ := json.Marshal(m)
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	return dec.Decode(out)
}

func toMap(v any) map[string]any {
	raw, _ := json.Marshal(v)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return out
}

func validText(v string, max int) bool {
	return utf8.ValidString(v) && len(v) <= max && !strings.ContainsAny(v, "\x00")
}

func (s *Server) pageRoutes(ready, admin *gin.RouterGroup) {
	ready.GET("/pages/:slug", s.publicPage)
	ready.GET("/pages/:slug/cards/:index", s.pageCard)
	ready.GET("/pages/:slug/guestbook", s.publicGuestbook)
	ready.POST("/pages/:slug/guestbook", s.submitGuestbook)
	admin.GET("/public-pages", s.listPages)
	admin.POST("/public-pages", s.savePage)
	admin.PATCH("/public-pages/:id", s.savePage)
	admin.DELETE("/public-pages/:id", s.deletePage)
	admin.GET("/public-pages/:id/guestbook/config", s.getGuestbookConfig)
	admin.PUT("/public-pages/:id/guestbook/config", s.putGuestbookConfig)
	admin.GET("/public-pages/:id/guestbook/messages", s.adminGuestbook)
	admin.POST("/public-pages/:id/guestbook/messages", s.moderateGuestbook)
}

func pageView(r *ent.Resource) gin.H {
	return gin.H{"id": r.ID, "slug": r.Key, "data": r.Data, "created_at": r.CreatedAt}
}

func (s *Server) listPages(c *gin.Context) {
	items, err := s.Store.Ent.Resource.Query().Where(resource.KindEQ(kindPage)).Order(ent.Asc(resource.FieldID)).All(c.Request.Context())
	if err != nil {
		failure(c, 503, "query_failed")
		return
	}
	out := make([]gin.H, 0, len(items))
	for _, it := range items {
		out = append(out, pageView(it))
	}
	c.JSON(200, gin.H{"items": out})
}

func (s *Server) pageByID(c *gin.Context) (*ent.Resource, bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		failure(c, 400, "invalid_id")
		return nil, false
	}
	r, err := s.Store.Ent.Resource.Query().Where(resource.IDEQ(id), resource.KindEQ(kindPage)).Only(c.Request.Context())
	if err != nil {
		failure(c, 404, "page_not_found")
		return nil, false
	}
	return r, true
}

// savePage 保存页面内容；留言板设置单独保存（需要重新验证）。
func (s *Server) savePage(c *gin.Context) {
	var req struct {
		Slug string         `json:"slug"`
		Data publicPageData `json:"data"`
	}
	if !bind(c, &req) {
		return
	}
	d := req.Data
	if !validText(d.Title, 200) || strings.TrimSpace(d.Title) == "" || !validText(d.Description, 4000) || !validText(d.Notice, 2000) || len(d.Cards) > 24 || !catalogChoice(d.Theme, "", "violet", "ocean", "forest") || !validText(d.NoticeTitle, 200) || !catalogChoice(d.NoticeType, "", "info", "success", "warning", "error") {
		failure(c, 400, "invalid_page")
		return
	}
	ctx := c.Request.Context()
	for _, card := range d.Cards {
		if _, err := s.Store.Ent.SubProfile.Get(ctx, card.ProfileID); err != nil || !catalogChoice(card.Target, "clash", "singbox", "surge", "loon", "quanx", "egern", "base64") || !validText(card.Title, 100) {
			failure(c, 400, "invalid_page_card")
			return
		}
	}
	var saved *ent.Resource
	var err error
	if c.Param("id") == "" {
		if !slugPattern.MatchString(req.Slug) {
			failure(c, 400, "invalid_slug")
			return
		}
		d.Guestbook = guestbookConfig{}
		saved, err = s.Store.Ent.Resource.Create().SetKind(kindPage).SetKey(req.Slug).SetData(toMap(d)).Save(ctx)
		if err != nil {
			failure(c, 409, "slug_taken")
			return
		}
	} else {
		old, ok := s.pageByID(c)
		if !ok {
			return
		}
		var prev publicPageData
		_ = decodeStrict(old.Data, &prev)
		d.Guestbook = prev.Guestbook
		if saved, err = old.Update().SetData(toMap(d)).Save(ctx); err != nil {
			failure(c, 500, "save_failed")
			return
		}
	}
	s.audit(c, "page.save", "page:"+saved.Key, "ok")
	c.JSON(200, pageView(saved))
}

// deletePage 删除页面并一并删除它的留言。
func (s *Server) deletePage(c *gin.Context) {
	r, ok := s.pageByID(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	if _, err := s.Store.Ent.Resource.Delete().Where(resource.KindEQ(kindGuestbook), resource.OwnerIDEQ(r.ID)).Exec(ctx); err != nil {
		failure(c, 500, "delete_failed")
		return
	}
	if err := s.Store.Ent.Resource.DeleteOne(r).Exec(ctx); err != nil {
		failure(c, 500, "delete_failed")
		return
	}
	s.audit(c, "page.delete", "page:"+r.Key, "ok")
	c.Status(204)
}

// publishedPage 取启用的页面；页面不存在、停用或数据损坏都按 404 处理（不区分，避免探测）。
func (s *Server) publishedPage(c *gin.Context) (*ent.Resource, publicPageData, bool) {
	var d publicPageData
	slug := c.Param("slug")
	if !slugPattern.MatchString(slug) {
		failure(c, 404, "page_not_found")
		return nil, d, false
	}
	r, err := s.Store.Ent.Resource.Query().Where(resource.KindEQ(kindPage), resource.KeyEQ(slug)).Only(c.Request.Context())
	if err != nil || decodeStrict(r.Data, &d) != nil || !d.Enabled {
		failure(c, 404, "page_not_found")
		return nil, d, false
	}
	return r, d, true
}

// sharedProfile 返回仍在分享期内的订阅组（卡片只对这种订阅组生效）。
func (s *Server) sharedProfile(c *gin.Context, id int) (*ent.SubProfile, bool) {
	p, err := s.Store.Ent.SubProfile.Get(c.Request.Context(), id)
	if err != nil || !p.Enabled || p.ShareToken == "" || p.ShareExpiresAt == nil || time.Now().After(*p.ShareExpiresAt) {
		return nil, false
	}
	return p, true
}

func (s *Server) publicPage(c *gin.Context) {
	if !s.rate(c, "page", 240) {
		return
	}
	_, d, ok := s.publishedPage(c)
	if !ok {
		return
	}
	cards := []gin.H{}
	for i, card := range d.Cards {
		p, ok := s.sharedProfile(c, card.ProfileID)
		if !ok {
			continue
		}
		title := card.Title
		if title == "" {
			title = p.Name
		}
		cards = append(cards, gin.H{"title": title, "target": card.Target, "expires_at": p.ShareExpiresAt.Unix(), "path": "/api/v1/pages/" + c.Param("slug") + "/cards/" + strconv.Itoa(i)})
	}
	// 公告版本只随可见内容变化：关闭偏好在内容更新后失效。
	rev := digest(d.Notice + "\x00" + d.NoticeTitle + "\x00" + d.NoticeType)
	out := gin.H{"title": d.Title, "description": d.Description, "notice": d.Notice, "notice_title": d.NoticeTitle, "notice_type": d.NoticeType,
		"notice_dismissible": d.NoticeClose, "notice_revision": rev, "show_login": d.ShowLogin, "theme": d.Theme, "show_share": d.ShowShare, "subscriptions": cards, "cards": []gin.H{}}
	if d.Guestbook.Enabled {
		out["guestbook"] = gin.H{"enabled": true, "allow_anonymous": d.Guestbook.AllowAnonymous}
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(200, out)
}

// pageCard 下载页面上的订阅卡片：每次都重新核对页面启用、卡片存在与订阅组仍在分享期内。
func (s *Server) pageCard(c *gin.Context) {
	if !s.rate(c, "page-card", 120) {
		return
	}
	_, d, ok := s.publishedPage(c)
	if !ok {
		return
	}
	i, err := strconv.Atoi(c.Param("index"))
	if err != nil || i < 0 || i >= len(d.Cards) {
		failure(c, 404, "subscription_not_found")
		return
	}
	p, ok := s.sharedProfile(c, d.Cards[i].ProfileID)
	if !ok {
		failure(c, 404, "subscription_not_found")
		return
	}
	s.writeGroup(c, p, d.Cards[i].Target)
}

// ---- 留言板 ----

type guestbookMessage struct {
	Nickname  string     `json:"nickname"`
	Content   string     `json:"content"`
	Type      string     `json:"type"`
	Status    string     `json:"status"` // pending / approved / replied
	Reply     string     `json:"reply"`
	ReplyAt   *time.Time `json:"reply_at"`
	CreatedAt time.Time  `json:"created_at"`
	UserID    int        `json:"user_id,omitempty"`
}

func messageRevision(r *ent.Resource) string { return digest(string(mustJSONBytes(r.Data))) }

func mustJSONBytes(v any) []byte { b, _ := json.Marshal(v); return b }

func messageView(r *ent.Resource, admin bool) gin.H {
	var m guestbookMessage
	_ = decodeStrict(r.Data, &m)
	v := gin.H{"id": r.ID, "nickname": m.Nickname, "content": m.Content, "type": m.Type, "status": m.Status, "reply": m.Reply, "reply_at": m.ReplyAt, "created_at": m.CreatedAt}
	if admin {
		v["revision"], v["visible"] = messageRevision(r), m.Status != "pending"
	}
	return v
}

// listMessages 分页列出留言；visibleOnly 时只含已审核 / 已回复的。
func (s *Server) listMessages(c *gin.Context, pageID int, visibleOnly bool, status, search string) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 || page > 10000 {
		page = 1
	}
	const size = 20
	rows, err := s.Store.Ent.Resource.Query().Where(resource.KindEQ(kindGuestbook), resource.OwnerIDEQ(pageID)).Order(ent.Desc(resource.FieldID)).All(c.Request.Context())
	if err != nil {
		failure(c, 503, "guestbook_unavailable")
		return
	}
	var picked []*ent.Resource
	for _, r := range rows {
		var m guestbookMessage
		if decodeStrict(r.Data, &m) != nil {
			continue
		}
		if visibleOnly && m.Status == "pending" || status != "" && m.Status != status || search != "" && !strings.Contains(m.Content+m.Nickname, search) {
			continue
		}
		picked = append(picked, r)
	}
	items := []gin.H{}
	for i := (page - 1) * size; i < len(picked) && i < page*size; i++ {
		items = append(items, messageView(picked[i], !visibleOnly))
	}
	c.JSON(200, gin.H{"items": items, "page": page, "page_size": size, "total": len(picked)})
}

func (s *Server) publicGuestbook(c *gin.Context) {
	if !s.rate(c, "guestbook-read", 240) {
		return
	}
	r, d, ok := s.publishedPage(c)
	if !ok {
		return
	}
	if !d.Guestbook.Enabled {
		failure(c, 404, "guestbook_not_found")
		return
	}
	c.Header("Cache-Control", "no-store")
	s.listMessages(c, r.ID, true, "", "")
}

var guestbookTypes = []string{"general", "feature", "bug"}

// submitGuestbook 投稿：纯文本、有长度上限、按 IP 限速；需审核时先隐藏；不允许匿名时须登录。
func (s *Server) submitGuestbook(c *gin.Context) {
	if !s.rate(c, "guestbook-post", 5) {
		return
	}
	r, d, ok := s.publishedPage(c)
	if !ok {
		return
	}
	if !d.Guestbook.Enabled {
		failure(c, 404, "guestbook_not_found")
		return
	}
	var req struct {
		Nickname string `json:"nickname"`
		Content  string `json:"content"`
		Type     string `json:"type"`
	}
	if !bind(c, &req) {
		return
	}
	u := s.viewer(c)
	if u == nil && !d.Guestbook.AllowAnonymous {
		failure(c, 401, "login_required")
		return
	}
	req.Nickname, req.Content = strings.TrimSpace(req.Nickname), strings.TrimSpace(req.Content)
	if !validText(req.Nickname, 60) || req.Nickname == "" || !validText(req.Content, 2000) || req.Content == "" || !catalogChoice(req.Type, guestbookTypes...) {
		failure(c, 400, "invalid_guestbook")
		return
	}
	ctx := c.Request.Context()
	if n, _ := s.Store.Ent.Resource.Query().Where(resource.KindEQ(kindGuestbook), resource.OwnerIDEQ(r.ID)).Count(ctx); n >= 5000 {
		failure(c, 409, "guestbook_full")
		return
	}
	m := guestbookMessage{Nickname: req.Nickname, Content: req.Content, Type: req.Type, Status: "approved", CreatedAt: time.Now()}
	if d.Guestbook.RequireAudit {
		m.Status = "pending"
	}
	if u != nil {
		m.UserID = u.ID
	}
	if _, err := s.Store.Ent.Resource.Create().SetKind(kindGuestbook).SetKey(randomToken(12)).SetOwnerID(r.ID).SetData(toMap(m)).Save(ctx); err != nil {
		failure(c, 503, "guestbook_save_failed")
		return
	}
	c.JSON(201, gin.H{"pending": m.Status == "pending"})
}

func (s *Server) getGuestbookConfig(c *gin.Context) {
	r, ok := s.pageByID(c)
	if !ok {
		return
	}
	var d publicPageData
	if decodeStrict(r.Data, &d) != nil {
		failure(c, 503, "guestbook_unavailable")
		return
	}
	c.JSON(200, gin.H{"config": d.Guestbook, "revision": digest(string(mustJSONBytes(d.Guestbook)))})
}

// putGuestbookConfig 修改留言板设置需要重新验证并携带版本号。
func (s *Server) putGuestbookConfig(c *gin.Context) {
	r, ok := s.pageByID(c)
	if !ok {
		return
	}
	var req struct {
		Config   guestbookConfig `json:"config"`
		Revision string          `json:"revision"`
		Password string          `json:"password"`
		Code     string          `json:"code"`
		Confirm  bool            `json:"confirm"`
	}
	if !bind(c, &req) || !s.reauth(c, req.Password, req.Code) {
		return
	}
	err := s.updateResource(c.Request.Context(), kindPage, r.Key, func(cur map[string]any) (map[string]any, error) {
		var d publicPageData
		if cur == nil || decodeStrict(cur, &d) != nil {
			return nil, errors.New("page_unreadable")
		}
		if digest(string(mustJSONBytes(d.Guestbook))) != req.Revision {
			return nil, errResourceConflict
		}
		d.Guestbook = req.Config
		return toMap(d), nil
	})
	if errors.Is(err, errResourceConflict) {
		failure(c, 409, "guestbook_changed")
		return
	}
	if err != nil {
		failure(c, 503, "guestbook_save_failed")
		return
	}
	s.audit(c, "guestbook.config", "page:"+r.Key, "ok")
	c.JSON(200, gin.H{"config": req.Config, "revision": digest(string(mustJSONBytes(req.Config)))})
}

func (s *Server) adminGuestbook(c *gin.Context) {
	r, ok := s.pageByID(c)
	if !ok {
		return
	}
	status := c.Query("status")
	if status == "all" {
		status = ""
	}
	s.listMessages(c, r.ID, false, status, strings.TrimSpace(c.Query("search")))
}

// moderateGuestbook 审核 / 回复 / 删除；携带留言版本号，留言被其他操作修改时拒绝。
func (s *Server) moderateGuestbook(c *gin.Context) {
	page, ok := s.pageByID(c)
	if !ok {
		return
	}
	var req struct {
		ID       int    `json:"id"`
		Revision string `json:"revision"`
		Action   string `json:"action"` // status / reply / delete
		Status   string `json:"status"`
		Reply    string `json:"reply"`
	}
	if !bind(c, &req) {
		return
	}
	ctx := c.Request.Context()
	err := s.Store.Lock.Do(ctx, "guestbook:"+itoa(page.ID), func(ctx context.Context) error {
		r, err := s.Store.Ent.Resource.Query().Where(resource.IDEQ(req.ID), resource.KindEQ(kindGuestbook), resource.OwnerIDEQ(page.ID)).Only(ctx)
		if err != nil {
			return errNotFound
		}
		if messageRevision(r) != req.Revision {
			return errResourceConflict
		}
		var m guestbookMessage
		if decodeStrict(r.Data, &m) != nil {
			return errors.New("unreadable")
		}
		switch req.Action {
		case "delete":
			return s.Store.Ent.Resource.DeleteOne(r).Exec(ctx)
		case "status":
			if !catalogChoice(req.Status, "pending", "approved") {
				return errBadRequest
			}
			m.Status = req.Status
		case "reply":
			reply := strings.TrimSpace(req.Reply)
			if reply == "" || !validText(reply, 2000) {
				return errBadRequest
			}
			now := time.Now()
			m.Reply, m.ReplyAt, m.Status = reply, &now, "replied"
		default:
			return errBadRequest
		}
		return r.Update().SetData(toMap(m)).Exec(ctx)
	})
	switch {
	case errors.Is(err, errResourceConflict):
		failure(c, 409, "guestbook_changed")
	case errors.Is(err, errNotFound):
		failure(c, 404, "guestbook_not_found")
	case errors.Is(err, errBadRequest):
		failure(c, 400, "invalid_guestbook")
	case err != nil:
		failure(c, 503, "guestbook_save_failed")
	default:
		s.audit(c, "guestbook."+req.Action, "page:"+page.Key, "ok")
		c.JSON(200, gin.H{"ok": true})
	}
}

var (
	errNotFound   = errors.New("not_found")
	errBadRequest = errors.New("bad_request")
)
