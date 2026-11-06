package center

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/setting"
)

const logoKey = "console-logo"
const maxLogoBytes = 512 * 1024

func (s *Server) getLogo(c *gin.Context) {
	if s.Store == nil {
		c.JSON(http.StatusOK, gin.H{"data": ""})
		return
	}
	row, err := s.Store.Ent.Setting.Query().Where(setting.KeyEQ(logoKey)).Only(c.Request.Context())
	if ent.IsNotFound(err) {
		c.JSON(http.StatusOK, gin.H{"data": ""})
		return
	}
	if err != nil {
		failInternal(c, 503, "settings_unavailable", err)
		return
	}
	c.JSON(http.StatusOK, row.Value)
}

func (s *Server) putLogo(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxLogoBytes*2)
	var req struct {
		Data string `json:"data"`
	}
	d := json.NewDecoder(c.Request.Body)
	d.DisallowUnknownFields()
	if d.Decode(&req) != nil || d.Decode(new(any)) != io.EOF {
		failure(c, 400, "invalid_logo")
		return
	}
	var mime string
	var raw []byte
	if req.Data != "" {
		for _, kind := range []string{"image/png", "image/jpeg", "image/gif"} {
			prefix := "data:" + kind + ";base64,"
			if len(req.Data) >= len(prefix) && req.Data[:len(prefix)] == prefix {
				mime = kind
				raw, _ = base64.StdEncoding.Strict().DecodeString(req.Data[len(prefix):])
				break
			}
		}
		if len(raw) == 0 || len(raw) > maxLogoBytes || http.DetectContentType(raw) != mime {
			failure(c, 400, "invalid_logo")
			return
		}
		cfg, format, err := image.DecodeConfig(bytes.NewReader(raw))
		if err != nil || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 1024 || cfg.Height > 1024 || "image/"+format != mime {
			failure(c, 400, "invalid_logo")
			return
		}
		if _, _, err = image.Decode(bytes.NewReader(raw)); err != nil {
			failure(c, 400, "invalid_logo")
			return
		}
	}
	value := map[string]any{"data": req.Data}
	if err := s.Store.Ent.Setting.Create().SetKey(logoKey).SetValue(value).OnConflictColumns(setting.FieldKey).UpdateNewValues().Exec(c.Request.Context()); err != nil {
		failInternal(c, 503, "settings_save_failed", err)
		return
	}
	s.audit(c, "settings.logo", "console-logo", "ok")
	c.JSON(http.StatusOK, value)
}

func (s *Server) deleteLogo(c *gin.Context) {
	if _, err := s.Store.Ent.Setting.Delete().Where(setting.KeyEQ(logoKey)).Exec(c.Request.Context()); err != nil {
		failInternal(c, 503, "settings_save_failed", err)
		return
	}
	s.audit(c, "settings.logo", "console-logo", "deleted")
	c.JSON(http.StatusOK, gin.H{"data": ""})
}
