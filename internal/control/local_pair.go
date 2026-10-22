package control

import (
	"crypto/subtle"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// localPairHashFile 由安装器在单机角色下写入：一次性凭证的 SHA-256 十六进制摘要。
// 中心只保存摘要，凭证原文仅 root 可读，配对成功后删除摘要文件。
func (s *Server) localPairHashFile() string {
	return filepath.Join(filepath.Dir(s.Path), "local-pair.sha256")
}

// pairLocalDevice 单机角色的本机 Gate 自动配对：只接受直接来自回环地址、
// 不经任何反代转发的请求，并核对一次性凭证；通过即批准并销毁凭证。
func (s *Server) pairLocalDevice(c *gin.Context, name, publicKey, token string) {
	if !directLoopback(c) {
		failure(c, 403, "local_pair_refused")
		return
	}
	file := s.localPairHashFile()
	raw, err := os.ReadFile(file)
	expected := strings.ToLower(strings.TrimSpace(string(raw)))
	if err != nil || len(expected) != 64 || subtle.ConstantTimeCompare([]byte(expected), []byte(digest(token))) != 1 {
		s.securityEvent(c, "device", "local-pair-rejected", "warning")
		failure(c, 403, "local_pair_refused")
		return
	}
	// 先删除摘要再建设备：并发的第二个请求读不到摘要，凭证不可重放。
	if err = os.Remove(file); err != nil {
		failure(c, 503, "local_pair_unavailable")
		return
	}
	d, err := s.Store.Device.Create().SetName(name).SetPublicKey(publicKey).SetStatus("approved").SetPairExpires(time.Now()).Save(c.Request.Context())
	if err != nil {
		failure(c, 409, "device_already_registered")
		return
	}
	s.audit(c, "device.local-pair", d.Name, "ok")
	c.JSON(201, gin.H{"device_id": d.ID, "status": "approved"})
}

func directLoopback(c *gin.Context) bool {
	for _, header := range []string{"X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto", "X-Real-IP", "Forwarded"} {
		if c.GetHeader(header) != "" {
			return false
		}
	}
	host, _, err := net.SplitHostPort(c.Request.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
