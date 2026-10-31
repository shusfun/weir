package gate

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/shusfun/weir/packages/contracts"
)

// 自更新失败的原因随心跳上报给中心（VPS 卡片上显示），不只留在这台 VPS 的日志里；更新成功或同版本时清空。
func TestSelfUpdateFailureIsReportedInHeartbeat(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	var mu sync.Mutex
	var beats []contracts.Heartbeat
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
			if !ed25519.Verify(pub, []byte("c-1"), sig) {
				w.WriteHeader(401)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"token": "t-1"})
		case "/api/v1/agent/heartbeat":
			var hb contracts.Heartbeat
			_ = json.NewDecoder(r.Body).Decode(&hb)
			mu.Lock()
			beats = append(beats, hb)
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(contracts.HeartbeatReply{})
		default:
			// 中心没有这个平台的程序
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)

	// 套接字路径不能太长（macOS 上限约 104 字节）：不用 t.TempDir()
	dir, _ := os.MkdirTemp("", "gsu")
	t.Cleanup(func() { os.RemoveAll(dir) })
	p := Paths{Config: filepath.Join(dir, "agent.json"), Socket: filepath.Join(dir, "h.sock"), Data: dir}
	cfg := Config{Center: srv.URL, GateID: 7, PrivateKey: base64.StdEncoding.EncodeToString(priv)}
	l, err := net.Listen("unix", p.Socket)
	if err != nil {
		t.Fatal(err)
	}
	hs := &http.Server{Handler: NewHelper(p).Handler()}
	go func() { _ = hs.Serve(l) }()
	t.Cleanup(func() { hs.Close() })
	cc, err := newCenterClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	cc.http, cc.long = srv.Client(), srv.Client()
	a := &Agent{P: p, cfg: cfg, center: cc, helper: newHelperClient(p.Socket), pids: map[string]int{}}

	last := func() contracts.Heartbeat {
		t.Helper()
		if err := a.heartbeat(t.Context()); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		defer mu.Unlock()
		return beats[len(beats)-1]
	}
	if hb := last(); hb.SelfUpdate != nil {
		t.Fatalf("no update attempted yet: %+v", hb.SelfUpdate)
	}
	if a.selfUpdate(t.Context(), "9.9.9") {
		t.Fatal("self update must fail when the center has no release")
	}
	hb := last()
	if hb.SelfUpdate == nil || hb.SelfUpdate.Version != "9.9.9" || !strings.Contains(hb.SelfUpdate.Error, "下载") || hb.SelfUpdate.At.IsZero() {
		t.Fatalf("self update failure not reported: %+v", hb.SelfUpdate)
	}
	a.setUpdateErr(nil)
	if hb := last(); hb.SelfUpdate != nil {
		t.Fatalf("cleared failure still reported: %+v", hb.SelfUpdate)
	}
}
