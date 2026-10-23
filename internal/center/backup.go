package center

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/imzyb/MiGate/internal/misub"
	"github.com/imzyb/MiGate/internal/store/ent"
	"github.com/imzyb/MiGate/internal/store/ent/ruletemplate"
	"github.com/imzyb/MiGate/internal/store/ent/setting"
	"github.com/imzyb/MiGate/internal/store/ent/subnode"
	"github.com/imzyb/MiGate/internal/store/ent/subprofile"
	"github.com/imzyb/MiGate/internal/store/ent/subsource"
)

// 备份与导入：格式与 MiSub 的 misub-backup 一致（订阅、订阅组、规则模板），旧 MiSub 的备份可直接导入。
// 导入只新增、不删除现有数据；不安全的订阅地址与无效节点会被跳过并计数。用户、VPS、节点等中心数据不在其中。

func (s *Server) backupRoutes(admin *gin.RouterGroup) {
	admin.GET("/backup/export", s.exportBackup)
	admin.POST("/backup/import", s.importBackup)
	admin.GET("/backup/webdav", s.getWebdav)
	admin.PUT("/backup/webdav", s.putWebdav)
	admin.POST("/backup/webdav/run", s.runWebdav)
	admin.GET("/backup/webdav/files", s.listWebdav)
	admin.POST("/backup/webdav/restore", s.restoreWebdav)
}

type backupFile struct {
	Version    int       `json:"version"`
	App        string    `json:"app"`
	Type       string    `json:"type"`
	ExportedAt time.Time `json:"exportedAt"`
	Data       struct {
		Subscriptions []map[string]any `json:"subscriptions"`
		Profiles      []map[string]any `json:"profiles"`
		RuleTemplates []map[string]any `json:"ruleTemplates"`
	} `json:"data"`
	// 旧版导出把三类数据放在顶层。
	Subscriptions []map[string]any `json:"subscriptions,omitempty"`
	Profiles      []map[string]any `json:"profiles,omitempty"`
}

// ImportStats 是导入结果。
type ImportStats struct {
	Sources   int      `json:"sources"`
	Nodes     int      `json:"nodes"`
	Profiles  int      `json:"profiles"`
	Templates int      `json:"templates"`
	Skipped   []string `json:"skipped"`
}

func str(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func boolOr(m map[string]any, key string, def bool) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return def
}

func anyList(v any) []any {
	l, _ := v.([]any)
	return l
}

func idOf(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return fmt.Sprint(int64(t))
	case map[string]any:
		return idOf(t["id"])
	}
	return ""
}

var iniShape = regexp.MustCompile(`(?i)\[(custom|proxy\s*group|rule|ruleset|proxy)\]`)

// ImportMisub 在一个事务里导入 MiSub 备份。
func (s *Server) ImportMisub(ctx context.Context, raw []byte) (ImportStats, error) {
	var st ImportStats
	var b backupFile
	if err := json.Unmarshal(raw, &b); err != nil {
		return st, errors.New("not_a_backup")
	}
	subs, profiles := b.Data.Subscriptions, b.Data.Profiles
	if len(subs) == 0 && len(profiles) == 0 {
		subs, profiles = b.Subscriptions, b.Profiles
	}
	if len(subs)+len(profiles)+len(b.Data.RuleTemplates) == 0 {
		return st, errors.New("empty_backup")
	}
	if len(subs) > 5000 || len(profiles) > 500 || len(b.Data.RuleTemplates) > 200 {
		return st, errors.New("backup_too_large")
	}
	tx, err := s.Store.Ent.Tx(ctx)
	if err != nil {
		return st, err
	}
	defer tx.Rollback()
	sources, nodes := map[string]int{}, map[string]int{}
	srcPos, _ := tx.SubSource.Query().Count(ctx)
	nodePos, _ := tx.SubNode.Query().Count(ctx)
	for _, it := range subs {
		u, id, name := str(it, "url"), idOf(it["id"]), str(it, "name")
		ops := anyList(it["operators"])
		if checkOperators(ops) != nil {
			ops = nil
			st.Skipped = append(st.Skipped, fmt.Sprintf("%s：处理链不受支持，已忽略", name))
		}
		include, exclude := str(it, "filterInclude"), str(it, "filterExclude", "exclude")
		switch {
		case strings.HasPrefix(strings.ToLower(u), "http://") || strings.HasPrefix(strings.ToLower(u), "https://"):
			if _, err := misub.CheckURL(u); err != nil && !(s.AllowPrivateSources && err.Error() == "source_url_private") {
				st.Skipped = append(st.Skipped, fmt.Sprintf("%s：订阅地址不安全（%s）", firstNonEmpty(name, u), err.Error()))
				continue
			}
			ua := str(it, "customUserAgent")
			for _, r := range ua {
				if r < 0x20 || r > 0x7e {
					ua = ""
				}
			}
			row, err := tx.SubSource.Create().SetName(trimTo(name, 100)).SetURL(u).SetEnabled(boolOr(it, "enabled", true)).SetUserAgent(trimTo(ua, 256)).
				SetFilterInclude(include).SetFilterExclude(exclude).SetOperators(ops).SetPlusAsSpace(boolOr(it, "plusAsSpace", false)).
				SetNodeCache(boolOr(it, "enableNodeCache", false)).SetExcludeTraffic(boolOr(it, "excludeTraffic", false)).
				SetWebsite(trimTo(str(it, "website"), 2048)).SetRemark(trimTo(str(it, "notes", "remark"), 500)).SetPosition(srcPos).Save(ctx)
			if err != nil {
				return st, err
			}
			srcPos++
			sources[id] = row.ID
			st.Sources++
		case validNodeURL(u):
			row, err := tx.SubNode.Create().SetName(trimTo(name, 200)).SetURL(u).SetGroup(trimTo(str(it, "group"), 100)).SetEnabled(boolOr(it, "enabled", true)).
				SetFilterInclude(include).SetFilterExclude(exclude).SetOperators(ops).SetPlusAsSpace(boolOr(it, "plusAsSpace", false)).SetPosition(nodePos).Save(ctx)
			if err != nil {
				return st, err
			}
			nodePos++
			nodes[id] = row.ID
			st.Nodes++
		default:
			st.Skipped = append(st.Skipped, fmt.Sprintf("%s：不是订阅地址或可识别的节点", firstNonEmpty(name, id)))
		}
	}
	for _, t := range b.Data.RuleTemplates {
		content, name := str(t, "content"), str(t, "name")
		format := "ini"
		if !iniShape.MatchString(content) {
			format = "yaml"
		}
		if content == "" || len(content) > 512<<10 || misub.CheckTemplate(format, content) != nil {
			st.Skipped = append(st.Skipped, fmt.Sprintf("模板 %s：内容无效", name))
			continue
		}
		if _, err := tx.RuleTemplate.Create().SetName(trimTo(firstNonEmpty(name, "导入的模板"), 100)).SetFormat(ruletemplate.Format(format)).SetContent(content).Save(ctx); err != nil {
			return st, err
		}
		st.Templates++
	}
	pos, _ := tx.SubProfile.Query().Count(ctx)
	for _, p := range profiles {
		name := trimTo(firstNonEmpty(str(p, "name"), "导入的订阅组"), 100)
		var srcIDs, nodeIDs []int
		for _, ref := range anyList(p["subscriptions"]) {
			if v, ok := sources[idOf(ref)]; ok {
				srcIDs = append(srcIDs, v)
			} else if v, ok := nodes[idOf(ref)]; ok {
				nodeIDs = append(nodeIDs, v)
			}
		}
		for _, ref := range anyList(p["manualNodes"]) {
			if v, ok := nodes[idOf(ref)]; ok {
				nodeIDs = append(nodeIDs, v)
			}
		}
		ops := anyList(p["operators"])
		if checkOperators(ops) != nil {
			ops = nil
		}
		settings := map[string]any{}
		if lvl := str(p, "ruleLevel", "clashRuleLevel"); validRuleLevel(lvl) && lvl != "" {
			settings["rule_level"] = lvl
		}
		if ps, ok := p["prefixSettings"].(map[string]any); ok {
			if v, ok := ps["enableManualNodes"].(bool); ok {
				settings["prepend_manual"] = v
			}
			if v, ok := ps["enableSubscriptions"].(bool); ok {
				settings["prepend_sources"] = v
			}
			if v := str(ps, "manualNodePrefix"); v != "" {
				settings["manual_prefix"] = v
			}
		}
		if tpl := str(p, "transformConfig"); tpl != "" {
			if t, ok := misub.BuiltinTemplate(tpl); ok && t.Text != "" {
				settings["template"] = "builtin:" + tpl
			} else if u, err := misub.CheckURL(tpl); err == nil && u.Scheme == "https" {
				settings["template"] = "remote:" + tpl
			}
		}
		create := tx.SubProfile.Create().SetName(name).SetEnabled(boolOr(p, "enabled", true)).SetSourceIds(dedupe(srcIDs)).SetNodeIds(dedupe(nodeIDs)).
			SetOperators(ops).SetSettings(settings).SetPosition(pos)
		if e := str(p, "expiresAt"); e != "" {
			if t, err := time.Parse(time.RFC3339, e); err == nil {
				create.SetExpiresAt(t)
			} else if t, err := time.Parse("2006-01-02", e); err == nil {
				create.SetExpiresAt(t)
			}
		}
		if _, err := create.Save(ctx); err != nil {
			return st, err
		}
		pos++
		st.Profiles++
	}
	if err = tx.Commit(); err != nil {
		return st, err
	}
	s.invalidateGroups(ctx)
	return st, nil
}

func trimTo(v string, n int) string {
	if len(v) <= n {
		return v
	}
	r := []rune(v)
	for len(string(r)) > n {
		r = r[:len(r)-1]
	}
	return string(r)
}

// ExportMisub 生成 MiSub 格式的备份（订阅源 s<ID>、手动节点 n<ID>），可再导入本项目或上游 MiSub。
func (s *Server) ExportMisub(ctx context.Context) ([]byte, error) {
	b := backupFile{Version: 1, App: "MiSub", Type: "misub-backup", ExportedAt: time.Now().UTC()}
	sources, err := s.Store.Ent.SubSource.Query().Order(ent.Asc(subsource.FieldPosition), ent.Asc(subsource.FieldID)).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, v := range sources {
		b.Data.Subscriptions = append(b.Data.Subscriptions, map[string]any{"id": fmt.Sprintf("s%d", v.ID), "name": v.Name, "url": v.URL, "enabled": v.Enabled,
			"customUserAgent": v.UserAgent, "filterInclude": v.FilterInclude, "filterExclude": v.FilterExclude, "operators": v.Operators, "plusAsSpace": v.PlusAsSpace,
			"enableNodeCache": v.NodeCache, "excludeTraffic": v.ExcludeTraffic, "website": v.Website, "notes": v.Remark})
	}
	nodes, err := s.Store.Ent.SubNode.Query().Order(ent.Asc(subnode.FieldPosition), ent.Asc(subnode.FieldID)).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, v := range nodes {
		b.Data.Subscriptions = append(b.Data.Subscriptions, map[string]any{"id": fmt.Sprintf("n%d", v.ID), "name": v.Name, "url": v.URL, "group": v.Group, "enabled": v.Enabled,
			"filterInclude": v.FilterInclude, "filterExclude": v.FilterExclude, "operators": v.Operators, "plusAsSpace": v.PlusAsSpace})
	}
	profiles, err := s.Store.Ent.SubProfile.Query().Order(ent.Asc(subprofile.FieldPosition), ent.Asc(subprofile.FieldID)).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, p := range profiles {
		subs, man := []string{}, []string{}
		srcIDs, nodeIDs := p.SourceIds, p.NodeIds
		if p.AllSources {
			srcIDs, nodeIDs = nil, nil
			for _, v := range sources {
				srcIDs = append(srcIDs, v.ID)
			}
			for _, v := range nodes {
				nodeIDs = append(nodeIDs, v.ID)
			}
		}
		for _, id := range srcIDs {
			subs = append(subs, fmt.Sprintf("s%d", id))
		}
		for _, id := range nodeIDs {
			man = append(man, fmt.Sprintf("n%d", id))
		}
		item := map[string]any{"id": fmt.Sprintf("p%d", p.ID), "name": p.Name, "enabled": p.Enabled, "subscriptions": subs, "manualNodes": man, "operators": p.Operators}
		if p.ExpiresAt != nil {
			item["expiresAt"] = p.ExpiresAt.UTC().Format(time.RFC3339)
		}
		ps, _ := decodeProfileSettings(p.Settings)
		if ps.RuleLevel != nil {
			item["ruleLevel"] = *ps.RuleLevel
		}
		if ps.Template != nil && strings.HasPrefix(*ps.Template, "builtin:") {
			item["transformConfigMode"], item["transformConfig"] = "builtin", strings.TrimPrefix(*ps.Template, "builtin:")
		}
		b.Data.Profiles = append(b.Data.Profiles, item)
	}
	templates, err := s.Store.Ent.RuleTemplate.Query().Order(ent.Asc(ruletemplate.FieldID)).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, t := range templates {
		b.Data.RuleTemplates = append(b.Data.RuleTemplates, map[string]any{"id": fmt.Sprintf("t%d", t.ID), "name": t.Name, "content": t.Content, "type": string(t.Format)})
	}
	return json.MarshalIndent(b, "", "  ")
}

func (s *Server) exportBackup(c *gin.Context) {
	data, err := s.ExportMisub(c.Request.Context())
	if err != nil {
		failure(c, 503, "export_failed")
		return
	}
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="misub-backup-%s.json"`, time.Now().Format("20060102-150405")))
	c.Data(200, "application/json", data)
}

func (s *Server) importBackup(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<20)
	var req struct {
		Content  string `json:"content"`
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		failure(c, 400, "invalid_request")
		return
	}
	if !s.reauth(c, req.Password, req.Code) {
		return
	}
	stats, err := s.ImportMisub(c.Request.Context(), []byte(req.Content))
	if err != nil {
		failDetail(c, 400, "import_failed", err.Error(), nil)
		return
	}
	s.audit(c, "backup.import", "", fmt.Sprintf("ok sources=%d nodes=%d profiles=%d templates=%d skipped=%d", stats.Sources, stats.Nodes, stats.Profiles, stats.Templates, len(stats.Skipped)))
	c.JSON(200, stats)
}

// ---- WebDAV ----

const webdavKey = "webdav-backup"

// WebdavConfig 是 WebDAV 备份设置；密码加密保存、只写不读。
type WebdavConfig struct {
	URL       string     `json:"url"`
	Username  string     `json:"username"`
	Password  string     `json:"password,omitempty"`
	Keep      int        `json:"keep"`
	Auto      bool       `json:"auto"`
	Hours     int        `json:"hours"`
	LastRunAt *time.Time `json:"last_run_at,omitempty"`
	LastError string     `json:"last_error,omitempty"`
}

func (s *Server) webdavConfig(ctx context.Context) (WebdavConfig, error) {
	cfg := WebdavConfig{Keep: 10, Hours: 24}
	row, err := s.Store.Ent.Setting.Query().Where(setting.KeyEQ(webdavKey)).Only(ctx)
	if ent.IsNotFound(err) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	b, _ := json.Marshal(row.Value)
	return cfg, json.Unmarshal(b, &cfg)
}

func (s *Server) saveWebdav(ctx context.Context, cfg WebdavConfig) error {
	return s.Store.Ent.Setting.Create().SetKey(webdavKey).SetValue(toMap(cfg)).OnConflictColumns(setting.FieldKey).UpdateNewValues().Exec(ctx)
}

func webdavView(cfg WebdavConfig) gin.H {
	return gin.H{"url": cfg.URL, "username": cfg.Username, "password_set": cfg.Password != "", "keep": cfg.Keep, "auto": cfg.Auto, "hours": cfg.Hours, "last_run_at": cfg.LastRunAt, "last_error": cfg.LastError}
}

func (s *Server) getWebdav(c *gin.Context) {
	cfg, err := s.webdavConfig(c.Request.Context())
	if err != nil {
		failure(c, 503, "settings_unavailable")
		return
	}
	c.JSON(200, webdavView(cfg))
}

func (s *Server) putWebdav(c *gin.Context) {
	var req struct {
		WebdavConfig
		AccountPassword string `json:"account_password"`
		Code            string `json:"code"`
	}
	if !bind(c, &req) || !s.reauth(c, req.AccountPassword, req.Code) {
		return
	}
	u, err := url.Parse(strings.TrimSpace(req.URL))
	if req.URL != "" && (err != nil || u.Scheme != "https" && u.Scheme != "http" || u.Host == "" || u.User != nil || u.RawQuery != "") {
		failure(c, 400, "invalid_webdav_url")
		return
	}
	if req.Keep < 1 || req.Keep > 100 || req.Hours < 1 || req.Hours > 720 || len(req.Username) > 255 || len(req.Password) > 1024 {
		failure(c, 400, "invalid_webdav_settings")
		return
	}
	ctx := c.Request.Context()
	old, _ := s.webdavConfig(ctx)
	cfg := req.WebdavConfig
	cfg.URL = strings.TrimRight(strings.TrimSpace(req.URL), "/")
	cfg.LastRunAt, cfg.LastError = old.LastRunAt, old.LastError
	if req.Password == "" {
		cfg.Password = old.Password
	} else if cfg.Password, err = s.Crypto.Encrypt(req.Password); err != nil {
		failure(c, 500, "encrypt_failed")
		return
	}
	if err = s.saveWebdav(ctx, cfg); err != nil {
		failure(c, 500, "save_failed")
		return
	}
	s.audit(c, "backup.webdav", "", "ok")
	c.JSON(200, webdavView(cfg))
}

// webdavRequest 向 WebDAV 服务发请求（地址由管理员配置，可以是内网 NAS）。
func (s *Server) webdavRequest(ctx context.Context, cfg WebdavConfig, method, name string, body []byte, header map[string]string) (*http.Response, error) {
	if cfg.URL == "" {
		return nil, errors.New("webdav_not_configured")
	}
	target := cfg.URL + "/"
	if name != "" {
		target += url.PathEscape(name)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	pw, err := s.reveal(cfg.Password)
	if err != nil {
		return nil, err
	}
	if cfg.Username != "" {
		req.SetBasicAuth(cfg.Username, pw)
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return client.Do(req)
}

type davMultistatus struct {
	Responses []struct {
		Href string `xml:"href"`
	} `xml:"response"`
}

// WebdavFiles 列出备份目录里的 misub-backup-*.json，按名称（即时间）从新到旧。
func (s *Server) WebdavFiles(ctx context.Context, cfg WebdavConfig) ([]string, error) {
	resp, err := s.webdavRequest(ctx, cfg, "PROPFIND", "", nil, map[string]string{"Depth": "1", "Content-Type": "application/xml"})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 207 {
		return nil, fmt.Errorf("webdav_http_%d", resp.StatusCode)
	}
	var ms davMultistatus
	if err = xml.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&ms); err != nil {
		return nil, errors.New("webdav_invalid_listing")
	}
	var out []string
	for _, r := range ms.Responses {
		name, _ := url.PathUnescape(path.Base(strings.TrimRight(r.Href, "/")))
		if strings.HasPrefix(name, "misub-backup-") && strings.HasSuffix(name, ".json") {
			out = append(out, name)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(out)))
	return out, nil
}

// RunWebdavBackup 上传一份备份，并按保留份数删除最旧的。
func (s *Server) RunWebdavBackup(ctx context.Context) (string, error) {
	cfg, err := s.webdavConfig(ctx)
	if err != nil {
		return "", err
	}
	data, err := s.ExportMisub(ctx)
	if err == nil {
		name := "misub-backup-" + time.Now().UTC().Format("20060102-150405") + ".json"
		var resp *http.Response
		if resp, err = s.webdavRequest(ctx, cfg, "PUT", name, data, map[string]string{"Content-Type": "application/json"}); err == nil {
			resp.Body.Close()
			if resp.StatusCode >= 300 {
				err = fmt.Errorf("webdav_http_%d", resp.StatusCode)
			}
		}
		if err == nil {
			if files, lerr := s.WebdavFiles(ctx, cfg); lerr == nil {
				for _, old := range files[min(len(files), cfg.Keep):] {
					if r, derr := s.webdavRequest(ctx, cfg, "DELETE", old, nil, nil); derr == nil {
						r.Body.Close()
					}
				}
			}
			now := time.Now()
			cfg.LastRunAt, cfg.LastError = &now, ""
			_ = s.saveWebdav(ctx, cfg)
			return name, nil
		}
	}
	cfg.LastError = err.Error()
	_ = s.saveWebdav(ctx, cfg)
	return "", err
}

func (s *Server) runWebdav(c *gin.Context) {
	name, err := s.RunWebdavBackup(c.Request.Context())
	if err != nil {
		failDetail(c, 502, "webdav_failed", err.Error(), nil)
		return
	}
	s.audit(c, "backup.webdav.run", name, "ok")
	c.JSON(200, gin.H{"file": name})
}

func (s *Server) listWebdav(c *gin.Context) {
	ctx := c.Request.Context()
	cfg, err := s.webdavConfig(ctx)
	if err != nil {
		failure(c, 503, "settings_unavailable")
		return
	}
	files, err := s.WebdavFiles(ctx, cfg)
	if err != nil {
		failDetail(c, 502, "webdav_failed", err.Error(), nil)
		return
	}
	c.JSON(200, gin.H{"files": files})
}

var backupName = regexp.MustCompile(`^misub-backup-[0-9A-Za-z_.-]{1,64}\.json$`)

func (s *Server) restoreWebdav(c *gin.Context) {
	var req struct {
		File     string `json:"file"`
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if !bind(c, &req) || !s.reauth(c, req.Password, req.Code) {
		return
	}
	if !backupName.MatchString(req.File) {
		failure(c, 400, "invalid_backup_file")
		return
	}
	ctx := c.Request.Context()
	cfg, err := s.webdavConfig(ctx)
	if err != nil {
		failure(c, 503, "settings_unavailable")
		return
	}
	resp, err := s.webdavRequest(ctx, cfg, "GET", req.File, nil, nil)
	if err != nil {
		failDetail(c, 502, "webdav_failed", err.Error(), nil)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		failDetail(c, 502, "webdav_failed", fmt.Sprintf("webdav_http_%d", resp.StatusCode), nil)
		return
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		failure(c, 502, "webdav_failed")
		return
	}
	stats, err := s.ImportMisub(ctx, data)
	if err != nil {
		failDetail(c, 400, "import_failed", err.Error(), nil)
		return
	}
	s.audit(c, "backup.webdav.restore", req.File, "ok")
	c.JSON(200, stats)
}
