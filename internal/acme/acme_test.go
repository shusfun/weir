package acme_test

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/shusfun/weir/internal/acme"
	"github.com/shusfun/weir/internal/acme/acmetest"
)

func TestIssueIPCertificateWithShortLivedProfile(t *testing.T) {
	ca := acmetest.New(t)
	var mu sync.Mutex
	served := map[string]string{}
	ca.Fetch = func(_, token string) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		if v, ok := served[token]; ok {
			return v, nil
		}
		return "", errors.New("not served")
	}
	key, _ := acme.NewKey()
	c := &acme.Client{Directory: ca.Directory(), Key: key, Email: "ops@example.com"}
	chain, keyPEM, err := c.Issue(t.Context(), "203.0.113.10", acme.ProfileShortLived, func(_ context.Context, ch acme.Challenge) error {
		mu.Lock()
		served[ch.Token] = ch.KeyAuth
		mu.Unlock()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	ident, profile := ca.Order()
	if ident["type"] != "ip" || ident["value"] != "203.0.113.10" || profile != "shortlived" {
		t.Fatalf("order = %v profile %q", ident, profile)
	}
	block, _ := pem.Decode([]byte(chain))
	x, err := x509.ParseCertificate(block.Bytes)
	if err != nil || len(x.IPAddresses) != 1 || x.IPAddresses[0].String() != "203.0.113.10" {
		t.Fatalf("cert = %v %v", x, err)
	}
	k, err := acme.DecodeKey(keyPEM)
	if err != nil || !k.PublicKey.Equal(x.PublicKey) {
		t.Fatal("key does not match certificate")
	}
	if strings.Count(chain, "BEGIN CERTIFICATE") != 2 {
		t.Fatal("chain must include the intermediate")
	}
}

func TestIssueFailsWhenChallengeIsNotServed(t *testing.T) {
	ca := acmetest.New(t)
	ca.Fetch = func(string, string) (string, error) { return "", errors.New("connection refused") }
	key, _ := acme.NewKey()
	c := &acme.Client{Directory: ca.Directory(), Key: key}
	_, _, err := c.Issue(t.Context(), "edge.example.com", "", func(context.Context, acme.Challenge) error { return nil })
	if err == nil {
		t.Fatal("issue must fail when the challenge is not served")
	}
}
