package center

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/imzyb/MiGate/internal/store/ent"
	"github.com/imzyb/MiGate/internal/store/ent/certificate"
	"github.com/imzyb/MiGate/internal/store/ent/node"
	"github.com/imzyb/MiGate/packages/contracts"
)

func (s *Server) certRoutes(admin *gin.RouterGroup) {
	admin.GET("/certs", s.listCerts)
	admin.POST("/certs", s.createCert)
	admin.DELETE("/certs/:id", s.deleteCert)
}

// certFingerprint 是证书 DER 的 SHA-256，订阅中用来固定自签证书。
func certFingerprint(certPEM string) string {
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		return ""
	}
	sum := sha256.Sum256(block.Bytes)
	return hex.EncodeToString(sum[:])
}

// parseCertPair 校验证书与私钥匹配，返回首张证书。
func parseCertPair(certPEM, keyPEM string) (*x509.Certificate, error) {
	pair, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		return nil, errors.New("certificate and key do not match")
	}
	return x509.ParseCertificate(pair.Certificate[0])
}

// selfSigned 生成十年有效的 ECDSA 自签证书；客户端靠订阅里的指纹固定它。
func selfSigned(domain string) (string, string, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		return "", "", err
	}
	tpl := x509.Certificate{
		SerialNumber: serial, Subject: pkix.Name{CommonName: domain},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(10, 0, 0),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	if ip := net.ParseIP(domain); ip != nil {
		tpl.IPAddresses = []net.IP{ip}
	} else {
		tpl.DNSNames = []string{domain}
	}
	der, err := x509.CreateCertificate(rand.Reader, &tpl, &tpl, &key.PublicKey, key)
	if err != nil {
		return "", "", err
	}
	kder, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return "", "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kder})), nil
}

func certView(c *ent.Certificate) gin.H {
	return gin.H{"id": c.ID, "name": c.Name, "domain": c.Domain, "source": c.Source, "not_after": c.NotAfter, "fingerprint": certFingerprint(c.CertPem), "created_at": c.CreatedAt}
}

func (s *Server) listCerts(c *gin.Context) {
	items, err := s.Store.Ent.Certificate.Query().Order(ent.Asc(certificate.FieldID)).All(c.Request.Context())
	if err != nil {
		failure(c, 503, "query_failed")
		return
	}
	out := make([]gin.H, 0, len(items))
	for _, it := range items {
		out = append(out, certView(it))
	}
	c.JSON(200, gin.H{"items": out})
}

// createCert 上传证书或生成自签证书（domain 为空时用 Gate 地址无意义，要求填写）。
func (s *Server) createCert(c *gin.Context) {
	var req struct {
		Name       string `json:"name"`
		Domain     string `json:"domain"`
		SelfSigned bool   `json:"self_signed"`
		CertPEM    string `json:"cert_pem"`
		KeyPEM     string `json:"key_pem"`
	}
	if !bind(c, &req) {
		return
	}
	cert, err := s.saveCert(c, req.Name, req.Domain, req.SelfSigned, req.CertPEM, req.KeyPEM)
	if err != nil {
		failure(c, 400, err.Error())
		return
	}
	s.audit(c, "cert.create", "cert:"+strconv.Itoa(cert.ID), "ok")
	c.JSON(200, certView(cert))
}

func (s *Server) saveCert(c *gin.Context, name, domain string, self bool, certPEM, keyPEM string) (*ent.Certificate, error) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" || len(domain) > 253 {
		return nil, errors.New("invalid_domain")
	}
	source := certificate.SourceUpload
	if self {
		var err error
		if certPEM, keyPEM, err = selfSigned(domain); err != nil {
			return nil, errors.New("generate_failed")
		}
		source = certificate.SourceSelfSigned
	}
	x, err := parseCertPair(certPEM, keyPEM)
	if err != nil {
		return nil, errors.New("invalid_certificate")
	}
	sealed, err := s.Crypto.Encrypt(keyPEM)
	if err != nil {
		return nil, errors.New("encrypt_failed")
	}
	if strings.TrimSpace(name) == "" {
		name = domain
	}
	return s.Store.Ent.Certificate.Create().SetName(name).SetDomain(domain).SetSource(source).
		SetCertPem(certPEM).SetKeyPem(sealed).SetSha256(contracts.CertSum(certPEM, keyPEM)).SetNotAfter(x.NotAfter).
		Save(c.Request.Context())
}

func (s *Server) deleteCert(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		failure(c, 400, "invalid_id")
		return
	}
	ctx := c.Request.Context()
	if n, _ := s.Store.Ent.Node.Query().Where(node.CertIDEQ(id)).Count(ctx); n > 0 {
		failure(c, 409, "cert_in_use")
		return
	}
	if err = s.Store.Ent.Certificate.DeleteOneID(id).Exec(ctx); err != nil {
		failure(c, 404, "cert_not_found")
		return
	}
	s.audit(c, "cert.delete", "cert:"+strconv.Itoa(id), "ok")
	c.Status(204)
}

// agentCert 只返回本 Gate 节点引用的证书。
func (s *Server) agentCert(c *gin.Context) {
	g := gateFrom(c)
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		failure(c, 400, "invalid_id")
		return
	}
	ctx := c.Request.Context()
	if n, _ := s.Store.Ent.Node.Query().Where(node.GateIDEQ(g.ID), node.CertIDEQ(id)).Count(ctx); n == 0 {
		failure(c, 404, "cert_not_found")
		return
	}
	cert, err := s.Store.Ent.Certificate.Get(ctx, id)
	if err != nil {
		failure(c, 404, "cert_not_found")
		return
	}
	key, err := s.reveal(cert.KeyPem)
	if err != nil {
		failure(c, 500, "cert_unreadable")
		return
	}
	c.JSON(200, gin.H{"cert_pem": cert.CertPem, "key_pem": key, "sha256": cert.Sha256})
}
