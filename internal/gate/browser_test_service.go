package gate

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"time"

	"github.com/shusfun/weir/internal/browsertest"
	"github.com/shusfun/weir/packages/contracts"
)

// Caller holds h.mu, shared with apply, status and identity revocation.
func (h *Helper) closeBrowserTest() {
	if h.probe != nil {
		h.probe.Close()
		h.probe = nil
	}
	h.probePlan = nil
	h.probeCertificate = ""
}

func (h *Helper) browserTestObservation() *contracts.BrowserTestObservation {
	if h.probe == nil {
		return nil
	}
	observation := h.probe.Observation()
	return &observation
}

func (h *Helper) applyBrowserTest(plan *contracts.BrowserTestPlan) error {
	if plan == nil {
		h.closeBrowserTest()
		return nil
	}
	if err := plan.Validate(); err != nil {
		return err
	}
	cert, err := tls.LoadX509KeyPair(filepath.Join(h.P.CertsDir, fmt.Sprint(plan.CertID)+".crt"), filepath.Join(h.P.CertsDir, fmt.Sprint(plan.CertID)+".key"))
	if err != nil {
		return errors.New("测试证书不可读")
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return err
	}
	if err = leaf.VerifyHostname(plan.Host); err != nil {
		return errors.New("测试地址与证书不匹配")
	}
	if time.Now().Before(leaf.NotBefore) || !time.Now().Before(leaf.NotAfter) {
		return errors.New("测试证书不在有效期内")
	}
	sum := sha256.Sum256(cert.Certificate[0])
	fingerprint := hex.EncodeToString(sum[:])
	wanted := *plan
	wanted.Tokens = append([]string(nil), plan.Tokens...)
	static := wanted
	static.Tokens = nil
	var current *contracts.BrowserTestPlan
	if h.probePlan != nil {
		v := *h.probePlan
		v.Tokens = nil
		current = &v
	}
	if h.probe != nil && reflect.DeepEqual(current, &static) && h.probeCertificate == fingerprint && h.probe.Observation().Healthy {
		if err := h.probe.Sync(wanted.Tokens); err != nil {
			return err
		}
		h.probePlan = &wanted
		return nil
	}
	previous := h.probePlan
	h.closeBrowserTest()
	next, err := browsertest.Start(static, cert)
	if err != nil {
		if previous != nil {
			if recovery := h.applyBrowserTest(previous); recovery != nil {
				return errors.Join(err, fmt.Errorf("测试服务回滚失败：%w", recovery))
			}
		}
		return err
	}
	if err = next.Sync(wanted.Tokens); err != nil {
		next.Close()
		return err
	}
	h.probe, h.probePlan, h.probeCertificate = next, &wanted, fingerprint
	return nil
}
