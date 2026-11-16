package contracts

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"strings"
	"time"
)

const ConfigBrowserTest = "browser-test"
const BrowserTestMaxBytes int64 = 16 << 20
const BrowserTestLifetime = 2 * time.Minute

// WebTransport certificate hashes require a short-lived P-256 certificate.
// The authenticated center supplies the hash of the configured leaf, never a
// value learned from the unauthenticated test endpoint.
func BrowserTestCertificateHash(leaf *x509.Certificate, now time.Time) string {
	key, ok := leaf.PublicKey.(*ecdsa.PublicKey)
	if !ok || key.Curve != elliptic.P256() || leaf.NotAfter.Sub(leaf.NotBefore) > 14*24*time.Hour || now.Before(leaf.NotBefore) || !now.Before(leaf.NotAfter) {
		return ""
	}
	sum := sha256.Sum256(leaf.Raw)
	return hex.EncodeToString(sum[:])
}

var ErrBrowserTestExpired = errors.New("browser test expired")

func BrowserTestIDValid(id string) bool {
	raw, err := hex.DecodeString(id)
	return err == nil && len(raw) == 16 && id == strings.ToLower(id)
}

// BrowserTestPlan exposes only bounded test data, never an outbound destination.
type BrowserTestPlan struct {
	GateID    int      `json:"gate_id"`
	Port      int      `json:"port"`
	RTCPort   int      `json:"rtc_port"`
	CertID    int64    `json:"cert_id"`
	Host      string   `json:"host"`
	Origin    string   `json:"origin"`
	PublicKey string   `json:"public_key"`
	Tokens    []string `json:"tokens,omitempty"`
}

func (p BrowserTestPlan) Validate() error {
	key, err := base64.RawURLEncoding.DecodeString(p.PublicKey)
	origin, e := url.Parse(p.Origin)
	host, h := url.Parse("https://" + net.JoinHostPort(p.Host, "443"))
	if p.GateID < 1 || p.Port < 1024 || p.Port > 65535 || p.RTCPort < 1024 || p.RTCPort > 65535 || p.Port == p.RTCPort || p.CertID < 1 || err != nil || len(key) != ed25519.PublicKeySize || e != nil || origin.Host == "" || origin.User != nil || origin.RawQuery != "" || origin.ForceQuery || origin.Fragment != "" || origin.Path != "" || (origin.Scheme != "https" && !(origin.Scheme == "http" && (origin.Hostname() == "localhost" || origin.Hostname() == "127.0.0.1" || origin.Hostname() == "::1"))) || p.Host == "" || strings.ContainsAny(p.Host, " /\\\t\r\n%?#@[]") || (strings.Contains(p.Host, ":") && net.ParseIP(p.Host) == nil) || h != nil || host.Hostname() != p.Host || host.User != nil || host.Path != "" || host.RawQuery != "" || host.Fragment != "" || len(p.Tokens) > 16 {
		return errors.New("invalid browser test plan")
	}
	return nil
}

type BrowserTestClaim struct {
	ID      string    `json:"id"`
	GateID  int       `json:"gate_id"`
	UserID  int       `json:"user_id"`
	Boot    string    `json:"boot"`
	Purpose string    `json:"purpose"`
	Bytes   int64     `json:"bytes"`
	Expires time.Time `json:"expires"`
	TraceID string    `json:"trace_id"`
}

func (c BrowserTestClaim) Validate(now time.Time) error {
	if !BrowserTestIDValid(c.ID) || c.GateID < 1 || c.UserID < 1 || !BrowserTestIDValid(c.Boot) || (c.TraceID != "" && !BrowserTestIDValid(c.TraceID)) || c.Bytes < 1 || c.Bytes > BrowserTestMaxBytes || c.Expires.IsZero() || c.Expires.After(now.Add(BrowserTestLifetime+5*time.Second)) || (c.Purpose != "https" && c.Purpose != "webrtc" && c.Purpose != "webtransport") {
		return errors.New("invalid browser test claim")
	}
	if !c.Expires.After(now) {
		return ErrBrowserTestExpired
	}
	return nil
}

func SignBrowserTest(c BrowserTestClaim, key ed25519.PrivateKey) (string, error) {
	if len(key) != ed25519.PrivateKeySize {
		return "", errors.New("invalid signing key")
	}
	if err := c.Validate(time.Now()); err != nil {
		return "", err
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw) + "." + base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, raw)), nil
}

func VerifyBrowserTest(token string, key ed25519.PublicKey, now time.Time) (BrowserTestClaim, error) {
	var c BrowserTestClaim
	a, b, ok := strings.Cut(token, ".")
	if !ok || len(token) > 2048 || len(key) != ed25519.PublicKeySize {
		return c, errors.New("invalid test token")
	}
	raw, err := base64.RawURLEncoding.DecodeString(a)
	sig, e := base64.RawURLEncoding.DecodeString(b)
	if err != nil || e != nil || !ed25519.Verify(key, raw, sig) || DecodeStrict(raw, &c, 2048) != nil {
		return c, errors.New("invalid test token")
	}
	return c, c.Validate(now)
}

type BrowserTestUsage struct {
	ID    string `json:"id"`
	Bytes int64  `json:"bytes"`
}
type BrowserTestObservation struct {
	Healthy    bool               `json:"healthy"`
	Reason     string             `json:"reason,omitempty"`
	Boot       string             `json:"boot"`
	ObservedAt time.Time          `json:"observed_at"`
	Sessions   []BrowserTestUsage `json:"sessions"`
}
