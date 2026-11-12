package accountsecurity

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/shusfun/weir/internal/policydata"
)

type MailLimits struct {
	IP      int64 `json:"mail_ip_hour"`
	Network int64 `json:"mail_network_hour"`
	Email   int64 `json:"mail_email_hour"`
	Domain  int64 `json:"mail_domain_hour"`
	Site    int64 `json:"mail_site_hour"`
}

type Policy struct {
	LifetimeSeconds int    `json:"code_lifetime_seconds"`
	Attempts        int    `json:"code_attempts"`
	CooldownSeconds [3]int `json:"code_cooldown_seconds"`
	MailLimits
	EmailPolicy
	HumanPolicy
	AccessPolicy
	DisposableSnapshot string `json:"email_disposable_snapshot"`
}

func DefaultPolicy() Policy {
	return Policy{LifetimeSeconds: 600, Attempts: 5, CooldownSeconds: [3]int{60, 300, 900}, MailLimits: MailLimits{IP: 30, Network: 100, Email: 10, Domain: 200, Site: 1000}, EmailPolicy: DefaultEmailPolicy(), HumanPolicy: DefaultHumanPolicy(), AccessPolicy: DefaultAccessPolicy(), DisposableSnapshot: DisposableSourceSHA256}
}

func (p Policy) Codes() CodePolicy {
	c := CodePolicy{Lifetime: time.Duration(p.LifetimeSeconds) * time.Second, Attempts: p.Attempts}
	for i, n := range p.CooldownSeconds {
		c.Cooldown[i] = time.Duration(n) * time.Second
	}
	return c
}

func (p Policy) Validate() error {
	if err := p.AccessPolicy.Validate(); err != nil {
		return err
	}
	if err := p.HumanPolicy.Validate(); err != nil {
		return err
	}
	if p.DisposableSnapshot != DisposableSourceSHA256 {
		return errors.New("email disposable snapshot does not match bundled source")
	}
	if p.LifetimeSeconds < 60 || p.LifetimeSeconds > 1800 {
		return errors.New("code_lifetime_seconds must be between 60 and 1800")
	}
	for _, n := range p.CooldownSeconds {
		if n < 30 || n > 3600 {
			return errors.New("code_cooldown_seconds must be between 30 and 3600")
		}
	}
	if err := p.Codes().Validate(); err != nil {
		return err
	}
	for _, v := range []struct {
		name       string
		value, max int64
	}{
		{"mail_ip_hour", p.IP, 1000}, {"mail_network_hour", p.Network, 10000},
		{"mail_email_hour", p.Email, 100}, {"mail_domain_hour", p.Domain, 100000}, {"mail_site_hour", p.Site, 1000000},
	} {
		if v.value < 1 || v.value > v.max {
			return fmt.Errorf("%s must be between 1 and %d", v.name, v.max)
		}
	}
	return p.EmailPolicy.Validate()
}

func (p Policy) Values() policydata.Values {
	b, _ := json.Marshal(p)
	var values policydata.Values
	_ = json.Unmarshal(b, &values)
	return values
}

func ParsePolicy(values policydata.Values) (Policy, error) {
	var p Policy
	b, err := json.Marshal(values)
	if err != nil {
		return p, err
	}
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&p); err != nil {
		return p, errors.New("invalid account policy field type")
	}
	// Arrays must have exactly three stages, rather than json's silent padding
	// and truncation when unmarshalling into a fixed array.
	var cooldown []int
	var accessCooldown []int
	if json.Unmarshal(values["access_code_cooldown_seconds"], &accessCooldown) != nil || len(accessCooldown) != 3 {
		return p, errors.New("access code cooldown requires three stages")
	}
	if json.Unmarshal(values["code_cooldown_seconds"], &cooldown) != nil || len(cooldown) != 3 {
		return p, errors.New("email cooldown requires three stages")
	}
	return p, p.Validate()
}

func ResolvePolicy(doc policydata.Document) (policydata.View, error) {
	return policydata.Resolve(DefaultPolicy().Values(), doc, func(v policydata.Values) error { _, err := ParsePolicy(v); return err })
}
