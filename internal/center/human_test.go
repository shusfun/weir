package center

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	altcha "github.com/altcha-org/altcha-lib-go/v2"
)

func httpHumanProof(t *testing.T, client *testClient, purpose string) string {
	t.Helper()
	start := time.Now()
	w := client.do("GET", "/api/v1/auth/human/challenge?purpose="+purpose, nil, nil)
	if w.Code != 200 {
		t.Fatalf("challenge: %d %s", w.Code, w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Trace-Id") == "" {
		t.Fatal("challenge missing cache or trace boundary")
	}
	var ch altcha.Challenge
	if err := json.Unmarshal(w.Body.Bytes(), &ch); err != nil {
		t.Fatal(err)
	}
	sol, err := altcha.SolveChallenge(altcha.SolveChallengeOptions{Challenge: ch, DeriveKey: altcha.DeriveKeyPBKDF2(), Timeout: 5 * time.Second})
	if err != nil || sol == nil {
		t.Fatal("solve", err)
	}
	b, _ := json.Marshal(altcha.Payload{Challenge: ch, Solution: *sol})
	if d := time.Duration(ch.Parameters.Data["minimumSeconds"].(float64))*time.Second - time.Since(start) + 20*time.Millisecond; d > 0 {
		time.Sleep(d)
	}
	return base64.StdEncoding.EncodeToString(b)
}

func TestHumanHTTPProtectedActionsAndReplay(t *testing.T) {
	_, admin := newInstalledServer(t)
	if w := admin.do("PUT", "/api/v1/admin/settings/security", map[string]bool{"registration_enabled": true}, nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, path := range []string{"register", "verify", "reset"} {
		w := admin.do("POST", "/api/v1/auth/"+path, map[string]string{"email": "human@example.test", "password": "Passw0rd", "code": "123456", "altcha": ""}, nil)
		if w.Code != 400 || decode(t, w)["error"].(map[string]any)["code"] != "human_verification_required" {
			t.Fatal("unprotected action", path, w.Code, w.Body.String())
		}
	}
	proof := httpHumanProof(t, admin, "register")
	req := map[string]string{"email": "human@example.test", "password": "Passw0rd", "altcha": proof}
	w := admin.do("POST", "/api/v1/auth/register", req, nil)
	if w.Code != 201 {
		t.Fatal("valid registration", w.Code, w.Body.String())
	}
	w = admin.do("POST", "/api/v1/auth/register", req, nil)
	if w.Code != 400 || decode(t, w)["error"].(map[string]any)["code"] != "human_verification_invalid" {
		t.Fatal("proof replay accepted", w.Code, w.Body.String())
	}
	proof = httpHumanProof(t, admin, "reset")
	req["altcha"] = proof
	w = admin.do("POST", "/api/v1/auth/register", req, nil)
	if w.Code != 400 || decode(t, w)["error"].(map[string]any)["code"] != "human_verification_invalid" {
		t.Fatal("cross-purpose accepted", w.Code, w.Body.String())
	}
	if w := admin.do("GET", "/api/v1/auth/human/challenge?purpose=anything", nil, nil); w.Code != 400 {
		t.Fatal("unknown purpose accepted")
	}
}

func TestHumanHTTPLoginThresholdAndHoneypot(t *testing.T) {
	_, admin := newInstalledServer(t)
	login := map[string]string{"email": "admin@example.test", "password": "bad", "altcha": ""}
	for range 5 {
		if w := admin.do("POST", "/api/v1/auth/login", login, nil); w.Code != 401 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if p := decode(t, admin.do("GET", "/api/v1/public", nil, nil)); p["human_login_required"] != true {
		t.Fatal("threshold not visible", p)
	}
	login["password"] = "Passw0rd"
	if w := admin.do("POST", "/api/v1/auth/login", login, nil); w.Code != 400 {
		t.Fatal("valid password bypassed challenge", w.Code)
	}
	login["altcha"] = httpHumanProof(t, admin, "login")
	login["website"] = "https://spam.test"
	if w := admin.do("POST", "/api/v1/auth/login", login, nil); w.Code != 400 {
		t.Fatal("honeypot accepted")
	}
	delete(login, "website")
	if w := admin.do("POST", "/api/v1/auth/login", login, nil); w.Code != 200 {
		t.Fatal("solved login", w.Code, w.Body.String())
	}
	if p := decode(t, admin.do("GET", "/api/v1/public", nil, nil)); p["human_login_required"] != false {
		t.Fatal("successful login did not clear threshold")
	}
	login["website"] = strings.Repeat("x", 2)
	if w := admin.do("POST", "/api/v1/auth/login", login, nil); w.Code != 400 {
		t.Fatal("honeypot bypass without threshold")
	}
}

// AUTH-02：换 IP 继续猜同一个账号也要过人机验证；只要求验证，不锁账号。
func TestLoginFailuresCountedPerAccount(t *testing.T) {
	s, admin := newInstalledServer(t)
	login := map[string]string{"email": "admin@example.test", "password": "bad", "altcha": ""}
	for range 5 {
		if w := admin.do("POST", "/api/v1/auth/login", login, nil); w.Code != 401 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	// 模拟攻击者换了来源 IP：IP 计数清零，账号计数还在。
	if err := s.Store.KV.Del(t.Context(), "failed-login:"+digest("192.0.2.1")); err != nil {
		t.Fatal(err)
	}
	if p := decode(t, admin.do("GET", "/api/v1/public", nil, nil)); p["human_login_required"] != false {
		t.Fatal("IP counter not cleared", p)
	}
	login["password"] = "Passw0rd"
	if w := admin.do("POST", "/api/v1/auth/login", login, nil); w.Code != 400 {
		t.Fatal("account threshold bypassed from a new IP", w.Code, w.Body.String())
	}
	login["altcha"] = httpHumanProof(t, admin, "login")
	if w := admin.do("POST", "/api/v1/auth/login", login, nil); w.Code != 200 {
		t.Fatal("account must stay usable after solving the challenge", w.Code, w.Body.String())
	}
	if _, ok, _ := s.Store.KV.Get(t.Context(), failedLoginAccountKey("admin@example.test")); ok {
		t.Fatal("successful login did not clear the account counter")
	}
}

// AUTH-02：登录接口按来源限速，超过后返回 429。
func TestLoginRateLimited(t *testing.T) {
	s, admin := newInstalledServer(t)
	if err := s.Store.KV.Set(t.Context(), "limit:login:"+digest("192.0.2.1"), "20", time.Minute); err != nil {
		t.Fatal(err)
	}
	if w := admin.do("POST", "/api/v1/auth/login", map[string]string{"email": "admin@example.test", "password": "x", "altcha": ""}, nil); w.Code != 429 {
		t.Fatal("rate limit not enforced", w.Code, w.Body.String())
	}
}
