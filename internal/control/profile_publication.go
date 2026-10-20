package control

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type profilePublication struct {
	Enabled      bool               `json:"enabled"`
	Target       string             `json:"target"`
	ExpiresAt    int64              `json:"expires_at"`
	PublishedAt  int64              `json:"published_at"`
	TokenHash    string             `json:"token_hash"`
	SealedOutput string             `json:"sealed_output"`
	Generation   string             `json:"generation"`
	Visits       []publicationVisit `json:"visits"`
}
type publicationVisit struct {
	At     int64  `json:"at"`
	Result string `json:"result"`
	Target string `json:"target"`
}

func publicationRevision(p profilePublication) string { return digest(p.Generation) }
func publicationKey(id int) string                    { return strconv.Itoa(id) }
func publicationTarget(t string) bool {
	return catalogChoice(t, "links", "base64", "clash", "singbox", "surge", "loon", "quanx", "egern")
}
func publicationView(p profilePublication) gin.H {
	return gin.H{"enabled": p.Enabled, "target": p.Target, "expires_at": p.ExpiresAt, "published_at": p.PublishedAt, "revision": publicationRevision(p)}
}
func (s *Server) getProfilePublication(c *gin.Context) {
	id, e := strconv.Atoi(c.Param("id"))
	if e != nil || id < 1 {
		failure(c, 400, "invalid_publication")
		return
	}
	r, e := s.Store.Resource.Get(c.Request.Context(), id)
	if e != nil || r.Kind != "profiles" {
		failure(c, 404, "profile_not_found")
		return
	}
	var raw []byte
	e = s.DB.QueryRowContext(c.Request.Context(), `SELECT data FROM migate.resources WHERE kind='profile-publication' AND key=$1`, publicationKey(id)).Scan(&raw)
	p := profilePublication{}
	if e != nil && !errors.Is(e, sql.ErrNoRows) || e == nil && json.Unmarshal(raw, &p) != nil {
		failure(c, 503, "publication_unavailable")
		return
	}
	view := publicationView(p)
	view["profile_enabled"] = r.Data["enabled"] != false
	page, _ := strconv.Atoi(c.Query("page"))
	if page < 1 {
		page = 1
	}
	visits := []publicationVisit{}
	for i := len(p.Visits) - 1; i >= 0; i-- {
		v := p.Visits[i]
		if v.At >= time.Now().Add(-7*24*time.Hour).Unix() {
			visits = append(visits, v)
		}
	}
	total := len(visits)
	last := (total + 9) / 10
	if last < 1 {
		last = 1
	}
	if page > last {
		page = last
	}
	start := (page - 1) * 10
	end := start + 10
	if end > total {
		end = total
	}
	view["visits"] = visits[start:end]
	view["page"] = page
	view["total"] = total
	c.JSON(200, view)
}
func (s *Server) putProfilePublication(c *gin.Context) {
	id, e := strconv.Atoi(c.Param("id"))
	if e != nil || id < 1 {
		failure(c, 400, "invalid_publication")
		return
	}
	var req struct {
		Action   string `json:"action"`
		Target   string `json:"target"`
		Days     int    `json:"days"`
		Revision string `json:"revision"`
		Confirm  bool   `json:"confirm"`
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8192)
	d := json.NewDecoder(c.Request.Body)
	d.DisallowUnknownFields()
	if d.Decode(&req) != nil || d.Decode(new(any)) != io.EOF || !req.Confirm || !catalogChoice(req.Action, "publish", "revoke") || req.Action == "publish" && (!publicationTarget(req.Target) || req.Days < 1 || req.Days > 30) {
		failure(c, 400, "invalid_publication")
		return
	}
	if !s.reauth(c, req.Password, req.Code) {
		return
	}
	ctx := c.Request.Context()
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		failure(c, 503, "publication_save_failed")
		return
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `SET LOCAL lock_timeout='3s'`); e == nil {
		_, e = tx.ExecContext(ctx, `LOCK TABLE migate.resources IN SHARE ROW EXCLUSIVE MODE`)
	}
	if e != nil {
		failure(c, 503, "publication_save_failed")
		return
	}
	var data []byte
	if e = tx.QueryRowContext(ctx, `SELECT data FROM migate.resources WHERE id=$1 AND kind='profiles'`, id).Scan(&data); e != nil {
		failure(c, 404, "profile_not_found")
		return
	}
	var profile map[string]any
	if json.Unmarshal(data, &profile) != nil {
		failure(c, 503, "publication_unavailable")
		return
	}
	var oldRaw []byte
	p := profilePublication{}
	e = tx.QueryRowContext(ctx, `SELECT data FROM migate.resources WHERE kind='profile-publication' AND key=$1`, publicationKey(id)).Scan(&oldRaw)
	if e != nil && !errors.Is(e, sql.ErrNoRows) || e == nil && json.Unmarshal(oldRaw, &p) != nil {
		failure(c, 503, "publication_unavailable")
		return
	}
	if req.Revision != publicationRevision(p) {
		failure(c, 409, "publication_changed")
		return
	}
	token := ""
	p.Generation = randomToken(24)
	if req.Action == "publish" {
		if profile["enabled"] == false {
			failure(c, 409, "profile_disabled")
			return
		}
		result, status, code := s.renderProfile(ctx, id, req.Target)
		if code != "" {
			failure(c, status, code)
			return
		}
		output, ok := result["output"].(string)
		if !ok || len(output) == 0 || len(output) > 2<<20 {
			failure(c, 422, "publication_output_invalid")
			return
		}
		// Do not publish fallback inventory without an explicit successful refresh.
		warnings, _ := result["warnings"].([]string)
		if len(warnings) > 0 {
			failure(c, 422, "publication_stale_source")
			return
		}
		sealed, err := s.Crypto.Encrypt("profile-publication:" + publicationKey(id) + ":" + output)
		if err != nil {
			failure(c, 503, "publication_save_failed")
			return
		}
		token = randomToken(32)
		p.Enabled = true
		p.Target = req.Target
		p.TokenHash = digest(token)
		p.SealedOutput = sealed
		p.PublishedAt = time.Now().Unix()
		p.ExpiresAt = time.Now().Add(time.Duration(req.Days) * 24 * time.Hour).Unix()
	} else {
		p.Enabled = false
		p.TokenHash = ""
		p.SealedOutput = ""
	}
	raw, _ := json.Marshal(p)
	_, e = tx.ExecContext(ctx, `INSERT INTO migate.resources(kind,key,data,owner_id,created_at) VALUES('profile-publication',$1,$2,0,NOW()) ON CONFLICT(kind,key) DO UPDATE SET data=EXCLUDED.data`, publicationKey(id), raw)
	if e != nil || tx.Commit() != nil {
		failure(c, 503, "publication_save_failed")
		return
	}
	s.audit(c, "profile.publication."+req.Action, publicationKey(id), "ok")
	view := publicationView(p)
	if token != "" {
		view["url"] = publicLink(c.Request, "/share/"+token)
	}
	c.JSON(200, view)
}
func (s *Server) serveProfilePublication(c *gin.Context) {
	token := c.Param("token")
	if len(token) != 64 || !validHex(token) {
		failure(c, 404, "share_not_found")
		return
	}
	s.servePublication(c, token, 0)
}
func (s *Server) servePublication(c *gin.Context, token string, directoryProfile int) {
	if !s.rate(c, "profile-share", 60) {
		return
	}
	ctx := c.Request.Context()
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		failure(c, 503, "publication_unavailable")
		return
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `SET LOCAL lock_timeout='3s'`); e == nil {
		_, e = tx.ExecContext(ctx, `LOCK TABLE migate.resources IN ROW EXCLUSIVE MODE`)
	}
	if e != nil {
		failure(c, 503, "publication_unavailable")
		return
	}
	// Join and lock the profile as well: disable/delete cannot race a successful response.
	var rowID, profileID int
	var raw, profileRaw []byte
	where := "p.data->>'token_hash'=$1"
	var parameter any = digest(token)
	if directoryProfile > 0 {
		where = "r.id=$1"
		parameter = directoryProfile
	}
	e = tx.QueryRowContext(ctx, `SELECT p.id,r.id,p.data,r.data FROM migate.resources p JOIN migate.resources r ON p.key=r.id::text AND r.kind='profiles' WHERE p.kind='profile-publication' AND `+where+` FOR UPDATE OF p,r`, parameter).Scan(&rowID, &profileID, &raw, &profileRaw)
	if errors.Is(e, sql.ErrNoRows) {
		failure(c, 404, "share_not_found")
		return
	}
	if e != nil {
		failure(c, 503, "publication_unavailable")
		return
	}
	var p profilePublication
	var profile map[string]any
	if json.Unmarshal(raw, &p) != nil || json.Unmarshal(profileRaw, &profile) != nil {
		failure(c, 503, "publication_unavailable")
		return
	}
	if directoryProfile > 0 {
		var pageID int
		var pageRaw, directoryRaw []byte
		err := tx.QueryRowContext(ctx, `SELECT r.id,r.data FROM migate.resources r WHERE r.kind='public-page' AND r.key=$1 FOR UPDATE`, c.Param("slug")).Scan(&pageID, &pageRaw)
		if errors.Is(err, sql.ErrNoRows) {
			failure(c, 404, "share_not_found")
			return
		}
		if err != nil {
			failure(c, 503, "publication_directory_unavailable")
			return
		}
		var data map[string]any
		if json.Unmarshal(pageRaw, &data) != nil {
			failure(c, 503, "publication_directory_unavailable")
			return
		}
		page, err := decodePublicPage(data)
		if err != nil || !page.Enabled {
			failure(c, 404, "share_not_found")
			return
		}
		err = tx.QueryRowContext(ctx, `SELECT data->'entries' FROM migate.resources WHERE kind='page-publications' AND key=$1 FOR UPDATE`, strconv.Itoa(pageID)).Scan(&directoryRaw)
		if errors.Is(err, sql.ErrNoRows) {
			failure(c, 404, "share_not_found")
			return
		}
		if err != nil {
			failure(c, 503, "publication_directory_unavailable")
			return
		}
		var entries []directoryPublication
		if json.Unmarshal(directoryRaw, &entries) != nil || len(entries) > 24 {
			failure(c, 503, "publication_directory_unavailable")
			return
		}
		allowed := false
		for _, v := range entries {
			if v.ProfileID == profileID && v.Generation == p.Generation {
				allowed = true
			}
		}
		if !allowed {
			failure(c, 404, "share_not_found")
			return
		}
	}
	now := time.Now().Unix()
	result := "served"
	if !p.Enabled || p.ExpiresAt <= now || profile["enabled"] == false {
		result = "unavailable"
	}
	output := ""
	if result == "served" {
		plain, err := s.Crypto.Decrypt(p.SealedOutput)
		prefix := "profile-publication:" + publicationKey(profileID) + ":"
		if err != nil || !strings.HasPrefix(plain, prefix) {
			failure(c, 503, "publication_unavailable")
			return
		}
		output = strings.TrimPrefix(plain, prefix)
	}
	visits := []publicationVisit{}
	for _, v := range p.Visits {
		if v.At >= now-7*86400 {
			visits = append(visits, v)
		}
	}
	visits = append(visits, publicationVisit{At: now, Result: result, Target: p.Target})
	if len(visits) > 200 {
		visits = visits[len(visits)-200:]
	}
	p.Visits = visits
	updated, _ := json.Marshal(p)
	if _, e = tx.ExecContext(ctx, `UPDATE migate.resources SET data=$1 WHERE id=$2`, updated, rowID); e != nil {
		failure(c, 503, "publication_unavailable")
		return
	}
	if tx.Commit() != nil {
		failure(c, 503, "publication_unavailable")
		return
	}
	if result != "served" {
		failure(c, 404, "share_not_found")
		return
	}
	mime := "text/plain; charset=utf-8"
	if p.Target == "singbox" {
		mime = "application/json; charset=utf-8"
	}
	c.Header("Content-Disposition", `attachment; filename="migate-share.txt"`)
	c.Data(200, mime, []byte(output))
}
func validHex(s string) bool {
	return len(s) > 0 && bytes.IndexFunc([]byte(s), func(r rune) bool { return !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') }) < 0
}
