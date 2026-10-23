package center

// UX/data mapping: fixed MiSub 4952c16 client-handler.js / ClientSettings.vue.
// Catalog links are metadata only: never fetched by the center, no token URL templates.
import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/imzyb/MiGate/internal/store/ent"
	"github.com/imzyb/MiGate/internal/store/ent/resource"
)

type catalogClient struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Platforms   []string `json:"platforms"`
	URL         string   `json:"url"`
	Repo        string   `json:"repo"`
	Icon        string   `json:"icon"`
	IconData    string   `json:"icon_data,omitempty"`
	Format      string   `json:"format"`
	Import      string   `json:"import"`
	Enabled     bool     `json:"enabled"`
}

func (v *catalogClient) UnmarshalJSON(raw []byte) error {
	type wire catalogClient
	var parsed wire
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if e := d.Decode(&parsed); e != nil {
		return e
	}
	*v = catalogClient(parsed)
	return nil
}

type catalogDocument struct {
	Clients  []catalogClient `json:"clients"`
	Revision string          `json:"revision"`
}

var catalogID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,47}$`)
var catalogRepo = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

//go:embed default-client-catalog.json
var defaultCatalogJSON []byte

func defaultClientCatalog() []catalogClient {
	var items []catalogClient
	if json.Unmarshal(defaultCatalogJSON, &items) != nil {
		panic("invalid embedded client catalog")
	}
	return items
}
func catalogChoice(value string, values ...string) bool {
	for _, v := range values {
		if value == v {
			return true
		}
	}
	return false
}
func catalogText(value string, max int, required bool) bool {
	if len(value) > max || required && strings.TrimSpace(value) == "" {
		return false
	}
	for _, c := range value {
		if c < 32 || c == 127 {
			return false
		}
	}
	return true
}
func validateClientCatalog(items []catalogClient) error {
	if len(items) > 50 {
		return errors.New("catalog_limit")
	}
	raw, e := json.Marshal(items)
	if e != nil || len(raw) > 768*1024 {
		return errors.New("catalog_size_limit")
	}
	seen := map[string]bool{}
	for _, v := range items {
		if !catalogID.MatchString(v.ID) || seen[v.ID] || !catalogText(v.Name, 120, true) || !catalogText(v.Description, 600, false) || !catalogChoice(v.Format, "base64", "clash", "singbox", "surge", "loon", "quanx", "egern") || !catalogChoice(v.Import, "copy", "clash") || v.Import == "clash" && v.Format != "clash" || !catalogChoice(v.Icon, "monitor", "phone", "globe", "shield") {
			return errors.New("invalid_catalog_client")
		}
		if _, e := normalizeClientIcon(v.IconData); e != nil {
			return e
		}
		seen[v.ID] = true
		if len(v.Platforms) == 0 || len(v.Platforms) > 6 {
			return errors.New("invalid_catalog_platform")
		}
		platforms := map[string]bool{}
		for _, p := range v.Platforms {
			if !catalogChoice(p, "windows", "macos", "linux", "android", "ios", "harmonyos") || platforms[p] {
				return errors.New("invalid_catalog_platform")
			}
			platforms[p] = true
		}
		if v.Repo != "" && !catalogRepo.MatchString(v.Repo) {
			return errors.New("invalid_catalog_repository")
		}
		if !catalogText(v.URL, 2000, true) || strings.ContainsAny(v.URL, "{}") {
			return errors.New("invalid_catalog_url")
		}
		u, e := url.Parse(v.URL)
		if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return errors.New("invalid_catalog_url")
		}
	}
	return nil
}
func catalogRevision(items []catalogClient) string {
	raw, _ := json.Marshal(items)
	return digest(string(raw))
}
func (s *Server) readClientCatalog(ctx context.Context) (catalogDocument, error) {
	r, e := s.Store.Ent.Resource.Query().Where(resource.KindEQ("settings"), resource.KeyEQ("client-catalog")).Only(ctx)
	if ent.IsNotFound(e) {
		items := defaultClientCatalog()
		return catalogDocument{items, catalogRevision(items)}, nil
	}
	if e != nil {
		return catalogDocument{}, e
	}
	raw, e := json.Marshal(r.Data["clients"])
	if e != nil {
		return catalogDocument{}, e
	}
	var items []catalogClient
	if e = json.Unmarshal(raw, &items); e != nil {
		return catalogDocument{}, e
	}
	if e = validateClientCatalog(items); e != nil {
		return catalogDocument{}, e
	}
	if items == nil {
		items = []catalogClient{}
	}
	return catalogDocument{items, catalogRevision(items)}, nil
}
func (s *Server) getClientCatalog(c *gin.Context) {
	d, e := s.readClientCatalog(c.Request.Context())
	if e != nil {
		failure(c, 503, "client_catalog_unavailable")
		return
	}
	if c.GetString("role") != "admin" {
		items := []catalogClient{}
		for _, v := range d.Clients {
			if v.Enabled {
				items = append(items, v)
			}
		}
		d.Clients = items
		d.Revision = ""
	}
	c.JSON(200, d)
}
func (s *Server) putClientCatalog(c *gin.Context) {
	var req struct {
		Clients  []catalogClient `json:"clients"`
		Revision string          `json:"revision"`
		Password string          `json:"password"`
		Code     string          `json:"code"`
		Reset    bool            `json:"reset"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1024*1024)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&req) != nil || decoder.Decode(new(any)) != io.EOF || (!req.Reset && req.Clients == nil) {
		failure(c, 400, "invalid_request")
		return
	}
	if !s.reauth(c, req.Password, req.Code) {
		return
	}
	if req.Reset {
		req.Clients = defaultClientCatalog()
	}
	if req.Clients == nil {
		req.Clients = []catalogClient{}
	}
	for i := range req.Clients {
		icon, e := normalizeClientIcon(req.Clients[i].IconData)
		if e != nil {
			failure(c, 400, "invalid_catalog_icon")
			return
		}
		req.Clients[i].IconData = icon
	}
	if e := validateClientCatalog(req.Clients); e != nil {
		failure(c, 400, e.Error())
		return
	}
	ctx := c.Request.Context()
	e := s.updateResource(ctx, "settings", "client-catalog", func(cur map[string]any) (map[string]any, error) {
		old := defaultClientCatalog()
		if cur != nil {
			raw, _ := json.Marshal(cur["clients"])
			if err := json.Unmarshal(raw, &old); err != nil {
				return nil, err
			}
		}
		if catalogRevision(old) != req.Revision {
			return nil, errResourceConflict
		}
		raw, _ := json.Marshal(map[string]any{"clients": req.Clients})
		var next map[string]any
		return next, json.Unmarshal(raw, &next)
	})
	if errors.Is(e, errResourceConflict) {
		failure(c, 409, "catalog_changed")
		return
	}
	if e != nil {
		failure(c, 503, "catalog_save_failed")
		return
	}
	s.audit(c, "settings.client-catalog", fmt.Sprintf("%d clients", len(req.Clients)), "ok")
	c.JSON(200, catalogDocument{req.Clients, catalogRevision(req.Clients)})
}
