package center

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shusfun/weir/internal/store/ent/setting"
	"github.com/shusfun/weir/packages/contracts"
	"github.com/shusfun/weir/packages/laya"
)

type layaAttachmentPreview struct {
	ID               string       `json:"id"`
	Actor            int          `json:"actor"`
	ExpiresAt        time.Time    `json:"expires_at"`
	SettingsRevision string       `json:"settings_revision"`
	Configured       layaSettings `json:"configured"`
	RuntimeVersion   string       `json:"runtime_version"`
	ModelDigest      string       `json:"model_digest"`
}

type layaDiscoveredInstance struct {
	URL            string                   `json:"url"`
	Model          string                   `json:"model"`
	State          string                   `json:"state"`
	Code           string                   `json:"code,omitempty"`
	RuntimeVersion string                   `json:"runtime_version,omitempty"`
	ModelDigest    string                   `json:"model_digest,omitempty"`
	Compatibility  *layaCompatibilityStatus `json:"compatibility,omitempty"`
	PreviewID      string                   `json:"preview_id,omitempty"`
	ExpiresAt      *time.Time               `json:"expires_at,omitempty"`
}

func (s *Server) layaDiscoveryRoutes(admin *gin.RouterGroup) {
	admin.POST("/defense/laya/discover", s.discoverLaya)
	admin.POST("/defense/laya/attach/:id", s.attachLaya)
}

func (s *Server) probeLayaInstance(ctx context.Context, configured layaSettings) (layaDiscoveredInstance, error) {
	out := layaDiscoveredInstance{URL: configured.URL, Model: configured.Model, State: "unknown"}
	client := laya.Client{BaseURL: configured.URL, APIKey: configured.Key, Observer: func(ctx context.Context, e laya.Event) {
		slog.InfoContext(ctx, "laya instance discovery", "trace", taskTrace(ctx), "operation", e.Operation, "status", e.Status, "code", e.Code, "ms", e.Finished.Sub(e.Started).Milliseconds())
	}}
	ctx = laya.WithTrace(ctx, taskTrace(ctx))
	version, err := client.RuntimeVersion(ctx)
	if err != nil {
		out.Code = "laya_runtime_version_unknown"
		return out, err
	}
	out.RuntimeVersion = version
	model, err := client.ShowModel(ctx, configured.Model)
	if err != nil {
		out.Code = "laya_model_status_unavailable"
		return out, err
	}
	out.ModelDigest = model.Digest
	if strings.Split(configured.Model, "@")[0] == laya.DefaultModel && model.Digest != laya.ModelManifestSHA256 {
		out.Code = "laya_model_digest_mismatch"
		return out, errors.New(out.Code)
	}
	out.State = "available"
	return out, nil
}

// Discovery examines only the saved address, the default loopback address, or
// an explicit administrator address. It never scans ports or modifies services.
func (s *Server) discoverLaya(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var req struct {
		URL   string `json:"url"`
		Model string `json:"model"`
		Key   string `json:"key"`
	}
	if !bindWorker(c, &req, 16<<10) {
		return
	}
	if len(req.URL) > 2048 || len(req.Model) > 200 || len(req.Key) > 8192 || strings.ContainsAny(req.Model, "\r\n") || strings.ContainsAny(req.Key, "\r\n") {
		failure(c, 400, "invalid_laya_settings")
		return
	}
	if strings.TrimSpace(req.Key) != "" && strings.TrimSpace(req.URL) == "" {
		failure(c, 400, "invalid_laya_url")
		return
	}
	ctx := c.Request.Context()
	n, err := s.Store.KV.Incr(ctx, "laya-discovery-rate:"+itoa(c.GetInt("user_id")), time.Minute)
	if err != nil {
		failInternal(c, 503, "laya_discovery_unavailable", err)
		return
	}
	if n > 5 {
		failure(c, 429, "laya_discovery_rate_limited")
		return
	}
	old, revision, err := s.readLayaSettings(ctx)
	if err != nil {
		failInternal(c, 503, "laya_configuration_unavailable", err)
		return
	}
	profile, err := s.aiProfile(ctx)
	if err != nil {
		failInternal(c, 503, "laya_configuration_unavailable", err)
		return
	}
	addresses := []string{strings.TrimSpace(req.URL)}
	if addresses[0] == "" {
		addresses = []string{"http://127.0.0.1:11435"}
		if old.Mode == "center" && old.URL != "" && old.URL != addresses[0] {
			addresses = append([]string{old.URL}, addresses...)
		}
	}
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = laya.DefaultModel
	}
	items := []layaDiscoveredInstance{}
	for _, address := range addresses {
		if _, err := layaEndpoint(address); err != nil {
			failure(c, 400, "invalid_laya_url")
			return
		}
		configured := layaSettings{Mode: "center", URL: address, Model: model, Key: strings.TrimSpace(req.Key)}
		if configured.Key == "" && old.Mode == "center" && address == old.URL && old.Key != "" {
			configured.Key, err = s.reveal(old.Key)
			if err != nil {
				failInternal(c, 503, "laya_credentials_unavailable", err)
				return
			}
		}
		probe, cancel := context.WithTimeout(ctx, 6*time.Second)
		item, probeErr := s.probeLayaInstance(probe, configured)
		cancel()
		status := resolveLayaCompatibility(*profile.Effective.LayaCompatibility, item.RuntimeVersion)
		item.Compatibility = &status
		if probeErr == nil {
			p := layaAttachmentPreview{ID: randomHex(24), Actor: c.GetInt("user_id"), ExpiresAt: time.Now().UTC().Add(10 * time.Minute), SettingsRevision: revision, Configured: configured, RuntimeVersion: item.RuntimeVersion, ModelDigest: item.ModelDigest}
			raw, _ := json.Marshal(p)
			sealed, err := s.Crypto.Encrypt(string(raw))
			if err == nil {
				err = s.Store.KV.Set(ctx, "laya-attach-preview:"+p.ID, sealed, 10*time.Minute)
			}
			if err != nil {
				failInternal(c, 503, "laya_discovery_unavailable", err)
				return
			}
			item.PreviewID, item.ExpiresAt = p.ID, &p.ExpiresAt
		}
		items = append(items, item)
	}
	s.audit(c, "defense.laya.discover", "", "ok")
	c.JSON(200, gin.H{"items": items, "inference": "not_checked"})
}

func (s *Server) attachLaya(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	id := c.Param("id")
	if !validTraceHex(id, 48) {
		failure(c, 404, "laya_attachment_expired")
		return
	}
	ctx := c.Request.Context()
	err := s.Store.Lock.Do(ctx, "laya-settings", func(ctx context.Context) error {
		sealed, ok, err := s.Store.KV.Get(ctx, "laya-attach-preview:"+id)
		if err != nil {
			return err
		}
		if !ok {
			return errors.New("laya_attachment_expired")
		}
		raw, err := s.Crypto.Decrypt(sealed)
		var p layaAttachmentPreview
		if err != nil || contracts.DecodeStrict([]byte(raw), &p, 32<<10) != nil || p.ID != id || p.Actor != c.GetInt("user_id") || !time.Now().Before(p.ExpiresAt) {
			return errors.New("laya_attachment_expired")
		}
		_, revision, err := s.readLayaSettings(ctx)
		if err != nil {
			return err
		}
		if revision != p.SettingsRevision {
			return errors.New("laya_configuration_changed")
		}
		probe, cancel := context.WithTimeout(ctx, 6*time.Second)
		defer cancel()
		actual, err := s.probeLayaInstance(probe, p.Configured)
		if err != nil || actual.RuntimeVersion != p.RuntimeVersion || actual.ModelDigest != p.ModelDigest {
			return errors.New("laya_instance_changed")
		}
		configured := p.Configured
		configured.Enabled = false
		if configured.Key != "" {
			configured.Key, err = s.Crypto.Encrypt(configured.Key)
			if err != nil {
				return err
			}
		}
		tx, err := s.Store.Ent.Tx(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if err = tx.Setting.Create().SetKey("laya").SetValue(toMap(configured)).OnConflictColumns(setting.FieldKey).UpdateNewValues().Exec(ctx); err != nil {
			return err
		}
		if _, err = tx.Audit.Create().SetActor(p.Actor).SetAction("defense.laya.attach").SetTarget(id).SetResult("ok").SetDetail(map[string]any{"trace_id": taskTrace(ctx), "runtime_version": p.RuntimeVersion, "model_digest": p.ModelDigest}).Save(ctx); err != nil {
			return err
		}
		return tx.Commit()
	})
	if err != nil {
		switch err.Error() {
		case "laya_attachment_expired":
			failure(c, 404, err.Error())
		case "laya_configuration_changed", "laya_instance_changed":
			failure(c, 409, err.Error())
		default:
			failInternal(c, 503, "laya_attachment_unavailable", err)
		}
		return
	}
	_ = s.Store.KV.Del(ctx, "laya-attach-preview:"+id)
	s.probeCenterLaya(ctx)
	s.getLaya(c)
}
