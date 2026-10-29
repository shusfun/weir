package center

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/gate"
	"github.com/shusfun/weir/internal/store/ent/setting"
)

// 中心公共地址：安装时不填域名。管理员第一次经 HTTPS 域名打开控制台时由前端写入；
// 之后访问地址与记录不同，由管理员重新验证身份后更新，已配对的 Gate 经心跳收到新地址后自行切换。
// 订阅地址与「添加 Gate」命令都用这个地址，未记录时退回按请求推导。
const originSettingKey = "center-origin"

// requireHTTPS 拒绝经公网或反代的明文请求：中心必须配好 HTTPS 才能使用。
// 直接来自本机回环、不带转发头的请求（SSH 隧道、本地开发、单机角色的本机 Gate）不受限。
func requireHTTPS(c *gin.Context) {
	if requestSecure(c.Request) || directLoopback(c) || c.Request.URL.Path == "/healthz" {
		c.Next()
		return
	}
	if strings.HasPrefix(c.Request.URL.Path, "/api/") {
		failDetail(c, 403, "https_required", "中心只接受 HTTPS 访问，请先在反代上配置证书", nil)
		return
	}
	c.Data(403, "text/plain; charset=utf-8", []byte("中心只接受 HTTPS 访问，请先在反代上配置证书。\n"))
	c.Abort()
}

// domainOrigin 判断是否为可记录的公共地址：HTTPS + 域名（不是 IP、localhost 或单标签主机名）。
func domainOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.Path != "" || u.User != nil || u.RawQuery != "" {
		return false
	}
	h := strings.ToLower(u.Hostname())
	return net.ParseIP(h) == nil && h != "localhost" && !strings.HasSuffix(h, ".localhost") && strings.Contains(h, ".") && validHost(u.Host)
}

func (s *Server) loadCenterOrigin(ctx context.Context) string {
	row, err := s.Store.Ent.Setting.Query().Where(setting.KeyEQ(originSettingKey)).Only(ctx)
	if err != nil {
		return ""
	}
	v, _ := row.Value["origin"].(string)
	return v
}

// centerOrigin 返回已记录的公共地址（未记录为空）；结果缓存在内存，心跳每次都会读。
func (s *Server) centerOrigin(ctx context.Context) string {
	s.origin.Lock()
	defer s.origin.Unlock()
	if !s.origin.loaded && s.Store != nil {
		s.origin.value, s.origin.loaded = s.loadCenterOrigin(ctx), true
	}
	return s.origin.value
}

// siteOrigin 是对外展示的地址：优先用记录的公共地址，未记录时按请求推导。
func (s *Server) siteOrigin(c *gin.Context) string {
	if o := s.centerOrigin(c.Request.Context()); o != "" {
		return o
	}
	return requestOrigin(c.Request)
}

func (s *Server) originRoutes(admin *gin.RouterGroup) {
	admin.GET("/center-origin", s.getCenterOrigin)
	admin.PUT("/center-origin", s.putCenterOrigin)
}

// pendingGates 是仍连接其他地址的 Gate（单机角色经回环连接，不需要切换）。
func (s *Server) pendingGates(ctx context.Context, origin string) ([]gin.H, error) {
	gates, err := s.Store.Ent.Gate.Query().Where(gate.StatusEQ(gate.StatusActive)).Order(ent.Asc(gate.FieldID)).All(ctx)
	if err != nil {
		return nil, err
	}
	out := []gin.H{}
	for _, g := range gates {
		if origin == "" || g.CenterURL == origin || loopbackOrigin(g.CenterURL) {
			continue
		}
		out = append(out, gin.H{"id": g.ID, "name": g.Name, "center_url": g.CenterURL})
	}
	return out, nil
}

func loopbackOrigin(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	ip := net.ParseIP(u.Hostname())
	return strings.EqualFold(u.Hostname(), "localhost") || ip != nil && ip.IsLoopback()
}

func (s *Server) getCenterOrigin(c *gin.Context) {
	ctx := c.Request.Context()
	origin, current := s.centerOrigin(ctx), requestOrigin(c.Request)
	pending, err := s.pendingGates(ctx, origin)
	if err != nil {
		failInternal(c, 503, "query_failed", err)
		return
	}
	c.JSON(200, gin.H{"origin": origin, "current": current, "recordable": domainOrigin(current), "pending_gates": pending})
}

// putCenterOrigin 只能记录管理员此刻正在访问的地址，不能填任意值。
// 第一次记录不需要重新验证；更改已记录的地址会影响订阅地址和全部 Gate，需要重新验证身份。
func (s *Server) putCenterOrigin(c *gin.Context) {
	var req struct {
		Origin   string `json:"origin"`
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if !bind(c, &req) {
		return
	}
	ctx := c.Request.Context()
	current := requestOrigin(c.Request)
	if req.Origin != current || !domainOrigin(current) {
		failDetail(c, 400, "invalid_origin", "只能记录当前正在访问的 HTTPS 域名地址", nil)
		return
	}
	before := s.centerOrigin(ctx)
	if before == current {
		c.JSON(200, gin.H{"origin": current})
		return
	}
	if before != "" && !s.reauth(c, req.Password, req.Code) {
		return
	}
	err := s.Store.Ent.Setting.Create().SetKey(originSettingKey).SetValue(map[string]any{"origin": current}).
		OnConflictColumns(setting.FieldKey).UpdateNewValues().Exec(ctx)
	if err != nil {
		failInternal(c, 500, "save_failed", err)
		return
	}
	s.origin.Lock()
	s.origin.value, s.origin.loaded = current, true
	s.origin.Unlock()
	action := "center.origin.set"
	if before != "" {
		action = "center.origin.change"
	}
	s.audit(c, action, before+" → "+current, "ok")
	c.JSON(http.StatusOK, gin.H{"origin": current})
}
