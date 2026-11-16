package center

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

var errBackupPreviewStale = errors.New("backup_preview_stale")
var errBackupPreviewRequired = errors.New("backup_preview_required")

type restoreControl struct {
	Preview  bool
	Revision string
	Receipt  string
}

type backupPreviewClaim struct {
	Actor    int       `json:"actor"`
	Digest   string    `json:"digest"`
	Revision string    `json:"revision"`
	Origin   string    `json:"origin"`
	Expires  time.Time `json:"expires"`
	Nonce    string    `json:"nonce"`
}

func backupDigest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (s *Server) previewBackup(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<20)
	var req struct {
		Content  string `json:"content"`
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if !bind(c, &req) {
		return
	}
	s.respondBackupPreview(c, []byte(req.Content), "local")
}

func (s *Server) respondBackupPreview(c *gin.Context, raw []byte, origin string) {
	ctx := c.Request.Context()
	finish := operationStage(ctx, "backup.preview")
	control := &restoreControl{Preview: true}
	var stats ImportStats
	err := s.Store.Lock.Do(ctx, "subscription-profiles", func(ctx context.Context) error {
		var err error
		stats, err = s.importSubscriptionsLocked(ctx, raw, control)
		return err
	})
	if err != nil {
		finish(err)
		backupFailure(c, err)
		return
	}
	claim := backupPreviewClaim{Actor: c.GetInt("user_id"), Digest: backupDigest(raw), Revision: control.Revision, Origin: origin, Expires: time.Now().Add(10 * time.Minute), Nonce: randomToken(24)}
	encoded, err := json.Marshal(claim)
	if err != nil {
		finish(err)
		backupFailure(c, err)
		return
	}
	token, err := s.Crypto.Encrypt(string(encoded))
	finish(err)
	if err != nil {
		failInternal(c, 503, "preview_failed", err)
		return
	}
	s.audit(c, "backup.preview", "", "ok")
	c.JSON(200, gin.H{"preview_token": token, "digest": claim.Digest, "data_version": claim.Revision, "expires_at": claim.Expires, "added": stats, "overwritten": 0, "deleted": 0})
}

func (s *Server) confirmBackup(ctx context.Context, actor int, raw []byte, token, origin string) (stats ImportStats, err error) {
	finish := operationStage(ctx, "backup.restore", "actor_id", actor)
	defer func() { finish(err) }()
	if token == "" {
		return stats, errBackupPreviewRequired
	}
	encoded, e := s.Crypto.Decrypt(token)
	var claim backupPreviewClaim
	if e != nil || json.Unmarshal([]byte(encoded), &claim) != nil || claim.Actor != actor || claim.Origin != origin || claim.Digest != backupDigest(raw) || !time.Now().Before(claim.Expires) || claim.Revision == "" || claim.Nonce == "" {
		return stats, errBackupPreviewStale
	}
	err = s.Store.Lock.Do(ctx, "subscription-profiles", func(ctx context.Context) error {
		var e error
		stats, e = s.importSubscriptionsLocked(ctx, raw, &restoreControl{Revision: claim.Revision, Receipt: digest(claim.Nonce)})
		return e
	})
	return
}

func backupFailure(c *gin.Context, err error) {
	if errors.Is(err, errBackupPreviewStale) || errors.Is(err, errBackupPreviewRequired) {
		failure(c, 409, err.Error())
		return
	}
	failDetail(c, 400, "import_failed", err.Error(), nil)
}
