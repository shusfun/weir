package gate

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// fakeCenter 只实现设备认证：挑战 + 签名换令牌。accept 为 false 时拒绝认证。
func fakeCenter(t *testing.T, pub ed25519.PublicKey, accept *atomic.Bool) *httptest.Server {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/agent/challenge":
			_ = json.NewEncoder(w).Encode(map[string]string{"challenge": "c-1"})
		case "/api/v1/agent/auth":
			var req struct {
				Signature string `json:"signature"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			sig, _ := base64.StdEncoding.DecodeString(req.Signature)
			if !accept.Load() || !ed25519.Verify(pub, []byte("c-1"), sig) {
				w.WriteHeader(401)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"token": "t-1"})
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestSwitchCenterAuthenticatesBeforeSwitching(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	var accept atomic.Bool
	srv := fakeCenter(t, pub, &accept)
	// 新地址用域名形式（测试证书含 example.com），连接时拨到测试服务。
	addr := srv.Listener.Addr().String()
	_, port, _ := net.SplitHostPort(addr)
	newCenter := "https://example.com:" + port
	centerClientFor = func(cfg Config) (*centerClient, error) {
		c, err := newCenterClient(cfg)
		if err != nil {
			return nil, err
		}
		tr := srv.Client().Transport.(*http.Transport).Clone()
		tr.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		}
		c.http = &http.Client{Transport: tr, Timeout: 5 * time.Second}
		return c, nil
	}
	t.Cleanup(func() { centerClientFor = newCenterClient })

	dir, _ := os.MkdirTemp("", "gsw")
	t.Cleanup(func() { os.RemoveAll(dir) })
	p := Paths{Config: filepath.Join(dir, "agent.json"), Socket: filepath.Join(dir, "h.sock"), Data: dir}
	cfg := Config{Center: "https://old.example.net", GateID: 7, PrivateKey: base64.StdEncoding.EncodeToString(priv)}
	if err := saveConfig(p.Config, cfg, ""); err != nil {
		t.Fatal(err)
	}
	l, err := net.Listen("unix", p.Socket)
	if err != nil {
		t.Fatal(err)
	}
	hs := &http.Server{Handler: NewHelper(p).Handler()}
	go func() { _ = hs.Serve(l) }()
	t.Cleanup(func() { hs.Close() })

	cc, _ := newCenterClient(cfg)
	a := &Agent{P: p, cfg: cfg, center: cc, helper: newHelperClient(p.Socket)}

	// 新地址认证失败：保持旧地址，设备配置不变；10 分钟内不再尝试。
	if err := a.switchCenter(t.Context(), newCenter); err == nil {
		t.Fatal("switch must fail when the new address rejects authentication")
	}
	if a.center.origin() != cfg.Center {
		t.Fatalf("origin = %s", a.center.origin())
	}
	if saved, _ := LoadConfig(p.Config); saved.Center != cfg.Center {
		t.Fatalf("config center = %s", saved.Center)
	}
	accept.Store(true)
	if err := a.switchCenter(t.Context(), newCenter); err != nil || a.center.origin() != cfg.Center {
		t.Fatalf("retry before backoff: %v %s", err, a.center.origin())
	}

	// 退避过后、新地址认证成功：辅助程序改写设备配置，之后改用新地址；设备 ID 与私钥不变。
	a.switchTried = time.Now().Add(-switchRetry - time.Second)
	if err := a.switchCenter(t.Context(), newCenter); err != nil {
		t.Fatal(err)
	}
	if a.center.origin() != newCenter {
		t.Fatalf("origin after switch = %s", a.center.origin())
	}
	saved, _ := LoadConfig(p.Config)
	if saved.Center != newCenter || saved.GateID != 7 || saved.PrivateKey != cfg.PrivateKey {
		t.Fatalf("config after switch = %+v", saved)
	}

	// 只接受 HTTPS；回环连接（单机角色）不跟随。
	a.switchTried = time.Time{}
	if err := a.switchCenter(t.Context(), "http://example.com"); err == nil {
		t.Fatal("plain http center must be rejected")
	}
	if !loopbackCenter("http://127.0.0.1:19810") || loopbackCenter(newCenter) {
		t.Fatal("loopback detection")
	}
}
