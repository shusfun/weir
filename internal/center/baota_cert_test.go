package center

import (
	"errors"
	"github.com/shusfun/weir/packages/contracts"
	"sync"
	"testing"
)

func TestBaotaIPCertificateUsesExistingHTTPServer(t *testing.T) {
	s, admin, g := setupGate(t)
	ca := useFakeCA(t)
	var mu sync.Mutex
	served := map[string]string{}
	heartbeat(t, g, contracts.Heartbeat{Panel: &contracts.PanelStatus{Detected: true, Kind: "baota", HTTP01Webroot: true}, ListeningPorts: []contracts.ListeningPort{{Port: 80, Proto: "tcp", Process: "nginx"}}})
	ca.Fetch = func(host, token string) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		if host != "192.0.2.1" {
			return "", errors.New("wrong validation host")
		}
		return served[token], nil
	}
	g.serveActions(t, s, map[string]func(contracts.Action) contracts.ActionResult{contracts.ActionACMEHTTP01: func(a contracts.Action) contracts.ActionResult {
		if a.Payload["host"] != "192.0.2.1" {
			return contracts.ActionResult{Error: "validation Host missing"}
		}
		mu.Lock()
		served[a.Payload["token"].(string)] = a.Payload["key_auth"].(string)
		mu.Unlock()
		return contracts.ActionResult{OK: true}
	}})
	n := mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": g.id, "preset": "trojan-tls", "skip_probe": true}, 200)
	c := s.Store.Ent.Certificate.GetX(t.Context(), int(n["cert_id"].(float64)))
	if c.Source != "acme" || c.NotAfter == nil {
		t.Fatal("Baota did not use centrally managed ACME certificate")
	}
	ident, profile := ca.Order()
	if ident["type"] != "ip" || profile != "shortlived" {
		t.Fatal("wrong ACME order")
	}
}
