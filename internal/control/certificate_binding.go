package control

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/imzyb/MiGate/ent"
	"github.com/imzyb/MiGate/ent/resource"
	"github.com/imzyb/MiGate/packages/contracts"
)

func (s *Server) checkCertificateBinding(c *gin.Context, deviceID int, payload map[string]any) bool {
	d, err := s.Store.Device.Get(c.Request.Context(), deviceID)
	if err != nil {
		failure(c, 404, "device_not_found")
		return false
	}
	capable := false
	if caps, ok := d.Metadata["capabilities"].([]any); ok {
		for _, cap := range caps {
			capable = capable || cap == "certificate.bind.v1"
		}
	}
	if !capable {
		failure(c, 400, "agent_capability_required")
		return false
	}
	b, err := contracts.DecodeCertificateBinding(payload)
	if err != nil {
		failure(c, 400, "invalid_certificate_binding")
		return false
	}
	cert, err := s.Store.Resource.Query().Where(resource.KindEQ("device-certificate"), resource.OwnerIDEQ(deviceID), resource.KeyEQ(fmt.Sprintf("%d:%d", deviceID, b.CertificateID))).Only(c.Request.Context())
	if err != nil {
		failure(c, 409, "certificate_binding_resource_changed")
		return false
	}
	block, _ := pem.Decode([]byte(text(cert.Data["certificate_pem"])))
	if block == nil {
		failure(c, 400, "certificate_invalid")
		return false
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil || time.Now().Before(leaf.NotBefore) || !time.Now().Before(leaf.NotAfter) {
		failure(c, 400, "certificate_not_currently_valid")
		return false
	}
	for _, target := range b.Inbounds {
		r, err := s.Store.Resource.Query().Where(resource.KindEQ("device-inbound"), resource.OwnerIDEQ(deviceID), resource.KeyEQ(fmt.Sprintf("%d:%d", deviceID, target.ID))).Only(c.Request.Context())
		if err != nil || int64(number(r.Data["certificate_id"])) != target.PreviousCertificateID {
			failure(c, 409, "certificate_binding_resource_changed")
			return false
		}
		security, protocol := text(r.Data["security"]), text(r.Data["protocol"])
		if !(security == "tls" || protocol == "trojan" && security != "reality" || protocol == "hysteria2" || protocol == "tuic") {
			failure(c, 400, "certificate_binding_requires_tls")
			return false
		}
		if sni := text(r.Data["tls_sni"]); sni != "" && leaf.VerifyHostname(sni) != nil {
			failure(c, 400, "certificate_hostname_mismatch")
			return false
		}
	}
	return true
}

func saveCertificateBinding(ctx context.Context, tx *ent.Tx, deviceID int, payload map[string]any) error {
	b, err := contracts.DecodeCertificateBinding(payload)
	if err != nil {
		return err
	}
	for _, target := range b.Inbounds {
		r, err := tx.Resource.Query().Where(resource.KindEQ("device-inbound"), resource.OwnerIDEQ(deviceID), resource.KeyEQ(fmt.Sprintf("%d:%d", deviceID, target.ID))).Only(ctx)
		if err != nil {
			return err
		}
		if int64(number(r.Data["certificate_id"])) != target.PreviousCertificateID {
			return errors.New("certificate_binding_resource_changed")
		}
		r.Data["certificate_id"] = b.CertificateID
		// File paths are helper-owned and will be regenerated from the selected
		// certificate. Never restore stale raw paths from an old UI snapshot.
		delete(r.Data, "tls_cert_file")
		delete(r.Data, "tls_key_file")
		if _, err = tx.Resource.UpdateOne(r).SetData(r.Data).Save(ctx); err != nil {
			return err
		}
	}
	return nil
}
