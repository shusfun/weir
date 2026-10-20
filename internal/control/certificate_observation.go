package control

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/imzyb/MiGate/ent"
	"github.com/imzyb/MiGate/ent/resource"
)

func certificateObservation(data map[string]any, now time.Time) map[string]any {
	result := map[string]any{"category": "certificate_expiry", "status": "unknown", "severity": "warning", "reason": "certificate_metadata_invalid", "observed_at": now.Format(time.RFC3339)}
	block, _ := pem.Decode([]byte(text(data["certificate_pem"])))
	if block == nil {
		return result
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return result
	}
	result["not_before"] = leaf.NotBefore.UTC().Format(time.RFC3339)
	result["not_after"] = leaf.NotAfter.UTC().Format(time.RFC3339)
	result["remaining_seconds"] = int64(leaf.NotAfter.Sub(now).Seconds())
	switch {
	case now.Before(leaf.NotBefore):
		result["status"] = "observed"
		result["severity"] = "critical"
		result["reason"] = "certificate_not_yet_valid"
	case !now.Before(leaf.NotAfter):
		result["status"] = "observed"
		result["severity"] = "critical"
		result["reason"] = "certificate_expired"
	case leaf.NotAfter.Sub(now) <= 14*24*time.Hour:
		result["status"] = "observed"
		result["reason"] = "certificate_expires_within_14_days"
	default:
		result["status"] = "resolved"
		result["severity"] = "info"
		delete(result, "reason")
	}
	return result
}

func (s *Server) observeCertificates(ctx context.Context, now time.Time) {
	// Cursor iteration avoids hiding certificates beyond the first UI page.
	after := 0
	present := map[string]bool{}
	for ctx.Err() == nil {
		items, err := s.Store.Resource.Query().Where(resource.KindEQ("device-certificate"), resource.IDGT(after)).Order(ent.Asc("id")).Limit(100).All(ctx)
		if err != nil {
			return
		}
		if len(items) == 0 {
			break
		}
		for _, r := range items {
			after = r.ID
			present["certificate:"+r.Key] = true
			data := certificateObservation(r.Data, now)
			parts := strings.Split(r.Key, ":")
			if len(parts) != 2 {
				continue
			}
			deviceID, err := strconv.Atoi(parts[0])
			if err != nil {
				continue
			}
			data["device_id"] = deviceID
			data["certificate_id"] = number(r.Data["id"])
			s.saveSecurityObservation(ctx, fmt.Sprintf("certificate:%s", r.Key), data, data["status"] != "resolved")
		}
	}
	if ctx.Err() != nil {
		return
	}
	events, err := s.Store.Resource.Query().Where(resource.KindEQ("security-event"), resource.KeyHasPrefix("certificate:")).All(ctx)
	if err != nil {
		return
	}
	for _, event := range events {
		if !present[event.Key] && event.Data["status"] != "resolved" {
			event.Data["status"] = "resolved"
			event.Data["severity"] = "info"
			event.Data["reason"] = "certificate_removed"
			event.Data["observed_at"] = now.Format(time.RFC3339)
			s.saveSecurityObservation(ctx, event.Key, event.Data, false)
		}
	}
}
