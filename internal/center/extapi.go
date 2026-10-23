package center

import (
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/imzyb/MiGate/internal/store/ent"
	"github.com/imzyb/MiGate/internal/store/ent/resource"
	"github.com/imzyb/MiGate/internal/store/ent/user"
)

// 外部接口（对应 MiSub 的 external API）：管理员创建的 API 令牌以 Bearer 方式调用，
// 只能管理订阅源、手动节点与订阅组，不能触及用户、VPS、节点或系统设置。令牌只在创建时显示一次，库里存摘要。

const kindAPIToken = "api-token"

func (s *Server) extRoutes(r *gin.Engine, admin *gin.RouterGroup) {
	admin.GET("/api-tokens", s.listAPITokens)
	admin.POST("/api-tokens", s.createAPIToken)
	admin.DELETE("/api-tokens/:id", s.deleteAPIToken)

	ext := r.Group("/api/ext/v1")
	ext.Use(func(c *gin.Context) {
		if !s.Config.Installed || s.Store == nil {
			failure(c, 503, "setup_required")
			return
		}
		c.Next()
	}, s.apiTokenAuth)
	ext.GET("/sources", s.listSources)
	ext.POST("/sources", s.saveSource)
	ext.PATCH("/sources/:id", s.saveSource)
	ext.DELETE("/sources/:id", s.deleteSource)
	ext.POST("/sources/:id/refresh", s.refreshSource)
	ext.GET("/nodes", s.listManualNodes)
	ext.POST("/nodes", s.saveManualNode)
	ext.POST("/nodes/import", s.importManualNodes)
	ext.PATCH("/nodes/:id", s.saveManualNode)
	ext.DELETE("/nodes/:id", s.deleteManualNode)
	ext.GET("/profiles", s.listSubProfiles)
	ext.POST("/profiles", s.saveProfile)
	ext.PATCH("/profiles/:id", s.saveProfile)
	ext.DELETE("/profiles/:id", s.deleteProfile)
	ext.POST("/profiles/:id/preview", s.previewProfile)
}

// apiTokenAuth 校验令牌，并以创建它的管理员身份执行（该管理员被停用或降级后令牌随之失效）。
func (s *Server) apiTokenAuth(c *gin.Context) {
	if !s.rate(c, "ext-api", 600) {
		return
	}
	raw, ok := strings.CutPrefix(c.GetHeader("Authorization"), "Bearer ")
	if !ok || len(raw) < 32 || len(raw) > 128 {
		failure(c, 401, "invalid_api_token")
		return
	}
	ctx := c.Request.Context()
	r, err := s.Store.Ent.Resource.Query().Where(resource.KindEQ(kindAPIToken), resource.KeyEQ(digest(raw))).Only(ctx)
	if err != nil {
		failure(c, 401, "invalid_api_token")
		return
	}
	creator, _ := r.Data["created_by"].(float64)
	u, err := s.Store.Ent.User.Get(ctx, int(creator))
	if err != nil || !u.Enabled || u.Role != user.RoleAdmin {
		failure(c, 401, "invalid_api_token")
		return
	}
	c.Set("user_id", u.ID)
	c.Set("account", u)
	c.Set("role", string(u.Role))
	data := r.Data
	data["last_used_at"] = time.Now().UTC().Format(time.RFC3339)
	_ = r.Update().SetData(data).Exec(ctx)
	c.Next()
}

func tokenView(r *ent.Resource) gin.H {
	return gin.H{"id": r.ID, "name": r.Data["name"], "created_at": r.CreatedAt, "last_used_at": r.Data["last_used_at"]}
}

func (s *Server) listAPITokens(c *gin.Context) {
	items, err := s.Store.Ent.Resource.Query().Where(resource.KindEQ(kindAPIToken)).Order(ent.Asc(resource.FieldID)).All(c.Request.Context())
	if err != nil {
		failure(c, 503, "query_failed")
		return
	}
	out := make([]gin.H, 0, len(items))
	for _, it := range items {
		out = append(out, tokenView(it))
	}
	c.JSON(200, gin.H{"items": out})
}

func (s *Server) createAPIToken(c *gin.Context) {
	var req struct {
		Name     string `json:"name"`
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if !bind(c, &req) || !s.reauth(c, req.Password, req.Code) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || !validText(name, 100) {
		failure(c, 400, "invalid_name")
		return
	}
	ctx := c.Request.Context()
	if n, _ := s.Store.Ent.Resource.Query().Where(resource.KindEQ(kindAPIToken)).Count(ctx); n >= 20 {
		failure(c, 409, "api_token_limit")
		return
	}
	token := "mg_" + randomToken(24)
	r, err := s.Store.Ent.Resource.Create().SetKind(kindAPIToken).SetKey(digest(token)).SetData(map[string]any{"name": name, "created_by": c.GetInt("user_id")}).Save(ctx)
	if err != nil {
		failure(c, 500, "save_failed")
		return
	}
	s.audit(c, "api_token.create", "token:"+strconv.Itoa(r.ID), "ok")
	v := tokenView(r)
	v["token"] = token
	c.JSON(200, v)
}

func (s *Server) deleteAPIToken(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		failure(c, 400, "invalid_id")
		return
	}
	n, err := s.Store.Ent.Resource.Delete().Where(resource.IDEQ(id), resource.KindEQ(kindAPIToken)).Exec(c.Request.Context())
	if err != nil || n == 0 {
		failure(c, 404, "api_token_not_found")
		return
	}
	s.audit(c, "api_token.delete", "token:"+strconv.Itoa(id), "ok")
	c.Status(204)
}
