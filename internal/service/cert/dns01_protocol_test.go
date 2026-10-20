package cert

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"testing"
	"time"
)

type dnsProofFixture struct {
	created, cleaned int
	value            string
	failLookup       bool
}

func (p *dnsProofFixture) Present(ctx context.Context, name, value string) (func(context.Context) error, error) {
	p.created++
	p.value = value
	return func(ctx context.Context) error {
		p.cleaned++
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return nil
	}, nil
}
func TestDNS01ACMEProtocolAndCleanup(t *testing.T) {
	// A local protocol fixture, not a public CA acceptance. Inspect CSR, DNS proof,
	// account registration, challenge validation and cleanup through real acme.Client.
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	provider := &dnsProofFixture{}
	accepted := false
	finalized := false
	registered := false
	var certPEM string
	calls := 0
	root := "https://acme-v02.api.letsencrypt.org"
	endpoint := func(path string) string { return root + path }
	transport := dnsRoundTrip(func(req *http.Request) (*http.Response, error) {
		calls++
		headers := http.Header{"Replay-Nonce": []string{base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("fixture-nonce-%d", calls)))}}
		status := 200
		var body any
		payload := map[string]any{}
		if req.Method == "POST" {
			var envelope struct {
				Payload string `json:"payload"`
			}
			if json.NewDecoder(req.Body).Decode(&envelope) != nil {
				t.Fatal("invalid JWS")
			}
			raw, _ := base64.RawURLEncoding.DecodeString(envelope.Payload)
			if len(raw) > 0 && json.Unmarshal(raw, &payload) != nil {
				t.Fatal("invalid JWS payload")
			}
		}
		order := func() map[string]any {
			state := "pending"
			if accepted {
				state = "ready"
			}
			if finalized {
				state = "valid"
			}
			out := map[string]any{"status": state, "authorizations": []string{endpoint("/authz/1")}, "finalize": endpoint("/finalize/1"), "identifiers": []map[string]string{{"type": "dns", "value": "example.com"}}}
			if finalized {
				out["certificate"] = endpoint("/certificate/1")
			}
			return out
		}
		switch req.URL.Path {
		case "/directory":
			body = map[string]any{"newNonce": endpoint("/nonce"), "newAccount": endpoint("/account"), "newOrder": endpoint("/new-order")}
		case "/nonce":
			body = map[string]any{}
		case "/account":
			registered = true
			status = 201
			headers.Set("Location", endpoint("/account/1"))
			body = map[string]any{"status": "valid"}
		case "/new-order":
			if !registered {
				t.Fatal("order before account")
			}
			status = 201
			headers.Set("Location", endpoint("/order/1"))
			body = order()
		case "/authz/1":
			state := "pending"
			if accepted {
				state = "valid"
			}
			body = map[string]any{"status": state, "identifier": map[string]string{"type": "dns", "value": "example.com"}, "challenges": []map[string]string{{"type": "dns-01", "url": endpoint("/challenge/1"), "token": "fixture-token", "status": state}}}
		case "/challenge/1":
			if provider.created != 1 || provider.value == "" {
				t.Fatal("challenge accepted before proof")
			}
			accepted = true
			body = map[string]any{"type": "dns-01", "url": endpoint("/challenge/1"), "status": "valid", "token": "fixture-token"}
		case "/order/1":
			body = order()
		case "/finalize/1":
			if !accepted {
				t.Fatal("finalize before validation")
			}
			raw, err := base64.RawURLEncoding.DecodeString(payload["csr"].(string))
			if err != nil {
				t.Fatal(err)
			}
			csr, err := x509.ParseCertificateRequest(raw)
			if err != nil || csr.CheckSignature() != nil || len(csr.DNSNames) != 1 || csr.DNSNames[0] != "example.com" {
				t.Fatal("CSR domain or signature")
			}
			leaf := &x509.Certificate{SerialNumber: big.NewInt(3), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(90 * 24 * time.Hour), DNSNames: csr.DNSNames}
			der, err := x509.CreateCertificate(rand.Reader, leaf, leaf, csr.PublicKey, key)
			if err != nil {
				t.Fatal(err)
			}
			certPEM = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
			finalized = true
			body = order()
		case "/certificate/1":
			headers.Set("Content-Type", "application/pem-certificate-chain")
			return &http.Response{StatusCode: 200, Header: headers, Body: io.NopCloser(strings.NewReader(certPEM)), Request: req}, nil
		default:
			t.Fatalf("unexpected ACME endpoint %s", req.URL.Path)
		}
		raw, _ := json.Marshal(body)
		headers.Set("Content-Type", "application/json")
		return &http.Response{StatusCode: status, Header: headers, Body: io.NopCloser(strings.NewReader(string(raw))), Request: req}, nil
	})
	issuer := DNS01Issuer{AccountKey: key, Provider: provider, HTTPClient: &http.Client{Transport: transport}, LookupTXT: func(context.Context, string) ([]string, error) { return []string{provider.value}, nil }}
	result, err := issuer.Issue(context.Background(), IssueRequest{Domains: []string{"example.com"}, Email: "a@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if !finalized || len(result.CertPEM) == 0 || len(result.KeyPEM) == 0 || provider.cleaned != 1 {
		t.Fatal("incomplete DNS01 result or leaked proof")
	}
	if _, err = tls.X509KeyPair(result.CertPEM, result.KeyPEM); err != nil {
		t.Fatal("issued certificate/private key mismatch")
	}
}

func TestDNS01CleanupAfterCancellation(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	provider := &dnsProofFixture{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	root := "https://acme-v02.api.letsencrypt.org"
	calls := 0
	client := &http.Client{Transport: dnsRoundTrip(func(req *http.Request) (*http.Response, error) {
		calls++
		status := 200
		headers := http.Header{"Replay-Nonce": []string{base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprint(calls)))}}
		var body any
		switch req.URL.Path {
		case "/directory":
			body = map[string]any{"newNonce": root + "/nonce", "newAccount": root + "/account", "newOrder": root + "/new-order"}
		case "/nonce":
			body = map[string]any{}
		case "/account":
			status = 201
			headers.Set("Location", root+"/account/1")
			body = map[string]any{"status": "valid"}
		case "/new-order":
			status = 201
			headers.Set("Location", root+"/order/1")
			body = map[string]any{"status": "pending", "authorizations": []string{root + "/authz/1"}, "finalize": root + "/finalize/1"}
		case "/authz/1":
			body = map[string]any{"status": "pending", "identifier": map[string]string{"type": "dns", "value": "example.com"}, "challenges": []map[string]string{{"type": "dns-01", "url": root + "/challenge/1", "token": "fixture", "status": "pending"}}}
		default:
			t.Fatalf("request after propagation canceled: %s", req.URL.Path)
		}
		raw, _ := json.Marshal(body)
		return &http.Response{StatusCode: status, Header: headers, Body: io.NopCloser(strings.NewReader(string(raw))), Request: req}, nil
	})}
	issuer := DNS01Issuer{AccountKey: key, Provider: provider, HTTPClient: client, LookupTXT: func(context.Context, string) ([]string, error) { cancel(); return nil, context.Canceled }}
	_, err := issuer.Issue(ctx, IssueRequest{Domains: []string{"example.com"}, Email: "a@example.com"})
	if err == nil || err.Error() != "dns01_propagation_failed" || provider.created != 1 || provider.cleaned != 1 {
		t.Fatal("canceled issuance leaked created TXT proof")
	}
}
