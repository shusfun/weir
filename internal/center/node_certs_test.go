package center

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/acme/acmetest"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/audit"
	"github.com/shusfun/weir/internal/store/ent/certificate"
	"github.com/shusfun/weir/packages/contracts"
)

// 中心测试一律不访问真实的 Let's Encrypt：默认指向一个连不上的地址，需要签发的测试换成假 CA。
func TestMain(m *testing.M) {
	acmeDirectory = func(NodeCertSettings) string { return "http://127.0.0.1:1/directory" }
	os.Exit(m.Run())
}

// serveActions 让假 Gate 按种类处理动作；没有处理函数的种类返回成功。
// 结束时下发一个空动作让正在等待的长轮询立即返回（通知只会让它重新查询），等协程退出，避免关闭服务时等满 25 秒。
func (g *fakeGate) serveActions(t *testing.T, s *Server, handlers map[string]func(contracts.Action) contracts.ActionResult) {
	stop, done := make(chan struct{}), make(chan struct{})
	t.Cleanup(func() {
		close(stop)
		_, _ = s.enqueueAction(context.Background(), int(g.id), "test.stop", nil, time.Minute)
		<-done
	})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
			}
			code, body := g.call("GET", "/api/v1/agent/actions", nil)
			if code != 200 {
				time.Sleep(20 * time.Millisecond)
				continue
			}
			var acts []contracts.Action
			_ = json.Unmarshal(body, &acts)
			for _, a := range acts {
				res := contracts.ActionResult{OK: true}
				if h := handlers[a.Kind]; h != nil {
					res = h(a)
				}
				g.call("POST", "/api/v1/agent/actions/"+itoa(int(a.ID)), res)
			}
		}
	}()
}

func useFakeCA(t *testing.T) *acmetest.CA {
	ca := acmetest.New(t)
	before := acmeDirectory
	acmeDirectory = func(NodeCertSettings) string { return ca.Directory() }
	t.Cleanup(func() { acmeDirectory = before })
	return ca
}

func heartbeat(t *testing.T, g *fakeGate, hb contracts.Heartbeat) {
	t.Helper()
	hb.Version, hb.Arch = "test", "amd64"
	if code, body := g.call("POST", "/api/v1/agent/heartbeat", hb); code != 200 {
		t.Fatalf("heartbeat = %d %s", code, body)
	}
}

func TestRealityPrefersFree443(t *testing.T) {
	s, admin, g := setupGate(t)
	gid := int(g.id)
	first := mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": gid, "preset": "vless-reality", "skip_probe": true}, 200)
	if first["port"].(float64) != 443 {
		t.Fatalf("first REALITY node on a free 443 = %v", first["port"])
	}
	// 这台 VPS 上已有本项目节点用 443：第二个 REALITY 节点照常随机分配。
	second := mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": gid, "preset": "vless-reality", "skip_probe": true}, 200)
	if p := second["port"].(float64); p < portMin || p > portMax {
		t.Fatalf("second REALITY node = %v", p)
	}
	// 其他协议不分配 443。
	if p := mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": gid, "preset": "ss2022", "skip_probe": true}, 200)["port"].(float64); p == 443 {
		t.Fatal("non-REALITY node must not get 443")
	}

	// 另一台 VPS 的 443 被 1Panel 的 OpenResty 占用：随机分配，不抢占。
	g2 := pairFakeGate(t, admin)
	heartbeat(t, g2, contracts.Heartbeat{ListeningPorts: []contracts.ListeningPort{{Port: 443, Proto: "tcp", Process: "openresty"}}})
	n := mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": int(g2.id), "preset": "vless-reality", "skip_probe": true}, 200)
	if n["port"].(float64) == 443 {
		t.Fatal("443 in use by another program must not be taken")
	}
	_ = s
}

func TestSelfSignedSharedPerGate(t *testing.T) {
	s, admin, g := setupGate(t)
	gid := int(g.id)
	a := mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": gid, "preset": "hysteria2", "skip_probe": true}, 200)
	b := mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": gid, "preset": "tuic", "skip_probe": true}, 200)
	if a["cert_id"] == nil || a["cert_id"] != b["cert_id"] {
		t.Fatalf("nodes on one VPS must share one self-signed cert: %v %v", a["cert_id"], b["cert_id"])
	}
	ct, _ := s.Store.Ent.Certificate.Get(t.Context(), int(a["cert_id"].(float64)))
	if ct.Source != certificate.SourceSelfSigned || ct.GateID == nil || *ct.GateID != gid || ct.NotAfter.Before(time.Now().AddDate(9, 0, 0)) {
		t.Fatalf("self-signed cert = %+v", ct)
	}
}

func TestTrojanWithoutAnyCertSourceExplains(t *testing.T) {
	_, admin, g := setupGate(t)
	w := admin.do("POST", "/api/v1/admin/nodes", map[string]any{"gate_id": int(g.id), "preset": "trojan-tls", "skip_probe": true}, nil)
	body := decode(t, w)
	e := body["error"].(map[string]any)
	if w.Code != 422 || e["code"] != "cert_unavailable" || !strings.Contains(e["detail"].(string), "1Panel") || !strings.Contains(e["detail"].(string), "节点证书域名") {
		t.Fatalf("no cert source = %d %v", w.Code, body)
	}
}

func TestIPCertViaACMEThenExpiryAndRenewal(t *testing.T) {
	s, admin, g := setupGate(t)
	ca := useFakeCA(t)
	var mu sync.Mutex
	served := map[string]string{}
	ca.Fetch = func(ident, token string) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		if ident != "192.0.2.1" {
			return "", errors.New("wrong host " + ident)
		}
		return served[token], nil
	}
	// 假 Gate 在 80 端口应答：把验证内容记下，CA 来取。
	g.serveActions(t, s, map[string]func(contracts.Action) contracts.ActionResult{
		contracts.ActionACMEHTTP01: func(a contracts.Action) contracts.ActionResult {
			mu.Lock()
			served[a.Payload["token"].(string)] = a.Payload["key_auth"].(string)
			mu.Unlock()
			return contracts.ActionResult{OK: true}
		},
	})
	n := mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": int(g.id), "preset": "trojan-tls", "skip_probe": true}, 200)
	ident, profile := ca.Order()
	if ident["type"] != "ip" || profile != "shortlived" {
		t.Fatalf("order = %v %q", ident, profile)
	}
	cid := int(n["cert_id"].(float64))
	ct, _ := s.Store.Ent.Certificate.Get(t.Context(), cid)
	if ct.Source != certificate.SourceAcme || ct.Domain != "192.0.2.1" || n["settings"].(map[string]any)["tls_sni"] != "192.0.2.1" {
		t.Fatalf("ip cert = %+v node %v", ct, n["settings"])
	}
	// 第二个需要证书的节点沿用同一张。
	if again := mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": int(g.id), "preset": "vless-ws-tls", "skip_probe": true}, 200); int(again["cert_id"].(float64)) != cid {
		t.Fatal("second TLS node must reuse the VPS certificate")
	}
	// Gate 能下载这张证书（私钥在中心）。
	cfg := g.config(0)
	if len(cfg.Certs) != 1 || cfg.Certs[0].Panel != "" || cfg.Certs[0].SHA256 == "" {
		t.Fatalf("cert refs = %+v", cfg.Certs)
	}
	if code, _ := g.call("GET", "/api/v1/agent/certs/"+itoa(cid), nil); code != 200 {
		t.Fatalf("agent cert = %d", code)
	}

	// 订阅：证书有效时写进订阅；过期后不写进订阅，显示原因。
	u := mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "bob@example.test"}, 200)
	detail := mustJSON(t, admin, "GET", "/api/v1/admin/users/"+itoa(int(u["id"].(float64))), nil, 200)
	token := detail["link_items"].([]any)[0].(map[string]any)["token"].(string)
	count := func() int {
		w := admin.do("GET", "/sub/"+token, nil, map[string]string{"User-Agent": "v2rayN/7.0"})
		raw, _ := base64.StdEncoding.DecodeString(w.Body.String())
		return strings.Count(string(raw), "trojan://") + strings.Count(string(raw), "vless://")
	}
	if count() != 2 {
		t.Fatal("valid certificate nodes must be in the subscription")
	}
	past := time.Now().Add(-time.Hour)
	_ = ct.Update().SetNotAfter(past).Exec(t.Context())
	if count() != 0 {
		t.Fatal("expired certificate nodes must not be in the subscription")
	}
	entries := mustJSON(t, admin, "GET", "/api/v1/admin/users/"+itoa(int(u["id"].(float64))), nil, 200)["link_items"].([]any)[0].(map[string]any)["entries"].([]any)
	if e := entries[0].(map[string]any); e["error"] != "证书已过期" {
		t.Fatalf("entry = %v", e)
	}
	if v := mustJSON(t, admin, "GET", "/api/v1/admin/certs", nil, 200)["items"].([]any)[0].(map[string]any); v["health"] != "expired" {
		t.Fatalf("cert view = %v", v)
	}

	// 续签：换上新证书，用到它的 Gate 重新下发。
	before := g.config(0).Revision
	renewed := mustJSON(t, admin, "POST", "/api/v1/admin/certs/"+itoa(cid)+"/renew", nil, 200)
	if renewed["health"] != "ready" {
		t.Fatalf("renewed = %v", renewed)
	}
	if after := g.config(0); after.Revision == before || after.Certs[0].SHA256 == cfg.Certs[0].SHA256 {
		t.Fatal("renewal must push the new certificate")
	}
	if count() != 2 {
		t.Fatal("renewed certificate nodes must be back in the subscription")
	}
}

func TestPanelKeyHandoffAndPanelCert(t *testing.T) {
	s, admin, g := setupGate(t)
	const apiKey = "panel-api-key-SECRET-123"
	var fetched []int
	var mu sync.Mutex
	g.serveActions(t, s, map[string]func(contracts.Action) contracts.ActionResult{
		contracts.ActionPanelConfigure: func(a contracts.Action) contracts.ActionResult {
			// Gate 凭编号取回密钥：只能取一次。
			nonce := a.Payload["nonce"].(string)
			code1, body := g.call("POST", "/api/v1/agent/panel-secret", map[string]string{"nonce": nonce})
			code2, _ := g.call("POST", "/api/v1/agent/panel-secret", map[string]string{"nonce": nonce})
			mu.Lock()
			fetched = append(fetched, code1, code2)
			mu.Unlock()
			if !strings.Contains(string(body), apiKey) {
				return contracts.ActionResult{Error: "secret missing"}
			}
			return contracts.ActionResult{OK: true, Result: map[string]any{"version": "v2.0.16"}}
		},
		contracts.ActionPanelCert: func(a contracts.Action) contracts.ActionResult {
			certPEM, keyPEM, _ := selfSigned(a.Payload["ip"].(string))
			return contracts.ActionResult{OK: true, Result: map[string]any{"cert_pem": certPEM, "sha256": contracts.CertSum(certPEM, keyPEM)}}
		},
	})
	mustJSON(t, admin, "PUT", "/api/v1/admin/gates/"+itoa(int(g.id))+"/panel", map[string]any{"port": 10086, "api_key": apiKey, "password": "wrong"}, 401)
	v := mustJSON(t, admin, "PUT", "/api/v1/admin/gates/"+itoa(int(g.id))+"/panel", map[string]any{"port": 10086, "api_key": apiKey, "password": "Passw0rd"}, 200)
	mu.Lock()
	if v["version"] != "v2.0.16" || len(fetched) != 2 || fetched[0] != 200 || fetched[1] != 404 {
		t.Fatalf("handoff = %v fetched %v", v, fetched)
	}
	mu.Unlock()
	// 密钥不落库：设置、审计里都没有。
	ctx := t.Context()
	settings, _ := s.Store.Ent.Setting.Query().All(ctx)
	audits, _ := s.Store.Ent.Audit.Query().Where(audit.ActionHasPrefix("gate.panel")).All(ctx)
	for _, row := range settings {
		b, _ := json.Marshal(row.Value)
		if strings.Contains(string(b), apiKey) {
			t.Fatal("api key stored in settings")
		}
	}
	if len(audits) != 1 || strings.Contains(audits[0].Target+audits[0].Result, apiKey) {
		t.Fatalf("audit = %+v", audits)
	}

	// Gate 上报 1Panel 已配置：Trojan 节点用 1Panel 证书，私钥留在 Gate。
	heartbeat(t, g, contracts.Heartbeat{Panel: &contracts.PanelStatus{Detected: true, Configured: true, Version: "v2.0.16"}})
	n := mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": int(g.id), "preset": "trojan-tls", "skip_probe": true}, 200)
	cid := int(n["cert_id"].(float64))
	ct, _ := s.Store.Ent.Certificate.Get(ctx, cid)
	if ct.Source != certificate.SourcePanel || ct.KeyPem != "" || ct.NotAfter == nil {
		t.Fatalf("panel cert = %+v", ct)
	}
	cfg := g.config(0)
	if len(cfg.Certs) != 1 || cfg.Certs[0].Panel != "192.0.2.1" || cfg.Certs[0].SHA256 != "" {
		t.Fatalf("panel cert ref = %+v", cfg.Certs)
	}
	if code, _ := g.call("GET", "/api/v1/agent/certs/"+itoa(cid), nil); code != 404 {
		t.Fatal("center has no private key for a 1Panel certificate")
	}
	// 1Panel 续签后 Gate 心跳上报新证书：中心更新记录，不重新下发配置。
	rev := cfg.Revision
	newPEM, newKey, _ := selfSigned("192.0.2.1")
	heartbeat(t, g, contracts.Heartbeat{PanelCerts: map[string]contracts.PanelCert{itoa(cid): {CertPEM: newPEM, SHA256: contracts.CertSum(newPEM, newKey), NotAfter: time.Now().Add(6 * 24 * time.Hour)}}})
	if ct, _ = s.Store.Ent.Certificate.Get(ctx, cid); ct.CertPem != newPEM {
		t.Fatal("panel renewal not recorded")
	}
	if g.config(0).Revision != rev {
		t.Fatal("panel renewal must not re-push the config")
	}
}

func TestEdgeDomainFallback(t *testing.T) {
	s, admin, g := setupGate(t)
	ca := useFakeCA(t)
	router := s.Router()
	// CA 经反代访问节点证书域名的验证路径：明文 HTTP，从本机回环转发过来。
	ca.Fetch = func(ident, token string) (string, error) {
		req := httptest.NewRequest("GET", "http://"+ident+acmeChallengePath+token, nil)
		req.RemoteAddr = "127.0.0.1:40000"
		req.Header.Set("X-Forwarded-Proto", "http")
		req.Header.Set("X-Forwarded-For", "198.51.100.20")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != 200 {
			return "", errors.New("challenge " + itoa(w.Code))
		}
		b, _ := io.ReadAll(w.Body)
		return string(b), nil
	}
	// 80 端口被 nginx 占用，也没有 1Panel：只能用节点证书域名。
	heartbeat(t, g, contracts.Heartbeat{ListeningPorts: []contracts.ListeningPort{{Port: 80, Proto: "tcp", Process: "nginx"}}})
	mustJSON(t, admin, "PUT", "/api/v1/admin/node-certs", map[string]any{"edge_domain": "1.2.3.4"}, 400)
	mustJSON(t, admin, "PUT", "/api/v1/admin/node-certs", map[string]any{"edge_domain": "Edge.Example.com", "email": "ops@example.com"}, 200)
	n := mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": int(g.id), "preset": "trojan-tls", "skip_probe": true}, 200)
	ct, _ := s.Store.Ent.Certificate.Get(t.Context(), int(n["cert_id"].(float64)))
	if ct.Source != certificate.SourceEdge || ct.GateID != nil || n["settings"].(map[string]any)["tls_sni"] != "edge.example.com" {
		t.Fatalf("edge cert = %+v sni %v", ct, n["settings"])
	}
	if ident, profile := ca.Order(); ident["type"] != "dns" || profile != "" {
		t.Fatalf("edge order = %v %q", ident, profile)
	}
	// 订阅里的连接地址仍是 VPS 的 IP，SNI 是节点证书域名。
	u := mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "carol@example.test"}, 200)
	detail := mustJSON(t, admin, "GET", "/api/v1/admin/users/"+itoa(int(u["id"].(float64))), nil, 200)
	uri := detail["link_items"].([]any)[0].(map[string]any)["entries"].([]any)[0].(map[string]any)["uri"].(string)
	if !strings.Contains(uri, "@192.0.2.1:") || !strings.Contains(uri, "sni=edge.example.com") {
		t.Fatalf("uri = %s", uri)
	}
	// 验证路径只对节点证书域名、只回应已签发过程中的 token；其他主机一律 404。
	req := httptest.NewRequest("GET", "http://example.com"+acmeChallengePath+"tok_abcdefghijklmnop0123", nil)
	req.RemoteAddr = "127.0.0.1:40000"
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != 404 {
		t.Fatalf("challenge on another host = %d", w.Code)
	}
}

func TestRenewDueAtTwoThirds(t *testing.T) {
	now := time.Now()
	start, end := now.Add(-4*24*time.Hour-time.Hour), now.Add(2*24*time.Hour-time.Hour)
	c := &ent.Certificate{Source: certificate.SourceAcme, Status: certificate.StatusReady, CreatedAt: start, NotAfter: &end}
	if !renewDue(c, now) || certHealth(c, now) != "renewing" {
		t.Fatal("must renew with less than a third of the lifetime left")
	}
	fresh := now.Add(5 * 24 * time.Hour)
	c.NotAfter, c.CreatedAt = &fresh, now.Add(-24*time.Hour)
	if renewDue(c, now) || certHealth(c, now) != "ready" {
		t.Fatal("fresh certificate must not renew")
	}
	c.NotAfter = nil
	if certHealth(c, now) != "unknown" {
		t.Fatal("missing expiry must be unknown, not ready")
	}
}
