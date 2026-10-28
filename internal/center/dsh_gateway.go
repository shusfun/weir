package center

import (
	"encoding/json"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/shusfun/weir/internal/store/ent/user"
)

// dsh 子域名网关：请求的 Host 是设置里的 dsh 子域名时，由这里处理，永远不会进入 Weir 的页面与接口。
// 只有同时满足以下条件才转给 dsh，否则一律返回同一个空正文 404（不区分原因、不带 Weir 特有的响应头），
// 看起来就像这个域名不存在：
//   - 由控制台签发的一次性票据进入（60 秒、只能用一次、绑定管理员会话），换得网关会话；
//   - 浏览器标明请求来自同站（Sec-Fetch-Site），页面只能作为 iframe 加载（Sec-Fetch-Dest）；
//   - 网关会话对应的管理员会话仍然有效、账号仍是启用的管理员（退出登录或降级立即失效）；
//   - 经 HTTPS 访问（本地开发直连回环除外），dsh 正在运行。
// dsh 自己只监听回环；启动令牌只在这里用来换取 dsh 的会话 Cookie，从不出现在浏览器里。

const (
	dshOpenPath   = "/__migate/open"
	dshGateCookie = "migate_dsh"
	dshGateTTL    = 12 * time.Hour
)

type dshGrant struct {
	UserID     int    `json:"user_id"`
	SessionKey string `json:"session_key"`
	Origin     string `json:"origin"` // 打开抽屉的控制台来源，只允许它嵌入
}

// dshGatewayMiddleware 在所有其他处理之前按 Host 分流。
func (s *Server) dshGatewayMiddleware(c *gin.Context) {
	domain := s.dshDomain()
	if domain == "" || requestHost(c.Request) != domain {
		c.Next()
		return
	}
	c.Abort()
	s.serveDSH(c.Writer, c.Request)
	// 空正文的 404 也要立即写出，否则 gin 会在未匹配路由时补上自己的默认正文。
	c.Writer.WriteHeaderNow()
}

// dshDeny 是唯一的拒绝响应：404、正文为空，只有 Go 默认的 Date / Content-Length。
func dshDeny(w http.ResponseWriter) {
	for k := range w.Header() {
		delete(w.Header(), k)
	}
	w.WriteHeader(http.StatusNotFound)
}

// dshTransport 只接受 HTTPS；本地开发直接连回环、不带转发头的明文请求放行。
func dshTransport(r *http.Request) bool {
	if requestSecure(r) {
		return true
	}
	for _, h := range []string{"X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto", "X-Real-IP", "Forwarded"} {
		if r.Header.Get(h) != "" {
			return false
		}
	}
	return fromLocalProxy(r)
}

// browserContext 检查浏览器自带、页面脚本无法伪造的请求头：普通请求看 Fetch Metadata（必须来自同站，
// 导航只能是 iframe）；Chromium 的 WebSocket 握手不带 Fetch Metadata，改看 Origin，必须是 dsh 子域名自己。
func browserContext(r *http.Request) bool {
	if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		u, err := url.Parse(r.Header.Get("Origin"))
		return err == nil && u.Host != "" && strings.EqualFold(u.Host, requestHost(r))
	}
	switch r.Header.Get("Sec-Fetch-Site") {
	case "same-origin", "same-site":
	default:
		return false
	}
	if r.Header.Get("Sec-Fetch-Mode") == "navigate" || r.Header.Get("Sec-Fetch-Dest") == "document" {
		return r.Header.Get("Sec-Fetch-Dest") == "iframe"
	}
	return true
}

// validGrant 确认网关会话背后的管理员会话仍然有效。
func (s *Server) validGrant(r *http.Request, g dshGrant) bool {
	s.state.RLock()
	defer s.state.RUnlock()
	if s.Store == nil || g.SessionKey == "" {
		return false
	}
	raw, ok, err := s.Store.KV.Get(r.Context(), g.SessionKey)
	var sess Session
	if err != nil || !ok || json.Unmarshal([]byte(raw), &sess) != nil || sess.UserID != g.UserID {
		return false
	}
	u, err := s.Store.Ent.User.Get(r.Context(), g.UserID)
	return err == nil && u.Enabled && u.Role == user.RoleAdmin && !s.blocked(r)
}

func (s *Server) serveDSH(w http.ResponseWriter, r *http.Request) {
	if !dshTransport(r) || !browserContext(r) {
		dshDeny(w)
		return
	}
	if r.URL.Path == dshOpenPath {
		s.openDSH(w, r)
		return
	}
	c, err := r.Cookie(dshGateCookie)
	if err != nil || len(c.Value) != 64 {
		dshDeny(w)
		return
	}
	var g dshGrant
	s.state.RLock()
	raw, ok, err := "", false, error(nil)
	if s.Store != nil {
		raw, ok, err = s.Store.KV.Get(r.Context(), "dsh-gw:"+digest(c.Value))
	}
	s.state.RUnlock()
	if err != nil || !ok || json.Unmarshal([]byte(raw), &g) != nil || !s.validGrant(r, g) {
		dshDeny(w)
		return
	}
	addr, _, running := s.dshEndpoint()
	if !running {
		dshDeny(w)
		return
	}
	s.proxyDSH(w, r, addr, g.Origin)
}

// openDSH 校验一次性票据，在服务端用启动令牌向 dsh 换取会话 Cookie，再发放网关会话。
func (s *Server) openDSH(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet || r.Header.Get("Sec-Fetch-Dest") != "iframe" {
		dshDeny(w)
		return
	}
	t := r.URL.Query().Get("t")
	var g dshGrant
	s.state.RLock()
	raw, ok, err := "", false, error(nil)
	if s.Store != nil && len(t) == 64 {
		raw, ok, err = s.Store.KV.GetDel(r.Context(), "dsh-ticket:"+digest(t))
	}
	s.state.RUnlock()
	if err != nil || !ok || json.Unmarshal([]byte(raw), &g) != nil || !s.validGrant(r, g) {
		dshDeny(w)
		return
	}
	addr, token, running := s.dshEndpoint()
	if !running {
		dshDeny(w)
		return
	}
	req, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, "http://"+addr+"/?token="+url.QueryEscape(token), nil)
	req.Host = requestHost(r)
	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		dshDeny(w)
		return
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		dshDeny(w)
		return
	}
	secure := requestSecure(r)
	for _, v := range resp.Header.Values("Set-Cookie") {
		w.Header().Add("Set-Cookie", secureCookie(v, secure))
	}
	session := randomToken(32)
	b, _ := json.Marshal(g)
	s.state.RLock()
	err = s.Store.KV.Set(r.Context(), "dsh-gw:"+digest(session), string(b), dshGateTTL)
	s.state.RUnlock()
	if err != nil {
		dshDeny(w)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: dshGateCookie, Value: session, Path: "/", MaxAge: int(dshGateTTL.Seconds()), HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode})
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Location", "/")
	w.WriteHeader(http.StatusSeeOther)
}

// secureCookie 在 HTTPS 下给 dsh 的会话 Cookie 补上 Secure（dsh 自己监听的是明文回环）。
func secureCookie(v string, secure bool) string {
	if secure && !strings.Contains(strings.ToLower(v), "; secure") {
		return v + "; Secure"
	}
	return v
}

// proxyDSH 只把 dsh 自己的 Cookie 转给 dsh（控制台会话、网关会话一律去掉），保留浏览器看到的 Host，
// 允许被打开它的控制台嵌入；长连接（WebSocket、事件流）不受服务的读写超时限制。
func (s *Server) proxyDSH(w http.ResponseWriter, r *http.Request, addr, origin string) {
	rc := http.NewResponseController(w)
	_ = rc.SetReadDeadline(time.Time{})
	_ = rc.SetWriteDeadline(time.Time{})
	host := requestHost(r)
	secure := requestSecure(r)
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(&url.URL{Scheme: "http", Host: addr})
			pr.Out.Host = host
			var keep []string
			for _, ck := range pr.In.Cookies() {
				if strings.HasPrefix(ck.Name, "dsh-auth-") {
					keep = append(keep, ck.Name+"="+ck.Value)
				}
			}
			pr.Out.Header.Del("Cookie")
			if len(keep) > 0 {
				pr.Out.Header.Set("Cookie", strings.Join(keep, "; "))
			}
			for _, h := range []string{"X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto", "X-Real-IP", "Forwarded"} {
				pr.Out.Header.Del(h)
			}
		},
		ModifyResponse: func(resp *http.Response) error {
			resp.Header.Del("X-Frame-Options")
			resp.Header.Set("Content-Security-Policy", "frame-ancestors "+origin)
			resp.Header.Set("Referrer-Policy", "no-referrer")
			if cookies := resp.Header.Values("Set-Cookie"); len(cookies) > 0 {
				resp.Header.Del("Set-Cookie")
				for _, v := range cookies {
					resp.Header.Add("Set-Cookie", secureCookie(v, secure))
				}
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) { dshDeny(w) },
	}
	proxy.ServeHTTP(w, r)
}
