package center

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

type testClient struct {
	t      *testing.T
	router *gin.Engine
	cookie *http.Cookie
}

func (tc *testClient) do(method, path string, body any, header map[string]string) *httptest.ResponseRecorder {
	tc.t.Helper()
	var r *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	} else {
		r = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, r)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range header {
		req.Header.Set(k, v)
	}
	if tc.cookie != nil {
		req.AddCookie(tc.cookie)
	}
	w := httptest.NewRecorder()
	tc.router.ServeHTTP(w, req)
	for _, c := range w.Result().Cookies() {
		if c.Name == sessionCookie {
			if c.MaxAge < 0 {
				tc.cookie = nil
			} else {
				tc.cookie = c
			}
		}
	}
	return w
}

func decode(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %q: %v", w.Body.String(), err)
	}
	return out
}

// newInstalledServer 走一遍真实的初始化流程：生成凭证 → Web 安装（SQLite）→ 返回已登录的客户端。
func newInstalledServer(t *testing.T) (*Server, *testClient) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "center.json")
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	token, err := Initialize(path, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if cfg, err = LoadConfig(path); err != nil {
		t.Fatal(err)
	}
	s, err := New(cfg, path, fstest.MapFS{"index.html": {Data: []byte("<!doctype html>console")}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	tc := &testClient{t: t, router: s.Router()}
	if w := tc.do("POST", "/api/v1/auth/login", map[string]string{"email": "a@b.c", "password": "x"}, nil); w.Code != 503 {
		t.Fatalf("login before install = %d", w.Code)
	}
	install := map[string]any{"store": testStore(t, dir), "email": "Admin@Example.test", "password": "Passw0rd"}
	if w := tc.do("POST", "/api/v1/setup/install", install, map[string]string{"Authorization": "Bearer wrong"}); w.Code != 401 {
		t.Fatalf("install with wrong token = %d", w.Code)
	}
	if w := tc.do("POST", "/api/v1/setup/install", install, map[string]string{"Authorization": "Bearer " + token}); w.Code != 201 {
		t.Fatalf("install = %d %s", w.Code, w.Body.String())
	}
	if tc.cookie == nil {
		t.Fatal("install must log the administrator in")
	}
	if w := tc.do("POST", "/api/v1/setup/install", install, map[string]string{"Authorization": "Bearer " + token}); w.Code != 409 {
		t.Fatalf("second install = %d", w.Code)
	}
	reloaded, err := LoadConfig(path)
	if err != nil || !reloaded.Installed || reloaded.SetupHash != "" {
		t.Fatalf("config after install = %+v %v", reloaded, err)
	}
	return s, tc
}

func TestInstallLoginSessionsLogout(t *testing.T) {
	_, admin := newInstalledServer(t)
	me := decode(t, admin.do("GET", "/api/v1/me", nil, nil))
	if me["role"] != "admin" || me["email"] != "admin@example.test" {
		t.Fatalf("me = %v", me)
	}
	if w := admin.do("GET", "/healthz", nil, nil); w.Code != 200 || decode(t, w)["status"] != "ok" {
		t.Fatalf("healthz = %d %s", w.Code, w.Body.String())
	}
	other := &testClient{t: t, router: admin.router}
	if w := other.do("POST", "/api/v1/auth/login", map[string]string{"email": "admin@example.test", "password": "wrong"}, nil); w.Code != 401 {
		t.Fatalf("wrong password = %d", w.Code)
	}
	if w := other.do("POST", "/api/v1/auth/login", map[string]string{"email": "ADMIN@example.test", "password": "Passw0rd"}, nil); w.Code != 200 {
		t.Fatalf("login = %d %s", w.Code, w.Body.String())
	}
	list := decode(t, admin.do("GET", "/api/v1/sessions", nil, nil))
	items, _ := list["items"].([]any)
	if list["all_total"] != float64(2) || len(items) != 2 || items[0].(map[string]any)["current"] != true {
		t.Fatalf("sessions = %v", list)
	}
	if w := admin.do("DELETE", "/api/v1/sessions/not-a-session", nil, nil); w.Code != 400 {
		t.Fatalf("invalid session id = %d", w.Code)
	}
	if r := decode(t, admin.do("POST", "/api/v1/sessions/revoke-others", map[string]bool{"confirm": true}, nil)); r["revoked"] != float64(1) {
		t.Fatalf("revoke-others = %v", r)
	}
	if w := other.do("GET", "/api/v1/me", nil, nil); w.Code != 401 {
		t.Fatalf("revoked session still valid: %d", w.Code)
	}
	if w := admin.do("POST", "/api/v1/auth/logout", nil, nil); w.Code != 200 {
		t.Fatalf("logout = %d", w.Code)
	}
	if w := admin.do("GET", "/api/v1/me", nil, nil); w.Code != 401 {
		t.Fatalf("me after logout = %d", w.Code)
	}
}

func TestCrossSiteWriteRejected(t *testing.T) {
	_, admin := newInstalledServer(t)
	w := admin.do("POST", "/api/v1/auth/logout", nil, map[string]string{"Sec-Fetch-Site": "cross-site"})
	if w.Code != 403 {
		t.Fatalf("cross-site POST = %d", w.Code)
	}
}

func TestTOTPEnrollAndSecondFactorLogin(t *testing.T) {
	_, admin := newInstalledServer(t)
	setup := decode(t, admin.do("POST", "/api/v1/security/totp/setup", map[string]string{"password": "Passw0rd"}, nil))
	secret, _ := setup["secret"].(string)
	if secret == "" {
		t.Fatalf("setup = %v", setup)
	}
	code, _ := totp.GenerateCodeCustom(secret, time.Now(), totp.ValidateOpts{Period: 30, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
	confirm := decode(t, admin.do("POST", "/api/v1/security/totp/confirm", map[string]string{"code": code}, nil))
	codes, _ := confirm["recovery_codes"].([]any)
	if len(codes) != 8 {
		t.Fatalf("confirm = %v", confirm)
	}
	// 启用后旧会话全部失效，必须重新登录并完成二次验证。
	if w := admin.do("GET", "/api/v1/me", nil, nil); w.Code != 401 {
		t.Fatalf("session after enroll = %d", w.Code)
	}
	fresh := &testClient{t: t, router: admin.router}
	login := decode(t, fresh.do("POST", "/api/v1/auth/login", map[string]string{"email": "admin@example.test", "password": "Passw0rd"}, nil))
	challenge, _ := login["challenge"].(string)
	if login["mfa_required"] != true || challenge == "" || fresh.cookie != nil {
		t.Fatalf("login with totp = %v", login)
	}
	if w := fresh.do("POST", "/api/v1/auth/mfa", map[string]string{"challenge": challenge, "code": "000000"}, nil); w.Code != 401 {
		t.Fatalf("wrong code = %d", w.Code)
	}
	// 同一时间片的验证码已在确认时使用，只能用下一时间片的验证码或恢复码。
	recovery := codes[0].(string)
	if w := fresh.do("POST", "/api/v1/auth/mfa", map[string]string{"challenge": challenge, "code": recovery}, nil); w.Code != 200 {
		t.Fatalf("recovery code = %d %s", w.Code, w.Body.String())
	}
	if decode(t, fresh.do("GET", "/api/v1/me", nil, nil))["totp_enrolled"] != true {
		t.Fatal("totp not enrolled")
	}
	again := &testClient{t: t, router: admin.router}
	challenge = decode(t, again.do("POST", "/api/v1/auth/login", map[string]string{"email": "admin@example.test", "password": "Passw0rd"}, nil))["challenge"].(string)
	if w := again.do("POST", "/api/v1/auth/mfa", map[string]string{"challenge": challenge, "code": recovery}, nil); w.Code != 401 {
		t.Fatalf("recovery code reused = %d", w.Code)
	}
}

func TestSPAFallbackAndAPINotFound(t *testing.T) {
	_, admin := newInstalledServer(t)
	if w := admin.do("GET", "/users/12", nil, nil); w.Code != 200 || !strings.Contains(w.Body.String(), "console") {
		t.Fatalf("spa = %d %q", w.Code, w.Body.String())
	}
	if w := admin.do("GET", "/api/v1/nope", nil, nil); w.Code != 404 {
		t.Fatalf("api 404 = %d", w.Code)
	}
}
