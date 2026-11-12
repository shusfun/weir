package accountsecurity

import (
	"context"
	"errors"
	"net"
	"strings"

	"golang.org/x/net/idna"
)

var (
	ErrInvalidEmail    = errors.New("invalid_email")
	ErrEmailDenied     = errors.New("email_suffix_denied")
	ErrDisposableEmail = errors.New("email_disposable")
	ErrNoMX            = errors.New("email_mx_missing")
	ErrMXUnavailable   = errors.New("email_mx_unavailable")
)

type AliasRule struct {
	Domain          string `json:"domain"`
	CanonicalDomain string `json:"canonical_domain"`
	StripPlus       bool   `json:"strip_plus"`
	StripDots       bool   `json:"strip_dots"`
}

type EmailPolicy struct {
	Allow           []string    `json:"email_allow_suffixes"`
	Deny            []string    `json:"email_deny_suffixes"`
	DisposableAllow []string    `json:"email_disposable_exceptions"`
	DisposableDeny  []string    `json:"email_disposable_additions"`
	Aliases         []AliasRule `json:"email_alias_rules"`
}

func DefaultEmailPolicy() EmailPolicy {
	return EmailPolicy{Allow: []string{}, Deny: []string{}, DisposableAllow: []string{}, DisposableDeny: []string{}, Aliases: []AliasRule{
		{Domain: "gmail.com", CanonicalDomain: "gmail.com", StripPlus: true, StripDots: true},
		{Domain: "googlemail.com", CanonicalDomain: "gmail.com", StripPlus: true, StripDots: true},
		{Domain: "outlook.com", CanonicalDomain: "outlook.com", StripPlus: true},
		{Domain: "hotmail.com", CanonicalDomain: "hotmail.com", StripPlus: true},
		{Domain: "live.com", CanonicalDomain: "live.com", StripPlus: true},
	}}
}

func NormalizeDomain(raw string) (string, error) {
	d, err := idna.Lookup.ToASCII(strings.ToLower(strings.TrimSuffix(raw, ".")))
	if err != nil || len(d) > 253 || !strings.Contains(d, ".") || strings.ContainsAny(raw, " \t\r\n/@:*[]\\") {
		return "", ErrInvalidEmail
	}
	for _, label := range strings.Split(d, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", ErrInvalidEmail
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return "", ErrInvalidEmail
			}
		}
	}
	if ip := net.ParseIP(d); ip != nil {
		return "", ErrInvalidEmail
	}
	return d, nil
}

// Mailbox is the deliverable address, not its provider-specific alias identity.
// Only the domain is converted to IDNA; SMTPUTF8 local parts are unsupported.
func Mailbox(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if strings.Count(s, "@") != 1 {
		return "", ErrInvalidEmail
	}
	local, domain, _ := strings.Cut(s, "@")
	if len(local) == 0 || len(local) > 64 || strings.HasPrefix(local, ".") || strings.HasSuffix(local, ".") || strings.Contains(local, "..") {
		return "", ErrInvalidEmail
	}
	for _, c := range local {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune(".!#$%&'*+-/=?^_`{|}~", c)) {
			return "", ErrInvalidEmail
		}
	}
	d, err := NormalizeDomain(domain)
	if err != nil || len(local)+len(d)+1 > 254 {
		return "", ErrInvalidEmail
	}
	return strings.ToLower(local) + "@" + d, nil
}

func suffix(raw string) (string, bool, error) {
	if strings.HasPrefix(raw, "@") {
		d, err := NormalizeDomain(raw[1:])
		return d, false, err
	}
	if strings.HasPrefix(raw, "*.") {
		d, err := NormalizeDomain(raw[2:])
		return d, true, err
	}
	return "", false, ErrInvalidEmail
}

func (p EmailPolicy) Validate() error {
	for _, list := range [][]string{p.Allow, p.Deny} {
		if len(list) > 1000 {
			return errors.New("too many email suffix rules")
		}
		seen := map[string]bool{}
		for _, v := range list {
			d, sub, err := suffix(v)
			key := d
			if sub {
				key = "*." + key
			}
			if err != nil || seen[key] {
				return errors.New("invalid or duplicate email suffix rule")
			}
			seen[key] = true
		}
	}
	for _, list := range [][]string{p.DisposableAllow, p.DisposableDeny} {
		if len(list) > 1000 {
			return errors.New("too many disposable email overrides")
		}
		seen := map[string]bool{}
		for _, v := range list {
			d, err := NormalizeDomain(v)
			if err != nil || seen[d] {
				return errors.New("invalid or duplicate disposable email override")
			}
			seen[d] = true
		}
	}
	if len(p.Aliases) > 100 {
		return errors.New("too many email alias rules")
	}
	seen := map[string]bool{}
	for _, r := range p.Aliases {
		d, err := NormalizeDomain(r.Domain)
		if err != nil || seen[d] {
			return errors.New("invalid or duplicate email alias domain")
		}
		seen[d] = true
		if _, err := NormalizeDomain(r.CanonicalDomain); err != nil {
			return errors.New("invalid email alias target")
		}
	}
	return nil
}

func suffixMatch(domain string, list []string) bool {
	for _, v := range list {
		d, sub, _ := suffix(v)
		if !sub && domain == d || sub && strings.HasSuffix(domain, "."+d) {
			return true
		}
	}
	return false
}

func exactDomain(domain string, list []string) bool {
	for _, v := range list {
		d, _ := NormalizeDomain(v)
		if domain == d {
			return true
		}
	}
	return false
}

// Identity applies at most one registered rule. The address used for login and
// delivery remains unchanged, including its dots, plus suffix and domain.
func (p EmailPolicy) Identity(mailbox string) (string, error) {
	email, err := Mailbox(mailbox)
	if err != nil {
		return "", err
	}
	local, domain, _ := strings.Cut(email, "@")
	for _, r := range p.Aliases {
		d, _ := NormalizeDomain(r.Domain)
		if d != domain {
			continue
		}
		if r.StripPlus {
			local, _, _ = strings.Cut(local, "+")
		}
		if r.StripDots {
			local = strings.ReplaceAll(local, ".", "")
		}
		domain, _ = NormalizeDomain(r.CanonicalDomain)
		break
	}
	if local == "" || domain == "" {
		return "", ErrInvalidEmail
	}
	return local + "@" + domain, nil
}

func (p EmailPolicy) CheckAddress(email string) error {
	address, err := Mailbox(email)
	if err != nil {
		return err
	}
	_, domain, _ := strings.Cut(address, "@")
	if suffixMatch(domain, p.Deny) || len(p.Allow) > 0 && !suffixMatch(domain, p.Allow) {
		return ErrEmailDenied
	}
	if exactDomain(domain, p.DisposableDeny) || bundledDisposable(domain) && !exactDomain(domain, p.DisposableAllow) {
		return ErrDisposableEmail
	}
	return nil
}

type MXResolver interface {
	LookupMX(context.Context, string) ([]*net.MX, error)
}

func CheckMX(ctx context.Context, resolver MXResolver, email string) error {
	address, err := Mailbox(email)
	if err != nil {
		return err
	}
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	_, domain, _ := strings.Cut(address, "@")
	records, err := resolver.LookupMX(ctx, domain)
	if err != nil {
		var dns *net.DNSError
		if errors.As(err, &dns) && dns.IsNotFound {
			return ErrNoMX
		}
		return ErrMXUnavailable
	}
	// RFC 7505 null MX explicitly declares that the domain accepts no mail.
	for _, mx := range records {
		if mx != nil && mx.Host == "." {
			return ErrNoMX
		}
	}
	for _, mx := range records {
		if mx == nil || mx.Host == "." || mx.Host == "" {
			continue
		}
		if _, err := NormalizeDomain(mx.Host); err == nil {
			return nil
		}
	}
	return ErrNoMX
}
