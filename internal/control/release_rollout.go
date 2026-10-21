package control

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/imzyb/MiGate/ent/task"
	"github.com/imzyb/MiGate/internal/buildinfo"
	"github.com/imzyb/MiGate/internal/service/update"
)

func GateRolloutState(heartbeat, target string, capable bool, taskStatus string) string {
	if target == "" {
		return "unknown"
	}
	if heartbeat == target {
		return "applied"
	}
	if !capable {
		return "pending"
	}
	switch taskStatus {
	case "failed":
		return "failed"
	case "running", "applied":
		return "running"
	default:
		return "pending"
	}
}

func (s *Server) releaseStatus(c *gin.Context) {
	checked, err := (update.Service{}).Check(c.Request.Context(), buildinfo.Version)
	if err != nil {
		checked = update.CheckResponse{CurrentVersion: buildinfo.Version, Status: "unknown", Message: "发布信息不可用"}
	}
	stage, detail, pending := s.releaseJob()
	c.JSON(200, gin.H{
		"current_version":  checked.CurrentVersion,
		"latest_version":   checked.LatestVersion,
		"update_available": checked.UpdateAvailable,
		"release_url":      checked.ReleaseURL,
		"status":           checked.Status,
		"message":          checked.Message,
		"gates":            s.gateRollout(c.Request.Context()),
		"job": gin.H{
			"stage":           stage,
			"message":         detail,
			"request_pending": pending,
		},
	})
}

func (s *Server) releaseJob() (string, string, bool) {
	requestPath := filepath.Join(s.releaseStateDir(), "release-request.json")
	_, requestErr := os.Stat(requestPath)
	pending := requestErr == nil
	body, err := os.ReadFile(filepath.Join(s.releaseStateDir(), "release-status.json"))
	if err != nil {
		if pending {
			return "requested", releaseStageText("requested"), true
		}
		return "idle", "", false
	}
	var raw struct {
		Status  string `json:"status"`
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &raw) != nil {
		return "unknown", "更新状态无法读取", pending
	}
	stage := raw.Status
	switch stage {
	case "requested", "downloading", "verifying", "verified", "installing", "restarting", "failed":
	default:
		stage = "unknown"
	}
	detail := releaseStageText(stage)
	if stage == "failed" && safeReleaseDetail(raw.Message) {
		detail = raw.Message
	}
	return stage, detail, pending
}

func releaseStageText(stage string) string {
	switch stage {
	case "requested":
		return "已提交，等待安装单元"
	case "downloading":
		return "正在下载"
	case "verifying", "verified":
		return "正在校验"
	case "installing":
		return "正在安装"
	case "restarting":
		return "即将重启"
	case "failed":
		return "更新失败"
	default:
		return "更新状态无法读取"
	}
}

func safeReleaseDetail(message string) bool {
	if message == "" || len(message) > 80 {
		return false
	}
	for _, r := range message {
		if r < 32 || r == '/' || r == '\\' {
			return false
		}
	}
	return true
}

func (s *Server) requestRelease(c *gin.Context) {
	var req struct {
		Confirm bool   `json:"confirm"`
		Tag     string `json:"tag"`
	}
	if !bind(c, &req) || !req.Confirm {
		failure(c, 400, "release_confirmation_required")
		return
	}
	version, ok := buildinfo.ParseScheme(req.Tag)
	if !ok || req.Tag != "v"+version.String() {
		failure(c, 400, "release_tag_refused")
		return
	}
	dir := s.releaseStateDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		failure(c, 503, "release_request_failed")
		return
	}
	statusBody, _ := json.Marshal(map[string]string{"status": "requested", "message": "已提交，等待安装单元"})
	if err := os.WriteFile(filepath.Join(dir, "release-status.json"), statusBody, 0600); err != nil {
		failure(c, 503, "release_request_failed")
		return
	}
	path := filepath.Join(dir, "release-request.json")
	// 先删除再创建，已存在的请求文件也能再次触发 PathExists。
	_ = os.Remove(path)
	body, _ := json.Marshal(map[string]string{"tag": req.Tag})
	if err := os.WriteFile(path, body, 0600); err != nil {
		failure(c, 503, "release_request_failed")
		return
	}
	s.audit(c, "release.request", req.Tag, "requested")
	c.JSON(200, gin.H{"status": "requested"})
}

func (s *Server) gateRollout(ctx context.Context) []gin.H {
	manifest, err := readGateManifest(s.gateReleaseDir())
	target := ""
	if err == nil {
		target = manifest.Version
	}
	if s == nil || s.Store == nil {
		return []gin.H{{"name": "", "version": "", "rollout": "unknown"}}
	}
	devices, err := s.Store.Device.Query().All(ctx)
	if err != nil {
		return []gin.H{{"name": "", "version": "", "rollout": "unknown"}}
	}
	rows := make([]gin.H, 0, len(devices))
	for _, device := range devices {
		if device.Status != "approved" {
			continue
		}
		version := ""
		if device.Metadata != nil {
			version = text(device.Metadata["version"])
		}
		taskStatus := ""
		newest := 0
		if existing, queryErr := s.Store.Task.Query().Where(task.DeviceIDEQ(device.ID), task.OperationEQ("release.replace")).All(ctx); queryErr == nil {
			for _, item := range existing {
				if item.ID >= newest {
					newest = item.ID
					taskStatus = item.Status
				}
			}
		}
		rows = append(rows, gin.H{
			"id":      device.ID,
			"name":    device.Name,
			"version": version,
			"rollout": GateRolloutState(version, target, deviceCapable(device.Metadata, "release.replace.v1"), taskStatus),
		})
	}
	return rows
}

type gateManifest struct {
	Version      string `json:"version"`
	GateSHA256   string `json:"gate_sha256"`
	ManageSHA256 string `json:"manage_sha256"`
}

func readGateManifest(dir string) (gateManifest, error) {
	var manifest gateManifest
	body, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return manifest, err
	}
	err = json.Unmarshal(body, &manifest)
	return manifest, err
}

func (s *Server) enqueueGateRelease(ctx context.Context) {
	if s == nil || s.Store == nil {
		return
	}
	manifest, err := readGateManifest(s.gateReleaseDir())
	if err != nil || manifest.Version == "" {
		return
	}
	devices, err := s.Store.Device.Query().All(ctx)
	if err != nil {
		return
	}
	gateBody, gateErr := os.ReadFile(filepath.Join(s.gateReleaseDir(), "migate-gate"))
	manageBody, manageErr := os.ReadFile(filepath.Join(s.gateReleaseDir(), "migate-manage"))
	if gateErr != nil || manageErr != nil {
		return
	}
	for _, device := range devices {
		heartbeat := ""
		if device.Metadata != nil {
			heartbeat = text(device.Metadata["version"])
		}
		if device.Status != "approved" || heartbeat == manifest.Version {
			continue
		}
		if !deviceCapable(device.Metadata, "release.replace.v1") {
			continue
		}
		if s.releaseTaskExists(ctx, device.ID, manifest.Version) {
			continue
		}
		payload := map[string]any{
			"version":       manifest.Version,
			"gate_sha256":   manifest.GateSHA256,
			"manage_sha256": manifest.ManageSHA256,
			"gate_body":     encodeRelease(gateBody),
			"manage_body":   encodeRelease(manageBody),
		}
		_, _ = s.enqueue(ctx, device.ID, "release.replace", "release.replace:"+strconv.Itoa(device.ID)+":"+manifest.Version, time.Now().Unix(), payload)
	}
}

func (s *Server) releaseTaskExists(ctx context.Context, deviceID int, version string) bool {
	items, err := s.Store.Task.Query().Where(task.DeviceIDEQ(deviceID), task.OperationEQ("release.replace")).All(ctx)
	if err != nil {
		return true
	}
	for _, item := range items {
		if item.Payload != nil && text(item.Payload["version"]) == version && item.Status != "failed" {
			return true
		}
	}
	return false
}

func (s *Server) gateReleaseDir() string {
	if value := os.Getenv("MIGATE_GATE_RELEASE_DIR"); value != "" {
		return value
	}
	return filepath.Join(s.releaseStateDir(), "gate-release")
}

func (s *Server) releaseStateDir() string {
	if value := os.Getenv("MIGATE_RELEASE_STATE_DIR"); value != "" {
		return value
	}
	return "/var/lib/migate-control"
}

func deviceCapable(metadata map[string]any, want string) bool {
	if metadata == nil {
		return false
	}
	switch values := metadata["capabilities"].(type) {
	case []any:
		for _, value := range values {
			if value == want {
				return true
			}
		}
	case []string:
		for _, value := range values {
			if value == want {
				return true
			}
		}
	}
	return false
}

func encodeRelease(body []byte) string {
	return base64.StdEncoding.EncodeToString(body)
}
