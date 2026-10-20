package control

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

var revokeOwnedSession = redis.NewScript(`local raw=redis.call('GET',KEYS[1]);if not raw then return 0 end;local ok,s=pcall(cjson.decode,raw);if not ok or tonumber(s.user_id)~=tonumber(ARGV[1]) then return 0 end;return redis.call('DEL',KEYS[1])`)

func validSessionID(v string) bool { return len(v) == 64 && strings.Trim(v, "0123456789abcdef") == "" }

type sessionItem struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	IP        string    `json:"ip"`
	Current   bool      `json:"current"`
	MFA       bool      `json:"mfa"`
	ExpiresIn int64     `json:"expires_in"`
}

// The legacy Redis layout has no per-user index. Bound the full scan by time,
// deduplicate SCAN results, and never return a partial list as a complete one.
func (s *Server) collectSessions(ctx context.Context, id int, current string) ([]sessionItem, error) {
	items := []sessionItem{}
	seen := map[string]bool{}
	iter := s.Redis.Scan(ctx, 0, "migate:session:*", 100).Iterator()
	for iter.Next(ctx) {
		key := iter.Val()
		if seen[key] {
			continue
		}
		seen[key] = true
		raw, err := s.Redis.Get(ctx, key).Bytes()
		if err == redis.Nil {
			continue
		}
		if err != nil {
			return nil, err
		}
		var ses Session
		if json.Unmarshal(raw, &ses) != nil || ses.UserID != id {
			continue
		}
		ttl, err := s.Redis.PTTL(ctx, key).Result()
		if err != nil {
			return nil, err
		}
		if ttl <= 0 {
			continue
		}
		token := strings.TrimPrefix(key, "migate:session:")
		if !validSessionID(token) {
			continue
		}
		items = append(items, sessionItem{token, ses.CreatedAt, ses.IP, key == current, ses.MFA, int64(ttl / time.Second)})
		if len(items) > 2000 {
			return nil, errors.New("session directory limit")
		}
	}
	if err := iter.Err(); err != nil {
		return nil, err
	}
	sort.Slice(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if a.Current != b.Current {
			return a.Current
		}
		if !a.CreatedAt.Equal(b.CreatedAt) {
			return a.CreatedAt.After(b.CreatedAt)
		}
		return a.ID < b.ID
	})
	return items, nil
}
func (s *Server) listSessions(c *gin.Context, id int) {
	page, size := 1, 10
	paginated := c.Query("page") != "" || c.Query("page_size") != ""
	for key, p := range map[string]*int{"page": &page, "page_size": &size} {
		if v := c.Query(key); v != "" {
			n, e := strconv.Atoi(v)
			if e != nil || n < 1 || key == "page_size" && n > 100 || key == "page" && n > 100000 {
				failure(c, 400, "invalid_session_query")
				return
			}
			*p = n
		}
	}
	q := strings.TrimSpace(c.Query("q"))
	scope := c.DefaultQuery("scope", "all")
	if len(q) > 100 || strings.ContainsAny(q, "\r\n\x00") || !catalogChoice(scope, "all", "current", "other") {
		failure(c, 400, "invalid_session_query")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	items, err := s.collectSessions(ctx, id, c.GetString("session_key"))
	if err != nil {
		failure(c, 503, "session_unavailable")
		return
	}
	total := len(items)
	filtered := []sessionItem{}
	for _, item := range items {
		if (scope == "current" && !item.Current) || (scope == "other" && item.Current) || !strings.Contains(strings.ToLower(item.IP), strings.ToLower(q)) {
			continue
		}
		filtered = append(filtered, item)
	}
	c.Header("Cache-Control", "no-store")
	if !paginated {
		c.JSON(200, gin.H{"items": filtered})
		return
	}
	count := len(filtered)
	pages := (count + size - 1) / size
	if pages < 1 {
		pages = 1
	}
	if page > pages {
		page = pages
	}
	start := (page - 1) * size
	end := start + size
	if end > count {
		end = count
	}
	c.JSON(200, gin.H{"items": filtered[start:end], "total": count, "all_total": total, "page": page, "page_size": size, "pages": pages})
}
func (s *Server) revokeOtherSessions(c *gin.Context) {
	var req struct {
		Confirm bool `json:"confirm"`
	}
	if !bind(c, &req) {
		return
	}
	if !req.Confirm {
		failure(c, 400, "session_confirmation_required")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	items, err := s.collectSessions(ctx, c.GetInt("user_id"), c.GetString("session_key"))
	if err != nil {
		failure(c, 503, "session_unavailable")
		return
	}
	keys := []string{}
	for _, item := range items {
		if !item.Current {
			keys = append(keys, "migate:session:"+item.ID)
		}
	}
	// One bounded atomic operation, with ownership and current-key rechecked in
	// Redis. Logins created after the snapshot are deliberately not included.
	revoked := 0
	if len(keys) > 0 {
		script := redis.NewScript(`local n=0;for _,k in ipairs(KEYS) do if k~=ARGV[2] then local raw=redis.call('GET',k);if raw then local ok,s=pcall(cjson.decode,raw);if ok and tonumber(s.user_id)==tonumber(ARGV[1]) then n=n+redis.call('DEL',k) end end end end;return n`)
		revoked, err = script.Run(ctx, s.Redis, keys, c.GetInt("user_id"), c.GetString("session_key")).Int()
		if err != nil {
			failure(c, 503, "session_revoke_unconfirmed")
			return
		}
	}
	s.audit(c, "auth.sessions.revoke-others", "self", "ok")
	c.JSON(200, gin.H{"ok": true, "revoked": revoked, "selected": len(keys)})
}
