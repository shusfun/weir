package center

import (
	"bytes"
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/shusfun/weir/internal/store"
)

// 开机时 PostgreSQL 比中心晚起来：中心一直等（期间记「等待存储」），不立即退出；停止服务（ctx 结束）时不再等。
func TestNewWaitsForUnavailableStore(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close() // 端口上没有服务：连接被拒，就像数据库容器还没起来
	old := storeRetry
	storeRetry = 50 * time.Millisecond
	defer func() { storeRetry = old }()
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(prev)

	cfg := Config{Installed: true, EncryptionKey: randomToken(32), Store: store.Config{Driver: store.DriverPostgres,
		Postgres: store.PostgresConfig{Host: "127.0.0.1", Port: port, User: "weir", Password: "x", Database: "weir", SSLMode: "disable"},
		Redis:    store.RedisConfig{Address: "127.0.0.1:1"}}}
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	begin := time.Now()
	_, err := NewWait(ctx, cfg, t.TempDir()+"/c.json", nil)
	if !errors.Is(err, store.ErrPostgresUnavailable) || time.Since(begin) < 300*time.Millisecond {
		t.Fatalf("must keep waiting until ctx ends: err=%v after %v", err, time.Since(begin))
	}
	if n := strings.Count(buf.String(), "waiting for store"); n != 1 {
		t.Fatalf("waiting must be logged once per minute, got %d:\n%s", n, buf.String())
	}
	// 配置本身有问题（不是暂时连不上）：立即返回，不等
	cfg.Store.Postgres.Host = ""
	begin = time.Now()
	if _, err := NewWait(context.Background(), cfg, t.TempDir()+"/c.json", nil); err == nil || time.Since(begin) > time.Second {
		t.Fatalf("invalid config must fail fast: %v after %v", err, time.Since(begin))
	}
}

// 浏览器断开时反向代理用 http.ErrAbortHandler 中止响应：不记成 panic；其他 panic 照常记下并返回 500。
func TestRecoveryIgnoresAbortedRequests(t *testing.T) {
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(prev)
	r := gin.New()
	r.Use(recovery)
	r.GET("/abort", func(*gin.Context) { panic(http.ErrAbortHandler) })
	r.GET("/boom", func(*gin.Context) { panic("boom") })
	srv := httptest.NewServer(r)
	defer srv.Close()
	srv.Config.ErrorLog = log.New(&buf, "", 0)

	if _, err := http.Get(srv.URL + "/abort"); err == nil {
		t.Fatal("aborted handler must drop the connection")
	}
	if strings.Contains(buf.String(), "panic") {
		t.Fatalf("aborted request must not be logged as a panic:\n%s", buf.String())
	}
	res, err := http.Get(srv.URL + "/boom")
	if err != nil || res.StatusCode != 500 || !strings.Contains(buf.String(), "ERROR panic method=GET route=/boom err=boom") {
		t.Fatalf("real panic = %v %v\n%s", res, err, buf.String())
	}
}
