package center

// 安装包加速镜像：管理员维护前缀列表，浏览器把 GitHub 下载地址拼在前缀后测速并选择最快来源。
// 中心不访问镜像、不代取安装包；下载后由浏览器按 GitHub 公布的 SHA-256 校验。
import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/resource"
)

type mirrorDocument struct {
	Mirrors  []string `json:"mirrors"`
	Revision string   `json:"revision"`
}

func validMirror(v string) bool {
	if len(v) > 200 || !strings.HasSuffix(v, "/") {
		return false
	}
	u, e := url.Parse(v)
	return e == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && !strings.ContainsAny(v, " \t\r\n")
}

func validateMirrors(items []string) error {
	if len(items) > 8 {
		return errors.New("mirror_limit")
	}
	seen := map[string]bool{}
	for _, v := range items {
		if !validMirror(v) || seen[v] {
			return errors.New("invalid_mirror")
		}
		seen[v] = true
	}
	return nil
}

func mirrorRevision(items []string) string {
	raw, _ := json.Marshal(items)
	return digest(string(raw))
}

func (s *Server) readMirrors(c *gin.Context) (mirrorDocument, error) {
	r, e := s.Store.Ent.Resource.Query().Where(resource.KindEQ("settings"), resource.KeyEQ("client-mirrors")).Only(c.Request.Context())
	if ent.IsNotFound(e) {
		return mirrorDocument{[]string{}, mirrorRevision([]string{})}, nil
	}
	if e != nil {
		return mirrorDocument{}, e
	}
	raw, _ := json.Marshal(r.Data["mirrors"])
	var items []string
	if json.Unmarshal(raw, &items) != nil || validateMirrors(items) != nil {
		return mirrorDocument{}, errors.New("invalid_stored_mirrors")
	}
	if items == nil {
		items = []string{}
	}
	return mirrorDocument{items, mirrorRevision(items)}, nil
}

func (s *Server) getClientMirrors(c *gin.Context) {
	d, e := s.readMirrors(c)
	if e != nil {
		failure(c, 503, "client_mirrors_unavailable")
		return
	}
	if c.GetString("role") != "admin" {
		d.Revision = ""
	}
	c.JSON(200, d)
}

func (s *Server) putClientMirrors(c *gin.Context) {
	var req struct {
		Mirrors  []string `json:"mirrors"`
		Revision string   `json:"revision"`
		Password string   `json:"password"`
		Code     string   `json:"code"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16*1024)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&req) != nil || decoder.Decode(new(any)) != io.EOF || req.Mirrors == nil {
		failure(c, 400, "invalid_request")
		return
	}
	if e := validateMirrors(req.Mirrors); e != nil {
		failure(c, 400, e.Error())
		return
	}
	if !s.reauth(c, req.Password, req.Code) {
		return
	}
	ctx := c.Request.Context()
	e := s.updateResource(ctx, "settings", "client-mirrors", func(cur map[string]any) (map[string]any, error) {
		old := []string{}
		if cur != nil {
			raw, _ := json.Marshal(cur["mirrors"])
			if err := json.Unmarshal(raw, &old); err != nil {
				return nil, err
			}
		}
		if old == nil {
			old = []string{}
		}
		if mirrorRevision(old) != req.Revision {
			return nil, errResourceConflict
		}
		raw, _ := json.Marshal(map[string]any{"mirrors": req.Mirrors})
		var next map[string]any
		return next, json.Unmarshal(raw, &next)
	})
	if errors.Is(e, errResourceConflict) {
		failure(c, 409, "mirrors_changed")
		return
	}
	if e != nil {
		failure(c, 503, "mirror_save_failed")
		return
	}
	s.audit(c, "settings.client-mirrors", fmt.Sprintf("%d mirrors", len(req.Mirrors)), "ok")
	c.JSON(200, mirrorDocument{req.Mirrors, mirrorRevision(req.Mirrors)})
}
