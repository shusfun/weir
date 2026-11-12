package protection

import (
	"encoding/json"
	"errors"
	"net/netip"
	"strings"

	"github.com/shusfun/weir/internal/accountsecurity"
	"github.com/shusfun/weir/internal/policydata"
	"github.com/shusfun/weir/internal/publicnet"
)

type Policy struct {
	Enabled           bool     `json:"enabled"`
	HourlyLimit       int      `json:"hourly_limit"`
	MaxMinutes        int      `json:"max_minutes"`
	ProtectedNetworks []string `json:"protected_networks"`
	ProtectedDomains  []string `json:"protected_domains"`
	ProtectedFields   []string `json:"protected_fields"`
}

func Default() Policy {
	return Policy{HourlyLimit: 5, MaxMinutes: 1440, ProtectedNetworks: []string{}, ProtectedDomains: []string{}, ProtectedFields: []string{}}
}
func (p Policy) Validate() error {
	if p.HourlyLimit < 1 || p.HourlyLimit > 5 || p.MaxMinutes < 5 || p.MaxMinutes > 1440 || len(p.ProtectedNetworks) > 256 || len(p.ProtectedDomains) > 256 || len(p.ProtectedFields) > 64 {
		return errors.New("automatic protection policy out of bounds")
	}
	for _, value := range p.ProtectedNetworks {
		if _, err := netip.ParsePrefix(value); err != nil {
			return errors.New("invalid protected network")
		}
	}
	for _, value := range p.ProtectedDomains {
		if normalized, err := accountsecurity.NormalizeDomain(value); err != nil || normalized != value {
			return errors.New("invalid protected domain")
		}
	}
	for _, value := range p.ProtectedFields {
		if _, ok := accountsecurity.DefaultPolicy().Values()[value]; !ok {
			return errors.New("unknown protected policy field")
		}
	}
	return nil
}
func (p Policy) ProtectsIP(raw string) bool {
	ip, err := netip.ParseAddr(raw)
	if err != nil || !publicnet.Public(ip) {
		return true
	}
	ip = ip.Unmap()
	for _, raw := range p.ProtectedNetworks {
		prefix, err := netip.ParsePrefix(raw)
		if err != nil || prefix.Contains(ip) {
			return true
		}
	}
	return false
}
func (p Policy) ProtectsField(field string) bool {
	for _, value := range p.ProtectedFields {
		if field == value {
			return true
		}
	}
	return false
}

// Tighten is a closed registry of monotonic changes. Full policy replacement,
// model instructions and arbitrary keys never enter automatic execution.
func Tighten(values policydata.Values, field string, raw json.RawMessage, policy Policy) (policydata.Values, error) {
	if policy.ProtectsField(field) {
		return nil, errors.New("protected policy field")
	}
	out := policydata.Values{}
	for key, value := range values {
		out[key] = append(json.RawMessage(nil), value...)
	}
	switch field {
	case "mail_ip_hour", "mail_network_hour", "mail_email_hour", "mail_domain_hour", "mail_site_hour", "human_login_failures":
		var old, value int64
		if json.Unmarshal(values[field], &old) != nil || json.Unmarshal(raw, &value) != nil || value < 1 || value > old {
			return nil, errors.New("policy change is not a tightening")
		}
		out[field] = raw
	case "human_pow_cost", "human_pow_counter", "human_min_seconds":
		var old, value int64
		if json.Unmarshal(values[field], &old) != nil || json.Unmarshal(raw, &value) != nil || value < old {
			return nil, errors.New("policy change is not a tightening")
		}
		out[field] = raw
	case "email_deny_suffixes", "email_disposable_additions":
		var previous, additions []string
		if json.Unmarshal(values[field], &previous) != nil || json.Unmarshal(raw, &additions) != nil || len(additions) == 0 || len(additions) > 64 {
			return nil, errors.New("invalid deny-list additions")
		}
		for _, addition := range additions {
			domain := strings.TrimPrefix(strings.TrimPrefix(addition, "@"), "*.")
			if normalized, err := accountsecurity.NormalizeDomain(domain); err != nil || normalized != domain {
				return nil, errors.New("deny-list domain must be normalized")
			}
			for _, protected := range policy.ProtectedDomains {
				if protected == domain || strings.HasSuffix(protected, "."+domain) || strings.HasSuffix(domain, "."+protected) {
					return nil, errors.New("protected domain")
				}
			}
			found := false
			for _, existing := range previous {
				found = found || existing == addition
			}
			if !found {
				previous = append(previous, addition)
			}
		}
		out[field], _ = json.Marshal(previous)
	default:
		return nil, errors.New("policy field is not eligible for automatic tightening")
	}
	if _, err := accountsecurity.ParsePolicy(out); err != nil {
		return nil, err
	}
	return out, nil
}
