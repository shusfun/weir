package agent

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type ManagedCertificate struct {
	ID          int64  `json:"id"`
	Certificate string `json:"certificate_pem"`
	Key         string `json:"key_pem"`
}

func (c ManagedCertificate) Validate() (*x509.Certificate, error) {
	leaf, err := c.parse()
	if err != nil {
		return nil, err
	}
	if time.Now().Before(leaf.NotBefore) || !time.Now().Before(leaf.NotAfter) {
		return nil, errors.New("certificate_not_currently_valid")
	}
	return leaf, nil
}

func (c ManagedCertificate) parse() (*x509.Certificate, error) {
	if c.ID <= 0 || len(c.Certificate) > 65536 || len(c.Key) > 16384 {
		return nil, errors.New("invalid certificate size or ID")
	}
	pair, err := tls.X509KeyPair([]byte(c.Certificate), []byte(c.Key))
	if err != nil || len(pair.Certificate) == 0 {
		return nil, errors.New("certificate_key_mismatch")
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, errors.New("certificate_invalid")
	}
	return leaf, nil
}

// Content-addressed versions avoid overwriting certificate material referenced
// by a running core or rollback config. Retired versions remain owned config.
func materializeCertificates(state *State) (func(), error) {
	if len(state.Certificates) > 16 {
		return nil, errors.New("certificate limit")
	}
	created := []string{}
	cleanup := func() {
		for _, path := range created {
			os.Remove(path)
		}
		_ = newCertificateHistoryManager().forgetMissing()
	}
	dir := "/etc/migate-gate/certs"
	if len(state.Certificates) > 0 {
		if err := os.MkdirAll(dir, 0750); err != nil {
			return cleanup, err
		}
		info, err := os.Lstat(dir)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return cleanup, errors.New("unsafe certificate directory")
		}
	}
	seen := map[int64]bool{}
	for _, c := range state.Certificates {
		if seen[c.ID] {
			return cleanup, errors.New("duplicate certificate")
		}
		seen[c.ID] = true
		// A retained expired certificate must not prevent revocation or a
		// configuration update on another inbound. Disable its listeners below.
		leaf, err := c.parse()
		if err != nil {
			return cleanup, err
		}
		hash := sha256.Sum256([]byte(c.Certificate + c.Key))
		base := filepath.Join(dir, fmt.Sprintf("%d-%x", c.ID, hash[:16]))
		for suffix, body := range map[string]string{".pem": c.Certificate, ".key": c.Key} {
			path := base + suffix
			f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0640)
			if os.IsExist(err) {
				info, e := os.Lstat(path)
				if e != nil || !info.Mode().IsRegular() {
					return cleanup, errors.New("unsafe certificate file")
				}
				old, e := os.ReadFile(path)
				if e != nil || string(old) != body {
					return cleanup, errors.New("certificate version conflict")
				}
			} else {
				if err != nil {
					return cleanup, err
				}
				created = append(created, path)
				_, err = f.WriteString(body)
				if err == nil {
					err = f.Sync()
				}
				closeErr := f.Close()
				if err != nil {
					return cleanup, err
				}
				if closeErr != nil {
					return cleanup, closeErr
				}
			}
		}
		for i := range state.Inbounds {
			if state.Inbounds[i].CertificateID != c.ID {
				continue
			}
			in := &state.Inbounds[i]
			if time.Now().Before(leaf.NotBefore) || !time.Now().Before(leaf.NotAfter) {
				in.Enabled = false
			}
			if in.TLSSNI != "" && leaf.VerifyHostname(in.TLSSNI) != nil {
				return cleanup, errors.New("certificate_hostname_mismatch")
			}
			in.TLSCertFile = base + ".pem"
			in.TLSKeyFile = base + ".key"
		}
	}
	for _, in := range state.Inbounds {
		if in.CertificateID > 0 && !seen[in.CertificateID] {
			return cleanup, errors.New("certificate_missing")
		}
	}
	manager := newCertificateHistoryManager()
	for _, c := range state.Certificates {
		hash := sha256.Sum256([]byte(c.Certificate + c.Key))
		if err := manager.register(c, filepath.Join(dir, fmt.Sprintf("%d-%x", c.ID, hash[:16]))); err != nil {
			return cleanup, err
		}
	}
	return cleanup, nil
}
