package center

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/shusfun/weir/internal/misub"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/gate"
	"github.com/shusfun/weir/internal/store/ent/setting"
	"github.com/shusfun/weir/internal/store/ent/subsource"
	"github.com/shusfun/weir/internal/store/ent/user"
)

// Telegram 通知与周期任务（订阅源定时刷新、WebDAV 自动备份）。通知只含名称与数值，不含任何凭据或订阅地址；
// 同一事件 24 小时内只发一次。

const notifyKey = "notify"

// NotifySettings 是通知设置；Bot Token 加密保存、只写不读。
type NotifySettings struct {
	Enabled       bool   `json:"enabled"`
	BotToken      string `json:"bot_token,omitempty"`
	ChatID        string `json:"chat_id"`
	APIBase       string `json:"api_base"` // 默认 https://api.telegram.org，可填自建反代
	GateOffline   bool   `json:"gate_offline"`
	UserQuota     int    `json:"user_quota_percent"` // 用户用量达到该百分比时提醒，0 关闭
	UserExpiry    int    `json:"user_expiry_days"`   // 用户到期前若干天提醒，0 关闭
	SourceExpiry  int    `json:"source_expiry_days"` // 订阅源（机场）到期前若干天提醒，0 关闭
	SourceRefresh int    `json:"source_refresh_hours"`
}

func defaultNotify() NotifySettings {
	return NotifySettings{APIBase: "https://api.telegram.org", GateOffline: true, UserQuota: 90, UserExpiry: 3, SourceExpiry: 3}
}

func (s *Server) notifyRoutes(admin *gin.RouterGroup) {
	admin.GET("/settings/notify", s.getNotify)
	admin.PUT("/settings/notify", s.putNotify)
	admin.POST("/settings/notify/test", s.testNotify)
}

func (s *Server) notifySettings(ctx context.Context) (NotifySettings, error) {
	cfg := defaultNotify()
	row, err := s.Store.Ent.Setting.Query().Where(setting.KeyEQ(notifyKey)).Only(ctx)
	if ent.IsNotFound(err) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	b, _ := json.Marshal(row.Value)
	return cfg, json.Unmarshal(b, &cfg)
}

func notifyView(cfg NotifySettings) gin.H {
	v := toMap(cfg)
	delete(v, "bot_token")
	v["token_set"] = cfg.BotToken != ""
	return v
}

func (s *Server) getNotify(c *gin.Context) {
	cfg, err := s.notifySettings(c.Request.Context())
	if err != nil {
		failure(c, 503, "settings_unavailable")
		return
	}
	c.JSON(200, notifyView(cfg))
}

func (s *Server) putNotify(c *gin.Context) {
	var req struct {
		NotifySettings
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if !bind(c, &req) || !s.reauth(c, req.Password, req.Code) {
		return
	}
	cfg := req.NotifySettings
	cfg.APIBase = strings.TrimRight(strings.TrimSpace(cfg.APIBase), "/")
	if cfg.APIBase == "" {
		cfg.APIBase = defaultNotify().APIBase
	}
	if u, err := url.Parse(cfg.APIBase); err != nil || u.Scheme != "https" && u.Scheme != "http" || u.Host == "" || u.User != nil || u.RawQuery != "" {
		failure(c, 400, "invalid_notify_settings")
		return
	}
	if len(cfg.ChatID) > 64 || strings.ContainsAny(cfg.ChatID, " /?&#\r\n") || cfg.UserQuota < 0 || cfg.UserQuota > 100 || cfg.UserExpiry < 0 || cfg.UserExpiry > 60 ||
		cfg.SourceExpiry < 0 || cfg.SourceExpiry > 60 || cfg.SourceRefresh < 0 || cfg.SourceRefresh > 168 || len(cfg.BotToken) > 256 || strings.ContainsAny(cfg.BotToken, "/?#\r\n ") {
		failure(c, 400, "invalid_notify_settings")
		return
	}
	ctx := c.Request.Context()
	old, _ := s.notifySettings(ctx)
	if cfg.BotToken == "" {
		cfg.BotToken = old.BotToken
	} else {
		var err error
		if cfg.BotToken, err = s.Crypto.Encrypt(cfg.BotToken); err != nil {
			failure(c, 500, "encrypt_failed")
			return
		}
	}
	if err := s.Store.Ent.Setting.Create().SetKey(notifyKey).SetValue(toMap(cfg)).OnConflictColumns(setting.FieldKey).UpdateNewValues().Exec(ctx); err != nil {
		failure(c, 500, "save_failed")
		return
	}
	s.audit(c, "settings.notify", "", "ok")
	c.JSON(200, notifyView(cfg))
}

// Notify 发送一条 Telegram 消息（未开启或未配置时忽略）。
func (s *Server) Notify(ctx context.Context, text string) error {
	cfg, err := s.notifySettings(ctx)
	if err != nil {
		return err
	}
	if !cfg.Enabled || cfg.BotToken == "" || cfg.ChatID == "" {
		return errors.New("notify_not_configured")
	}
	token, err := s.reveal(cfg.BotToken)
	if err != nil {
		return err
	}
	body, _ := json.Marshal(map[string]any{"chat_id": cfg.ChatID, "text": text, "disable_web_page_preview": true})
	req, err := http.NewRequestWithContext(ctx, "POST", cfg.APIBase+"/bot"+token+"/sendMessage", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return errors.New("notify_unreachable") // 不回传含 Token 的地址
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("notify_http_%d", resp.StatusCode)
	}
	return nil
}

func (s *Server) testNotify(c *gin.Context) {
	if err := s.Notify(c.Request.Context(), "Weir 测试通知：通知设置可用。"); err != nil {
		failDetail(c, 502, "notify_failed", err.Error(), nil)
		return
	}
	c.JSON(200, gin.H{"sent": true})
}

// notifyOnce 同一事件 24 小时内只发一次。
func (s *Server) notifyOnce(ctx context.Context, key, text string) {
	if ok, err := s.Store.KV.SetNX(ctx, "notified:"+key, "1", 24*time.Hour); err == nil && ok {
		if s.Notify(ctx, text) != nil {
			_ = s.Store.KV.Del(ctx, "notified:"+key) // 发送失败下次再试
		}
	}
}

// CheckAlerts 检查 VPS 离线、用户额度 / 到期、订阅源到期并发送提醒；没有数据的项不提醒（不当作 0）。
func (s *Server) CheckAlerts(ctx context.Context, now time.Time) error {
	cfg, err := s.notifySettings(ctx)
	if err != nil || !cfg.Enabled {
		return err
	}
	if cfg.GateOffline {
		gates, err := s.Store.Ent.Gate.Query().Where(gate.StatusEQ(gate.StatusActive)).All(ctx)
		if err != nil {
			return err
		}
		for _, g := range gates {
			if g.LastSeenAt != nil && now.Sub(*g.LastSeenAt) > 5*time.Minute {
				s.notifyOnce(ctx, fmt.Sprintf("gate-offline:%d:%d", g.ID, g.LastSeenAt.Unix()), fmt.Sprintf("VPS「%s」已离线，最近心跳 %s。", g.Name, g.LastSeenAt.Local().Format("01-02 15:04")))
			}
		}
	}
	users, err := s.Store.Ent.User.Query().Where(user.EnabledEQ(true), user.RoleEQ(user.RoleUser)).All(ctx)
	if err != nil {
		return err
	}
	for _, u := range users {
		name := firstNonEmpty(u.Name, u.Email)
		if cfg.UserQuota > 0 && u.Quota > 0 && u.Used*100 >= u.Quota*int64(cfg.UserQuota) {
			s.notifyOnce(ctx, fmt.Sprintf("quota:%d:%d", u.ID, u.UsageResetAt.Unix()), fmt.Sprintf("用户「%s」已用 %s / %s。", name, misub.FormatBytes(u.Used), misub.FormatBytes(u.Quota)))
		}
		if cfg.UserExpiry > 0 && u.ExpiresAt != nil && u.ExpiresAt.After(now) && u.ExpiresAt.Sub(now) <= time.Duration(cfg.UserExpiry)*24*time.Hour {
			s.notifyOnce(ctx, fmt.Sprintf("expiry:%d:%d", u.ID, u.ExpiresAt.Unix()), fmt.Sprintf("用户「%s」将于 %s 到期。", name, u.ExpiresAt.Local().Format("2006-01-02")))
		}
	}
	if cfg.SourceExpiry > 0 {
		sources, err := s.Store.Ent.SubSource.Query().Where(subsource.EnabledEQ(true)).All(ctx)
		if err != nil {
			return err
		}
		for _, src := range sources {
			if ui := parseUserInfo(src.UserInfo); ui != nil && ui["expire"] > 0 {
				exp := time.Unix(ui["expire"], 0)
				if exp.After(now) && exp.Sub(now) <= time.Duration(cfg.SourceExpiry)*24*time.Hour {
					s.notifyOnce(ctx, fmt.Sprintf("source-expiry:%d:%d", src.ID, ui["expire"]), fmt.Sprintf("订阅源「%s」将于 %s 到期。", firstNonEmpty(src.Name, src.Title, "未命名"), exp.Local().Format("2006-01-02")))
				}
			}
		}
	}
	return nil
}

// RefreshSources 按设置的间隔重新拉取全部启用的订阅源（保护性缓存照常生效），并清除订阅组输出缓存。
func (s *Server) RefreshSources(ctx context.Context, now time.Time) error {
	cfg, err := s.notifySettings(ctx)
	if err != nil || cfg.SourceRefresh <= 0 {
		return err
	}
	if ok, _ := s.Store.KV.SetNX(ctx, "sources-refreshed", now.Format(time.RFC3339), time.Duration(cfg.SourceRefresh)*time.Hour); !ok {
		return nil
	}
	rows, err := s.Store.Ent.SubSource.Query().Where(subsource.EnabledEQ(true)).All(ctx)
	if err != nil || len(rows) == 0 {
		return err
	}
	sources := make([]misub.Source, 0, len(rows))
	for _, r := range rows {
		sources = append(sources, toSource(r))
	}
	_, results := misub.Combine(ctx, s.subFetcher(), kvCache{s}, sources, nil, misub.Options{UserAgent: "clash-meta/2.5.0"})
	s.recordResults(ctx, results)
	s.invalidateGroups(ctx)
	return nil
}

// AutoBackup 按 WebDAV 设置的间隔自动备份。
func (s *Server) AutoBackup(ctx context.Context, now time.Time) error {
	cfg, err := s.webdavConfig(ctx)
	if err != nil || !cfg.Auto || cfg.URL == "" {
		return err
	}
	if cfg.LastRunAt != nil && now.Sub(*cfg.LastRunAt) < time.Duration(cfg.Hours)*time.Hour {
		return nil
	}
	_, err = s.RunWebdavBackup(ctx)
	return err
}
