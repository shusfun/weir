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

	"github.com/shusfun/weir/internal/buildinfo"
	"github.com/shusfun/weir/internal/geoip"
	"github.com/shusfun/weir/internal/ghrelease"
	"github.com/shusfun/weir/internal/kernels"
	"github.com/shusfun/weir/internal/lockfile"
	"github.com/shusfun/weir/internal/secrets"
	"github.com/shusfun/weir/internal/store"
	"github.com/shusfun/weir/internal/store/ent/user"
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
	// waiters 唤醒配置长轮询；actionWaiters 唤醒动作长轮询；actionDone 唤醒等待动作结果的管理请求。
	waiters       gateWaiters
	actionWaiters gateWaiters
	actionDone    gateWaiters
	// AllowPrivateSources 允许订阅源指向内网地址（只用于测试）。
	AllowPrivateSources bool
	// Mail 发送邮件；为空时使用 SMTP。
	Mail Mailer
	// dsh 是托管的 DeepSeek Harness（进程、子域名网关、MCP 工具）。
	dsh dshHost
	// GitHub 非空时代替真实的 GitHub（只用于测试）：API 地址、下载主机与 HTTP 客户端。
	GitHub *ghrelease.Client
	// jobs 是内核、dsh 下载等后台任务。
	jobs jobManager
	// ghCalls 是正在后台进行的 GitHub 查询（通知列表用，见 notices.go）。
	ghMu    sync.Mutex
	ghCalls map[string]chan error
	// driverCache 缓存合并后的内核驱动（每次编译 Gate 配置都会用到）。
	driverCache struct {
		sync.Mutex
		value *kernels.Drivers
		at    time.Time
	}
	// origin 缓存记录的中心公共地址（见 origin.go）。
	origin struct {
		sync.Mutex
		value  string
		loaded bool
	}
	// geo 是 IP 定位数据库（见 geo.go）；GeoDB 非空时代替（只用于测试）。
	geoOnce sync.Once
	geo     *geoip.DB
	GeoDB   geoLocator
	// live 是链路页的实时数据（只在内存，见 live.go）。
	live liveHub
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
	go s.maintenance(ctx)
	go s.superviseDSH(ctx)
}

// maintenance 每分钟执行按月重置用量等周期任务；未初始化时跳过。
func (s *Server) maintenance(ctx context.Context) {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	report := taskLog{}
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			s.state.RLock()
			ready := s.Config.Installed && s.Store != nil
			if ready {
				err := s.Store.Lock.Do(ctx, "maintenance", func(ctx context.Context) error {
					now := time.Now()
					// 各任务互不影响：一项失败不阻止其他项。
					report.note("reset-usage", s.ResetDueUsage(ctx, now))
					report.note("refresh-sources", s.RefreshSources(ctx, now))
					s.RefreshSourceExits(ctx)
					report.note("auto-backup", s.AutoBackup(ctx, now))
					report.note("check-alerts", s.CheckAlerts(ctx, now))
					report.note("observe-security", s.ObserveSecurity(ctx, now))
					report.note("reclaim-budgets", s.ReclaimStaleBudgets(ctx, now))
					report.note("renew-certs", s.RenewCerts(ctx, now))
					report.note("prune-samples", s.PruneSamples(ctx, now))
					s.CheckReachabilityAll(ctx, now)
					s.RefreshGeoIP(ctx, now)
					s.dropLegacyAIProvider(ctx)
					return nil
				})
				if ctx.Err() == nil {
					report.note("maintenance-lock", err)
				}
			}
			s.state.RUnlock()
		}
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
	// dsh 子域名的请求最先分走：不经过 HTTPS 拦截页、来源封禁与错误页，避免暴露这台机器上跑着 Weir。
	r.Use(s.dshGatewayMiddleware)
	// 节点证书域名的 HTTP-01 验证只能经明文 HTTP 到达，排在 HTTPS 拦截之前；只回应这一个路径。
	r.Use(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, acmeChallengePath) {
			s.state.RLock()
			defer s.state.RUnlock()
			s.serveACMEChallenge(c)
			return
		}
		c.Next()
	})
	// 排在 dsh 子域名之后：那边的响应不带 Weir 的头，也不记日志。
	r.Use(requestLog)
	r.Use(requireHTTPS)
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
		// frame-src 用站点通配，不写具体的 dsh 子域名；是否启用 dsh 都一样，页面看不出 dsh 的存在。
		csp := "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; frame-src 'self' " + frameSite(c.Request)
		c.Header("Content-Security-Policy", csp)
		if !sameOriginRequest(c.Request) {
			failure(c, 403, "origin_rejected")
			return
		}
		if s.blocked(c.Request) {
			failure(c, 403, "source_blocked")
			return
		}
		c.Next()
	})
	r.GET("/healthz", s.health)
	s.installRoutes(r)
	api := r.Group("/api/v1")
	api.GET("/setup/status", s.setupStatus)
	api.POST("/setup/verify", s.setupGuard, func(c *gin.Context) { c.JSON(200, gin.H{"valid": true}) })
	api.POST("/setup/detect-store", s.setupGuard, s.detectStoreHandler)
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
	// 回显来源 IP：链式代理实测时经出口访问它，得到实际出口 IP（只信任本机回环反代的转发头）。
	ready.GET("/ip", func(c *gin.Context) { c.Header("Cache-Control", "no-store"); c.JSON(200, gin.H{"ip": c.ClientIP()}) })
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
	admin := auth.Group("/admin")
	admin.Use(requireAdmin)
	s.gateRoutes(ready, admin)
	s.userRoutes(auth, admin)
	s.linkMapRoutes(auth, admin)
	s.subRoutes(r, ready)
	s.misubRoutes(r, auth, admin)
	s.addonRoutes(ready, auth, admin)
	s.pageRoutes(ready, admin)
	s.opsRoutes(admin)
	s.dshRoutes(admin)
	s.jobRoutes(admin)
	s.selfUpdateRoutes(admin)
	s.driverRoutes(admin)
	s.noticeRoutes(admin)
	s.objectRoutes(admin)
	s.contextRoutes(admin)
	s.sampleRoutes(admin)
	s.defenseRoutes(admin)
	s.backupRoutes(admin)
	s.originRoutes(admin)
	s.mailRoutes(ready, admin)
	s.notifyRoutes(admin)
	s.extRoutes(r, admin)

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
	if err := s.Store.Ping(ctx); err != nil {
		failInternal(c, 503, "dependency_unavailable", err)
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
		failInternal(c, 503, "setup_state_unavailable", err)
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
	return fillDetectedSecrets(cfg)
}

// checkInstallTarget 测试连通性，并拒绝已有 Weir 账户的存储（应恢复原配置，不能重新初始化）。
func checkInstallTarget(ctx context.Context, cfg store.Config) error {
	if err := store.Check(ctx, cfg); err != nil {
		return err
	}
	has, err := store.HasData(ctx, cfg)
	if err != nil {
		return err
	}
	if has {
		return store.ErrExistingData
	}
	return nil
}

var (
	errStoreOpen   = errors.New("store_connection_failed")
	errStoreSchema = errors.New("create schema failed")
)

// storeFailure 把存储错误转成页面能说明原因的错误码；原始错误只写服务日志。
func storeFailure(c *gin.Context, err error) {
	fmt.Fprintln(os.Stderr, "setup store:", err)
	switch {
	case errors.Is(err, store.ErrExistingData):
		failure(c, 409, "existing_installation_restore_config")
	case errors.Is(err, store.ErrPostgresUnavailable):
		failure(c, 400, "postgres_connection_failed")
	case errors.Is(err, store.ErrRedisUnavailable):
		failure(c, 400, "redis_connection_failed")
	case errors.Is(err, errStoreSchema):
		failure(c, 500, "store_schema_failed")
	default:
		failure(c, 400, "store_connection_failed")
	}
}

func (s *Server) testStore(c *gin.Context) {
	var cfg store.Config
	if !bind(c, &cfg) {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	if err := checkInstallTarget(ctx, normalizeStore(cfg)); err != nil {
		storeFailure(c, err)
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
		switch {
		case err.Error() == "already installed":
			failure(c, 409, "already_installed")
		case err.Error() == "existing_installation_restore_config":
			failure(c, 409, err.Error())
		case errors.Is(err, errStoreOpen):
			storeFailure(c, err)
		default:
			fmt.Fprintln(os.Stderr, "setup install:", err)
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
	// 先确认目标里没有 Weir 账户，再建表：旧版本留下的表结构对不上时，迁移会失败，而且不该在上面重新初始化。
	if err := checkInstallTarget(ctx, cfg.Store); err != nil {
		return nil, nil, fmt.Errorf("%w: %w", errStoreOpen, err)
	}
	st, err := store.Open(ctx, cfg.Store)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %w: %w", errStoreOpen, errStoreSchema, err)
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
		failInternal(c, 503, "frontend_not_built", err)
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
