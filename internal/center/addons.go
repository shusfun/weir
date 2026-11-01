package center

import "github.com/gin-gonic/gin"

// addonRoutes 注册附加功能：全局公告、客户端目录（含版本查询）。路径与旧版一致，控制台组件直接复用。
// 下载加速镜像并入「GitHub 加速」（github_sources.go）。
func (s *Server) addonRoutes(ready, auth, admin *gin.RouterGroup) {
	ready.GET("/announcements/public", s.publishedAnnouncement)
	auth.GET("/announcements", s.publishedAnnouncement)
	auth.GET("/clients", s.getClientCatalog)
	auth.GET("/clients/:id/release", s.getClientRelease)
	admin.POST("/announcement/preview", s.previewNotice)
	admin.GET("/announcement", s.getAnnouncement)
	admin.PUT("/announcement", s.putAnnouncement)
	admin.GET("/clients", s.getClientCatalog)
	admin.PUT("/clients", s.putClientCatalog)
}
