package center

// Adapted interaction/data contract from fixed MiSub AnnouncementSettings.vue.
// Plain text only; public distribution always requires explicit audience opt-in.
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/imzyb/MiGate/internal/store/ent"
	"github.com/imzyb/MiGate/internal/store/ent/resource"
)

type globalAnnouncement struct {
	ContentFormat string `json:"content_format,omitempty"`
	Enabled       bool   `json:"enabled"`
	Authenticated bool   `json:"authenticated"`
	Public        bool   `json:"public"`
	Title         string `json:"title"`
	Content       string `json:"content"`
	Type          string `json:"type"`
	Dismissible   bool   `json:"dismissible"`
}
type announcementDocument struct {
	Announcement globalAnnouncement `json:"announcement"`
	Revision     string             `json:"revision"`
}

func defaultAnnouncement() globalAnnouncement {
	return globalAnnouncement{Type: "info", Dismissible: true}
}
func announcementRevision(a globalAnnouncement) string {
	b, _ := json.Marshal(a)
	return digest(string(b))
}
func decodeAnnouncement(raw []byte) (globalAnnouncement, error) {
	var a globalAnnouncement
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if len(raw) > 24<<10 || d.Decode(&a) != nil || d.Decode(new(any)) != io.EOF || !utf8.ValidString(a.Title+a.Content) || !catalogText(a.Title, 200, false) || len(a.Content) > 8192 || !catalogChoice(a.Type, "info", "success", "warning", "error") || a.Enabled && (strings.TrimSpace(a.Content) == "" || !a.Authenticated && !a.Public) {
		return a, errors.New("invalid_announcement")
	}
	for _, r := range a.Content {
		if r < 32 && r != '\n' && r != '\r' && r != '\t' || r == 127 {
			return a, errors.New("invalid_announcement")
		}
	}
	if _, e := parseNoticeContent(a.Content, a.ContentFormat); e != nil {
		return a, e
	}
	return a, nil
}
func (s *Server) readAnnouncement(ctx context.Context) (globalAnnouncement, error) {
	r, e := s.Store.Ent.Resource.Query().Where(resource.KindEQ("settings"), resource.KeyEQ("global-announcement")).Only(ctx)
	if ent.IsNotFound(e) {
		return defaultAnnouncement(), nil
	}
	if e != nil {
		return globalAnnouncement{}, e
	}
	b, e := json.Marshal(r.Data)
	if e != nil {
		return globalAnnouncement{}, e
	}
	return decodeAnnouncement(b)
}
func (s *Server) getAnnouncement(c *gin.Context) {
	a, e := s.readAnnouncement(c.Request.Context())
	if e != nil {
		failure(c, 503, "announcement_unavailable")
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(200, announcementDocument{a, announcementRevision(a)})
}
func (s *Server) publishedAnnouncement(c *gin.Context) {
	if c.GetString("role") == "" && !s.rate(c, "announcement-public", 120) {
		return
	}
	a, e := s.readAnnouncement(c.Request.Context())
	if e != nil {
		failure(c, 503, "announcement_unavailable")
		return
	}
	c.Header("Cache-Control", "no-store")
	visible := a.Enabled && ((c.GetString("role") != "" && a.Authenticated) || (c.GetString("role") == "" && a.Public))
	if !visible {
		c.JSON(200, gin.H{"announcement": nil})
		return
	}
	// Revision follows visible content, not audience/settings changes.
	r := digest(a.Content + "\x00" + a.Title + "\x00" + a.Type + stringBool(a.Dismissible) + "\x00" + a.ContentFormat)
	projection := gin.H{"notice": a.Content, "notice_title": a.Title, "notice_type": a.Type, "notice_dismissible": a.Dismissible, "notice_revision": r}
	if a.ContentFormat == "html" {
		nodes, e := parseNoticeContent(a.Content, a.ContentFormat)
		if e != nil {
			failure(c, 503, "announcement_unavailable")
			return
		}
		projection["notice_nodes"] = nodes
	}
	c.JSON(200, gin.H{"announcement": projection})
}
func stringBool(v bool) string {
	if v {
		return "true"
	}
	return "false"
}
func (s *Server) putAnnouncement(c *gin.Context) {
	var req struct {
		Announcement json.RawMessage `json:"announcement"`
		Revision     string          `json:"revision"`
		Password     string          `json:"password"`
		Code         string          `json:"code"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32<<10)
	d := json.NewDecoder(c.Request.Body)
	d.DisallowUnknownFields()
	if d.Decode(&req) != nil || d.Decode(new(any)) != io.EOF || len(req.Announcement) == 0 {
		failure(c, 400, "invalid_announcement")
		return
	}
	a, e := decodeAnnouncement(req.Announcement)
	if e != nil {
		failure(c, 400, "invalid_announcement")
		return
	}
	if !s.reauth(c, req.Password, req.Code) {
		return
	}
	ctx := c.Request.Context()
	e = s.updateResource(ctx, "settings", "global-announcement", func(cur map[string]any) (map[string]any, error) {
		old := defaultAnnouncement()
		if cur != nil {
			raw, _ := json.Marshal(cur)
			var err error
			if old, err = decodeAnnouncement(raw); err != nil {
				return nil, err
			}
		}
		if req.Revision != announcementRevision(old) {
			return nil, errResourceConflict
		}
		raw, _ := json.Marshal(a)
		var next map[string]any
		return next, json.Unmarshal(raw, &next)
	})
	if errors.Is(e, errResourceConflict) {
		failure(c, 409, "announcement_changed")
		return
	}
	if e != nil {
		failure(c, 503, "announcement_save_failed")
		return
	}
	s.audit(c, "settings.announcement", "global-announcement", "ok")
	c.JSON(200, announcementDocument{a, announcementRevision(a)})
}

func (s *Server) previewNotice(c *gin.Context) {
	var req struct {
		Content string `json:"content"`
		Format  string `json:"format"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
	d := json.NewDecoder(c.Request.Body)
	d.DisallowUnknownFields()
	if d.Decode(&req) != nil || d.Decode(new(any)) != io.EOF || len(req.Content) > 8192 {
		failure(c, 400, "invalid_notice_markup")
		return
	}
	nodes, e := parseNoticeContent(req.Content, req.Format)
	if e != nil {
		failure(c, 400, "invalid_notice_markup")
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(200, gin.H{"nodes": nodes})
}
