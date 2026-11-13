package hoststats

import (
	"crypto/x509"
	"encoding/pem"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/shusfun/weir/packages/contracts"
)

// ObserveCertificates samples only certificates referenced by applied inbounds.
// A missing or invalid referenced file makes the summary unknown, never healthy.
func ObserveCertificates(dir string, ids []int64, now time.Time) *contracts.HealthCheck {
	out := &contracts.HealthCheck{}
	seen := map[int64]bool{}
	var earliest time.Time
	var earliestID int64
	for _, id := range ids {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		raw, err := os.ReadFile(filepath.Join(dir, strconv.FormatInt(id, 10)+".crt"))
		if err != nil {
			return out
		}
		block, _ := pem.Decode(raw)
		if block == nil {
			return out
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return out
		}
		if earliest.IsZero() || cert.NotAfter.Before(earliest) {
			earliest, earliestID = cert.NotAfter, id
		}
	}
	if earliest.IsZero() {
		return out
	}
	days := int(math.Floor(earliest.Sub(now).Hours() / 24))
	out.CertDays = &days
	out.CertID = &earliestID
	out.CertExpiresAt = &earliest
	out.CertObservedAt = &now
	return out
}
