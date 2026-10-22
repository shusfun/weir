package center

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"github.com/imzyb/MiGate/internal/buildinfo"
	"github.com/imzyb/MiGate/internal/lockfile"
	"github.com/imzyb/MiGate/internal/secrets"
	"github.com/imzyb/MiGate/internal/store"
	"github.com/imzyb/MiGate/internal/store/ent/user"
)

// Server 持有中心的配置与存储。初始化完成前 Store 为空，只开放初始化接口。
type Server struct {
	Config Config
	Path   string
	Store  *store.Store
	Crypto *secrets.AESEncryptor
	Assets fs.FS

	// state 保护 Config / Store 的切换：普通请求持读锁，安装完成的切换持写锁。
	state     sync.RWMutex
	installMu sync.Mutex
}

func New(c Config, path string, assets fs.FS) (*Server, error) {
	enc, err := secrets.NewAESEncryptor(c.EncryptionKey)
	if err != nil {
		return nil, err
	}
	s := &Server{Config: c, Path: path, Crypto: enc, Assets: assets}
	if c.Installed {
		if s.Store, err = openInstalled(context.Background(), c); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// openInstalled 打开已初始化的存储，并确认管理员存在，避免配置与数据不匹配时静默启动。
func openInstalled(ctx context.Context, c Config) (*store.Store, error) {
	st, err := store.Open(ctx, c.Store)
	if err != nil {
		return nil, err
	}
	ok, err := st.Ent.User.Query().Where(user.RoleEQ(user.RoleAdmin)).Exist(ctx)
	if err != nil || !ok {
		st.Close()
		return nil, errors.New("installed store has no administrator; restore the configuration before starting")
	}
	return st, nil
}

func (s *Server) Close() {
	s.state.Lock()
	defer s.state.Unlock()
	if s.Store != nil {
		s.Store.Close()
		s.Store = nil
	}
}

// StartBackground 未初始化时监视磁盘配置：终端完成初始化后，运行中的服务自动切换为已安装。
func (s *Server) StartBackground(ctx context.Context) {
	if !s.Config.Installed {
		go s.watchInstallation(ctx)
	}
}

func (s *Server) watchInstallation(ctx context.Context) {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if cred, err := readSetupCredential(s.Path); err != nil || !cred.Installed {
				continue
			}
			if s.promoteInstalled(ctx) == nil {
				return
			}
		}
	}
}

func (s *Server) promoteInstalled(ctx context.Context) error {
	s.state.Lock()
	defer s.state.Unlock()
	if s.Config.Installed {
		return nil
	}
	cfg, err := LoadConfig(s.Path)
	if err != nil || !cfg.Installed {
		return errors.New("not installed")
	}
	st, err := openInstalled(ctx, cfg)
	if err != nil {
		return err
	}
	s.Config, s.Store = cfg, st
	return nil
}

func (s *Server) audit(c *gin.Context, action, target, result string) {
	if s.Store == nil {
		return
	}
	_, _ = s.Store.Ent.Audit.Create().SetActor(c.GetInt("user_id")).SetAction(action).SetTarget(target).SetResult(result).SetIP(c.ClientIP()).Save(c.Request.Context())
}

func (s *Server) Router() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	_ = r.SetTrustedProxies([]string{"127.0.0.1", "::1"})
	r.Use(func(c *gin.Context) {
		if c.Request.URL.Path == "/api/v1/setup/install" {
			s.state.Lock()
			defer s.state.Unlock()
		} else {
			s.state.RLock()
			defer s.state.RUnlock()
		}
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
	r.GET("/healthz", s.health)
	api := r.Group("/api/v1")
	api.GET("/setup/status", s.setupStatus)
	api.POST("/setup/verify", s.setupGuard, func(c *gin.Context) { c.JSON(200, gin.H{"valid": true}) })
	api.POST("/setup/test-store", s.setupGuard, s.testStore)
	api.POST("/setup/install", s.setupGuard, s.install)

	ready := api.Group("")
	ready.Use(func(c *gin.Context) {
		if !s.Config.Installed || s.Store == nil {
			failure(c, 503, "setup_required")
			return
		}
		c.Next()
	})
	ready.GET("/public", s.publicSettings)
	ready.POST("/auth/login", s.login)
	ready.POST("/auth/mfa", s.completeMFA)
	auth := ready.Group("")
	auth.Use(s.authenticate)
	auth.GET("/me", s.me)
	auth.POST("/auth/logout", s.logout)
	auth.POST("/security/totp/setup", s.setupTOTP)
	auth.POST("/security/totp/confirm", s.confirmTOTP)
	auth.POST("/security/totp/disable", s.disableTOTP)
	auth.POST("/security/password", s.changePassword)
	s.sessionRoutes(auth)
	s.passkeyRoutes(ready, auth)

	r.NoRoute(s.spa)
	return r
}

func (s *Server) health(c *gin.Context) {
	if !s.Config.Installed || s.Store == nil {
		c.JSON(200, gin.H{"status": "setup_required"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	if s.Store.Ping(ctx) != nil {
		failure(c, 503, "dependency_unavailable")
		return
	}
	c.JSON(200, gin.H{"status": "ok", "version": buildinfo.Version})
}

func (s *Server) setupStatus(c *gin.Context) {
	ready := false
	if s.Store != nil {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		ready = s.Store.Ping(ctx) == nil
	}
	c.JSON(200, gin.H{"installed": s.Config.Installed, "ready": ready, "default_sqlite_path": DefaultSQLitePath()})
}

// setupGuard 每次从磁盘读取凭证：轮换后运行中的服务立即接受新凭证、拒绝旧凭证。
func (s *Server) setupGuard(c *gin.Context) {
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

func normalizeStore(cfg store.Config) store.Config {
	if cfg.Driver == store.DriverSQLite && cfg.SQLitePath == "" {
		cfg.SQLitePath = DefaultSQLitePath()
	}
	return cfg
}

func (s *Server) testStore(c *gin.Context) {
	var cfg store.Config
	if !bind(c, &cfg) {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	if err := store.Check(ctx, normalizeStore(cfg)); err != nil {
		failure(c, 400, "store_connection_failed")
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (s *Server) install(c *gin.Context) {
	var req struct {
		Store    store.Config `json:"store"`
		Email    string       `json:"email"`
		Password string       `json:"password"`
	}
	if !bind(c, &req) {
		return
	}
	if !validEmail(req.Email) || !validAccountPassword(req.Password) {
		failure(c, 400, "strong_password_and_valid_email_required")
		return
	}
	cfg := s.Config
	cfg.Store = normalizeStore(req.Store)
	st, admin, err := s.finishInstall(c.Request.Context(), cfg, req.Email, req.Password)
	if err != nil {
		switch err.Error() {
		case "already installed", "existing_installation_restore_config":
			failure(c, 409, "already_installed")
		case "store_connection_failed":
			failure(c, 400, err.Error())
		default:
			failure(c, 500, "install_failed")
		}
		return
	}
	cfg.Installed, cfg.SetupHash, cfg.SetupExpires = true, "", time.Time{}
	s.Config, s.Store = cfg, st
	if err = s.storeSession(c, admin, false); err == nil {
		s.audit(c, "auth.login", fmt.Sprint(admin.ID), "ok")
	}
	s.audit(c, "system.install", cfg.Store.Driver, "ok")
	c.JSON(201, gin.H{"installed": true, "ready": true})
}

// finishInstall 供 Web 安装使用；与终端安装共用 completeInstall。
func (s *Server) finishInstall(ctx context.Context, cfg Config, email, password string) (*store.Store, *entUser, error) {
	s.installMu.Lock()
	defer s.installMu.Unlock()
	if s.Config.Installed {
		return nil, nil, errors.New("already installed")
	}
	return completeInstall(ctx, s.Path, cfg, email, password)
}

// completeInstall 建表、创建首个管理员并写回配置。安装锁文件保证 Web 与终端同一时间只有一次安装。
func completeInstall(ctx context.Context, path string, cfg Config, email, password string) (*store.Store, *entUser, error) {
	unlock, err := lockfile.TryAcquire(filepath.Join(filepath.Dir(path), "setup.lock"))
	if err != nil {
		return nil, nil, errors.New("install_in_progress")
	}
	defer unlock()
	if cred, err := readSetupCredential(path); err != nil || cred.Installed {
		return nil, nil, errors.New("already installed")
	}
	st, err := store.Open(ctx, cfg.Store)
	if err != nil {
		return nil, nil, errors.New("store_connection_failed")
	}
	admin, err := createFirstAdmin(ctx, st, email, password)
	if err != nil {
		st.Close()
		return nil, nil, err
	}
	cfg.Installed, cfg.SetupHash, cfg.SetupExpires = true, "", time.Time{}
	if err = saveConfig(path, cfg); err != nil {
		st.Close()
		return nil, nil, errors.New("config_save_failed")
	}
	_ = os.Remove(setupTokenPath(path))
	_ = os.Remove(setupTokenPath(path) + ".new")
	return st, admin, nil
}

// createFirstAdmin 在同名锁下检查「尚无管理员」再创建，已有数据时拒绝覆盖。
func createFirstAdmin(ctx context.Context, st *store.Store, email, password string) (*entUser, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	var admin *entUser
	err = st.Lock.Do(ctx, "install", func(ctx context.Context) error {
		exists, err := st.Ent.User.Query().Where(user.RoleEQ(user.RoleAdmin)).Exist(ctx)
		if err != nil {
			return err
		}
		if exists {
			return errors.New("existing_installation_restore_config")
		}
		admin, err = st.Ent.User.Create().SetEmail(strings.ToLower(email)).SetName("管理员").SetPasswordHash(string(hash)).SetRole(user.RoleAdmin).SetAllNodes(true).Save(ctx)
		return err
	})
	return admin, err
}

func (s *Server) spa(c *gin.Context) {
	if strings.HasPrefix(c.Request.URL.Path, "/api/") {
		failure(c, 404, "not_found")
		return
	}
	if c.Request.Method != "GET" && c.Request.Method != "HEAD" {
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
		path = "index.html"
		b, err = fs.ReadFile(s.Assets, path)
	}
	if err != nil {
		failure(c, 503, "frontend_not_built")
		return
	}
	mime := "text/html; charset=utf-8"
	switch {
	case strings.HasSuffix(path, ".js"):
		mime = "application/javascript"
	case strings.HasSuffix(path, ".css"):
		mime = "text/css"
	case strings.HasSuffix(path, ".svg"):
		mime = "image/svg+xml"
	case strings.HasSuffix(path, ".png"):
		mime = "image/png"
	}
	c.Data(200, mime, b)
}
