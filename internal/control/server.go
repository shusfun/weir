package control

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/imzyb/MiGate/ent"
	"github.com/imzyb/MiGate/ent/resource"
	"github.com/imzyb/MiGate/ent/user"
	"github.com/imzyb/MiGate/internal/buildinfo"
	"github.com/imzyb/MiGate/internal/secrets"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
)

type Server struct {
	Config       Config
	Path         string
	DB           *sql.DB
	Store        *ent.Client
	Redis        *redis.Client
	Crypto       *secrets.AESEncryptor
	mu           sync.Mutex
	sourceMu     sync.Mutex
	configMu     sync.RWMutex
	Assets       fs.FS
	runtimeCtx   context.Context
	workerOnce   sync.Once
	workersReady atomic.Bool
}

func redisTLS() *tls.Config  { return &tls.Config{MinVersion: tls.VersionTLS12} }
func digest(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func New(c Config, path string, assets fs.FS) (*Server, error) {
	enc, err := secrets.NewAESEncryptor(c.EncryptionKey)
	if err != nil {
		return nil, err
	}
	s := &Server{Config: c, Path: path, Crypto: enc, Assets: assets}
	if c.Installed {
		if err = s.connect(c); err != nil {
			return nil, err
		}
		ok, e := s.Store.User.Query().Where(user.RoleEQ("admin")).Exist(context.Background())
		if e != nil || !ok {
			s.Close()
			return nil, errors.New("installed database cannot be verified; restore configuration before starting")
		}
	}
	return s, nil
}
func (s *Server) connect(c Config) error {
	db, store, err := openDatabase(c.Database)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if err = db.PingContext(ctx); err != nil {
		db.Close()
		return errors.New("PostgreSQL connection failed")
	}
	r := redisClient(c.Redis)
	if err = r.Ping(ctx).Err(); err != nil {
		db.Close()
		r.Close()
		return errors.New("Redis connection failed")
	}
	if s.Redis != nil {
		_ = s.Redis.Close()
	}
	if s.Store != nil {
		_ = s.Store.Close()
	}
	s.DB = db
	s.Store = store
	s.Redis = r
	return nil
}
func (s *Server) Close() {
	if s.Redis != nil {
		s.Redis.Close()
	}
	if s.Store != nil {
		s.Store.Close()
	}
}
func failure(c *gin.Context, status int, code string) {
	c.AbortWithStatusJSON(status, gin.H{"error": gin.H{"code": code, "message": code}})
}
func bind(c *gin.Context, v any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1024*1024)
	if err := c.ShouldBindJSON(v); err != nil {
		failure(c, 400, "invalid_request")
		return false
	}
	return true
}
func (s *Server) audit(c *gin.Context, action, target, result string) {
	if s.Store == nil {
		return
	}
	_, _ = s.Store.Audit.Create().SetActor(c.GetInt("user_id")).SetAction(action).SetTarget(target).SetResult(result).SetIP(c.ClientIP()).Save(c.Request.Context())
}
func (s *Server) Router() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	// Configuration and dependency handles change only during installation.
	// Hold a reader lock per request, and an exclusive lock for that transition.
	r.Use(func(c *gin.Context) {
		if c.Request.URL.Path == "/api/v1/setup/install" {
			s.configMu.Lock()
			defer s.configMu.Unlock()
		} else {
			s.configMu.RLock()
			defer s.configMu.RUnlock()
		}
		c.Next()
	})
	_ = r.SetTrustedProxies([]string{"127.0.0.1", "::1"})
	r.Use(func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Referrer-Policy", "no-referrer")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Cache-Control", "no-store")
		c.Header("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'")
		if !sameOriginRequest(c.Request) {
			failure(c, 403, "origin_rejected")
			return
		}
		c.Next()
	})
	r.GET("/healthz", func(c *gin.Context) {
		if !s.Config.Installed {
			c.JSON(200, gin.H{"status": "setup_required"})
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		if s.DB.PingContext(ctx) != nil || s.Redis.Ping(ctx).Err() != nil {
			failure(c, 503, "dependency_unavailable")
			return
		}
		c.JSON(200, gin.H{"status": "ok"})
	})
	api := r.Group("/api/v1")
	api.GET("/setup/status", s.setupStatus)
	api.POST("/setup/verify", s.setupGuard, func(c *gin.Context) { c.JSON(200, gin.H{"valid": true}) })
	api.POST("/setup/test-db", s.setupGuard, s.testDB)
	api.POST("/setup/test-redis", s.setupGuard, s.testRedis)
	api.POST("/setup/install", s.setupGuard, s.install)
	ready := api.Group("")
	ready.Use(func(c *gin.Context) {
		if !s.Config.Installed {
			failure(c, 503, "setup_required")
			return
		}
		c.Next()
	})
	ready.GET("/public", s.publicSettings)
	ready.GET("/pages/:slug/guestbook", s.listGuestbook)
	ready.POST("/pages/:slug/guestbook", s.submitGuestbook)
	ready.GET("/pages/:slug", s.publicPage)
	ready.GET("/pages/:slug/subscriptions/:id", s.serveDirectoryPublication)
	ready.GET("/announcements/public", s.publishedAnnouncement)
	ready.POST("/auth/login", s.login)
	ready.POST("/auth/mfa", s.completeMFA)
	ready.POST("/auth/register", s.register)
	ready.POST("/auth/email", s.emailCode)
	ready.POST("/auth/reset", s.resetPassword)
	ready.POST("/auth/verify", s.verifyEmail)
	auth := ready.Group("")
	auth.Use(s.authenticate)
	auth.GET("/me", s.me)
	auth.POST("/auth/logout", s.logout)
	auth.GET("/sessions", s.sessions)
	auth.POST("/sessions/revoke-others", s.revokeOtherSessions)
	auth.DELETE("/sessions/:id", s.revokeSession)
	auth.POST("/security/totp/setup", s.setupTOTP)
	auth.POST("/security/totp/confirm", s.confirmTOTP)
	auth.POST("/security/totp/disable", s.disableTOTP)
	auth.POST("/security/password", s.changePassword)
	auth.GET("/subscription", s.mySubscription)
	auth.GET("/announcements", s.publishedAnnouncement)
	auth.GET("/clients", s.getClientCatalog)
	auth.GET("/clients/:id/release", s.getClientRelease)
	admin := auth.Group("/admin")
	admin.Use(func(c *gin.Context) {
		if c.GetString("role") != "admin" {
			s.audit(c, "access.denied", "admin", "denied")
			failure(c, 403, "forbidden")
			return
		}
		c.Next()
	})
	admin.GET("/public-pages/:id/subscriptions", s.getPagePublications)
	admin.PUT("/public-pages/:id/subscriptions", s.putPagePublications)
	admin.GET("/profiles/:id/publication", s.getProfilePublication)
	admin.PUT("/profiles/:id/publication", s.putProfilePublication)
	admin.GET("/users", s.listUsers)
	admin.GET("/users/:id", s.getUser)
	admin.GET("/users/:id/sessions", s.adminUserSessions)
	admin.DELETE("/users/:id/sessions/:session", s.adminRevokeUserSession)
	admin.POST("/users", s.createUser)
	admin.PATCH("/users/:id", s.updateUser)
	admin.GET("/public-pages/:id/guestbook/config", s.getGuestbookConfig)
	admin.PUT("/public-pages/:id/guestbook/config", s.putGuestbookConfig)
	admin.GET("/public-pages/:id/guestbook/messages", s.listGuestbook)
	admin.POST("/public-pages/:id/guestbook/messages", s.moderateGuestbook)
	admin.GET("/subscription-naming", s.getSubscriptionNaming)
	admin.PUT("/subscription-naming", s.putSubscriptionNaming)
	admin.GET("/subscription-defaults", s.getSubscriptionDefaults)
	admin.PUT("/subscription-defaults", s.putSubscriptionDefaults)
	admin.GET("/settings", s.getSettings)
	admin.GET("/release", s.releaseStatus)
	admin.POST("/release", s.requestRelease)
	admin.POST("/announcement/preview", s.previewNotice)
	admin.GET("/announcement", s.getAnnouncement)
	admin.PUT("/announcement", s.putAnnouncement)
	admin.GET("/clients", s.getClientCatalog)
	admin.PUT("/clients", s.putClientCatalog)
	admin.PUT("/settings", s.putSettings)
	admin.POST("/smtp/test", s.smtpTest)
	admin.GET("/audit", s.listAudit)
	admin.GET("/overview", s.overview)
	admin.GET("/devices/:id/traffic-history", s.trafficHistory)
	admin.GET("/devices/:id/host-history", s.hostHistory)
	s.deviceRoutes(ready, admin)
	s.resourceRoutes(admin)
	s.passkeyRoutes(ready, auth)
	r.GET("/share/:token", s.serveProfilePublication)
	r.GET("/sub/:token", s.serveSubscription)
	r.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			failure(c, 404, "not_found")
			return
		}
		if c.Request.Method != "GET" {
			failure(c, 405, "method_not_allowed")
			return
		}
		path := strings.TrimPrefix(c.Request.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		b, err := fs.ReadFile(s.Assets, path)
		if err != nil {
			if strings.Contains(path, ".") {
				failure(c, 404, "not_found")
				return
			}
			b, err = fs.ReadFile(s.Assets, "index.html")
		}
		if err != nil {
			failure(c, 503, "frontend_not_built")
			return
		}
		mime := "text/html; charset=utf-8"
		if strings.HasSuffix(path, ".js") {
			mime = "application/javascript"
		}
		if strings.HasSuffix(path, ".css") {
			mime = "text/css"
		}
		if strings.HasSuffix(path, ".svg") {
			mime = "image/svg+xml"
		}
		c.Data(200, mime, b)
	})
	return r
}
func (s *Server) setupStatus(c *gin.Context) {
	c.JSON(200, gin.H{"installed": s.Config.Installed, "ready": s.serviceReady()})
}
func (s *Server) setupGuard(c *gin.Context) {
	// 每次从磁盘读取。--init 更新文件后，当前进程立即接受新凭证并拒绝旧凭证。
	cred, err := readSetupCredential(s.Path)
	if err != nil {
		failure(c, 503, "setup_state_unavailable")
		return
	}
	if cred.Installed || s.Config.Installed {
		failure(c, 409, "already_installed")
		return
	}
	provided := digest(strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer "))
	if cred.Hash == "" || !cred.Expires.After(time.Now()) || subtle.ConstantTimeCompare([]byte(provided), []byte(cred.Hash)) != 1 {
		failure(c, 401, "initialization_token_required")
		return
	}
	c.Next()
}
func (s *Server) testDB(c *gin.Context) {
	var cfg DatabaseConfig
	if !bind(c, &cfg) {
		return
	}
	db, _, err := openDatabase(cfg)
	if err == nil {
		defer db.Close()
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()
		err = db.PingContext(ctx)
	}
	if err != nil {
		failure(c, 400, "database_connection_failed")
		return
	}
	c.JSON(200, gin.H{"ok": true})
}
func (s *Server) testRedis(c *gin.Context) {
	var cfg RedisConfig
	if !bind(c, &cfg) {
		return
	}
	r := redisClient(cfg)
	defer r.Close()
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	if r.Ping(ctx).Err() != nil {
		failure(c, 400, "redis_connection_failed")
		return
	}
	c.JSON(200, gin.H{"ok": true})
}
func (s *Server) install(c *gin.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Config.Installed {
		failure(c, 409, "already_installed")
		return
	}
	var req struct {
		Database DatabaseConfig `json:"database"`
		Redis    RedisConfig    `json:"redis"`
		Email    string         `json:"email"`
		Password string         `json:"password"`
	}
	if !bind(c, &req) {
		return
	}
	if !validEmail(req.Email) || !validAccountPassword(req.Password) {
		failure(c, 400, "strong_password_and_valid_email_required")
		return
	}
	cfg := s.Config
	cfg.Database = req.Database
	cfg.Redis = req.Redis
	s.Config = cfg
	if err := s.connect(cfg); err != nil {
		failure(c, 400, "dependency_connection_failed")
		return
	}
	if err := s.finishInstall(c.Request.Context(), req.Email, req.Password); err != nil {
		switch err.Error() {
		case "schema_permission_required":
			failure(c, 400, err.Error())
		case "existing_installation_restore_config", "already installed":
			failure(c, 409, "already_installed")
		case "migration_failed":
			failure(c, 500, err.Error())
		default:
			failure(c, 500, "install_failed")
		}
		return
	}
	created, queryErr := s.Store.User.Query().Where(user.EmailEQ(strings.ToLower(req.Email)), user.RoleEQ("admin")).Only(c.Request.Context())
	if queryErr == nil {
		if err := s.storeSession(c, created, false); err == nil {
			s.audit(c, "auth.login", fmt.Sprint(created.ID), "ok")
		}
	}
	s.audit(c, "system.install", "system", "ok")
	c.JSON(201, gin.H{"installed": true, "ready": s.serviceReady()})
}
func (s *Server) finishInstall(ctx context.Context, email, password string) error {
	unlock, err := lockInstall(filepath.Join(filepath.Dir(s.Path), "setup.lock"))
	if err != nil {
		return err
	}
	defer unlock()
	cred, err := readSetupCredential(s.Path)
	if err != nil {
		return err
	}
	if cred.Installed || s.Config.Installed {
		return errors.New("already installed")
	}
	conn, err := s.DB.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, "SELECT pg_advisory_lock(69442899)"); err != nil {
		return err
	}
	defer conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock(69442899)")
	if _, err = s.DB.ExecContext(ctx, "CREATE SCHEMA IF NOT EXISTS migate"); err != nil {
		return errors.New("schema_permission_required")
	}
	if err = s.Store.Schema.Create(ctx); err != nil {
		return errors.New("migration_failed")
	}
	count, err := s.Store.User.Query().Where(user.RoleEQ("admin")).Count(ctx)
	if err != nil {
		return err
	}
	if count > 0 {
		return errors.New("existing_installation_restore_config")
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if _, err = s.Store.User.Create().SetEmail(strings.ToLower(email)).SetPasswordHash(string(hash)).SetRole("admin").SetEmailVerified(true).SetSubscriptionToken(randomToken(32)).Save(ctx); err != nil {
		return err
	}
	cfg := s.Config
	cfg.Installed = true
	cfg.SetupHash = ""
	if err = saveConfig(s.Path, cfg); err != nil {
		s.Config = cfg
		return errors.New("config_save_failed_restore_required")
	}
	s.Config = cfg
	dir := filepath.Dir(s.Path)
	_ = os.Remove(filepath.Join(dir, "setup-token"))
	_ = os.Remove(filepath.Join(dir, "setup-token.new"))
	_ = os.Remove(filepath.Join(dir, "initialization.txt"))
	if s.runtimeCtx != nil {
		s.ensureWorkers()
	}
	return nil
}
func (s *Server) setting(ctx context.Context, key string, out any) bool {
	r, err := s.Store.Resource.Query().Where(resource.KindEQ("settings"), resource.KeyEQ(key)).Only(ctx)
	if err != nil {
		return false
	}
	b, _ := json.Marshal(r.Data)
	return json.Unmarshal(b, out) == nil
}
func (s *Server) saveSetting(ctx context.Context, key string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var m map[string]any
	if err = json.Unmarshal(b, &m); err != nil {
		return err
	}
	r, err := s.Store.Resource.Query().Where(resource.KindEQ("settings"), resource.KeyEQ(key)).Only(ctx)
	if ent.IsNotFound(err) {
		_, err = s.Store.Resource.Create().SetKind("settings").SetKey(key).SetData(m).Save(ctx)
		return err
	}
	if err != nil {
		return err
	}
	_, err = s.Store.Resource.UpdateOneID(r.ID).SetData(m).Save(ctx)
	return err
}
func validEmail(s string) bool {
	if len(s) > 254 || strings.ContainsAny(s, "\r\n ") {
		return false
	}
	a := strings.Split(s, "@")
	return len(a) == 2 && len(a[0]) > 0 && strings.Contains(a[1], ".")
}
func (s *Server) publicSettings(c *gin.Context) {
	a, policyOK := s.securityPolicy(c)
	if !policyOK {
		return
	}
	c.JSON(200, gin.H{"registration_enabled": a.Registration, "email_verification": a.EmailVerification, "totp_enabled": a.TOTP, "passkey_enabled": a.Passkey})
}
func (s *Server) overview(c *gin.Context) {
	ctx := c.Request.Context()
	users, err := s.Store.User.Query().Count(ctx)
	if err != nil {
		failure(c, 503, "observation_unavailable")
		return
	}
	devices, err := s.Store.Device.Query().All(ctx)
	if err != nil {
		failure(c, 503, "observation_unavailable")
		return
	}
	tasks, err := s.Store.Task.Query().Count(ctx)
	if err != nil {
		failure(c, 503, "observation_unavailable")
		return
	}
	approved, online, unknown := 0, 0, 0
	now := time.Now()
	for _, d := range devices {
		if d.Status != "approved" {
			continue
		}
		approved++
		contact, meter := deviceQuality(d, now)
		if contact {
			online++
		}
		if !meter {
			unknown++
		}
	}
	status := "unknown"
	if approved > 0 && unknown == 0 {
		status = "fresh"
	}
	c.JSON(200, gin.H{"users": users, "devices": len(devices), "tasks": tasks, "approved_devices": approved, "online_devices": online, "offline_devices": approved - online, "unknown_meters": unknown, "traffic_status": status, "observed_at": now.UTC(), "version": buildinfo.Version, "revision": buildinfo.Revision})
}

var _ = fmt.Sprint
