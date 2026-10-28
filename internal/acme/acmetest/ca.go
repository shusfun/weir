// Package acmetest 提供测试用的假 ACME 服务。
package acmetest

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// CA 是测试用的最小 ACME 服务：校验 JWS 签名与 nonce；HTTP-01 验证时调用 Fetch 去取验证路径的内容
// （模拟 CA 访问 http://<标识>/.well-known/acme-challenge/<token>）。
type CA struct {
	t       testing.TB
	Fetch   func(identifier, token string) (string, error)
	srv     *httptest.Server
	mu      sync.Mutex
	nonces  map[string]bool
	key     *ecdsa.PublicKey
	thumb   string
	profile string
	ident   map[string]string
	token   string
	valid   bool
	fetched bool
	cert    []byte
	caKey   *ecdsa.PrivateKey
	caCert  *x509.Certificate
}

// New 启动假 CA；Directory() 是它的目录地址。
func New(t testing.TB) *CA {
	f := &CA{t: t, nonces: map[string]bool{}, token: "tok_abcdefghijklmnop0123"}
	f.caKey, _ = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "fake CA"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, _ := x509.CreateCertificate(rand.Reader, tpl, tpl, &f.caKey.PublicKey, f.caKey)
	f.caCert, _ = x509.ParseCertificate(der)
	f.srv = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *CA) url(p string) string { return f.srv.URL + p }

func (f *CA) newNonce(w http.ResponseWriter) {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	n := base64.RawURLEncoding.EncodeToString(b)
	f.nonces[n] = true
	w.Header().Set("Replay-Nonce", n)
}

// verify 校验 JWS，返回 payload；未注册时只接受带 jwk 的请求（newAccount）。
func (f *CA) verify(r *http.Request) ([]byte, map[string]any) {
	var jws struct{ Protected, Payload, Signature string }
	_ = json.NewDecoder(r.Body).Decode(&jws)
	ph, _ := base64.RawURLEncoding.DecodeString(jws.Protected)
	var prot map[string]any
	_ = json.Unmarshal(ph, &prot)
	n, _ := prot["nonce"].(string)
	if !f.nonces[n] {
		f.t.Errorf("bad nonce %q", n)
	}
	delete(f.nonces, n)
	if prot["url"] != "http://"+r.Host+r.URL.Path {
		f.t.Errorf("url %v != %s", prot["url"], r.URL.Path)
	}
	key := f.key
	if jwk, ok := prot["jwk"].(map[string]any); ok {
		x, _ := base64.RawURLEncoding.DecodeString(jwk["x"].(string))
		y, _ := base64.RawURLEncoding.DecodeString(jwk["y"].(string))
		key = &ecdsa.PublicKey{Curve: elliptic.P256(), X: new(big.Int).SetBytes(x), Y: new(big.Int).SetBytes(y)}
		f.key = key
		canon := fmt.Sprintf(`{"crv":"P-256","kty":"EC","x":"%s","y":"%s"}`, jwk["x"], jwk["y"])
		sum := sha256.Sum256([]byte(canon))
		f.thumb = base64.RawURLEncoding.EncodeToString(sum[:])
	} else if prot["kid"] != f.url("/acct/1") {
		f.t.Errorf("kid = %v", prot["kid"])
	}
	sig, _ := base64.RawURLEncoding.DecodeString(jws.Signature)
	sum := sha256.Sum256([]byte(jws.Protected + "." + jws.Payload))
	if key == nil || len(sig) != 64 || !ecdsa.Verify(key, sum[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
		f.t.Errorf("bad signature on %s", r.URL.Path)
	}
	payload, _ := base64.RawURLEncoding.DecodeString(jws.Payload)
	return payload, prot
}

func (f *CA) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.URL.Path == "/directory" {
		_ = json.NewEncoder(w).Encode(map[string]any{"newNonce": f.url("/nonce"), "newAccount": f.url("/account"), "newOrder": f.url("/order"), "meta": map[string]any{"profiles": map[string]any{"shortlived": "6 days"}}})
		return
	}
	f.newNonce(w)
	if r.URL.Path == "/nonce" {
		return
	}
	payload, _ := f.verify(r)
	switch r.URL.Path {
	case "/account":
		w.Header().Set("Location", f.url("/acct/1"))
		w.WriteHeader(201)
		_, _ = io.WriteString(w, `{"status":"valid"}`)
	case "/order":
		var req struct {
			Identifiers []map[string]string `json:"identifiers"`
			Profile     string              `json:"profile"`
		}
		_ = json.Unmarshal(payload, &req)
		f.ident, f.profile = req.Identifiers[0], req.Profile
		w.Header().Set("Location", f.url("/o/1"))
		w.WriteHeader(201)
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "pending", "authorizations": []string{f.url("/authz/1")}, "finalize": f.url("/finalize/1")})
	case "/authz/1":
		status := "pending"
		if f.valid {
			status = "valid"
		} else if f.ident != nil && f.fetched {
			status = "invalid"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": status, "challenges": []map[string]any{{"type": "http-01", "url": f.url("/chall/1"), "token": f.token, "status": status}}})
	case "/chall/1":
		// CA 去访问验证路径：内容必须是 token.指纹。
		f.fetched = true
		got, err := f.Fetch(f.ident["value"], f.token)
		// 内容不对时授权变为无效（和真实 CA 一样，由客户端轮询时看到失败）。
		f.valid = err == nil && got == f.token+"."+f.thumb
		_, _ = io.WriteString(w, `{"status":"processing"}`)
	case "/finalize/1":
		var req struct{ CSR string }
		_ = json.Unmarshal(payload, &req)
		der, _ := base64.RawURLEncoding.DecodeString(req.CSR)
		csr, err := x509.ParseCertificateRequest(der)
		if err != nil || csr.CheckSignature() != nil {
			f.t.Errorf("csr: %v", err)
			return
		}
		tpl := &x509.Certificate{SerialNumber: big.NewInt(2), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(6 * 24 * time.Hour), IPAddresses: csr.IPAddresses, DNSNames: csr.DNSNames, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
		f.cert, _ = x509.CreateCertificate(rand.Reader, tpl, f.caCert, csr.PublicKey, f.caKey)
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "processing"})
	case "/o/1":
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "valid", "certificate": f.url("/cert/1")})
	case "/cert/1":
		w.Header().Set("Content-Type", "application/pem-certificate-chain")
		_ = pem.Encode(w, &pem.Block{Type: "CERTIFICATE", Bytes: f.cert})
		_ = pem.Encode(w, &pem.Block{Type: "CERTIFICATE", Bytes: f.caCert.Raw})
	default:
		w.WriteHeader(404)
	}
}

// Directory 是假 CA 的目录地址。
func (f *CA) Directory() string { return f.url("/directory") }

// Order 返回最近一次下单的标识与 profile。
func (f *CA) Order() (map[string]string, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ident, f.profile
}
