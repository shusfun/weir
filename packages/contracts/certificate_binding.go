package contracts

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

type CertificateBindingTarget struct {
	ID                    int64 `json:"id"`
	PreviousCertificateID int64 `json:"previous_certificate_id"`
}
type CertificateBinding struct {
	CertificateID int64                      `json:"certificate_id"`
	Inbounds      []CertificateBindingTarget `json:"inbounds"`
}

func DecodeCertificateBinding(payload map[string]any) (CertificateBinding, error) {
	var out CertificateBinding
	body, err := json.Marshal(payload)
	if err != nil {
		return out, err
	}
	d := json.NewDecoder(bytes.NewReader(body))
	d.DisallowUnknownFields()
	if d.Decode(&out) != nil || d.Decode(&struct{}{}) != io.EOF || out.CertificateID <= 0 || len(out.Inbounds) == 0 || len(out.Inbounds) > 100 {
		return out, errors.New("invalid_certificate_binding")
	}
	seen := map[int64]bool{}
	for _, in := range out.Inbounds {
		if in.ID <= 0 || in.PreviousCertificateID < 0 || seen[in.ID] {
			return out, errors.New("invalid_certificate_binding")
		}
		seen[in.ID] = true
	}
	return out, nil
}
