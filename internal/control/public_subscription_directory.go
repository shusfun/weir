package control

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"io"
	"net/http"
	"strconv"
	"time"
)

type directoryPublication struct {
	ProfileID  int    `json:"profile_id"`
	Generation string `json:"generation"`
	Title      string `json:"title"`
}
type pagePublicationDocument struct {
	Entries  []directoryPublication `json:"entries"`
	Revision string                 `json:"revision"`
}

func directoryRevision(entries []directoryPublication) string {
	if entries == nil {
		entries = []directoryPublication{}
	}
	raw, _ := json.Marshal(entries)
	return digest(string(raw))
}
func (s *Server) getPagePublications(c *gin.Context) {
	id, e := strconv.Atoi(c.Param("id"))
	if e != nil || id < 1 {
		failure(c, 400, "invalid_publication_directory")
		return
	}
	page, e := s.Store.Resource.Get(c.Request.Context(), id)
	if e != nil || page.Kind != "public-page" {
		failure(c, 404, "page_not_found")
		return
	}
	entries := []directoryPublication{}
	var raw []byte
	e = s.DB.QueryRowContext(c.Request.Context(), `SELECT data->'entries' FROM migate.resources WHERE kind='page-publications' AND key=$1`, strconv.Itoa(id)).Scan(&raw)
	if e != nil && !errors.Is(e, sql.ErrNoRows) || e == nil && json.Unmarshal(raw, &entries) != nil {
		failure(c, 503, "publication_directory_unavailable")
		return
	}
	choices := []gin.H{}
	// Publication IDs are server-owned profile IDs. Never expose original node URLs or tokens.
	rows, e := s.DB.QueryContext(c.Request.Context(), `SELECT r.id,r.data->>'name',p.data FROM migate.resources p JOIN migate.resources r ON p.key=r.id::text AND r.kind='profiles' WHERE p.kind='profile-publication' AND r.data->'enabled' IS DISTINCT FROM 'false'::jsonb ORDER BY r.id DESC LIMIT 2001`)
	if e != nil {
		failure(c, 503, "publication_directory_unavailable")
		return
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var profile int
		var name sql.NullString
		var data []byte
		if rows.Scan(&profile, &name, &data) != nil {
			failure(c, 503, "publication_directory_unavailable")
			return
		}
		count++
		if count > 2000 {
			failure(c, 503, "publication_directory_limit")
			return
		}
		var p profilePublication
		if json.Unmarshal(data, &p) != nil {
			failure(c, 503, "publication_directory_unavailable")
			return
		}
		if p.Enabled && p.ExpiresAt > time.Now().Unix() {
			choices = append(choices, gin.H{"profile_id": profile, "name": name.String, "generation": p.Generation, "target": p.Target, "expires_at": p.ExpiresAt})
		}
	}
	if rows.Err() != nil {
		failure(c, 503, "publication_directory_unavailable")
		return
	}
	c.JSON(200, gin.H{"entries": entries, "revision": directoryRevision(entries), "choices": choices, "page_enabled": page.Data["enabled"] == true})
}
func (s *Server) putPagePublications(c *gin.Context) {
	id, e := strconv.Atoi(c.Param("id"))
	if e != nil || id < 1 {
		failure(c, 400, "invalid_publication_directory")
		return
	}
	var req struct {
		Entries  []directoryPublication `json:"entries"`
		Revision string                 `json:"revision"`
		Password string                 `json:"password"`
		Code     string                 `json:"code"`
		Confirm  bool                   `json:"confirm"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
	d := json.NewDecoder(c.Request.Body)
	d.DisallowUnknownFields()
	if d.Decode(&req) != nil || d.Decode(new(any)) != io.EOF || !req.Confirm || len(req.Entries) > 24 {
		failure(c, 400, "invalid_publication_directory")
		return
	}
	seen := map[int]bool{}
	for _, v := range req.Entries {
		if v.ProfileID < 1 || seen[v.ProfileID] || !catalogText(v.Title, 200, true) || len(v.Generation) != 48 || !validHex(v.Generation) {
			failure(c, 400, "invalid_publication_directory")
			return
		}
		seen[v.ProfileID] = true
	}
	if !s.reauth(c, req.Password, req.Code) {
		return
	}
	ctx := c.Request.Context()
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		failure(c, 503, "publication_directory_save_failed")
		return
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `SET LOCAL lock_timeout='3s'`); e == nil {
		_, e = tx.ExecContext(ctx, `LOCK TABLE migate.resources IN SHARE ROW EXCLUSIVE MODE`)
	}
	if e != nil {
		failure(c, 503, "publication_directory_save_failed")
		return
	}
	var pageRaw []byte
	if tx.QueryRowContext(ctx, `SELECT data FROM migate.resources WHERE kind='public-page' AND id=$1`, id).Scan(&pageRaw) != nil {
		failure(c, 404, "page_not_found")
		return
	}
	var raw []byte
	old := []directoryPublication{}
	e = tx.QueryRowContext(ctx, `SELECT data->'entries' FROM migate.resources WHERE kind='page-publications' AND key=$1`, strconv.Itoa(id)).Scan(&raw)
	if e != nil && !errors.Is(e, sql.ErrNoRows) || e == nil && json.Unmarshal(raw, &old) != nil {
		failure(c, 503, "publication_directory_unavailable")
		return
	}
	if req.Revision != directoryRevision(old) {
		failure(c, 409, "publication_directory_changed")
		return
	}
	for _, entry := range req.Entries {
		var data, profileRaw []byte
		if tx.QueryRowContext(ctx, `SELECT p.data,r.data FROM migate.resources p JOIN migate.resources r ON p.key=r.id::text AND r.kind='profiles' WHERE p.kind='profile-publication' AND r.id=$1`, entry.ProfileID).Scan(&data, &profileRaw) != nil {
			failure(c, 409, "publication_directory_changed")
			return
		}
		var p profilePublication
		var profile map[string]any
		if json.Unmarshal(data, &p) != nil || json.Unmarshal(profileRaw, &profile) != nil {
			failure(c, 503, "publication_directory_unavailable")
			return
		}
		if !p.Enabled || p.ExpiresAt <= time.Now().Unix() || p.Generation != entry.Generation || profile["enabled"] == false {
			failure(c, 409, "publication_directory_changed")
			return
		}
	}
	if req.Entries == nil {
		req.Entries = []directoryPublication{}
	}
	raw, _ = json.Marshal(map[string]any{"entries": req.Entries})
	_, e = tx.ExecContext(ctx, `INSERT INTO migate.resources(kind,key,data,owner_id,created_at) VALUES('page-publications',$1,$2,0,NOW()) ON CONFLICT(kind,key) DO UPDATE SET data=EXCLUDED.data`, strconv.Itoa(id), raw)
	if e != nil || tx.Commit() != nil {
		failure(c, 503, "publication_directory_save_failed")
		return
	}
	s.audit(c, "page.publications", strconv.Itoa(id), "ok")
	c.JSON(200, pagePublicationDocument{req.Entries, directoryRevision(req.Entries)})
}
func (s *Server) publicSubscriptionCards(ctx context.Context, pageID int, slug string) ([]gin.H, error) {
	var raw []byte
	entries := []directoryPublication{}
	e := s.DB.QueryRowContext(ctx, `SELECT data->'entries' FROM migate.resources WHERE kind='page-publications' AND key=$1`, strconv.Itoa(pageID)).Scan(&raw)
	if errors.Is(e, sql.ErrNoRows) {
		return []gin.H{}, nil
	}
	if e != nil {
		return nil, e
	}
	if e = json.Unmarshal(raw, &entries); e != nil {
		return nil, e
	}
	if len(entries) > 24 {
		return nil, errors.New("invalid directory")
	}
	cards := []gin.H{}
	for _, entry := range entries {
		var data, profileRaw []byte
		e = s.DB.QueryRowContext(ctx, `SELECT p.data,r.data FROM migate.resources p JOIN migate.resources r ON p.key=r.id::text AND r.kind='profiles' WHERE p.kind='profile-publication' AND r.id=$1`, entry.ProfileID).Scan(&data, &profileRaw)
		if errors.Is(e, sql.ErrNoRows) {
			continue
		}
		if e != nil {
			return nil, e
		}
		var p profilePublication
		var profile map[string]any
		if json.Unmarshal(data, &p) != nil || json.Unmarshal(profileRaw, &profile) != nil {
			return nil, errors.New("invalid publication")
		}
		if p.Enabled && p.Generation == entry.Generation && p.ExpiresAt > time.Now().Unix() && profile["enabled"] != false {
			cards = append(cards, gin.H{"title": entry.Title, "target": p.Target, "expires_at": p.ExpiresAt, "path": "/api/v1/pages/" + slug + "/subscriptions/" + strconv.Itoa(entry.ProfileID)})
		}
	}
	return cards, nil
}
func (s *Server) serveDirectoryPublication(c *gin.Context) {
	if !publicSlug.MatchString(c.Param("slug")) {
		failure(c, 404, "share_not_found")
		return
	}
	id, e := strconv.Atoi(c.Param("id"))
	if e != nil || id < 1 {
		failure(c, 404, "share_not_found")
		return
	}
	// The shared reader performs the page and generation checks under the same transaction.
	s.servePublication(c, "", id)
}
