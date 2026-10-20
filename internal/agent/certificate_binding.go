package agent

import (
	"errors"
	"fmt"

	"github.com/imzyb/MiGate/internal/db"
	"github.com/imzyb/MiGate/packages/contracts"
)

func certificateBindable(in db.Inbound) bool {
	return in.Security == "tls" || in.Protocol == "trojan" && in.Security != "reality" || in.Protocol == "hysteria2" || in.Protocol == "tuic"
}

func (a *Agent) bindCertificate(t contracts.Task) error {
	b, err := contracts.DecodeCertificateBinding(t.Payload)
	if err != nil {
		return err
	}
	var cert *ManagedCertificate
	for i := range a.State.Certificates {
		if a.State.Certificates[i].ID == b.CertificateID {
			cert = &a.State.Certificates[i]
			break
		}
	}
	if cert == nil {
		return errors.New("certificate_missing")
	}
	leaf, err := cert.Validate()
	if err != nil {
		return err
	}
	indices := make([]int, 0, len(b.Inbounds))
	for _, target := range b.Inbounds {
		index := -1
		for i, in := range a.State.Inbounds {
			if in.ID == target.ID {
				index = i
				break
			}
		}
		if index < 0 {
			return errors.New("certificate_binding_inbound_missing")
		}
		in := a.State.Inbounds[index]
		if a.State.Versions[fmt.Sprintf("inbound:%d", in.ID)] > t.Revision {
			return errors.New("stale_resource_revision")
		}
		if in.CertificateID != target.PreviousCertificateID {
			return errors.New("certificate_binding_changed")
		}
		if !certificateBindable(in) {
			return errors.New("certificate_binding_requires_tls")
		}
		if in.TLSSNI != "" && leaf.VerifyHostname(in.TLSSNI) != nil {
			return errors.New("certificate_hostname_mismatch")
		}
		indices = append(indices, index)
	}
	if a.State.Versions == nil {
		a.State.Versions = map[string]int64{}
	}
	for _, i := range indices {
		a.State.Inbounds[i].CertificateID = b.CertificateID
		a.State.Inbounds[i].TLSCertFile = ""
		a.State.Inbounds[i].TLSKeyFile = ""
		a.State.Versions[fmt.Sprintf("inbound:%d", a.State.Inbounds[i].ID)] = t.Revision
	}
	return nil
}
