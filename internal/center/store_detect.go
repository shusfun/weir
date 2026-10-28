package center

import (
	"bufio"
	"context"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/shusfun/weir/internal/store"
)

// 1Panel 应用商店装的 PostgreSQL / Redis：端口只映射到本机，凭据写在 <BASE_DIR>/1panel/apps/<应用>/<名称>/.env。
// 测试时替换这两个变量。
var (
	onePanelCtl     = "/usr/local/bin/1pctl"
	onePanelDefault = "/opt"
)

type onePanelApp struct {
	Port     int
	User     string
	Password string
}

// onePanelApps 读取指定应用的全部实例；没有 1Panel 或读不到时返回空。
func onePanelApps(app string) []onePanelApp {
	base := onePanelDefault
	if env := readDotenv(onePanelCtl); env["BASE_DIR"] != "" {
		base = env["BASE_DIR"]
	}
	files, _ := filepath.Glob(filepath.Join(base, "1panel", "apps", app, "*", ".env"))
	var out []onePanelApp
	for _, f := range files {
		env := readDotenv(f)
		port, err := strconv.Atoi(env["PANEL_APP_PORT_HTTP"])
		if err != nil || port < 1 || port > 65535 {
			continue
		}
		a := onePanelApp{Port: port, User: env["PANEL_DB_ROOT_USER"], Password: env["PANEL_DB_ROOT_PASSWORD"]}
		if app == "redis" {
			a.Password = env["PANEL_REDIS_ROOT_PASSWORD"]
		}
		out = append(out, a)
	}
	return out
}

// readDotenv 解析 KEY=VALUE 行，去掉成对的单引号或双引号；1pctl 本身是 shell 脚本，同样适用。
func readDotenv(path string) map[string]string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	out := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		k, v, ok := strings.Cut(line, "=")
		if !ok || strings.HasPrefix(line, "#") || strings.ContainsAny(k, " \t") {
			continue
		}
		if len(v) >= 2 && (v[0] == '\'' || v[0] == '"') && v[len(v)-1] == v[0] {
			v = v[1 : len(v)-1]
		}
		out[k] = v
	}
	return out
}

func loopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

type detectedPostgres struct {
	Source string `json:"source"` // 1panel | port
	Host   string `json:"host"`
	Port   int    `json:"port"`
	User   string `json:"user,omitempty"`
	// HasPassword 表示中心读到了密码；密码本身不返回给页面，安装时由中心自己补上。
	HasPassword bool                     `json:"has_password"`
	Connected   bool                     `json:"connected"`
	Version     string                   `json:"version,omitempty"`
	Databases   []store.PostgresDatabase `json:"databases,omitempty"`
	Recommended string                   `json:"recommended,omitempty"`
}

type detectedRedis struct {
	Source      string `json:"source"`
	Address     string `json:"address"`
	HasPassword bool   `json:"has_password"`
	// Status：ok 已连通；auth_required 需要密码但没读到；wrong_password 读到的密码不对。
	Status  string `json:"status"`
	Version string `json:"version,omitempty"`
}

type storeDetection struct {
	Postgres *detectedPostgres `json:"postgres"`
	Redis    *detectedRedis    `json:"redis"`
}

// detectStore 在中心本机寻找可用的 PostgreSQL 与 Redis：先认 1Panel 装的实例，再看默认端口有没有服务。只读。
func detectStore(ctx context.Context) storeDetection {
	return storeDetection{Postgres: detectPostgres(ctx), Redis: detectRedis(ctx)}
}

func detectPostgres(ctx context.Context) *detectedPostgres {
	for _, a := range onePanelApps("postgresql") {
		if a.User == "" {
			continue
		}
		d := &detectedPostgres{Source: "1panel", Host: "127.0.0.1", Port: a.Port, User: a.User, HasPassword: a.Password != ""}
		probe, cancel := context.WithTimeout(ctx, 8*time.Second)
		version, dbs, err := store.ProbePostgres(probe, store.PostgresConfig{Host: d.Host, Port: d.Port, User: a.User, Password: a.Password, Database: "postgres"})
		cancel()
		if err == nil {
			d.Connected, d.Version, d.Databases = true, version, dbs
			d.Recommended = recommendDatabase(dbs, a.User)
			return d
		}
		if portOpen(ctx, d.Host, d.Port) {
			return d
		}
	}
	if portOpen(ctx, "127.0.0.1", 5432) {
		return &detectedPostgres{Source: "port", Host: "127.0.0.1", Port: 5432}
	}
	return nil
}

// recommendDatabase 选一个还没有 Weir 账户的库：先认专门建的，再认 1Panel 账号同名库，最后是 postgres。
func recommendDatabase(dbs []store.PostgresDatabase, user string) string {
	for _, name := range []string{"migate", "weir", "gate", user, "postgres"} {
		if i := slices.IndexFunc(dbs, func(d store.PostgresDatabase) bool { return d.Name == name }); i >= 0 && !dbs[i].HasData && !dbs[i].Unknown {
			return name
		}
	}
	return ""
}

func detectRedis(ctx context.Context) *detectedRedis {
	for _, a := range onePanelApps("redis") {
		d := &detectedRedis{Source: "1panel", Address: net.JoinHostPort("127.0.0.1", strconv.Itoa(a.Port)), HasPassword: a.Password != ""}
		probe, cancel := context.WithTimeout(ctx, 5*time.Second)
		d.Status, d.Version = store.ProbeRedis(probe, store.RedisConfig{Address: d.Address, Password: a.Password})
		cancel()
		if d.Status != store.RedisUnreachable {
			return d
		}
	}
	probe, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	d := &detectedRedis{Source: "port", Address: "127.0.0.1:6379"}
	if d.Status, d.Version = store.ProbeRedis(probe, store.RedisConfig{Address: d.Address}); d.Status == store.RedisUnreachable {
		return nil
	}
	return d
}

func portOpen(ctx context.Context, host string, port int) bool {
	dial, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(dial, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// fillDetectedSecrets 给留空的密码补上 1Panel 里的密码：只在地址是本机、端口（和账号）与 1Panel 实例一致时补，
// 页面上填了密码就以页面为准。这样密码不经过浏览器。
func fillDetectedSecrets(cfg store.Config) store.Config {
	if cfg.Driver != store.DriverPostgres {
		return cfg
	}
	if p := &cfg.Postgres; p.Password == "" && loopbackHost(p.Host) {
		port := p.Port
		if port == 0 {
			port = 5432
		}
		for _, a := range onePanelApps("postgresql") {
			if a.Port == port && a.User == p.User {
				p.Password = a.Password
				break
			}
		}
	}
	if r := &cfg.Redis; r.Password == "" && r.Username == "" {
		if host, port, err := net.SplitHostPort(r.Address); err == nil && loopbackHost(host) {
			for _, a := range onePanelApps("redis") {
				if strconv.Itoa(a.Port) == port {
					r.Password = a.Password
					break
				}
			}
		}
	}
	return cfg
}

func (s *Server) detectStoreHandler(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	c.JSON(200, detectStore(ctx))
}
