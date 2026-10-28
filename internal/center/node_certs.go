package center

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/shusfun/weir/internal/acme"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/certificate"
	"github.com/shusfun/weir/internal/store/ent/node"
	"github.com/shusfun/weir/internal/store/ent/setting"
	"github.com/shusfun/weir/packages/contracts"
)

// 节点证书：加 VPS 不需要加 DNS 解析。建节点向导不再有证书一步，中心只决定「为这台 VPS 准备证书」：
//   - Hysteria2 / TUIC 未选证书：同一台 VPS 共用一张 10 年自签，订阅中固定指纹；
//   - Trojan、WS + TLS：VPS 公网 IP 的证书，按顺序取得：
//     1. VPS 上有 1Panel 且已填 API 密钥：Gate 经本机 1Panel 申请，1Panel 自动续签；
//     2. 没有 1Panel、Gate 上报 80 端口空闲：中心运行 ACME（shortlived），Gate 临时在 80 端口应答，到期前续签；
//     3. 都不可用：全项目共用的「节点证书域名」（兜底），中心经本机反代完成 HTTP 验证并续签。
// 证书已过期或状态不明的节点不写进订阅；手动上传保留为高级选项。

const nodeCertSettingKey = "node-certs"

// NodeCertSettings 是节点证书设置。account_key 是加密保存的 ACME 账户私钥，不返回给页面。
type NodeCertSettings struct {
	EdgeDomain string `json:"edge_domain"`
	Email      string `json:"email"`
	Staging    bool   `json:"staging"`
	AccountKey string `json:"account_key,omitempty"`
}

func (s *Server) nodeCertSettings(ctx context.Context) NodeCertSettings {
	var v NodeCertSettings
	if row, err := s.Store.Ent.Setting.Query().Where(setting.KeyEQ(nodeCertSettingKey)).Only(ctx); err == nil {
		b, _ := json.Marshal(row.Value)
		_ = json.Unmarshal(b, &v)
	}
	return v
}

func (s *Server) saveNodeCertSettings(ctx context.Context, v NodeCertSettings) error {
	b, _ := json.Marshal(v)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return s.Store.Ent.Setting.Create().SetKey(nodeCertSettingKey).SetValue(m).OnConflictColumns(setting.FieldKey).UpdateNewValues().Exec(ctx)
}

// acmeDirectory 是 ACME 服务地址（测试中替换为本地假 CA）。
var acmeDirectory = func(v NodeCertSettings) string {
	if v.Staging {
		return acme.LetsEncryptStaging
	}
	return acme.LetsEncrypt
}

// acmeClient 取得 ACME 客户端；账户私钥第一次使用时生成并加密保存。
func (s *Server) acmeClient(ctx context.Context) (*acme.Client, error) {
	v := s.nodeCertSettings(ctx)
	var key = v.AccountKey
	if key != "" {
		plain, err := s.reveal(key)
		if err != nil {
			return nil, err
		}
		k, err := acme.DecodeKey(plain)
		if err != nil {
			return nil, err
		}
		return &acme.Client{Directory: acmeDirectory(v), Key: k, Email: v.Email}, nil
	}
	k, err := acme.NewKey()
	if err != nil {
		return nil, err
	}
	plain, err := acme.EncodeKey(k)
	if err != nil {
		return nil, err
	}
	if v.AccountKey, err = s.Crypto.Encrypt(plain); err != nil {
		return nil, err
	}
	if err = s.saveNodeCertSettings(ctx, v); err != nil {
		return nil, err
	}
	return &acme.Client{Directory: acmeDirectory(v), Key: k, Email: v.Email}, nil
}

// —— 1Panel API 密钥：只转发一次，不落库、不写日志、不进审计详情 ——

type panelSecret struct {
	gate    int
	apiKey  string
	expires time.Time
}

var panelSecrets = struct {
	sync.Mutex
	m map[string]panelSecret
}{m: map[string]panelSecret{}}

func putPanelSecret(gate int, apiKey string) string {
	nonce := randomToken(24)
	panelSecrets.Lock()
	defer panelSecrets.Unlock()
	for k, v := range panelSecrets.m {
		if time.Now().After(v.expires) {
			delete(panelSecrets.m, k)
		}
	}
	panelSecrets.m[nonce] = panelSecret{gate: gate, apiKey: apiKey, expires: time.Now().Add(2 * time.Minute)}
	return nonce
}

// takePanelSecret 只能取一次，且只能由提交时指定的那台 Gate 取。
func takePanelSecret(gate int, nonce string) (string, bool) {
	panelSecrets.Lock()
	defer panelSecrets.Unlock()
	v, ok := panelSecrets.m[nonce]
	if !ok || v.gate != gate || time.Now().After(v.expires) {
		return "", false
	}
	delete(panelSecrets.m, nonce)
	return v.apiKey, true
}

func (s *Server) agentPanelSecret(c *gin.Context) {
	var req struct {
		Nonce string `json:"nonce"`
	}
	if !bind(c, &req) {
		return
	}
	key, ok := takePanelSecret(gateFrom(c).ID, req.Nonce)
	if !ok {
		failure(c, 404, "secret_not_found")
		return
	}
	c.JSON(200, gin.H{"api_key": key})
}

func (s *Server) nodeCertRoutes(admin *gin.RouterGroup) {
	admin.GET("/node-certs", s.getNodeCertSettings)
	admin.PUT("/node-certs", s.putNodeCertSettings)
	admin.PUT("/gates/:id/panel", s.putGatePanel)
	admin.DELETE("/gates/:id/panel", s.deleteGatePanel)
	admin.POST("/certs/:id/renew", s.renewCertNow)
}

func (s *Server) getNodeCertSettings(c *gin.Context) {
	v := s.nodeCertSettings(c.Request.Context())
	c.JSON(200, gin.H{"edge_domain": v.EdgeDomain, "email": v.Email, "staging": v.Staging, "center_origin": s.centerOrigin(c.Request.Context())})
}

func (s *Server) putNodeCertSettings(c *gin.Context) {
	var req struct {
		EdgeDomain string `json:"edge_domain"`
		Email      string `json:"email"`
		Staging    bool   `json:"staging"`
	}
	if !bind(c, &req) {
		return
	}
	req.EdgeDomain = strings.ToLower(strings.TrimSpace(req.EdgeDomain))
	if req.EdgeDomain != "" && (!validHost(req.EdgeDomain) || net.ParseIP(req.EdgeDomain) != nil || !strings.Contains(req.EdgeDomain, ".") || strings.Contains(req.EdgeDomain, ":")) {
		failDetail(c, 400, "invalid_edge_domain", "请填写一个域名，例如 edge.example.com", nil)
		return
	}
	if req.Email = strings.TrimSpace(req.Email); req.Email != "" && !validEmail(req.Email) {
		failure(c, 400, "invalid_email")
		return
	}
	ctx := c.Request.Context()
	v := s.nodeCertSettings(ctx)
	if v.Staging != req.Staging {
		v.AccountKey = "" // 换 ACME 服务时换账户
	}
	v.EdgeDomain, v.Email, v.Staging = req.EdgeDomain, req.Email, req.Staging
	if err := s.saveNodeCertSettings(ctx, v); err != nil {
		failure(c, 500, "save_failed")
		return
	}
	s.audit(c, "certs.settings", req.EdgeDomain, "ok")
	c.JSON(200, gin.H{"edge_domain": v.EdgeDomain, "email": v.Email, "staging": v.Staging})
}

// putGatePanel 把 1Panel API 密钥交给 Gate：中心只在内存里放 2 分钟，由 Gate 凭一次性编号取走；
// Gate 测试连接通过才保存。需要重新验证身份（这个密钥等同那台机器的 root）。
func (s *Server) putGatePanel(c *gin.Context) {
	g, ok := s.gateParam(c)
	if !ok {
		return
	}
	var req struct {
		Port     int    `json:"port"`
		HTTPS    bool   `json:"https"`
		APIKey   string `json:"api_key"`
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if !bind(c, &req) || !s.reauth(c, req.Password, req.Code) {
		return
	}
	if req.Port <= 0 || req.Port > 65535 {
		failure(c, 400, "invalid_port")
		return
	}
	if l := len(strings.TrimSpace(req.APIKey)); l < 8 || l > 256 {
		failure(c, 400, "invalid_api_key")
		return
	}
	nonce := putPanelSecret(g.ID, strings.TrimSpace(req.APIKey))
	res, err := s.gateProbe(c.Request.Context(), g, contracts.ActionPanelConfigure, map[string]any{"nonce": nonce, "port": req.Port, "https": req.HTTPS}, 60*time.Second)
	takePanelSecret(g.ID, nonce)
	if err != nil {
		failDetail(c, 422, "panel_failed", err.Error(), nil)
		return
	}
	s.audit(c, "gate.panel.set", "gate:"+itoa(g.ID), "ok")
	c.JSON(200, gin.H{"version": res["version"]})
}

func (s *Server) deleteGatePanel(c *gin.Context) {
	g, ok := s.gateParam(c)
	if !ok {
		return
	}
	if _, err := s.gateProbe(c.Request.Context(), g, contracts.ActionPanelRemove, nil, 30*time.Second); err != nil {
		failDetail(c, 422, "panel_failed", err.Error(), nil)
		return
	}
	s.audit(c, "gate.panel.remove", "gate:"+itoa(g.ID), "ok")
	c.Status(204)
}

// absorbPanelReport 记下心跳里的 1Panel 状态；1Panel 续签了证书时更新中心记录的证书与到期。
func (s *Server) absorbPanelReport(ctx context.Context, g *ent.Gate, hb contracts.Heartbeat) {
	if hb.Panel != nil {
		v := map[string]any{"detected": hb.Panel.Detected, "configured": hb.Panel.Configured, "version": hb.Panel.Version, "error": hb.Panel.Error, "at": time.Now().UTC()}
		_ = g.Update().SetPanel(v).Exec(ctx)
	}
	for id, pc := range hb.PanelCerts {
		ct, err := s.Store.Ent.Certificate.Query().Where(certificate.IDEQ(atoiOr(id)), certificate.GateIDEQ(g.ID), certificate.SourceEQ(certificate.SourcePanel)).Only(ctx)
		if err != nil || ct.Sha256 == pc.SHA256 {
			continue
		}
		upd := ct.Update().SetSha256(pc.SHA256).SetStatus(certificate.StatusReady).SetLastError("").SetRenewedAt(time.Now())
		if !pc.NotAfter.IsZero() {
			upd.SetNotAfter(pc.NotAfter)
		}
		if strings.Contains(pc.CertPEM, "BEGIN CERTIFICATE") {
			upd.SetCertPem(pc.CertPEM)
		}
		_ = upd.Exec(ctx)
	}
}

func atoiOr(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return -1
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// —— 为一台 VPS 准备证书 ——

// certUsable：证书能否写进订阅。自签有效期 10 年；其他证书没有到期时间（申请中）或已过期都不可用。
func certUsable(c *ent.Certificate, now time.Time) (bool, string) {
	switch {
	case c.Status == certificate.StatusPending:
		return false, "证书正在申请"
	case c.NotAfter == nil:
		return false, "证书状态未知"
	case now.After(*c.NotAfter):
		return false, "证书已过期"
	}
	return true, ""
}

// certHealth 是页面显示的证书状态：ready / renewing / pending / failed / expired / unknown。
func certHealth(c *ent.Certificate, now time.Time) string {
	switch {
	case c.Status == certificate.StatusPending:
		return "pending"
	case c.NotAfter == nil:
		return "unknown"
	case now.After(*c.NotAfter):
		return "expired"
	case c.Status == certificate.StatusFailed:
		return "failed"
	case renewDue(c, now):
		return "renewing"
	}
	return "ready"
}

// renewDue：剩余不足三分之一有效期时开始续签（IP 证书约 6 天，即剩 2 天时）。
func renewDue(c *ent.Certificate, now time.Time) bool {
	if c.NotAfter == nil {
		return false
	}
	start := c.CreatedAt
	if c.RenewedAt != nil {
		start = *c.RenewedAt
	}
	if x := certNotBefore(c.CertPem); !x.IsZero() {
		start = x
	}
	life := c.NotAfter.Sub(start)
	return life > 0 && c.NotAfter.Sub(now) < life/3
}

func certNotBefore(certPEM string) time.Time {
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		return time.Time{}
	}
	x, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return time.Time{}
	}
	return x.NotBefore
}

func certExpiry(certPEM string) *time.Time {
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		return nil
	}
	x, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil
	}
	return &x.NotAfter
}

// errCertUnavailable 带上每种方式不可用的原因。
type errCertUnavailable []string

func (e errCertUnavailable) Error() string {
	return "这台 VPS 暂时拿不到证书：" + strings.Join(e, "；") + "。可以在 VPS 详情里填写 1Panel API 密钥，或在「系统设置 → 节点证书」填写节点证书域名，或在建节点时手动上传证书。"
}

// gateCert 返回这台 VPS 上来源为 src 的可用证书（同一台 VPS 的节点共用）。
func (s *Server) gateCert(ctx context.Context, gid int, src certificate.Source) *ent.Certificate {
	list, err := s.Store.Ent.Certificate.Query().Where(certificate.GateIDEQ(gid), certificate.SourceEQ(src)).Order(ent.Desc(certificate.FieldID)).All(ctx)
	if err != nil {
		return nil
	}
	for _, c := range list {
		if ok, _ := certUsable(c, time.Now()); ok {
			return c
		}
	}
	return nil
}

// selfSignedFor 返回这台 VPS 共用的自签证书，没有时生成一张（10 年，指纹长期不变）。
func (s *Server) selfSignedFor(ctx context.Context, g *ent.Gate) (*ent.Certificate, error) {
	if c := s.gateCert(ctx, g.ID, certificate.SourceSelfSigned); c != nil {
		return c, nil
	}
	// 旧版按节点生成的自签：同一台 VPS 上已有的继续沿用（归到这台 VPS），客户端不必重新导入。
	if nodes, err := s.Store.Ent.Node.Query().Where(node.GateIDEQ(g.ID), node.CertIDNotNil()).All(ctx); err == nil {
		for _, n := range nodes {
			c, err := s.Store.Ent.Certificate.Query().Where(certificate.IDEQ(*n.CertID), certificate.SourceEQ(certificate.SourceSelfSigned), certificate.GateIDIsNil()).Only(ctx)
			if err == nil {
				return c.Update().SetGateID(g.ID).Save(ctx)
			}
		}
	}
	domain := g.PublicHost
	if domain == "" {
		domain = "gate-" + itoa(g.ID) + ".invalid"
	}
	certPEM, keyPEM, err := selfSigned(domain)
	if err != nil {
		return nil, err
	}
	return s.storeCert(ctx, fmt.Sprintf("%s 自签", g.Name), domain, certificate.SourceSelfSigned, &g.ID, certPEM, keyPEM)
}

// storeCert 保存一张证书（私钥加密）。
func (s *Server) storeCert(ctx context.Context, name, domain string, src certificate.Source, gid *int, certPEM, keyPEM string) (*ent.Certificate, error) {
	x, err := parseCertPair(certPEM, keyPEM)
	if err != nil {
		return nil, err
	}
	sealed, err := s.Crypto.Encrypt(keyPEM)
	if err != nil {
		return nil, err
	}
	return s.Store.Ent.Certificate.Create().SetName(name).SetDomain(domain).SetSource(src).SetNillableGateID(gid).
		SetCertPem(certPEM).SetKeyPem(sealed).SetSha256(contracts.CertSum(certPEM, keyPEM)).SetNotAfter(x.NotAfter).
		SetStatus(certificate.StatusReady).Save(ctx)
}

// port80Free：Gate 上报过端口且 80 没有被其他程序占用。
func (s *Server) port80Free(ctx context.Context, g *ent.Gate) bool {
	if g.LastSeenAt == nil || g.ListeningPorts == nil {
		return false
	}
	own, err := s.gateOwnPorts(ctx, g)
	return err == nil && !foreignPorts(g, own)[80]
}

func panelReady(g *ent.Gate) (bool, string) {
	switch {
	case g.Panel == nil:
		return false, "没有检测到 1Panel"
	case g.Panel["configured"] != true:
		if g.Panel["detected"] == true {
			return false, "检测到 1Panel，但还没有填写 API 密钥"
		}
		return false, "没有检测到 1Panel"
	case g.Panel["error"] != nil && g.Panel["error"] != "":
		return false, fmt.Sprint("1Panel 连接失败：", g.Panel["error"])
	}
	return true, ""
}

// ipCertFor 为 VPS 公网 IP 准备证书：1Panel → 中心 ACME（Gate 应答 80 端口）。
func (s *Server) ipCertFor(ctx context.Context, g *ent.Gate) (*ent.Certificate, []string) {
	for _, src := range []certificate.Source{certificate.SourcePanel, certificate.SourceAcme} {
		if c := s.gateCert(ctx, g.ID, src); c != nil {
			return c, nil
		}
	}
	var why []string
	ip := net.ParseIP(g.PublicHost)
	if ip == nil {
		return nil, []string{"VPS 还没有公网 IP"}
	}
	if ok, reason := panelReady(g); ok {
		c, err := s.panelCert(ctx, g, ip.String())
		if err == nil {
			return c, nil
		}
		why = append(why, "1Panel 申请失败："+err.Error())
	} else {
		why = append(why, reason)
	}
	if !s.port80Free(ctx, g) {
		why = append(why, "80 端口被其他程序占用（或 VPS 还没有上报端口），无法直接验证 IP")
		return nil, why
	}
	certPEM, keyPEM, err := s.issueIPCert(ctx, g, ip.String())
	if err != nil {
		return nil, append(why, "IP 证书申请失败："+err.Error())
	}
	c, err := s.storeCert(ctx, g.Name+" IP 证书", ip.String(), certificate.SourceAcme, &g.ID, certPEM, keyPEM)
	if err != nil {
		return nil, append(why, "保存证书失败")
	}
	return c, nil
}

// panelCert 让 Gate 经本机 1Panel 申请 IP 证书；私钥留在 Gate，中心只记证书与指纹。
func (s *Server) panelCert(ctx context.Context, g *ent.Gate, ip string) (*ent.Certificate, error) {
	c, err := s.Store.Ent.Certificate.Create().SetName(g.Name + " 1Panel 证书").SetDomain(ip).SetSource(certificate.SourcePanel).SetGateID(g.ID).
		SetCertPem("").SetKeyPem("").SetStatus(certificate.StatusPending).Save(ctx)
	if err != nil {
		return nil, err
	}
	res, err := s.gateProbe(ctx, g, contracts.ActionPanelCert, map[string]any{"cert_id": c.ID, "ip": ip}, 170*time.Second)
	certPEM, _ := res["cert_pem"].(string)
	sum, _ := res["sha256"].(string)
	if err == nil && !strings.Contains(certPEM, "BEGIN CERTIFICATE") {
		err = errors.New("1Panel 没有返回证书")
	}
	if err != nil {
		_ = s.Store.Ent.Certificate.DeleteOne(c).Exec(ctx)
		return nil, err
	}
	return c.Update().SetCertPem(certPEM).SetSha256(sum).SetNillableNotAfter(certExpiry(certPEM)).SetStatus(certificate.StatusReady).SetRenewedAt(time.Now()).Save(ctx)
}

// issueIPCert 由中心运行 ACME（shortlived profile），Gate 临时在 80 端口应答验证。
func (s *Server) issueIPCert(ctx context.Context, g *ent.Gate, ip string) (string, string, error) {
	client, err := s.acmeClient(ctx)
	if err != nil {
		return "", "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 150*time.Second)
	defer cancel()
	return client.Issue(ctx, ip, acme.ProfileShortLived, func(ctx context.Context, ch acme.Challenge) error {
		_, err := s.gateProbe(ctx, g, contracts.ActionACMEHTTP01, map[string]any{"token": ch.Token, "key_auth": ch.KeyAuth, "seconds": 150}, 30*time.Second)
		return err
	})
}

// edgeCert 返回全项目共用的节点证书域名证书；没有时由中心经本机反代完成 HTTP 验证签发。
func (s *Server) edgeCert(ctx context.Context) (*ent.Certificate, error) {
	v := s.nodeCertSettings(ctx)
	if v.EdgeDomain == "" {
		return nil, errors.New("没有设置节点证书域名")
	}
	list, _ := s.Store.Ent.Certificate.Query().Where(certificate.SourceEQ(certificate.SourceEdge), certificate.DomainEQ(v.EdgeDomain)).Order(ent.Desc(certificate.FieldID)).All(ctx)
	for _, c := range list {
		if ok, _ := certUsable(c, time.Now()); ok {
			return c, nil
		}
	}
	certPEM, keyPEM, err := s.issueEdgeCert(ctx, v.EdgeDomain)
	if err != nil {
		return nil, err
	}
	return s.storeCert(ctx, "节点证书域名 "+v.EdgeDomain, v.EdgeDomain, certificate.SourceEdge, nil, certPEM, keyPEM)
}

func (s *Server) issueEdgeCert(ctx context.Context, domain string) (string, string, error) {
	client, err := s.acmeClient(ctx)
	if err != nil {
		return "", "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 150*time.Second)
	defer cancel()
	return client.Issue(ctx, domain, "", func(ctx context.Context, ch acme.Challenge) error {
		return s.Store.KV.Set(ctx, "acme-http01:"+ch.Token, domain+"\n"+ch.KeyAuth, 10*time.Minute)
	})
}

// serveACMEChallenge 回应节点证书域名的 HTTP-01 验证（反代把该域名的 80 端口转到中心）。
// 只在这个路径、只对节点证书域名回应；验证必须经明文 HTTP，所以排在 HTTPS 拦截之前。
func (s *Server) serveACMEChallenge(c *gin.Context) {
	token := strings.TrimPrefix(c.Request.URL.Path, acmeChallengePath)
	if s.Store == nil || len(token) < 16 || len(token) > 128 || strings.ContainsAny(token, "/.") {
		c.AbortWithStatus(404)
		return
	}
	raw, ok, err := s.Store.KV.Get(c.Request.Context(), "acme-http01:"+token)
	domain, keyAuth, _ := strings.Cut(raw, "\n")
	if err != nil || !ok || !strings.EqualFold(requestHost(c.Request), domain) {
		c.AbortWithStatus(404)
		return
	}
	c.Data(http.StatusOK, "text/plain", []byte(keyAuth))
	c.Abort()
}

const acmeChallengePath = "/.well-known/acme-challenge/"

// certForNode 为新节点准备证书（建节点时没有手动选择证书）。返回证书与写进节点的 SNI。
func (s *Server) certForNode(ctx context.Context, g *ent.Gate, mode string) (*ent.Certificate, string, error) {
	switch mode {
	case "self_signed_ok":
		c, err := s.selfSignedFor(ctx, g)
		if err != nil {
			return nil, "", err
		}
		return c, c.Domain, nil
	case "required":
		c, why := s.ipCertFor(ctx, g)
		if c != nil {
			return c, c.Domain, nil
		}
		e, err := s.edgeCert(ctx)
		if err == nil {
			return e, e.Domain, nil
		}
		return nil, "", append(errCertUnavailable(why), err.Error())
	}
	return nil, "", nil
}

// —— 续签 ——

// RenewCerts 每 10 分钟检查一次：中心签发的 IP 证书与节点证书域名证书剩余不足三分之一时续签；
// 失败按退避重试（10 分钟起，最长 6 小时）并提醒。1Panel 证书由 1Panel 自己续签，Gate 心跳上报新证书。
func (s *Server) RenewCerts(ctx context.Context, now time.Time) error {
	if ok, err := s.Store.KV.SetNX(ctx, "cert-renew-scan", "1", 10*time.Minute); err != nil || !ok {
		return err
	}
	list, err := s.Store.Ent.Certificate.Query().Where(certificate.SourceIn(certificate.SourceAcme, certificate.SourceEdge)).All(ctx)
	if err != nil {
		return err
	}
	for _, c := range list {
		if !renewDue(c, now) && (c.NotAfter == nil || now.Before(*c.NotAfter)) {
			continue
		}
		if _, waiting, _ := s.Store.KV.Get(ctx, fmt.Sprintf("cert-renew-wait:%d", c.ID)); waiting {
			continue
		}
		if ok, _ := s.Store.KV.SetNX(ctx, fmt.Sprintf("cert-renew-run:%d", c.ID), "1", 5*time.Minute); !ok {
			continue
		}
		st := s.Store
		go func(c *ent.Certificate) {
			bg, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
			defer cancel()
			s.state.RLock()
			same := s.Store == st
			s.state.RUnlock()
			if same {
				_ = s.renewCert(bg, c)
			}
		}(c)
	}
	return nil
}

// renewCert 续签一张证书并下发给用到它的 Gate。
func (s *Server) renewCert(ctx context.Context, c *ent.Certificate) error {
	var certPEM, keyPEM string
	var err error
	switch c.Source {
	case certificate.SourceAcme:
		var g *ent.Gate
		if c.GateID == nil {
			return errors.New("certificate has no gate")
		}
		if g, err = s.Store.Ent.Gate.Get(ctx, *c.GateID); err == nil {
			certPEM, keyPEM, err = s.issueIPCert(ctx, g, c.Domain)
		}
	case certificate.SourceEdge:
		certPEM, keyPEM, err = s.issueEdgeCert(ctx, c.Domain)
	default:
		return nil
	}
	if err == nil {
		var x *x509.Certificate
		var sealed string
		if x, err = parseCertPair(certPEM, keyPEM); err == nil {
			if sealed, err = s.Crypto.Encrypt(keyPEM); err == nil {
				err = c.Update().SetCertPem(certPEM).SetKeyPem(sealed).SetSha256(contracts.CertSum(certPEM, keyPEM)).SetNotAfter(x.NotAfter).
					SetStatus(certificate.StatusReady).SetLastError("").SetRenewedAt(time.Now()).Exec(ctx)
			}
		}
	}
	if err != nil {
		backoff := s.renewBackoff(ctx, c.ID)
		_ = s.Store.KV.Set(ctx, fmt.Sprintf("cert-renew-wait:%d", c.ID), "1", backoff)
		_ = c.Update().SetStatus(certificate.StatusFailed).SetLastError(trimErr(err)).Exec(ctx)
		s.notifyOnce(ctx, fmt.Sprintf("cert-renew:%d:%s", c.ID, time.Now().Format("2006-01-02")), fmt.Sprintf("证书「%s」续签失败：%s。将在 %s 后重试。", c.Name, trimErr(err), backoff))
		return err
	}
	_ = s.Store.KV.Del(ctx, fmt.Sprintf("cert-renew-fail:%d", c.ID))
	return s.bumpCertGates(ctx, c.ID)
}

// renewBackoff 是连续失败后的等待时间：10 分钟起每次翻倍，最长 6 小时。
func (s *Server) renewBackoff(ctx context.Context, id int) time.Duration {
	n, _ := s.Store.KV.Incr(ctx, fmt.Sprintf("cert-renew-fail:%d", id), 7*24*time.Hour)
	d := 10 * time.Minute
	for i := int64(1); i < n && d < 6*time.Hour; i++ {
		d *= 2
	}
	if d > 6*time.Hour {
		d = 6 * time.Hour
	}
	return d
}

func trimErr(err error) string {
	m := err.Error()
	if len(m) > 300 {
		m = m[:300] + "…"
	}
	return m
}

// bumpCertGates 让用到这张证书的 Gate 重新下发配置（下载新证书）。
func (s *Server) bumpCertGates(ctx context.Context, id int) error {
	nodes, err := s.Store.Ent.Node.Query().Where(node.CertIDEQ(id)).All(ctx)
	if err != nil {
		return err
	}
	seen, gates := map[int]bool{}, []int{}
	for _, n := range nodes {
		if !seen[n.GateID] {
			seen[n.GateID] = true
			gates = append(gates, n.GateID)
		}
	}
	return s.bumpGates(ctx, gates)
}

func (s *Server) renewCertNow(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	ct, err := s.Store.Ent.Certificate.Get(ctx, id)
	if err != nil {
		failure(c, 404, "cert_not_found")
		return
	}
	if ct.Source != certificate.SourceAcme && ct.Source != certificate.SourceEdge {
		failDetail(c, 409, "cert_not_renewable", "只有中心签发的证书可以在这里续签；1Panel 证书由 1Panel 续签", nil)
		return
	}
	_ = s.Store.KV.Del(ctx, fmt.Sprintf("cert-renew-wait:%d", id))
	if err = s.renewCert(ctx, ct); err != nil {
		failDetail(c, 422, "renew_failed", trimErr(err), nil)
		return
	}
	s.audit(c, "cert.renew", "cert:"+itoa(id), "ok")
	ct, _ = s.Store.Ent.Certificate.Get(ctx, id)
	c.JSON(200, certView(ct))
}
