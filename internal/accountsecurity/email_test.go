package accountsecurity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"testing"

	"github.com/shusfun/weir/internal/policydata"
)

func TestMailboxAndProviderIdentity(t *testing.T) {
	p := DefaultEmailPolicy()
	for _, tc := range []struct{ input, address, identity string }{
		{" A.B+tag@GMAIL.COM. ", "a.b+tag@gmail.com", "ab@gmail.com"},
		{"a.b+tag@googlemail.com", "a.b+tag@googlemail.com", "ab@gmail.com"},
		{"a.b+tag@outlook.com", "a.b+tag@outlook.com", "a.b@outlook.com"},
		{"a.b+tag@example.com", "a.b+tag@example.com", "a.b+tag@example.com"},
		{"user@例子.公司.", "user@xn--fsqu00a.xn--55qx5d", "user@xn--fsqu00a.xn--55qx5d"},
	} {
		mail, err := Mailbox(tc.input)
		identity, identityErr := p.Identity(mail)
		if err != nil || identityErr != nil || mail != tc.address || identity != tc.identity {
			t.Fatalf("%q => %q / %q: %v %v", tc.input, mail, identity, err, identityErr)
		}
	}
	for _, s := range []string{"a@@example.com", "a@127.0.0.1", "a@[::1]", "a@-example.com", "a@foo..com", "a b@example.com", "用户@example.com", "a..b@example.com", "a\r\n@example.com", "a@foo.com/", "a@foo.com.."} {
		if _, err := Mailbox(s); err == nil {
			t.Fatal("invalid mailbox accepted", s)
		}
	}
}

func TestEmailSuffixPolicyAndDisposableSnapshot(t *testing.T) {
	p := DefaultEmailPolicy()
	p.Allow = []string{"@例子.公司.", "*.example.com", "@gmail.com"}
	p.Deny = []string{"@bad.example.com", "*.blocked.example.com"}
	for _, tc := range []struct {
		email   string
		allowed bool
	}{
		{"a@xn--fsqu00a.xn--55qx5d", true}, {"a@sub.example.com", true}, {"a@example.com", false},
		{"a@bad.example.com", false}, {"a@sub.blocked.example.com", false}, {"a@blocked.example.com", true},
		{"a@notexample.com", false}, {"a@gmail.com", true},
	} {
		if err := p.CheckAddress(tc.email); (err == nil) != tc.allowed {
			t.Fatal(tc, err)
		}
	}
	p = DefaultEmailPolicy()
	for _, domain := range []string{"mailinator.com", "sub.mailinator.com", "temp-mail.org"} {
		if !errors.Is(p.CheckAddress("a@"+domain), ErrDisposableEmail) {
			t.Fatal("disposable accepted", domain)
		}
	}
	p.DisposableAllow = []string{"mailinator.com"}
	if err := p.CheckAddress("a@mailinator.com"); err != nil {
		t.Fatal(err)
	}
	p.DisposableDeny = []string{"mailinator.com"}
	if !errors.Is(p.CheckAddress("a@mailinator.com"), ErrDisposableEmail) {
		t.Fatal("explicit deny must win")
	}
	h := sha256.Sum256([]byte(disposableSource))
	if hex.EncodeToString(h[:]) != DisposableSourceSHA256 {
		t.Fatal("unreviewed disposable source change")
	}
	if !strings.Contains(disposableSource, "mailinator.com") {
		t.Fatal("empty disposable source")
	}
}

func TestEmailPolicyLayersRejectInvalidAndExecutableRules(t *testing.T) {
	for _, raw := range []string{
		`{"email_allow_suffixes":["example.com"]}`, `{"email_deny_suffixes":["@example.com","@EXAMPLE.COM."]}`,
		`{"email_disposable_additions":["*.example.com"]}`, `{"email_alias_rules":[{"domain":"example.com","canonical_domain":"example.com","exec":"shell"}]}`,
		`{"email_alias_rules":[{"domain":"example.com","canonical_domain":""}]}`,
	} {
		doc := policydata.Empty()
		if err := json.Unmarshal([]byte(raw), &doc.External); err != nil {
			t.Fatal(err)
		}
		if _, err := ResolvePolicy(doc); err == nil {
			t.Fatal("invalid source policy accepted", raw)
		}
	}
}

type mxFunc func(context.Context, string) ([]*net.MX, error)

func (f mxFunc) LookupMX(ctx context.Context, d string) ([]*net.MX, error) { return f(ctx, d) }

func TestMXFailuresAreNotSuccess(t *testing.T) {
	for _, tc := range []struct {
		records []*net.MX
		failure error
		want    error
	}{
		{[]*net.MX{{Host: "mx.example.com."}}, nil, nil}, {nil, nil, ErrNoMX},
		{[]*net.MX{{Host: "."}}, nil, ErrNoMX},
		{nil, &net.DNSError{IsNotFound: true}, ErrNoMX}, {nil, &net.DNSError{IsTimeout: true}, ErrMXUnavailable},
		{nil, context.Canceled, ErrMXUnavailable},
	} {
		err := CheckMX(t.Context(), mxFunc(func(context.Context, string) ([]*net.MX, error) { return tc.records, tc.failure }), "a@example.com")
		if !errors.Is(err, tc.want) {
			t.Fatalf("MX result %v, want %v", err, tc.want)
		}
	}
}
