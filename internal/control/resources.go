package control

import (
	"encoding/json"
	"entgo.io/ent/dialect/sql"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/imzyb/MiGate/ent"
	"github.com/imzyb/MiGate/ent/resource"
	"github.com/imzyb/MiGate/internal/subscription"
	pipeline "github.com/imzyb/MiGate/services/subscriptions"
	"strconv"
	"strings"
	"time"
)

type ModelConfig struct {
	URL     string `json:"url"`
	Key     string `json:"key"`
	Model   string `json:"model"`
	Enabled bool   `json:"enabled"`
}

func (s *Server) resourceRoutes(admin *gin.RouterGroup) {
	admin.GET("/resources/:kind", s.listResources)
	admin.POST("/resources/:kind", s.putResource)
	admin.DELETE("/resources/:kind/:id", s.deleteResource)
	admin.POST("/sources/:id/refresh", s.refreshSource)
	admin.GET("/sources/usage", s.sourceUsage)
	admin.POST("/laya/test", s.layaTest)
	admin.POST("/mail/:id/retry", s.retryMail)
	admin.GET("/events", s.modelEvents)
	admin.POST("/subscriptions/preview", s.previewPipeline)
	admin.POST("/subscriptions/import", s.importInventory)
	admin.POST("/profiles/preview", s.previewProfile)
	admin.GET("/profiles/order", s.profileOrder)
	admin.POST("/profiles/order", s.saveProfileOrder)
	admin.POST("/nodes/order", s.reorderNodes)
	admin.GET("/nodes/groups", s.getNodeGroups)
	admin.POST("/nodes/groups", s.changeNodeGroups)
	admin.POST("/nodes/group", s.groupNodes)
	admin.POST("/nodes/dedup", s.deduplicateNodes)
}

func (s *Server) previewPipeline(c *gin.Context) {
	var req struct {
		Links     []string            `json:"links"`
		Operators []pipeline.Operator `json:"operators"`
		Target    string              `json:"target"`
	}
	if !bind(c, &req) {
		return
	}
	links, err := pipeline.Run(req.Links, req.Operators, req.Target)
	if err != nil {
		failure(c, 422, "pipeline_unsupported_or_invalid")
		return
	}
	switch req.Target {
	case "egern":
		b, e := egernOutput(links)
		if e != nil {
			failure(c, 422, "format_conversion_failed")
			return
		}
		c.JSON(200, gin.H{"links": links, "output": string(b)})
	case "surge", "loon", "quanx":
		b, e := iniOutput(links, req.Target)
		if e != nil {
			failure(c, 422, "format_conversion_failed")
			return
		}
		c.JSON(200, gin.H{"links": links, "output": string(b)})
	case "clash", "mihomo":
		b, e := clashOutput(links)
		if e != nil {
			failure(c, 422, "format_conversion_failed")
			return
		}
		c.JSON(200, gin.H{"links": links, "output": string(b)})
	case "singbox":
		b, e := singboxOutput(links)
		if e != nil {
			failure(c, 422, "format_conversion_failed")
			return
		}
		c.JSON(200, gin.H{"links": links, "output": string(b)})
	default:
		c.JSON(200, gin.H{"links": links, "output": strings.Join(links, "\n")})
	}
}
func resourceKind(k string) bool {
	switch k {
	case "sources", "nodes", "profiles", "templates", "operators", "public-page", "device-inbound", "device-outbound", "device-routing", "device-certificate", "mail":
		return true
	}
	return false
}
func (s *Server) listResources(c *gin.Context) {
	kind := c.Param("kind")
	if !resourceKind(kind) {
		failure(c, 400, "invalid_kind")
		return
	}
	query := s.Store.Resource.Query().Where(resource.KindEQ(kind))
	if q := strings.TrimSpace(c.Query("q")); q != "" {
		if len(q) > 200 {
			failure(c, 400, "invalid_query")
			return
		}
		q = "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(strings.ToLower(q)) + "%"
		query.Where(func(selector *sql.Selector) {
			selector.Where(sql.P(func(b *sql.Builder) { b.WriteString(`LOWER(COALESCE("data"->>'name',"key")) LIKE `).Arg(q) }))
		})
	}
	if owner := c.Query("owner_id"); owner != "" {
		id, err := strconv.Atoi(owner)
		if err != nil || id <= 0 {
			failure(c, 400, "invalid_owner")
			return
		}
		query.Where(resource.OwnerIDEQ(id))
	}
	page, size := 1, 500
	if c.Query("page") != "" || c.Query("page_size") != "" {
		var err error
		page, err = strconv.Atoi(c.DefaultQuery("page", "1"))
		if err != nil || page < 1 || page > 1000000 {
			failure(c, 400, "invalid_page")
			return
		}
		size, err = strconv.Atoi(c.DefaultQuery("page_size", "20"))
		if err != nil || size < 1 || size > 500 {
			failure(c, 400, "invalid_page_size")
			return
		}
	}
	if kind == "profiles" {
		query.Order(func(selector *sql.Selector) {
			selector.OrderExpr(sql.Expr("COALESCE(CASE WHEN jsonb_typeof(data->'position')='number' THEN (data->>'position')::numeric END,-id) ASC"))
		})
	}
	total, err := query.Clone().Count(c.Request.Context())
	if err != nil {
		failure(c, 500, "query_failed")
		return
	}
	items, err := query.Order(ent.Desc("id")).Offset((page - 1) * size).Limit(size).All(c.Request.Context())
	if err != nil {
		failure(c, 500, "query_failed")
		return
	}
	out := []gin.H{}
	for _, r := range items {
		data := r.Data
		if kind == "device-certificate" {
			data = map[string]any{}
			for key, value := range r.Data {
				data[key] = value
			}
			observation := certificateObservation(r.Data, time.Now())
			for _, key := range []string{"not_before", "not_after", "remaining_seconds"} {
				if value, ok := observation[key]; ok {
					data[key] = value
				}
			}
		}
		if kind == "device-inbound" || kind == "device-outbound" {
			data = map[string]any{}
			for key, value := range r.Data {
				data[key] = value
			}
			if kind == "device-outbound" {
				if text(data["protocol"]) != "shadowsocks" {
					if text(data["username"]) != "" {
						data["has_username"] = true
					}
					delete(data, "username")
				}
				if settings, ok := publicOutboundTransport(r.Data); ok {
					data["transport_options"] = settings
				} else {
					data["transport_options_unavailable"] = true
				}
			}
			for _, key := range []string{"password", "reality_private_key", "hy2_obfs_password", "wg_private_key", "wg_preshared_key", "shadowtls_password", "raw_link", "settings_json"} {
				if text(data[key]) != "" {
					data["has_"+key] = true
				}
				delete(data, key)
			}
		}
		if kind == "mail" {
			data = map[string]any{"to": r.Data["to"], "status": r.Data["status"], "attempts": r.Data["attempts"], "last_error": r.Data["last_error"]}
		}
		view := gin.H{"id": r.ID, "key": r.Key, "owner_id": r.OwnerID, "data": data, "created_at": r.CreatedAt}
		if kind == "nodes" {
			if node, err := subscription.ParseLink(text(r.Data["url"])); err == nil {
				view["node"] = gin.H{"valid": true, "protocol": node.Protocol, "server": node.Address, "port": node.Port}
			} else {
				view["node"] = gin.H{"valid": false}
			}
		}
		out = append(out, view)
	}
	c.JSON(200, gin.H{"items": out, "total": total, "page": page, "page_size": size})
}
func (s *Server) putResource(c *gin.Context) {
	kind := c.Param("kind")
	if kind == "sources" {
		s.sourceMu.Lock()
		defer s.sourceMu.Unlock()
	}
	if !resourceKind(kind) || kind == "mail" || strings.HasPrefix(kind, "device-") {
		failure(c, 400, "invalid_kind")
		return
	}
	var req struct {
		ID   int            `json:"id"`
		Data map[string]any `json:"data"`
	}
	if !bind(c, &req) {
		return
	}
	if len(text(req.Data["name"])) > 200 {
		failure(c, 400, "name_too_long")
		return
	}
	if kind == "sources" {
		if err := validateSource(req.Data); err != nil {
			failure(c, 400, "invalid_source_settings")
			return
		}
		// Cache state is server-owned. Changing the URL invalidates old credentials.
		for _, key := range sourceCacheFields {
			delete(req.Data, key)
		}
	}
	if kind == "nodes" {
		if _, err := subscription.ParseLink(text(req.Data["url"])); err != nil {
			failure(c, 400, "invalid_node")
			return
		}
	}
	if kind == "nodes" {
		if len(text(req.Data["group"])) > 120 || len(text(req.Data["notes"])) > 2000 {
			failure(c, 400, "invalid_node_metadata")
			return
		}
		delete(req.Data, "position")
	}
	if kind == "profiles" {
		if _, err := decodeProfilePrefix(req.Data); err != nil {
			failure(c, 400, "invalid_prefix_settings")
			return
		}
		if v, ok := req.Data["skip_default_operators"]; ok {
			if _, ok := v.(bool); !ok {
				failure(c, 400, "invalid_operator_settings")
				return
			}
		}
		delete(req.Data, "position")
		if err := s.validateProfileReferences(c.Request.Context(), req.Data); err != nil {
			failure(c, 400, "invalid_profile_references")
			return
		}
		ops, err := s.profileOperators(c.Request.Context(), req.Data)
		if err == nil {
			_, err = pipeline.Run(nil, ops, "base64")
		}
		if err != nil {
			failure(c, 400, "invalid_operator_settings")
			return
		}
	}
	if kind == "operators" {
		var ops []pipeline.Operator
		raw, err := json.Marshal(req.Data["operators"])
		if err == nil {
			err = json.Unmarshal(raw, &ops)
		}
		if err == nil {
			_, err = pipeline.Run(nil, ops, "base64")
		}
		if err != nil {
			failure(c, 400, "invalid_operator_settings")
			return
		}
	}
	var r *ent.Resource
	if kind == "templates" {
		if _, err := decodeOutputTemplate(req.Data); err != nil {
			failure(c, 400, "invalid_output_template")
			return
		}
	}
	var err error
	publicKey := ""
	if kind == "public-page" {
		page, e := decodePublicPage(req.Data)
		if e != nil {
			failure(c, 400, "invalid_public_page")
			return
		}
		publicKey = page.Slug
		if len(page.PageIDs) > 0 {
			count, queryErr := s.Store.Resource.Query().Where(resource.KindEQ("public-page"), resource.IDIn(page.PageIDs...)).Count(c.Request.Context())
			if queryErr != nil {
				failure(c, 503, "query_failed")
				return
			}
			if count != len(page.PageIDs) {
				failure(c, 400, "invalid_public_page")
				return
			}
			for _, id := range page.PageIDs {
				if id == req.ID {
					failure(c, 400, "invalid_public_page")
					return
				}
			}
		}
	}
	if req.ID > 0 {
		old, e := s.Store.Resource.Get(c.Request.Context(), req.ID)
		if e != nil || old.Kind != kind {
			failure(c, 404, "not_found")
			return
		}
		if kind == "nodes" || kind == "profiles" {
			if position, ok := old.Data["position"]; ok {
				req.Data["position"] = position
			}
		}
		if kind == "sources" && old.Data["url"] == req.Data["url"] && text(old.Data["user_agent"]) == text(req.Data["user_agent"]) && (old.Data["plus_as_space"] == true) == (req.Data["plus_as_space"] == true) {
			for _, key := range sourceCacheFields {
				if value, ok := old.Data[key]; ok {
					req.Data[key] = value
				}
			}
		}
		update := s.Store.Resource.UpdateOne(old).SetData(req.Data)
		if publicKey != "" {
			update.SetKey(publicKey)
		}
		if kind == "nodes" || kind == "profiles" {
			// Preserve the latest server-owned order even if an editor was opened
			// before a concurrent reorder committed.
			raw, e := json.Marshal(req.Data)
			if e != nil {
				failure(c, 400, "invalid_node_metadata")
				return
			}
			var saved []byte
			err = s.DB.QueryRowContext(c.Request.Context(), `UPDATE migate.resources SET data=($1::jsonb - 'position') || CASE WHEN data ? 'position' THEN jsonb_build_object('position',data->'position') ELSE '{}'::jsonb END WHERE id=$2 AND kind=$3 RETURNING data`, raw, old.ID, kind).Scan(&saved)
			if err == nil {
				err = json.Unmarshal(saved, &old.Data)
				r = old
			}
		} else {
			r, err = update.Save(c.Request.Context())
		}
	} else {
		key := publicKey
		if key == "" {
			key = randomToken(16)
		}
		r, err = s.Store.Resource.Create().SetKind(kind).SetKey(key).SetData(req.Data).Save(c.Request.Context())
	}
	if err != nil {
		if kind == "public-page" && ent.IsConstraintError(err) {
			failure(c, 409, "public_slug_exists")
			return
		}
		failure(c, 500, "save_failed")
		return
	}
	s.audit(c, "resource.save", fmt.Sprintf("%s:%d", kind, r.ID), "ok")
	c.JSON(200, gin.H{"id": r.ID, "data": r.Data})
}
func (s *Server) deleteResource(c *gin.Context) {
	if !resourceKind(c.Param("kind")) {
		failure(c, 400, "invalid_kind")
		return
	}
	if c.Param("kind") == "sources" {
		s.sourceMu.Lock()
		defer s.sourceMu.Unlock()
	}
	id, _ := strconv.Atoi(c.Param("id"))
	r, err := s.Store.Resource.Get(c.Request.Context(), id)
	if err != nil || r.Kind != c.Param("kind") || strings.HasPrefix(r.Kind, "device-") || r.Kind == "mail" {
		failure(c, 404, "not_found")
		return
	}
	if r.Kind == "profiles" || r.Kind == "public-page" {
		tx, e := s.DB.BeginTx(c.Request.Context(), nil)
		if e != nil {
			failure(c, 503, "delete_failed")
			return
		}
		defer tx.Rollback()
		// Same lock order as snapshot publication: publication state cannot survive deletion.
		if _, e = tx.ExecContext(c.Request.Context(), `SET LOCAL lock_timeout='3s'`); e == nil {
			_, e = tx.ExecContext(c.Request.Context(), `LOCK TABLE migate.resources IN SHARE ROW EXCLUSIVE MODE`)
		}
		if e == nil && r.Kind == "public-page" {
			_, e = tx.ExecContext(c.Request.Context(), `DELETE FROM migate.resources WHERE (kind='guestbook-config' AND key=$1) OR (kind='guestbook-message' AND data->>'page_id'=$1)`, strconv.Itoa(r.ID))
		}
		dependentKind := "profile-publication"
		if r.Kind == "public-page" {
			dependentKind = "page-publications"
		}
		if e == nil {
			_, e = tx.ExecContext(c.Request.Context(), `DELETE FROM migate.resources WHERE (kind=$3 AND key=$1) OR (kind=$4 AND id=$2)`, strconv.Itoa(r.ID), r.ID, dependentKind, r.Kind)
		}
		if e != nil {
			failure(c, 503, "delete_failed")
			return
		}
		err = tx.Commit()
	} else {
		err = s.Store.Resource.DeleteOne(r).Exec(c.Request.Context())
	}
	if err != nil {
		failure(c, 500, "delete_failed")
		return
	}
	s.audit(c, "resource.delete", fmt.Sprint(id), "ok")
	c.JSON(200, gin.H{"ok": true})
}
func (s *Server) refreshSource(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	r, err := s.refreshSourceCache(c.Request.Context(), id, fetchSource)
	if ent.IsNotFound(err) {
		failure(c, 404, "not_found")
		return
	}
	if err != nil {
		failure(c, 400, "source_fetch_failed")
		return
	}
	s.audit(c, "source.refresh", strconv.Itoa(id), "ok")
	c.JSON(200, gin.H{"node_count": r.Data["node_count"], "last_update": r.Data["last_update"]})
}
func (s *Server) layaTest(c *gin.Context) {
	var cfg ModelConfig
	if !s.setting(c.Request.Context(), "laya", &cfg) || !cfg.Enabled {
		failure(c, 400, "laya_not_enabled")
		return
	}
	decision, err := s.classify(c.Request.Context(), cfg, "Synthetic test: 12 failed logins within 60 seconds. No credentials included.")
	if err != nil {
		failure(c, 502, "model_unavailable_rules_continue")
		return
	}
	s.audit(c, "model.test", "laya", "ok")
	c.JSON(200, gin.H{"model": cfg.Model, "decision": decision, "mode": "observe_only"})
}
