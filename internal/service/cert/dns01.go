package cert

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"golang.org/x/crypto/acme"
	"net"
	"net/http"
	"net/mail"
	"strings"
	"time"
)

type TXTProvider interface {
	Present(context.Context, string, string) (func(context.Context) error, error)
}

// DNS01Issuer has no listener or file/process capability. Account and DNS
// credentials belong to the center; only signed certificates go to a Gate.
type DNS01Issuer struct {
	AccountKey   crypto.Signer
	Provider     TXTProvider
	Staging      bool
	LookupTXT    func(context.Context, string) ([]string, error)
	PollInterval time.Duration
	HTTPClient   *http.Client
}

func ValidateDNS01Request(req IssueRequest) ([]string, error) {
	if len(req.Domains) < 1 || len(req.Domains) > 10 {
		return nil, errors.New("dns01_invalid_domains")
	}
	if address, err := mail.ParseAddress(req.Email); err != nil || address.Address != req.Email || len(req.Email) > 254 {
		return nil, errors.New("dns01_invalid_email")
	}
	domains := []string{}
	seen := map[string]bool{}
	for _, domain := range req.Domains {
		domain = strings.ToLower(strings.TrimSpace(domain))
		base := strings.TrimPrefix(domain, "*.")
		if !strings.Contains(base, ".") || net.ParseIP(base) != nil || len(base) > 253 || !validDomain.MatchString(base) {
			return nil, errors.New("dns01_invalid_domains")
		}
		if seen[domain] {
			return nil, errors.New("dns01_duplicate_domain")
		}
		seen[domain] = true
		domains = append(domains, domain)
	}
	return domains, nil
}
func (i DNS01Issuer) Issue(ctx context.Context, req IssueRequest) (result IssueResult, issueErr error) {
	domains, err := ValidateDNS01Request(req)
	if err != nil {
		return result, err
	}
	if i.Provider == nil || i.AccountKey == nil {
		return result, errors.New("dns01_configuration_required")
	}
	directory := acme.LetsEncryptURL
	if i.Staging {
		directory = "https://acme-staging-v02.api.letsencrypt.org/directory"
	}
	client := &acme.Client{Key: i.AccountKey, DirectoryURL: directory, UserAgent: "MiGate DNS-01", HTTPClient: i.HTTPClient}
	if _, err = client.Register(ctx, &acme.Account{Contact: []string{"mailto:" + req.Email}}, acme.AcceptTOS); err != nil {
		return result, errors.New("dns01_account_failed")
	}
	order, err := client.AuthorizeOrder(ctx, acme.DomainIDs(domains...))
	if err != nil {
		return result, errors.New("dns01_order_failed")
	}
	for _, uri := range order.AuthzURLs {
		auth, err := client.GetAuthorization(ctx, uri)
		if err != nil {
			return result, errors.New("dns01_authorization_failed")
		}
		if auth.Status == acme.StatusValid {
			continue
		}
		challenge := findChallenge(auth.Challenges, "dns-01")
		if challenge == nil {
			return result, errors.New("dns01_challenge_unavailable")
		}
		value, err := client.DNS01ChallengeRecord(challenge.Token)
		if err != nil {
			return result, errors.New("dns01_challenge_failed")
		}
		name := "_acme-challenge." + strings.TrimPrefix(auth.Identifier.Value, "*.")
		cleanup, err := i.Provider.Present(ctx, name, value)
		if err != nil {
			return result, errors.New("dns01_present_failed")
		}
		// Cleanup uses an independent bounded context even when issuance is canceled.
		defer func() {
			cleanCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := cleanup(cleanCtx); err != nil {
				issueErr = errors.New("dns01_cleanup_failed")
			}
		}()
		if err = i.waitTXT(ctx, name, value); err != nil {
			return result, errors.New("dns01_propagation_failed")
		}
		if _, err = client.Accept(ctx, challenge); err != nil {
			return result, errors.New("dns01_accept_failed")
		}
		if _, err = client.WaitAuthorization(ctx, auth.URI); err != nil {
			return result, errors.New("dns01_validation_failed")
		}
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return result, errors.New("dns01_key_failed")
	}
	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: domains[0]}, DNSNames: domains}, key)
	if err != nil {
		return result, errors.New("dns01_csr_failed")
	}
	chain, _, err := client.CreateOrderCert(ctx, order.FinalizeURL, csr, true)
	if err != nil {
		return result, errors.New("dns01_finalize_failed")
	}
	for _, der := range chain {
		result.CertPEM = append(result.CertPEM, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})...)
	}
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return IssueResult{}, errors.New("dns01_key_failed")
	}
	result.KeyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})
	return result, nil
}
func (i DNS01Issuer) waitTXT(ctx context.Context, name, value string) error {
	lookup := i.LookupTXT
	if lookup == nil {
		lookup = net.DefaultResolver.LookupTXT
	}
	interval := i.PollInterval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	deadline, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	for {
		records, err := lookup(deadline, name)
		if err == nil {
			for _, record := range records {
				if record == value {
					return nil
				}
			}
		}
		timer := time.NewTimer(interval)
		select {
		case <-deadline.Done():
			timer.Stop()
			return deadline.Err()
		case <-timer.C:
		}
	}
}
