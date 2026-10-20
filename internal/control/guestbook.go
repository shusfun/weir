package control

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type guestbookConfig struct {
	Enabled        bool `json:"enabled"`
	AllowAnonymous bool `json:"allow_anonymous"`
	RequireAudit   bool `json:"require_audit"`
}
type guestbookMessage struct {
	PageID    int        `json:"page_id"`
	Nickname  string     `json:"nickname"`
	Content   string     `json:"content"`
	Type      string     `json:"type"`
	Status    string     `json:"status"`
	Visible   bool       `json:"visible"`
	CreatedAt time.Time  `json:"created_at"`
	Reply     string     `json:"reply"`
	ReplyAt   *time.Time `json:"reply_at"`
}
type guestbookQuery interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func strictGuestbookJSON(raw []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if len(raw) > 16<<10 || d.Decode(out) != nil || d.Decode(new(any)) != io.EOF {
		return errors.New("invalid guestbook document")
	}
	return nil
}
func guestbookReadPage(ctx context.Context, q guestbookQuery, id int, slug string) (int, guestbookConfig, error) {
	var data []byte
	var e error
	if id > 0 {
		e = q.QueryRowContext(ctx, `SELECT id,data FROM migate.resources WHERE id=$1 AND kind='public-page'`, id).Scan(&id, &data)
	} else {
		e = q.QueryRowContext(ctx, `SELECT id,data FROM migate.resources WHERE key=$1 AND kind='public-page'`, slug).Scan(&id, &data)
	}
	if e != nil {
		return 0, guestbookConfig{}, e
	}
	var m map[string]any
	if json.Unmarshal(data, &m) != nil {
		return 0, guestbookConfig{}, errors.New("page unavailable")
	}
	p, e := decodePublicPage(m)
	if e != nil {
		return 0, guestbookConfig{}, e
	}
	if slug != "" && !p.Enabled {
		return 0, guestbookConfig{}, sql.ErrNoRows
	}
	cfg := guestbookConfig{AllowAnonymous: true, RequireAudit: true}
	e = q.QueryRowContext(ctx, `SELECT data FROM migate.resources WHERE kind='guestbook-config' AND key=$1`, strconv.Itoa(id)).Scan(&data)
	if errors.Is(e, sql.ErrNoRows) {
		return id, cfg, nil
	}
	if e != nil {
		return 0, cfg, e
	}
	if e = strictGuestbookJSON(data, &cfg); e != nil {
		return 0, cfg, e
	}
	return id, cfg, nil
}
func guestbookRevision(v any) string { raw, _ := json.Marshal(v); return digest(string(raw)) }
func guestbookBody(c *gin.Context, out any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
	raw, e := io.ReadAll(c.Request.Body)
	if e != nil || strictGuestbookJSON(raw, out) != nil {
		failure(c, 400, "invalid_guestbook")
		return false
	}
	return true
}
func guestbookFailure(c *gin.Context, e error) {
	if errors.Is(e, sql.ErrNoRows) {
		failure(c, 404, "guestbook_not_found")
	} else {
		failure(c, 503, "guestbook_unavailable")
	}
}
func guestbookTx(c *gin.Context, s *Server) (*sql.Tx, error) {
	tx, e := s.DB.BeginTx(c.Request.Context(), nil)
	if e != nil {
		return nil, e
	}
	if _, e = tx.ExecContext(c.Request.Context(), `SET LOCAL lock_timeout='3s'`); e == nil {
		_, e = tx.ExecContext(c.Request.Context(), `LOCK TABLE migate.resources IN SHARE ROW EXCLUSIVE MODE`)
	}
	if e != nil {
		tx.Rollback()
		return nil, e
	}
	return tx, nil
}
func (s *Server) getGuestbookConfig(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if id <= 0 {
		failure(c, 400, "invalid_guestbook")
		return
	}
	_, cfg, e := guestbookReadPage(c.Request.Context(), s.DB, id, "")
	if e != nil {
		guestbookFailure(c, e)
		return
	}
	c.JSON(200, gin.H{"config": cfg, "revision": guestbookRevision(cfg)})
}
func (s *Server) putGuestbookConfig(c *gin.Context) {
	var req struct {
		Config   guestbookConfig `json:"config"`
		Revision string          `json:"revision"`
		Password string          `json:"password"`
		Code     string          `json:"code"`
		Confirm  bool            `json:"confirm"`
	}
	if !guestbookBody(c, &req) {
		return
	}
	id, _ := strconv.Atoi(c.Param("id"))
	if id <= 0 || !req.Confirm {
		failure(c, 400, "invalid_guestbook")
		return
	}
	if !s.reauth(c, req.Password, req.Code) {
		return
	}
	tx, e := guestbookTx(c, s)
	if e != nil {
		guestbookFailure(c, e)
		return
	}
	defer tx.Rollback()
	_, old, e := guestbookReadPage(c.Request.Context(), tx, id, "")
	if e != nil {
		guestbookFailure(c, e)
		return
	}
	if req.Revision != guestbookRevision(old) {
		failure(c, 409, "guestbook_changed")
		return
	}
	raw, _ := json.Marshal(req.Config)
	_, e = tx.ExecContext(c.Request.Context(), `INSERT INTO migate.resources(kind,key,data,owner_id,created_at) VALUES('guestbook-config',$1,$2,0,NOW()) ON CONFLICT(kind,key) DO UPDATE SET data=EXCLUDED.data`, strconv.Itoa(id), raw)
	if e != nil || tx.Commit() != nil {
		failure(c, 503, "guestbook_save_failed")
		return
	}
	s.audit(c, "guestbook.config", strconv.Itoa(id), "ok")
	c.JSON(200, gin.H{"config": req.Config, "revision": guestbookRevision(req.Config)})
}
func validGuestbookText(v string, max int) bool {
	return utf8.ValidString(v) && utf8.RuneCountInString(v) <= max && !strings.ContainsAny(v, "\x00")
}
func validateGuestbookMessage(m guestbookMessage) error {
	if m.PageID <= 0 || strings.TrimSpace(m.Nickname) == "" || !validGuestbookText(m.Nickname, 20) || strings.TrimSpace(m.Content) == "" || !validGuestbookText(m.Content, 500) || !validGuestbookText(m.Reply, 1000) || !catalogChoice(m.Type, "general", "feature", "bug") || !catalogChoice(m.Status, "pending", "approved", "replied") || m.CreatedAt.IsZero() || m.Status == "pending" && m.Visible || m.Status == "replied" && (m.Reply == "" || m.ReplyAt == nil) {
		return errors.New("invalid message")
	}
	return nil
}
func guestbookPublicMessage(id int, m guestbookMessage) gin.H {
	return gin.H{"id": id, "nickname": m.Nickname, "content": m.Content, "type": m.Type, "status": m.Status, "created_at": m.CreatedAt, "reply": m.Reply, "reply_at": m.ReplyAt}
}
func (s *Server) listGuestbook(c *gin.Context) {
	public := c.Param("slug") != ""
	id, _ := strconv.Atoi(c.Param("id"))
	if !public && id <= 0 {
		failure(c, 400, "invalid_guestbook")
		return
	}
	if public && !s.rate(c, "guestbook-read", 120) {
		return
	}
	// One read transaction gives page state, configuration and list the same snapshot.
	tx, e := s.DB.BeginTx(c.Request.Context(), &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if e != nil {
		guestbookFailure(c, e)
		return
	}
	defer tx.Rollback()
	id, cfg, e := guestbookReadPage(c.Request.Context(), tx, id, c.Param("slug"))
	if e != nil {
		guestbookFailure(c, e)
		return
	}
	if public && !cfg.Enabled {
		failure(c, 404, "guestbook_not_found")
		return
	}
	page, e := strconv.Atoi(c.DefaultQuery("page", "1"))
	status := c.Query("status")
	search := c.Query("search")
	if e != nil || page < 1 || page > 100000 || !catalogChoice(status, "", "pending", "approved", "replied") || len(search) > 200 {
		failure(c, 400, "invalid_guestbook")
		return
	}
	condition := `kind='guestbook-message' AND data->>'page_id'=$1`
	args := []any{strconv.Itoa(id)}
	if public {
		condition += ` AND data->>'visible'='true'`
	} else {
		if status != "" {
			args = append(args, status)
			condition += ` AND data->>'status'=$` + strconv.Itoa(len(args))
		}
		if search != "" {
			args = append(args, search)
			condition += ` AND (strpos(lower(data->>'nickname'),lower($` + strconv.Itoa(len(args)) + `))>0 OR strpos(lower(data->>'content'),lower($` + strconv.Itoa(len(args)) + `))>0)`
		}
	}
	var count int
	if e = tx.QueryRowContext(c.Request.Context(), `SELECT COUNT(*) FROM migate.resources WHERE `+condition, args...).Scan(&count); e != nil {
		guestbookFailure(c, e)
		return
	}
	args = append(args, (page-1)*20)
	rows, e := tx.QueryContext(c.Request.Context(), `SELECT id,data FROM migate.resources WHERE `+condition+` ORDER BY created_at DESC,id DESC LIMIT 20 OFFSET $`+strconv.Itoa(len(args)), args...)
	if e != nil {
		guestbookFailure(c, e)
		return
	}
	defer rows.Close()
	items := []gin.H{}
	for rows.Next() {
		var mid int
		var raw []byte
		var m guestbookMessage
		if rows.Scan(&mid, &raw) != nil || strictGuestbookJSON(raw, &m) != nil || validateGuestbookMessage(m) != nil {
			failure(c, 503, "guestbook_unavailable")
			return
		}
		view := guestbookPublicMessage(mid, m)
		if !public {
			view["visible"] = m.Visible
			view["revision"] = guestbookRevision(m)
		}
		items = append(items, view)
	}
	if rows.Err() != nil {
		failure(c, 503, "guestbook_unavailable")
		return
	}
	c.JSON(200, gin.H{"items": items, "page": page, "page_size": 20, "total": count, "config": gin.H{"enabled": cfg.Enabled, "allow_anonymous": cfg.AllowAnonymous}})
}
func (s *Server) submitGuestbook(c *gin.Context) {
	if !s.rate(c, "guestbook-submit", 6) {
		return
	}
	var req struct {
		Nickname string `json:"nickname"`
		Content  string `json:"content"`
		Type     string `json:"type"`
	}
	if !guestbookBody(c, &req) {
		return
	}
	req.Nickname = strings.TrimSpace(req.Nickname)
	if req.Nickname == "" {
		req.Nickname = "匿名用户"
	}
	if req.Type == "" {
		req.Type = "general"
	}
	m := guestbookMessage{Nickname: req.Nickname, Content: strings.TrimSpace(req.Content), Type: req.Type, Status: "pending", CreatedAt: time.Now().UTC()}
	tx, e := guestbookTx(c, s)
	if e != nil {
		guestbookFailure(c, e)
		return
	}
	defer tx.Rollback()
	id, cfg, e := guestbookReadPage(c.Request.Context(), tx, 0, c.Param("slug"))
	if e != nil {
		guestbookFailure(c, e)
		return
	}
	if !cfg.Enabled {
		failure(c, 404, "guestbook_not_found")
		return
	}
	if !cfg.AllowAnonymous {
		s.authenticate(c)
		if c.IsAborted() {
			return
		}
	}
	m.PageID = id
	m.Visible = !cfg.RequireAudit
	if m.Visible {
		m.Status = "approved"
	}
	if validateGuestbookMessage(m) != nil {
		failure(c, 400, "invalid_guestbook")
		return
	}
	var count int
	if e = tx.QueryRowContext(c.Request.Context(), `SELECT COUNT(*) FROM migate.resources WHERE kind='guestbook-message' AND data->>'page_id'=$1`, strconv.Itoa(id)).Scan(&count); e != nil {
		guestbookFailure(c, e)
		return
	}
	if count >= 5000 {
		failure(c, 409, "guestbook_full")
		return
	}
	raw, _ := json.Marshal(m)
	_, e = tx.ExecContext(c.Request.Context(), `INSERT INTO migate.resources(kind,key,data,owner_id,created_at) VALUES('guestbook-message',$1,$2,0,NOW())`, randomToken(24), raw)
	if e != nil || tx.Commit() != nil {
		failure(c, 503, "guestbook_save_failed")
		return
	}
	c.JSON(201, gin.H{"accepted": true, "pending": !m.Visible})
}
func (s *Server) moderateGuestbook(c *gin.Context) {
	var req struct {
		ID       int    `json:"id"`
		Revision string `json:"revision"`
		Action   string `json:"action"`
		Reply    string `json:"reply"`
		Status   string `json:"status"`
	}
	if !guestbookBody(c, &req) {
		return
	}
	id, _ := strconv.Atoi(c.Param("id"))
	if id <= 0 || req.ID <= 0 || !catalogChoice(req.Action, "approve", "hide", "reply", "delete", "status") || !validGuestbookText(req.Reply, 1000) {
		failure(c, 400, "invalid_guestbook")
		return
	}
	tx, e := guestbookTx(c, s)
	if e != nil {
		guestbookFailure(c, e)
		return
	}
	defer tx.Rollback()
	if _, _, e = guestbookReadPage(c.Request.Context(), tx, id, ""); e != nil {
		guestbookFailure(c, e)
		return
	}
	var raw []byte
	var m guestbookMessage
	e = tx.QueryRowContext(c.Request.Context(), `SELECT data FROM migate.resources WHERE kind='guestbook-message' AND id=$1`, req.ID).Scan(&raw)
	if e != nil {
		guestbookFailure(c, e)
		return
	}
	if strictGuestbookJSON(raw, &m) != nil || validateGuestbookMessage(m) != nil {
		failure(c, 503, "guestbook_unavailable")
		return
	}
	if m.PageID != id {
		failure(c, 404, "guestbook_not_found")
		return
	}
	if guestbookRevision(m) != req.Revision {
		failure(c, 409, "guestbook_changed")
		return
	}
	switch req.Action {
	case "approve":
		m.Visible = true
		m.Status = "approved"
		if m.Reply != "" {
			m.Status = "replied"
		}
	case "hide":
		m.Visible = false
	case "reply":
		if strings.TrimSpace(req.Reply) == "" {
			failure(c, 400, "invalid_guestbook")
			return
		}
		m.Reply = strings.TrimSpace(req.Reply)
		now := time.Now().UTC()
		m.ReplyAt = &now
		m.Visible = true
		m.Status = "replied"
	case "status":
		if !catalogChoice(req.Status, "pending", "approved", "replied") || req.Status == "replied" && m.Reply == "" {
			failure(c, 400, "invalid_guestbook")
			return
		}
		m.Status = req.Status
		if req.Status == "pending" {
			m.Visible = false
		}
	}
	if req.Action == "delete" {
		_, e = tx.ExecContext(c.Request.Context(), `DELETE FROM migate.resources WHERE id=$1 AND kind='guestbook-message'`, req.ID)
	} else {
		raw, _ = json.Marshal(m)
		_, e = tx.ExecContext(c.Request.Context(), `UPDATE migate.resources SET data=$1 WHERE id=$2 AND kind='guestbook-message'`, raw, req.ID)
	}
	if e != nil || tx.Commit() != nil {
		failure(c, 503, "guestbook_save_failed")
		return
	}
	s.audit(c, "guestbook."+req.Action, strconv.Itoa(req.ID), "ok")
	c.JSON(200, gin.H{"ok": true})
}
