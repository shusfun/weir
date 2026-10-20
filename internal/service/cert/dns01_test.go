package cert

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type dnsRoundTrip func(*http.Request) (*http.Response, error)

func (f dnsRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestCloudflareOwnedTXTOnly(t *testing.T) {
	methods := []string{}
	zone := strings.Repeat("a", 32)
	record := strings.Repeat("b", 32)
	provider := CloudflareDNS{Token: "test-only-secret", ZoneID: zone, Client: &http.Client{Transport: dnsRoundTrip(func(r *http.Request) (*http.Response, error) {
		methods = append(methods, r.Method)
		if r.URL.Host != "api.cloudflare.com" || r.Header.Get("Authorization") != "Bearer test-only-secret" {
			t.Fatal("wrong credential destination")
		}
		if r.Method == "POST" {
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), `"type":"TXT"`) || !strings.Contains(string(body), `"name":"_acme-challenge.example.com"`) {
				t.Fatal("non-challenge record request")
			}
		} else if r.Method != "DELETE" || r.URL.Path != "/client/v4/zones/"+zone+"/dns_records/"+record {
			t.Fatal("cleanup outside created record")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"success":true,"result":{"id":"` + record + `"}}`))}, nil
	})}}
	cleanup, err := provider.Present(context.Background(), "_acme-challenge.example.com", "proof")
	if err != nil {
		t.Fatal(err)
	}
	if err = cleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if strings.Join(methods, ",") != "POST,DELETE" {
		t.Fatal(methods)
	}
	if _, err = provider.Present(context.Background(), "www.example.com", "proof"); err == nil {
		t.Fatal("arbitrary TXT allowed")
	}
	provider.Client.Transport = dnsRoundTrip(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 403, Body: io.NopCloser(strings.NewReader(`test-only-secret challenge-proof`))}, nil
	})
	_, err = provider.Present(context.Background(), "_acme-challenge.example.com", "proof")
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatal("provider response leaked")
	}
}
func TestDNS01ValidationAndPropagation(t *testing.T) {
	for _, domain := range []string{"http://example.com", "127.0.0.1", "*.localhost", "a..example.com", "a.example.com/path"} {
		if _, err := ValidateDNS01Request(IssueRequest{Domains: []string{domain}, Email: "a@example.com"}); err == nil {
			t.Fatal("invalid DNS domain accepted")
		}
	}
	domains, err := ValidateDNS01Request(IssueRequest{Domains: []string{"*.Example.com", "example.com"}, Email: "a@example.com"})
	if err != nil || domains[0] != "*.example.com" {
		t.Fatal("wildcard DNS validation")
	}
	calls := 0
	i := DNS01Issuer{PollInterval: time.Millisecond, LookupTXT: func(ctx context.Context, name string) ([]string, error) {
		calls++
		if calls == 1 {
			return []string{"unrelated-existing-proof"}, nil
		}
		return []string{"proof"}, nil
	}}
	if err = i.waitTXT(context.Background(), "_acme-challenge.example.com", "proof"); err != nil || calls != 2 {
		t.Fatal("propagation accepted unrelated TXT")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	i.LookupTXT = func(context.Context, string) ([]string, error) { return nil, errors.New("timeout") }
	if err = i.waitTXT(ctx, "name", "proof"); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation ignored")
	}
	if _, err = i.Issue(context.Background(), IssueRequest{Domains: []string{"example.com"}, Email: "a@example.com"}); err == nil {
		t.Fatal("issuance without configuration accepted")
	}
}
