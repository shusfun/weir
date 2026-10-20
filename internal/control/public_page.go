package control

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/imzyb/MiGate/ent"
	"github.com/imzyb/MiGate/ent/resource"
)

var publicSlug = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}[a-z0-9]$`)

type publicPageSettings struct {
	Name              string `json:"name"`
	Slug              string `json:"slug"`
	Title             string `json:"title"`
	Description       string `json:"description"`
	Notice            string `json:"notice"`
	NoticeTitle       string `json:"notice_title,omitempty"`
	NoticeType        string `json:"notice_type,omitempty"`
	NoticeEnabled     *bool  `json:"notice_enabled,omitempty"`
	NoticeDismissible bool   `json:"notice_dismissible,omitempty"`
	Enabled           bool   `json:"enabled"`
	ShowLogin         bool   `json:"show_login"`
	Theme             string `json:"theme,omitempty"`
	ShowShare         bool   `json:"show_share,omitempty"`
	PageIDs           []int  `json:"page_ids,omitempty"`
}

func decodePublicPage(data map[string]any) (publicPageSettings, error) {
	var page publicPageSettings
	b, err := json.Marshal(data)
	if err != nil || len(b) > 32<<10 {
		return page, fmt.Errorf("invalid public page size")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err = d.Decode(&page); err != nil {
		return page, fmt.Errorf("unsupported public page field")
	}
	if !publicSlug.MatchString(page.Slug) || page.Title == "" || len(page.Title) > 200 || len(page.Name) > 200 || len(page.Description) > 8192 || len(page.Notice) > 8192 {
		return page, fmt.Errorf("invalid public page content")
	}
	if len(page.NoticeTitle) > 200 || !catalogChoice(page.NoticeType, "", "info", "success", "warning", "error") {
		return page, fmt.Errorf("invalid announcement settings")
	}
	if page.Theme != "" && page.Theme != "violet" && page.Theme != "ocean" && page.Theme != "forest" {
		return page, fmt.Errorf("invalid public theme")
	}
	if len(page.PageIDs) > 24 {
		return page, fmt.Errorf("too many public cards")
	}
	seen := map[int]bool{}
	for _, id := range page.PageIDs {
		if id <= 0 || seen[id] {
			return page, fmt.Errorf("invalid public card reference")
		}
		seen[id] = true
	}
	return page, nil
}
func (s *Server) publicPage(c *gin.Context) {
	if !publicSlug.MatchString(c.Param("slug")) {
		failure(c, 404, "page_not_found")
		return
	}
	if !s.rate(c, "public-page", 120) {
		return
	}
	r, err := s.Store.Resource.Query().Where(resource.KindEQ("public-page"), resource.KeyEQ(c.Param("slug"))).Only(c.Request.Context())
	if err != nil {
		if !ent.IsNotFound(err) {
			failure(c, 503, "page_unavailable")
			return
		}
		failure(c, 404, "page_not_found")
		return
	}
	p, err := decodePublicPage(r.Data)
	if err != nil || !p.Enabled {
		failure(c, 404, "page_not_found")
		return
	}
	cards := []gin.H{}
	if len(p.PageIDs) > 0 {
		// Resolve only independently published pages. A forged inventory/user ID,
		// deleted page or withdrawn page never becomes an anonymous resource.
		rows, queryErr := s.Store.Resource.Query().Where(resource.KindEQ("public-page"), resource.IDIn(p.PageIDs...)).All(c.Request.Context())
		if queryErr != nil {
			failure(c, 503, "page_unavailable")
			return
		}
		published := map[int]publicPageSettings{}
		for _, row := range rows {
			card, decodeErr := decodePublicPage(row.Data)
			if decodeErr == nil && card.Enabled && row.ID != r.ID {
				published[row.ID] = card
			}
		}
		for _, id := range p.PageIDs {
			if card, ok := published[id]; ok {
				description := card.Description
				if utf8.RuneCountInString(description) > 240 {
					description = string([]rune(description)[:240]) + "…"
				}
				cards = append(cards, gin.H{"title": card.Title, "description": description, "slug": card.Slug})
			}
		}
	}
	theme := p.Theme
	if theme == "" {
		theme = "violet"
	}
	// Explicit allowlist; no inventory, user, subscription, owner or model fields.
	c.Header("Cache-Control", "no-store")
	notice := p.Notice
	noticeTitle := p.NoticeTitle
	if p.NoticeEnabled != nil && !*p.NoticeEnabled {
		notice = ""
		noticeTitle = ""
	}
	noticeType := p.NoticeType
	if noticeType == "" {
		noticeType = "info"
	}
	revision := digest(notice + "\x00" + noticeTitle + "\x00" + noticeType + fmt.Sprint(p.NoticeDismissible))
	projection := gin.H{"title": p.Title, "description": p.Description, "notice": notice, "notice_title": noticeTitle, "notice_type": noticeType, "notice_dismissible": p.NoticeDismissible, "notice_revision": revision, "show_login": p.ShowLogin, "theme": theme, "show_share": p.ShowShare, "cards": cards}
	subscriptions, e := s.publicSubscriptionCards(c.Request.Context(), r.ID, p.Slug)
	if e != nil {
		failure(c, 503, "page_unavailable")
		return
	}
	if len(subscriptions) > 0 {
		projection["subscriptions"] = subscriptions
	}
	_, guestbook, e := guestbookReadPage(c.Request.Context(), s.DB, r.ID, "")
	if e != nil {
		failure(c, 503, "page_unavailable")
		return
	}
	if guestbook.Enabled {
		projection["guestbook"] = gin.H{"enabled": true, "allow_anonymous": guestbook.AllowAnonymous}
	}
	c.JSON(200, projection)
}
