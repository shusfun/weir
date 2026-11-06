package center

import (
	"regexp"
	"sync"
	"testing"

	"github.com/shusfun/weir/internal/mailer"
	"github.com/shusfun/weir/internal/store/ent/audit"
	"github.com/shusfun/weir/internal/store/ent/user"
)

type fakeMail struct {
	mu   sync.Mutex
	sent []string // to + body
}

func (f *fakeMail) Send(_ mailer.SMTPConfig, to, _, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, to+"|"+body)
	return nil
}

func (f *fakeMail) lastCode(t *testing.T) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sent) == 0 {
		t.Fatal("no mail sent")
	}
	m := regexp.MustCompile(`\d{6}`).FindString(f.sent[len(f.sent)-1])
	if m == "" {
		t.Fatal("no code in mail")
	}
	return m
}

func TestPasswordResetAndRegistration(t *testing.T) {
	s, admin := newInstalledServer(t)
	mail := &fakeMail{}
	s.Mail = mail
	anon := &testClient{t: t, router: admin.router}
	mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "frank@example.test", "password": "Passw0rd"}, 200)

	// 未配置邮件服务时明确返回不可用。
	mustJSON(t, anon, "POST", "/api/v1/auth/email", map[string]any{"email": "frank@example.test", "purpose": "reset"}, 503)
	mustJSON(t, admin, "PUT", "/api/v1/admin/settings/smtp", map[string]any{"host": "smtp.example.test", "port": 465, "username": "u", "password": "smtp-secret", "from": "noreply@example.test", "use_tls": true}, 200)
	if v := mustJSON(t, admin, "GET", "/api/v1/admin/settings/smtp", nil, 200); v["password_set"] != true || mustString(v) == "" || regexp.MustCompile("smtp-secret").MatchString(mustString(v)) {
		t.Fatalf("smtp view = %v", v)
	}

	// 找回密码：不存在的账号同样返回成功但不发信；错误验证码被拒绝；成功后旧密码失效。
	mustJSON(t, anon, "POST", "/api/v1/auth/email", map[string]any{"email": "nobody@example.test", "purpose": "reset"}, 200)
	if len(mail.sent) != 0 {
		t.Fatal("mail sent for an unknown account")
	}
	mustJSON(t, anon, "POST", "/api/v1/auth/email", map[string]any{"email": "frank@example.test", "purpose": "reset"}, 200)
	code := mail.lastCode(t)
	mustJSON(t, anon, "POST", "/api/v1/auth/reset", map[string]any{"email": "frank@example.test", "password": "123456", "code": "000000"}, 400)
	resetTrace := "0123456789abcdef0123456789abcdef"
	reset := anon.do("POST", "/api/v1/auth/reset", map[string]any{"email": "frank@example.test", "password": "123456", "code": code}, map[string]string{"traceparent": "00-" + resetTrace + "-1111111111111111-01"})
	if reset.Code != 200 || reset.Header().Get("X-Trace-Id") != resetTrace {
		t.Fatalf("six-digit password reset: status=%d headers=%v body=%s", reset.Code, reset.Header(), reset.Body)
	}
	resetAudit := s.Store.Ent.Audit.Query().Where(audit.ActionEQ("auth.reset")).OnlyX(t.Context())
	if resetAudit.Detail["trace_id"] != resetTrace || resetAudit.Detail["request_id"] != reset.Header().Get("X-Request-Id") {
		t.Fatalf("password reset audit trace = %v", resetAudit.Detail)
	}
	mustJSON(t, anon, "POST", "/api/v1/auth/reset", map[string]any{"email": "frank@example.test", "password": "654321", "code": code}, 400)
	login(t, s, "frank@example.test", "123456")
	if w := anon.do("POST", "/api/v1/auth/login", map[string]string{"email": "frank@example.test", "password": "Passw0rd"}, nil); w.Code == 200 {
		t.Fatal("old password still works")
	}

	// 自助注册默认关闭；开启并要求邮箱验证后，注册的用户没有任何节点。
	mustJSON(t, anon, "POST", "/api/v1/auth/register", map[string]any{"email": "gina@example.test", "password": "Passw0rd"}, 403)
	pub := mustJSON(t, anon, "GET", "/api/v1/public", nil, 200)
	if pub["registration_enabled"] != false || pub["password_reset"] != true {
		t.Fatalf("public = %v", pub)
	}
	mustJSON(t, admin, "PUT", "/api/v1/admin/settings/security", map[string]any{"totp_enabled": true, "passkey_enabled": true, "registration_enabled": true, "email_verification": true}, 200)
	mustJSON(t, anon, "POST", "/api/v1/auth/register", map[string]any{"email": "gina@example.test", "password": "Passw0rd"}, 400)
	mustJSON(t, anon, "POST", "/api/v1/auth/email", map[string]any{"email": "gina@example.test", "purpose": "register"}, 200)
	registerTrace := "fedcba9876543210fedcba9876543210"
	registered := anon.do("POST", "/api/v1/auth/register", map[string]any{"email": "gina@example.test", "password": "111111", "code": mail.lastCode(t)}, map[string]string{"traceparent": "00-" + registerTrace + "-2222222222222222-01"})
	if registered.Code != 201 || registered.Header().Get("X-Trace-Id") != registerTrace {
		t.Fatalf("six-digit password registration: status=%d headers=%v body=%s", registered.Code, registered.Header(), registered.Body)
	}
	registerAudit := s.Store.Ent.Audit.Query().Where(audit.ActionEQ("auth.register")).OnlyX(t.Context())
	if registerAudit.Detail["trace_id"] != registerTrace || registerAudit.Detail["request_id"] != registered.Header().Get("X-Request-Id") {
		t.Fatalf("registration audit trace = %v", registerAudit.Detail)
	}
	g := s.Store.Ent.User.Query().Where(user.EmailEQ("gina@example.test")).OnlyX(t.Context())
	if g.AllNodes || len(g.NodeIds) != 0 || g.AllowChain || g.Role != user.RoleUser {
		t.Fatalf("self-registered user must have no nodes: %+v", g)
	}
}
