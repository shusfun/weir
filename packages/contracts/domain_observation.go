package contracts

import (
	"net/netip"
	"strings"
	"time"

	"golang.org/x/net/idna"
)

const ConfigDomainObservation = "domain-observation"

// ConnectionObserver exposes only an authenticated loopback native API.
type ConnectionObserver struct {
	Port   int    `json:"port"`
	Secret string `json:"secret"`
}

type CountryNetworks struct {
	Built    time.Time      `json:"built"`
	Fetched  time.Time      `json:"fetched"`
	Prefixes []netip.Prefix `json:"prefixes"`
}

// DomainObservation contains no IPs, URLs, credentials or connection records.
// Users is an observed lower bound, or nil when native metadata has no identity.
type DomainObservation struct {
	Engine     string    `json:"engine"`
	Day        string    `json:"day"`
	Domain     string    `json:"domain"`
	NodeID     int64     `json:"node_id"`
	Bytes      int64     `json:"bytes"`
	Users      *int      `json:"users"`
	ObservedAt time.Time `json:"observed_at"`
}

type DomainCoverage struct {
	Engine     string    `json:"engine"`
	Status     string    `json:"status"` // partial / unavailable / unsupported
	Reason     string    `json:"reason"`
	ObservedAt time.Time `json:"observed_at"`
}

func ObservedDomain(raw string) string {
	if raw != strings.TrimSpace(raw) || strings.ContainsAny(raw, "/:@?#\\* ") {
		return ""
	}
	s, err := idna.Lookup.ToASCII(strings.TrimSuffix(strings.ToLower(raw), "."))
	if err != nil || len(s) > 253 || !strings.Contains(s, ".") {
		return ""
	}
	if _, err := netip.ParseAddr(s); err == nil {
		return ""
	}
	for _, label := range strings.Split(s, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return ""
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return ""
			}
		}
	}
	return s
}
