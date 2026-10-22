package center

import (
	"context"
	"encoding/json"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

var sessionIDPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type sessionRow struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	IP        string    `json:"ip"`
	Current   bool      `json:"current"`
	MFA       bool      `json:"mfa"`
	ExpiresIn int64     `json:"expires_in"`
}

func (s *Server) sessionRoutes(auth *gin.RouterGroup) {
	auth.GET("/sessions", func(c *gin.Context) { s.listSessions(c, c.GetInt("user_id")) })
	auth.DELETE("/sessions/:id", func(c *gin.Context) { s.revokeSession(c, c.GetInt("user_id"), c.Param("id")) })
	auth.POST("/sessions/revoke-others", s.revokeOtherSessions)
}

// loadSessions 读取某用户当前的全部有效会话；读取失败或超量时整体报错，不返回部分列表。
func (s *Server) loadSessions(ctx context.Context, uid int, currentKey string) ([]sessionRow, error) {
	keys, err := s.Store.KV.Keys(ctx, sessionPrefix(uid), 5000)
	if err != nil {
		return nil, err
	}
	rows := make([]sessionRow, 0, len(keys))
	for _, key := range keys {
		raw, ok, err := s.Store.KV.Get(ctx, key)
		if err != nil {
			return nil, err
		}
		var sess Session
		if !ok || json.Unmarshal([]byte(raw), &sess) != nil || sess.UserID != uid {
			continue
		}
		left := int64(time.Until(sess.ExpiresAt).Seconds())
		if left < 0 {
			left = 0
		}
		rows = append(rows, sessionRow{ID: strings.TrimPrefix(key, sessionPrefix(uid)), CreatedAt: sess.CreatedAt, IP: sess.IP, Current: key == currentKey, MFA: sess.MFA, ExpiresIn: left})
	}
	// 当前会话排最前，其余按登录时间从新到旧，顺序稳定。
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Current != rows[j].Current {
			return rows[i].Current
		}
		if !rows[i].CreatedAt.Equal(rows[j].CreatedAt) {
			return rows[i].CreatedAt.After(rows[j].CreatedAt)
		}
		return rows[i].ID < rows[j].ID
	})
	return rows, nil
}

func (s *Server) listSessions(c *gin.Context, uid int) {
	page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
	if err != nil || page < 1 || page > 100000 {
		failure(c, 400, "invalid_page")
		return
	}
	size, err := strconv.Atoi(c.DefaultQuery("page_size", "10"))
	if err != nil || size < 1 || size > 100 {
		failure(c, 400, "invalid_page_size")
		return
	}
	q := strings.TrimSpace(c.Query("q"))
	scope := c.DefaultQuery("scope", "all")
	if len(q) > 100 || (scope != "all" && scope != "current" && scope != "other") {
		failure(c, 400, "invalid_query")
		return
	}
	all, err := s.loadSessions(c.Request.Context(), uid, c.GetString("session_key"))
	if err != nil {
		failure(c, 503, "session_directory_unavailable")
		return
	}
	filtered := make([]sessionRow, 0, len(all))
	for _, row := range all {
		if q != "" && !strings.Contains(row.IP, q) {
			continue
		}
		if scope == "current" && !row.Current || scope == "other" && row.Current {
			continue
		}
		filtered = append(filtered, row)
	}
	pages := (len(filtered) + size - 1) / size
	start := (page - 1) * size
	if start > len(filtered) {
		start = len(filtered)
	}
	end := start + size
	if end > len(filtered) {
		end = len(filtered)
	}
	c.JSON(200, gin.H{"items": filtered[start:end], "total": len(filtered), "all_total": len(all), "page": page, "page_size": size, "pages": pages})
}

// revokeSession 只接受会话摘要 ID，并且键本身带用户前缀，天然只能撤销本人的会话。
func (s *Server) revokeSession(c *gin.Context, uid int, id string) {
	if !sessionIDPattern.MatchString(id) {
		failure(c, 400, "invalid_session")
		return
	}
	key := sessionPrefix(uid) + id
	if _, ok, err := s.Store.KV.GetDel(c.Request.Context(), key); err != nil {
		failure(c, 503, "session_unavailable")
		return
	} else if !ok {
		failure(c, 404, "session_not_found")
		return
	}
	s.audit(c, "auth.session.revoke", strconv.Itoa(uid), "ok")
	c.JSON(200, gin.H{"ok": true})
}

// revokeOtherSessions 以本次读取到的会话为范围，保留当前登录。
func (s *Server) revokeOtherSessions(c *gin.Context) {
	var req struct {
		Confirm bool `json:"confirm"`
	}
	if !bind(c, &req) || !req.Confirm {
		if !c.IsAborted() {
			failure(c, 400, "confirmation_required")
		}
		return
	}
	uid, current := c.GetInt("user_id"), c.GetString("session_key")
	keys, err := s.Store.KV.Keys(c.Request.Context(), sessionPrefix(uid), 5000)
	if err != nil {
		failure(c, 503, "session_directory_unavailable")
		return
	}
	others := make([]string, 0, len(keys))
	for _, key := range keys {
		if key != current {
			others = append(others, key)
		}
	}
	if err = s.Store.KV.Del(c.Request.Context(), others...); err != nil {
		failure(c, 503, "session_unavailable")
		return
	}
	s.audit(c, "auth.session.revoke-others", strconv.Itoa(uid), "ok")
	c.JSON(200, gin.H{"ok": true, "revoked": len(others), "selected": len(others)})
}
