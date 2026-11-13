package center

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/shusfun/weir/internal/publicnet"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/setting"
	js "github.com/shusfun/weir/internal/sub/jscompat"
	"github.com/shusfun/weir/internal/subgroup"
	"github.com/shusfun/weir/packages/contracts"
)

const converterSettingKey = "subscription-converter"

type converterSettings struct {
	Enabled       bool   `json:"enabled"`
	Endpoint      string `json:"endpoint"`
	Authorization string `json:"authorization,omitempty"`
	PreferredGate int    `json:"preferred_gate_id"`
	MaxBytes      int64  `json:"max_bytes"`
}

func (s *Server) converterSettings(ctx context.Context) (converterSettings, error) {
	out := converterSettings{MaxBytes: 8 << 20}
	row, err := s.Store.Ent.Setting.Query().Where(setting.KeyEQ(converterSettingKey)).Only(ctx)
	if ent.IsNotFound(err) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	err = contracts.DecodeStrict(mustJSONBytes(row.Value), &out, 32<<10)
	return out, err
}

func (cfg converterSettings) view() gin.H {
	return gin.H{"enabled": cfg.Enabled, "endpoint": cfg.Endpoint, "authorization_set": cfg.Authorization != "", "preferred_gate_id": cfg.PreferredGate, "max_bytes": cfg.MaxBytes}
}

func (s *Server) getConverterSettings(c *gin.Context) {
	cfg, err := s.converterSettings(c.Request.Context())
	if err != nil {
		failInternal(c, 503, "settings_unavailable", err)
		return
	}
	c.JSON(200, cfg.view())
}

func (s *Server) putConverterSettings(c *gin.Context) {
	var req struct {
		converterSettings
		ClearAuthorization bool `json:"clear_authorization"`
	}
	if !bindWorker(c, &req, 32<<10) {
		return
	}
	cfg := req.converterSettings
	cfg.Endpoint = strings.TrimSpace(cfg.Endpoint)
	if cfg.Endpoint != "" {
		u, err := publicnet.URL(cfg.Endpoint)
		if err != nil || u.RawQuery != "" || len(cfg.Endpoint) > 4096 {
			failure(c, 400, "converter_endpoint_invalid")
			return
		}
	}
	if cfg.Enabled && cfg.Endpoint == "" || cfg.MaxBytes < 1024 || cfg.MaxBytes > 10<<20 || cfg.PreferredGate < 0 || len(cfg.Authorization) > 8192 || strings.ContainsAny(cfg.Authorization, "\r\n\x00") || req.ClearAuthorization && cfg.Authorization != "" {
		failure(c, 400, "converter_settings_invalid")
		return
	}
	ctx := c.Request.Context()
	if cfg.PreferredGate > 0 {
		if _, err := s.Store.Ent.Gate.Get(ctx, cfg.PreferredGate); err != nil {
			failure(c, 400, "converter_gate_invalid")
			return
		}
	}
	old, err := s.converterSettings(ctx)
	if err != nil {
		failInternal(c, 503, "settings_unavailable", err)
		return
	}
	if req.ClearAuthorization {
		cfg.Authorization = ""
	} else if cfg.Authorization == "" {
		cfg.Authorization = old.Authorization
	} else if cfg.Authorization, err = s.Crypto.Encrypt(cfg.Authorization); err != nil {
		failInternal(c, 503, "encrypt_failed", err)
		return
	}
	if dryRun(c, dryResult{Preview: []string{"保存外部转换后端；启用并选择外部引擎的订阅会把处理后的节点（含连接凭据）、目标格式和远程模板地址交给该后端，Gate 代为请求，不向客户端跳转"}}) {
		return
	}
	if err = s.Store.Ent.Setting.Create().SetKey(converterSettingKey).SetValue(toMap(cfg)).OnConflictColumns(setting.FieldKey).UpdateNewValues().Exec(ctx); err != nil {
		failInternal(c, 503, "save_failed", err)
		return
	}
	s.invalidateGroups(ctx)
	s.audit(c, "subscription.converter.save", "", "ok")
	c.JSON(200, cfg.view())
}

func (s *Server) convertGroup(ctx context.Context, profile int, list, target string, options subgroup.RenderOptions) (body []byte, contentType string, err error) {
	started := time.Now().UTC()
	defer func() {
		subscriptionStageResult(ctx, profile, "subscription.convert", "center.convertGroup", started, err)
	}()
	cfg, err := s.converterSettings(ctx)
	if err != nil {
		return nil, "", errors.New("converter_settings_unavailable")
	}
	if !cfg.Enabled || cfg.Endpoint == "" {
		return nil, "", errors.New("converter_not_configured")
	}
	endpoint, err := subgroup.ConverterURL(cfg.Endpoint, list, target, options)
	if err != nil {
		return nil, "", err
	}
	authorization := ""
	if cfg.Authorization != "" {
		authorization, err = s.Crypto.Decrypt(cfg.Authorization)
		if err != nil {
			return nil, "", errors.New("converter_secret_unavailable")
		}
	}
	result, _, err := s.dispatchNetwork(ctx, contracts.TaskExternalConvert, contracts.ExternalConvertTask{Method: http.MethodGet, Credentials: contracts.FetchSecrets{URL: endpoint, Authorization: authorization}, MaxBytes: cfg.MaxBytes}, cfg.PreferredGate)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, errNoWorker) {
			return nil, "", err
		}
		return nil, "", errors.New("converter_request_failed")
	}
	if err = validateConvertedOutput(target, result.Body); err != nil {
		return nil, "", err
	}
	contentType = "text/plain; charset=utf-8"
	if target == "clash" {
		contentType = "text/yaml; charset=utf-8"
	}
	return result.Body, contentType, nil
}

func validateConvertedOutput(target string, body []byte) error {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 || !utf8.Valid(trimmed) || bytes.IndexByte(trimmed, 0) >= 0 || bytes.HasPrefix(trimmed, []byte("<")) {
		return errors.New("converter_result_invalid")
	}
	if target == "clash" {
		doc, err := js.YAMLLoad(string(trimmed))
		obj, ok := doc.(*js.Object)
		if err != nil || !ok || !js.IsArray(obj.Get("proxies")) {
			return errors.New("converter_result_invalid")
		}
	} else if target == "base64" || target == "v2ray" || target == "trojan" {
		decoded, err := base64.StdEncoding.DecodeString(string(bytes.Join(bytes.Fields(trimmed), nil)))
		if err != nil || len(decoded) == 0 || !utf8.Valid(decoded) || !strings.Contains(string(decoded), "://") {
			return errors.New("converter_result_invalid")
		}
	} else if (target == "surge" || target == "loon") && !bytes.Contains(bytes.ToLower(trimmed), []byte("[proxy]")) {
		return errors.New("converter_result_invalid")
	} else if target == "quanx" && !bytes.Contains(trimmed, []byte("tag=")) {
		return errors.New("converter_result_invalid")
	}
	return nil
}

func validSubscriptionEngine(engine string) bool {
	return engine == "" || engine == "builtin" || engine == "external"
}
