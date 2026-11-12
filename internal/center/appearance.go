package center

import (
	"context"
	"encoding/json"

	"github.com/gin-gonic/gin"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/setting"
)

// appearance 是跟着账号走的外观偏好。浏览器先读本地缓存保证首屏不闪，登录后用这里的值校正。
type appearance struct {
	Theme        string `json:"theme"`
	Motion       string `json:"motion"`
	FollowReduce bool   `json:"follow_reduce"`
	Stutter      bool   `json:"stutter"`
	Globe        bool   `json:"globe"`
}

func appearanceKey(uid int) string { return "appearance:" + itoa(uid) }

func validAppearance(a appearance) bool {
	switch a.Theme {
	case "system", "light", "dark":
	default:
		return false
	}
	switch a.Motion {
	case "full", "balanced", "lite":
		return true
	}
	return false
}

func (s *Server) appearanceRoutes(auth *gin.RouterGroup) {
	auth.GET("/me/appearance", s.getAppearance)
	auth.PUT("/me/appearance", s.putAppearance)
}

func (s *Server) loadAppearance(ctx context.Context, uid int) (*appearance, error) {
	row, err := s.Store.Ent.Setting.Query().Where(setting.KeyEQ(appearanceKey(uid))).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(row.Value)
	var a appearance
	if json.Unmarshal(raw, &a) != nil || !validAppearance(a) {
		return nil, nil
	}
	return &a, nil
}

// getAppearance 没保存过时返回 saved=false，浏览器继续用本地值并把它补存上来。
func (s *Server) getAppearance(c *gin.Context) {
	u := c.MustGet("account").(*ent.User)
	a, err := s.loadAppearance(c.Request.Context(), u.ID)
	if err != nil {
		failInternal(c, 503, "appearance_unavailable", err)
		return
	}
	if a == nil {
		c.JSON(200, gin.H{"saved": false})
		return
	}
	c.JSON(200, gin.H{"saved": true, "appearance": a})
}

func (s *Server) putAppearance(c *gin.Context) {
	u := c.MustGet("account").(*ent.User)
	var a appearance
	if !bind(c, &a) {
		return
	}
	if !validAppearance(a) {
		failDetail(c, 400, "invalid_appearance", "主题或动效档位不在可选范围内", nil)
		return
	}
	if err := s.Store.Ent.Setting.Create().SetKey(appearanceKey(u.ID)).SetValue(toMap(a)).OnConflictColumns(setting.FieldKey).UpdateNewValues().Exec(c.Request.Context()); err != nil {
		failInternal(c, 503, "appearance_unavailable", err)
		return
	}
	c.JSON(200, gin.H{"saved": true, "appearance": a})
}
