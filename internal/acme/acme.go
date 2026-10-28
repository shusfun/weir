// Package acme 是只支持 HTTP-01 的最小 ACME（RFC 8555）客户端。
// 自己实现而不用 x/crypto/acme：Let's Encrypt 的 IP 证书必须以 shortlived profile 下单（ACME profiles 扩展），
// x/crypto/acme 不支持指定 profile。
package acme

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	LetsEncrypt        = "https://acme-v02.api.letsencrypt.org/directory"
	LetsEncryptStaging = "https://acme-staging-v02.api.letsencrypt.org/directory"
	// ProfileShortLived 是 Let's Encrypt 的约 6 天短期证书，IP 证书只能用它。
	ProfileShortLived = "shortlived"
)

// Client 持有账户私钥；Account 为空时首次下单前自动注册（同意服务条款）。
type Client struct {
	Directory string
	Key       *ecdsa.PrivateKey
	Email     string
	HTTP      *http.Client

	mu      sync.Mutex
	dir     *directory
	account string
	nonces  []string
}

type directory struct {
	NewNonce   string `json:"newNonce"`
	NewAccount string `json:"newAccount"`
	NewOrder   string `json:"newOrder"`
	Meta       struct {
		Profiles map[string]any `json:"profiles"`
	} `json:"meta"`
}

// Problem 是 ACME 服务返回的错误（RFC 7807）。
type Problem struct {
	Type   string `json:"type"`
	Detail string `json:"detail"`
	Status int    `json:"status"`
}

func (p *Problem) Error() string {
	t := strings.TrimPrefix(p.Type, "urn:ietf:params:acme:error:")
	if p.Detail == "" {
		return "acme: " + t
	}
	return fmt.Sprintf("acme: %s: %s", t, p.Detail)
}

// NewKey 生成账户或证书用的 P-256 私钥。
func NewKey() (*ecdsa.PrivateKey, error) { return ecdsa.GenerateKey(elliptic.P256(), rand.Reader) }

// EncodeKey / DecodeKey 以 PEM 保存私钥。
func EncodeKey(k *ecdsa.PrivateKey) (string, error) {
	der, err := x509.MarshalECPrivateKey(k)
	if err != nil {
		return "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})), nil
}

func DecodeKey(s string) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(s))
	if block == nil {
		return nil, errors.New("invalid key")
	}
	return x509.ParseECPrivateKey(block.Bytes)
}

func (c *Client) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func pad32(b []byte) []byte {
	out := make([]byte, 32)
	copy(out[32-len(b):], b)
	return out
}

func (c *Client) jwk() map[string]string {
	return map[string]string{"crv": "P-256", "kty": "EC", "x": b64(pad32(c.Key.X.Bytes())), "y": b64(pad32(c.Key.Y.Bytes()))}
}

// Thumbprint 是账户公钥的 JWK 指纹（RFC 7638），HTTP-01 的应答内容为 token + "." + 指纹。
func (c *Client) Thumbprint() string {
	k := c.jwk()
	b := []byte(fmt.Sprintf(`{"crv":"%s","kty":"%s","x":"%s","y":"%s"}`, k["crv"], k["kty"], k["x"], k["y"]))
	sum := sha256.Sum256(b)
	return b64(sum[:])
}

// KeyAuthorization 是 HTTP-01 验证路径上要返回的内容。
func (c *Client) KeyAuthorization(token string) string { return token + "." + c.Thumbprint() }

func (c *Client) discover(ctx context.Context) (*directory, error) {
	c.mu.Lock()
	d := c.dir
	c.mu.Unlock()
	if d != nil {
		return d, nil
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", c.Directory, nil)
	resp, err := c.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	d = &directory{}
	if resp.StatusCode != 200 || json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(d) != nil || d.NewOrder == "" {
		return nil, fmt.Errorf("acme: directory %s unavailable (HTTP %d)", c.Directory, resp.StatusCode)
	}
	c.mu.Lock()
	c.dir = d
	c.mu.Unlock()
	return d, nil
}

func (c *Client) nonce(ctx context.Context) (string, error) {
	c.mu.Lock()
	if n := len(c.nonces); n > 0 {
		v := c.nonces[n-1]
		c.nonces = c.nonces[:n-1]
		c.mu.Unlock()
		return v, nil
	}
	c.mu.Unlock()
	d, err := c.discover(ctx)
	if err != nil {
		return "", err
	}
	req, _ := http.NewRequestWithContext(ctx, "HEAD", d.NewNonce, nil)
	resp, err := c.client().Do(req)
	if err != nil {
		return "", err
	}
	resp.Body.Close()
	if v := resp.Header.Get("Replay-Nonce"); v != "" {
		return v, nil
	}
	return "", errors.New("acme: no nonce")
}

// post 发送 JWS 签名的请求（payload 为 nil 时是 POST-as-GET）；badNonce 时重试一次。
func (c *Client) post(ctx context.Context, url string, payload any, out any) (*http.Response, []byte, error) {
	for attempt := 0; ; attempt++ {
		nonce, err := c.nonce(ctx)
		if err != nil {
			return nil, nil, err
		}
		protected := map[string]any{"alg": "ES256", "nonce": nonce, "url": url}
		c.mu.Lock()
		kid := c.account
		c.mu.Unlock()
		if kid != "" {
			protected["kid"] = kid
		} else {
			protected["jwk"] = c.jwk()
		}
		ph, _ := json.Marshal(protected)
		body := ""
		if payload != nil {
			pb, _ := json.Marshal(payload)
			body = b64(pb)
		}
		signing := b64(ph) + "." + body
		sum := sha256.Sum256([]byte(signing))
		r, s, err := ecdsa.Sign(rand.Reader, c.Key, sum[:])
		if err != nil {
			return nil, nil, err
		}
		sig := append(pad32(r.Bytes()), pad32(s.Bytes())...)
		jws, _ := json.Marshal(map[string]string{"protected": b64(ph), "payload": body, "signature": b64(sig)})
		req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jws))
		req.Header.Set("Content-Type", "application/jose+json")
		resp, err := c.client().Do(req)
		if err != nil {
			return nil, nil, err
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		resp.Body.Close()
		if n := resp.Header.Get("Replay-Nonce"); n != "" {
			c.mu.Lock()
			c.nonces = append(c.nonces, n)
			c.mu.Unlock()
		}
		if resp.StatusCode >= 400 {
			p := &Problem{Status: resp.StatusCode}
			_ = json.Unmarshal(raw, p)
			if strings.HasSuffix(p.Type, ":badNonce") && attempt == 0 {
				continue
			}
			return resp, raw, p
		}
		if out != nil && len(raw) > 0 {
			if err = json.Unmarshal(raw, out); err != nil {
				return resp, raw, err
			}
		}
		return resp, raw, nil
	}
}

// register 注册（或找回）账户：termsOfServiceAgreed。
func (c *Client) register(ctx context.Context) error {
	c.mu.Lock()
	done := c.account != ""
	c.mu.Unlock()
	if done {
		return nil
	}
	d, err := c.discover(ctx)
	if err != nil {
		return err
	}
	req := map[string]any{"termsOfServiceAgreed": true}
	if c.Email != "" {
		req["contact"] = []string{"mailto:" + c.Email}
	}
	resp, _, err := c.post(ctx, d.NewAccount, req, nil)
	if err != nil {
		return err
	}
	loc := resp.Header.Get("Location")
	if loc == "" {
		return errors.New("acme: account has no location")
	}
	c.mu.Lock()
	c.account = loc
	c.mu.Unlock()
	return nil
}

type order struct {
	url            string
	Status         string   `json:"status"`
	Authorizations []string `json:"authorizations"`
	Finalize       string   `json:"finalize"`
	Certificate    string   `json:"certificate"`
	Error          *Problem `json:"error"`
}

type authorization struct {
	Status     string `json:"status"`
	Challenges []struct {
		Type   string   `json:"type"`
		URL    string   `json:"url"`
		Token  string   `json:"token"`
		Status string   `json:"status"`
		Error  *Problem `json:"error"`
	} `json:"challenges"`
}

// Challenge 是一次 HTTP-01 验证：在 http://<标识>/.well-known/acme-challenge/<Token> 返回 KeyAuth。
type Challenge struct {
	Token   string
	KeyAuth string
}

// Issue 为一个域名或 IP 申请证书：responder 负责让验证路径返回 KeyAuth（返回后才通知 CA 验证）。
// profile 为空时不指定；IP 证书须用 ProfileShortLived。返回证书链与新私钥（PEM）。
func (c *Client) Issue(ctx context.Context, identifier, profile string, responder func(context.Context, Challenge) error) (string, string, error) {
	if err := c.register(ctx); err != nil {
		return "", "", err
	}
	d, _ := c.discover(ctx)
	idType := "dns"
	ip := net.ParseIP(identifier)
	if ip != nil {
		idType = "ip"
	}
	req := map[string]any{"identifiers": []map[string]string{{"type": idType, "value": identifier}}}
	if profile != "" {
		req["profile"] = profile
	}
	var o order
	resp, _, err := c.post(ctx, d.NewOrder, req, &o)
	if err != nil {
		return "", "", err
	}
	o.url = resp.Header.Get("Location")
	for _, az := range o.Authorizations {
		var a authorization
		if _, _, err = c.post(ctx, az, nil, &a); err != nil {
			return "", "", err
		}
		if a.Status == "valid" {
			continue
		}
		var url, token string
		for _, ch := range a.Challenges {
			if ch.Type == "http-01" {
				url, token = ch.URL, ch.Token
			}
		}
		if url == "" {
			return "", "", errors.New("acme: CA offered no http-01 challenge")
		}
		if err = responder(ctx, Challenge{Token: token, KeyAuth: c.KeyAuthorization(token)}); err != nil {
			return "", "", err
		}
		if _, _, err = c.post(ctx, url, map[string]any{}, nil); err != nil {
			return "", "", err
		}
		if err = c.wait(ctx, az, func(raw []byte) (bool, error) {
			var a authorization
			_ = json.Unmarshal(raw, &a)
			switch a.Status {
			case "valid":
				return true, nil
			case "invalid":
				for _, ch := range a.Challenges {
					if ch.Error != nil {
						return false, ch.Error
					}
				}
				return false, errors.New("acme: authorization invalid")
			}
			return false, nil
		}); err != nil {
			return "", "", err
		}
	}
	key, err := NewKey()
	if err != nil {
		return "", "", err
	}
	tpl := &x509.CertificateRequest{Subject: pkix.Name{}}
	if ip != nil {
		tpl.IPAddresses = []net.IP{ip}
	} else {
		tpl.DNSNames = []string{identifier}
		tpl.Subject.CommonName = identifier
	}
	csr, err := x509.CreateCertificateRequest(rand.Reader, tpl, crypto.Signer(key))
	if err != nil {
		return "", "", err
	}
	if _, _, err = c.post(ctx, o.Finalize, map[string]string{"csr": b64(csr)}, &o); err != nil {
		return "", "", err
	}
	if o.Certificate == "" {
		if err = c.wait(ctx, o.url, func(raw []byte) (bool, error) {
			_ = json.Unmarshal(raw, &o)
			switch o.Status {
			case "valid":
				return o.Certificate != "", nil
			case "invalid":
				if o.Error != nil {
					return false, o.Error
				}
				return false, errors.New("acme: order invalid")
			}
			return false, nil
		}); err != nil {
			return "", "", err
		}
	}
	_, chain, err := c.post(ctx, o.Certificate, nil, nil)
	if err != nil {
		return "", "", err
	}
	if !bytes.Contains(chain, []byte("BEGIN CERTIFICATE")) {
		return "", "", errors.New("acme: certificate download returned no PEM")
	}
	keyPEM, err := EncodeKey(key)
	return string(chain), keyPEM, err
}

// wait 轮询直到 done 返回 true（最多 2 分钟）。
func (c *Client) wait(ctx context.Context, url string, done func([]byte) (bool, error)) error {
	deadline := time.Now().Add(2 * time.Minute)
	for {
		_, raw, err := c.post(ctx, url, nil, nil)
		if err != nil {
			return err
		}
		ok, err := done(raw)
		if err != nil || ok {
			return err
		}
		if time.Now().After(deadline) {
			return errors.New("acme: timed out waiting for the CA")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}
