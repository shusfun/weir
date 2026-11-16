package contracts

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"math/big"
	"strings"
	"testing"
	"time"
)

func TestBrowserCertificateHashPolicy(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	p256, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	p384, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	_, ed, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name       string
		key        crypto.Signer
		start, end time.Time
		want       bool
	}{
		{"short-p256", p256, now.Add(-time.Minute), now.Add(time.Hour), true},
		{"max-validity", p256, now.Add(-time.Minute), now.Add(14*24*time.Hour - time.Minute), true},
		{"too-long", p256, now.Add(-time.Minute), now.Add(14 * 24 * time.Hour), false},
		{"not-yet-valid", p256, now.Add(time.Minute), now.Add(time.Hour), false},
		{"expired", p256, now.Add(-time.Hour), now, false},
		{"p384", p384, now.Add(-time.Minute), now.Add(time.Hour), false},
		{"ed25519", ed, now.Add(-time.Minute), now.Add(time.Hour), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			template := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: tc.start, NotAfter: tc.end}
			der, err := x509.CreateCertificate(rand.Reader, template, template, tc.key.Public(), tc.key)
			if err != nil {
				t.Fatal(err)
			}
			leaf, err := x509.ParseCertificate(der)
			if err != nil {
				t.Fatal(err)
			}
			got := BrowserTestCertificateHash(leaf, now)
			if tc.want {
				sum := sha256.Sum256(der)
				if got != hex.EncodeToString(sum[:]) {
					t.Fatal("hash does not authenticate the exact DER leaf")
				}
			} else if got != "" {
				t.Fatal("ineligible certificate advertised as hash-authenticated")
			}
		})
	}
}

func TestBrowserTokenScopeAndExpiry(t *testing.T) {
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	claim := BrowserTestClaim{ID: strings.Repeat("a", 32), Boot: strings.Repeat("b", 32), GateID: 1, UserID: 2, Purpose: "webrtc", Bytes: 1024, Expires: now.Add(time.Minute)}
	token, err := SignBrowserTest(claim, key)
	if err != nil {
		t.Fatal(err)
	}
	got, err := VerifyBrowserTest(token, pub, now)
	if err != nil || got.ID != claim.ID || !got.Expires.Equal(claim.Expires) {
		t.Fatal(got, err)
	}
	if _, err = VerifyBrowserTest(token, pub, now.Add(2*time.Minute)); err == nil {
		t.Fatal("expired token accepted")
	}
	for _, mutate := range []func(*BrowserTestClaim){
		func(c *BrowserTestClaim) { c.Bytes = BrowserTestMaxBytes + 1 }, func(c *BrowserTestClaim) { c.Purpose = "forward" }, func(c *BrowserTestClaim) { c.ID = strings.Repeat("z", 32) }, func(c *BrowserTestClaim) { c.Expires = now.Add(time.Hour) },
	} {
		invalid := claim
		mutate(&invalid)
		if _, err = SignBrowserTest(invalid, key); err == nil {
			t.Fatal("invalid claim signed")
		}
	}
	parts := strings.Split(token, ".")
	raw, _ := base64.RawURLEncoding.DecodeString(parts[0])
	raw[0] ^= 1
	if _, err = VerifyBrowserTest(base64.RawURLEncoding.EncodeToString(raw)+"."+parts[1], pub, now); err == nil {
		t.Fatal("modified token accepted")
	}
}

func TestBrowserPlanAddresses(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	p := BrowserTestPlan{GateID: 1, Port: 23001, RTCPort: 23002, CertID: 1, Host: "::1", Origin: "http://[::1]:22340", PublicKey: base64.RawURLEncoding.EncodeToString(pub)}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{"", "host:443", "user@host", "host/path", "host?query", "[::1]", "host%00"} {
		p.Host = host
		if p.Validate() == nil {
			t.Fatalf("accepted host %q", host)
		}
	}
	p.Host = "gate.example.test"
	for _, origin := range []string{"http://public.example.test", "https://center.example.test/path", "https://user@center.example.test", "https://center.example.test?"} {
		p.Origin = origin
		if p.Validate() == nil {
			t.Fatalf("accepted origin %q", origin)
		}
	}
}
