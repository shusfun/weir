package center

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeDSH 模拟 dsh Web：GET /?token=<启动令牌> 换会话 Cookie，其余请求都要带这个 Cookie。
type fakeDSH struct {
	mu   sync.Mutex
	seen []*http.Request
	srv  *httptest.Server
}

const fakeLaunchToken = "launch-token-test"

func newFakeDSH(t *testing.T) *fakeDSH {
	f := &fakeDSH{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.seen = append(f.seen, r.Clone(r.Context()))
		f.mu.Unlock()
		if r.URL.Path == "/" && r.URL.Query().Get("token") != "" {
			if r.URL.Query().Get("token") != fakeLaunchToken {
				w.WriteHeader(401)
				return
			}
			w.Header().Set("Set-Cookie", "dsh-auth-x=v1; Path=/; HttpOnly; SameSite=Strict")
			w.Header().Set("X-Frame-Options", "DENY")
			w.WriteHeader(303)
			return
		}
		if c, err := r.Cookie("dsh-auth-x"); err != nil || c.Value != "v1" {
			w.WriteHeader(401)
			return
		}
		w.Header().Set("X-Frame-Options", "DENY")
		_, _ = w.Write([]byte("dsh-ok"))
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeDSH) last() *http.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.seen[len(f.seen)-1]
}

type browser struct {
	t       *testing.T
	s       *Server
	r       http.Handler
	cookies map[string]string
}

// get 模拟浏览器请求 dsh 子域名；headers 为 Fetch Metadata 等。
func (b *browser) get(target string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", target, nil)
	req.RemoteAddr = "198.51.100.7:4000"
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	for k, v := range b.cookies {
		req.AddCookie(&http.Cookie{Name: k, Value: v})
	}
	rec := closeNotifyRecorder{httptest.NewRecorder()}
	b.r.ServeHTTP(rec, req)
	w := rec.ResponseRecorder
	for _, c := range w.Result().Cookies() {
		b.cookies[c.Name] = c.Value
	}
	return w
}

// closeNotifyRecorder 补上真实服务里 ResponseWriter 具备的 CloseNotify（gin 的包装会转调它）。
type closeNotifyRecorder struct{ *httptest.ResponseRecorder }

func (closeNotifyRecorder) CloseNotify() <-chan bool { return make(chan bool) }

var iframeNav = map[string]string{"Sec-Fetch-Site": "same-site", "Sec-Fetch-Mode": "navigate", "Sec-Fetch-Dest": "iframe"}

func TestDSHGatewayOnlyOpensFromConsoleDrawer(t *testing.T) {
	s, admin := newInstalledServer(t)
	f := newFakeDSH(t)
	s.dsh.endpoint = func() (string, string, bool) { return strings.TrimPrefix(f.srv.URL, "http://"), fakeLaunchToken, true }

	// 子域名必须与控制台同站、不同源；修改要验证身份。
	mustJSON(t, admin, "PUT", "/api/v1/admin/dsh/settings", map[string]any{"enabled": true, "domain": "dsh.example.com", "password": "wrong"}, 401)
	mustJSON(t, admin, "PUT", "/api/v1/admin/dsh/settings", map[string]any{"enabled": true, "domain": "dsh.other.net", "password": "Passw0rd"}, 400)
	mustJSON(t, admin, "PUT", "/api/v1/admin/dsh/settings", map[string]any{"enabled": true, "domain": "example.com", "password": "Passw0rd"}, 400)
	mustJSON(t, admin, "PUT", "/api/v1/admin/dsh/settings", map[string]any{"enabled": true, "domain": "DSH.example.com", "password": "Passw0rd"}, 200)
	s.reconcileDSH(t.Context())
	if s.dshDomain() != "dsh.example.com" {
		t.Fatalf("domain = %q", s.dshDomain())
	}
	// 控制台的 CSP 只允许嵌入这个子域名。
	if w := admin.do("GET", "/gates", nil, nil); !strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-src 'self' http://dsh.example.com") {
		t.Fatalf("console csp = %s", w.Header().Get("Content-Security-Policy"))
	}

	b := &browser{t: t, s: s, r: s.Router(), cookies: map[string]string{}}
	// 没有任何凭据：直接访问、伪造导航头、访问 Weir 接口路径，都是拒绝访问，且不会碰到 dsh。
	for _, h := range []map[string]string{nil, iframeNav, {"Sec-Fetch-Site": "none", "Sec-Fetch-Mode": "navigate", "Sec-Fetch-Dest": "document"}} {
		for _, p := range []string{"/", "/api/v1/me", "/api/remote.mux"} {
			if w := b.get("http://dsh.example.com"+p, h); w.Code != 403 || !strings.Contains(w.Body.String(), "拒绝访问") {
				t.Fatalf("GET %s %v = %d %s", p, h, w.Code, w.Body.String())
			}
		}
	}
	if len(f.seen) != 0 {
		t.Fatal("dsh must not see unauthenticated requests")
	}

	ticket := mustJSON(t, admin, "POST", "/api/v1/admin/dsh/ticket", nil, 200)["url"].(string)
	u, _ := url.Parse(ticket)
	if u.Host != "dsh.example.com" || u.Path != dshOpenPath {
		t.Fatalf("ticket url = %s", ticket)
	}
	open := "http://dsh.example.com" + u.RequestURI()
	// 票据只能在 iframe 里使用。
	if w := b.get(open, map[string]string{"Sec-Fetch-Site": "same-site", "Sec-Fetch-Mode": "navigate", "Sec-Fetch-Dest": "document"}); w.Code != 403 {
		t.Fatalf("top-level open = %d", w.Code)
	}
	w := b.get(open, iframeNav)
	if w.Code != 303 || b.cookies["dsh-auth-x"] != "v1" || len(b.cookies[dshGateCookie]) != 64 {
		t.Fatalf("open = %d %v", w.Code, b.cookies)
	}
	if strings.Contains(w.Header().Get("Location"), "token") || f.last().Host != "dsh.example.com" {
		t.Fatalf("launch token must stay server side; host = %s", f.last().Host)
	}
	// 票据只能用一次（换一个新浏览器也不行）。
	other := &browser{t: t, s: s, r: b.r, cookies: map[string]string{}}
	if w := other.get(open, iframeNav); w.Code != 403 {
		t.Fatalf("ticket reuse = %d", w.Code)
	}

	// 有了会话：iframe 里可以用，只转发 dsh 自己的 Cookie，允许控制台嵌入。
	b.cookies[sessionCookie] = "console-session-must-not-leak"
	w = b.get("http://dsh.example.com/", iframeNav)
	if w.Code != 200 || w.Body.String() != "dsh-ok" {
		t.Fatalf("iframe = %d %s", w.Code, w.Body.String())
	}
	fwd := f.last()
	if c := fwd.Header.Get("Cookie"); c != "dsh-auth-x=v1" || fwd.Host != "dsh.example.com" {
		t.Fatalf("forwarded cookie = %q host = %s", c, fwd.Host)
	}
	if w.Header().Get("X-Frame-Options") != "" || w.Header().Get("Content-Security-Policy") != "frame-ancestors http://example.com" {
		t.Fatalf("frame headers = %v", w.Header())
	}
	if w := b.get("http://dsh.example.com/api/session/list", map[string]string{"Sec-Fetch-Site": "same-origin", "Sec-Fetch-Mode": "cors", "Sec-Fetch-Dest": "empty"}); w.Code != 200 {
		t.Fatalf("api = %d", w.Code)
	}
	// WebSocket 握手（Chromium 不带 Fetch Metadata）按 Origin 判断：只接受 dsh 子域名自己发起的。
	ws := map[string]string{"Connection": "Upgrade", "Upgrade": "websocket", "Origin": "http://dsh.example.com"}
	if w := b.get("http://dsh.example.com/api/remote.mux", ws); w.Code == 403 {
		t.Fatalf("same-origin websocket = %d", w.Code)
	}
	ws["Origin"] = "http://example.com"
	if w := b.get("http://dsh.example.com/api/remote.mux", ws); w.Code != 403 {
		t.Fatalf("websocket from the console origin = %d", w.Code)
	}
	// 带着会话也不能在顶层窗口打开，也不能从别的网站发起。
	if w := b.get("http://dsh.example.com/", map[string]string{"Sec-Fetch-Site": "none", "Sec-Fetch-Mode": "navigate", "Sec-Fetch-Dest": "document"}); w.Code != 403 {
		t.Fatalf("top level = %d", w.Code)
	}
	if w := b.get("http://dsh.example.com/api/session/list", map[string]string{"Sec-Fetch-Site": "cross-site", "Sec-Fetch-Mode": "cors"}); w.Code != 403 {
		t.Fatalf("cross site = %d", w.Code)
	}
	// 管理员退出登录后立即失效。
	mustJSON(t, admin, "POST", "/api/v1/auth/logout", nil, 200)
	if w := b.get("http://dsh.example.com/", iframeNav); w.Code != 403 {
		t.Fatalf("after logout = %d", w.Code)
	}
}

func TestDefenseLayaThenDraftThenConfirmedBlock(t *testing.T) {
	s, admin := newInstalledServer(t)
	var mu sync.Mutex
	var layaBodies []string
	laya := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		layaBodies = append(layaBodies, r.URL.Path+" "+r.Header.Get("Authorization")+" "+string(b))
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"answers": map[string]any{"category": map[string]any{"choice": "suspicious", "confidence": 0.9}}})
	}))
	defer laya.Close()

	mustJSON(t, admin, "PUT", "/api/v1/admin/defense/laya", map[string]any{"enabled": true, "url": "http://example.com", "model": "observer", "key": "laya-key", "password": "Passw0rd"}, 400)
	view := mustJSON(t, admin, "PUT", "/api/v1/admin/defense/laya", map[string]any{"enabled": true, "url": laya.URL, "model": "observer", "key": "laya-key", "password": "Passw0rd"}, 200)
	if strings.Contains(mustString(view), "laya-key") || view["key_set"] != true {
		t.Fatalf("laya view = %v", view)
	}
	if v := mustJSON(t, admin, "POST", "/api/v1/admin/defense/laya/test", nil, 200); v["verdict"].(map[string]any)["category"] != "suspicious" {
		t.Fatalf("test = %v", v)
	}

	// 同一来源连续登录失败：规则产生安全事件。
	attacker := &testClient{t: t, router: s.Router()}
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(`{"email":"admin@example.test","password":"nope"}`))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "203.0.113.9:5000"
		w := httptest.NewRecorder()
		attacker.router.ServeHTTP(w, req)
	}
	if err := s.ObserveSecurity(t.Context(), time.Now()); err != nil {
		t.Fatal(err)
	}
	events := mustJSON(t, admin, "GET", "/api/v1/admin/defense/events", nil, 200)["events"].([]any)
	if len(events) != 1 {
		t.Fatalf("events = %v", events)
	}
	ev := events[0].(map[string]any)
	if ev["kind"] != "repeated-login-failure" || ev["laya"].(map[string]any)["category"] != "suspicious" || ev["failed_logins_10m"].(float64) < 5 {
		t.Fatalf("event = %v", ev)
	}
	mu.Lock()
	all := strings.Join(layaBodies, "\n")
	mu.Unlock()
	if strings.Contains(all, "203.0.113.9") || !strings.Contains(all, "/v1/systemone") || !strings.Contains(all, "Bearer laya-key") {
		t.Fatalf("laya requests = %s", all)
	}

	// dsh 只看得到来源代号，提交防御草稿；确认前不会封禁。
	m, _ := s.ensureMCP()
	h := s.mcpHandler(m)
	eid := int(ev["id"].(float64))
	detail, _ := toolText(t, h, m.analystToken, "get_security_event", map[string]any{"event_id": eid})
	if strings.Contains(detail, "203.0.113.9") || !strings.Contains(detail, "来源 A") {
		t.Fatalf("event for model = %s", detail)
	}
	if _, isErr := toolText(t, h, m.analystToken, "submit_defense_draft", map[string]any{"event_id": eid, "action": "block_source", "minutes": 1, "explain": "x"}); !isErr {
		t.Fatal("too short block must be rejected")
	}
	if text, isErr := toolText(t, h, m.analystToken, "submit_defense_draft", map[string]any{"event_id": eid, "action": "block_source", "minutes": 60, "explain": "短时间多次失败登录，建议封禁一小时"}); isErr {
		t.Fatal(text)
	}
	probe := func() int {
		req := httptest.NewRequest("GET", "/api/v1/public", nil)
		req.RemoteAddr = "203.0.113.9:5001"
		w := httptest.NewRecorder()
		attacker.router.ServeHTTP(w, req)
		return w.Code
	}
	if code := probe(); code != 200 {
		t.Fatalf("blocked before confirmation: %d", code)
	}
	pending := mustJSON(t, admin, "GET", "/api/v1/admin/ai/drafts", nil, 200)["drafts"].([]any)
	d := pending[0].(map[string]any)
	if d["kind"] != "defense" || d["defense"].(map[string]any)["source_ip"] != "203.0.113.9" {
		t.Fatalf("defense draft = %v", d)
	}
	mustJSON(t, admin, "POST", "/api/v1/admin/ai/drafts/"+d["id"].(string)+"/apply", nil, 200)
	if code := probe(); code != 403 {
		t.Fatalf("not blocked after confirmation: %d", code)
	}
	blocks := mustJSON(t, admin, "GET", "/api/v1/admin/defense/blocks", nil, 200)["blocks"].([]any)
	if len(blocks) != 1 {
		t.Fatalf("blocks = %v", blocks)
	}
	if e, _ := s.loadEvent(t.Context(), eid); e.Status != "blocked" {
		t.Fatalf("event status = %s", e.Status)
	}
	mustJSON(t, admin, "DELETE", "/api/v1/admin/defense/blocks/"+blocks[0].(map[string]any)["key"].(string), nil, 204)
	if code := probe(); code != 200 {
		t.Fatalf("still blocked after unblock: %d", code)
	}
}
