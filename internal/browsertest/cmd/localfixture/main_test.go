package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shusfun/weir/packages/contracts"
)

func TestSessionIssuerReplacesOnlyRequestedPurposeAndExpiresOldSessions(t *testing.T) {
	public, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var synced []string
	issuer := &sessionIssuer{plan: contracts.BrowserTestPlan{GateID: 1}, boot: strings.Repeat("a", 32), endpoint: "https://127.0.0.1:22443", key: key, certificateHash: strings.Repeat("b", 64), sync: func(tokens []string) error {
		synced = append([]string(nil), tokens...)
		return nil
	}}
	first, err := issuer.issue("https")
	if err != nil {
		t.Fatal(err)
	}
	rtc, err := issuer.issue("webrtc")
	if err != nil {
		t.Fatal(err)
	}
	renewed, err := issuer.issue("https")
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == renewed.ID || first.Token == renewed.Token || len(synced) != 2 {
		t.Fatal("renewal did not replace the previous HTTPS session")
	}
	for _, token := range synced {
		claim, err := contracts.VerifyBrowserTest(token, public, time.Now())
		if err != nil || claim.Boot != issuer.boot || claim.GateID != 1 {
			t.Fatalf("invalid synchronized claim: %v", err)
		}
		if claim.ID != renewed.ID && claim.ID != rtc.ID {
			t.Fatal("synchronized an unexpected or replaced session")
		}
	}
	// An idle fixture must issue fresh tokens rather than returning its expired startup token.
	stale := issuer.sessions["webrtc"]
	stale.ExpiresAt = time.Now().Add(-time.Minute)
	issuer.sessions["webrtc"] = stale
	quic, err := issuer.issue("webtransport")
	if err != nil {
		t.Fatal(err)
	}
	if quic.CertificateHash != issuer.certificateHash || first.CertificateHash != "" || rtc.CertificateHash != "" {
		t.Fatal("certificate hash must accompany only the QUIC session")
	}
	if _, found := issuer.sessions["webrtc"]; found || len(synced) != 2 {
		t.Fatal("expired session was kept")
	}
	before := issuer.sessions["https"]
	issuer.sync = func([]string) error { return errors.New("service stopped") }
	if _, err := issuer.issue("https"); err == nil || issuer.sessions["https"] != before {
		t.Fatal("failed synchronization changed the published session")
	}
}
