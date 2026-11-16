package center

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math"
	"net"
	"slices"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/certificate"
	"github.com/shusfun/weir/internal/store/ent/resource"
	"github.com/shusfun/weir/internal/store/ent/user"
	"github.com/shusfun/weir/packages/contracts"
)

const browserConfigKind = "browser-test-config"
const browserSessionKind = "browser-test-session"

type browserConfig struct {
	Plan contracts.BrowserTestPlan `json:"plan"`
	Key  string                    `json:"key"`
}
type browserResult struct {
	Status     string   `json:"status"`
	Reason     string   `json:"reason"`
	LatencyMS  *float64 `json:"latency_ms,omitempty"`
	DownloadMS *float64 `json:"download_ms,omitempty"`
	Bytes      int64    `json:"bytes"`
}
type browserSession struct {
	Claim     contracts.BrowserTestClaim `json:"claim"`
	NodeID    int                        `json:"node_id"`
	Token     string                     `json:"token"`
	Cancelled bool                       `json:"cancelled"`
	Result    *browserResult             `json:"result,omitempty"`
}

func (s *Server) browserTestRoutes(auth, admin *gin.RouterGroup) {
	admin.GET("/gates/:id/browser-test", s.getBrowserTestConfig)
	admin.PUT("/gates/:id/browser-test", s.putBrowserTestConfig)
	admin.POST("/gates/:id/browser-tests", func(c *gin.Context) { s.createBrowserTest(c, true) })
	auth.POST("/me/nodes/:id/browser-tests", func(c *gin.Context) { s.createBrowserTest(c, false) })
	auth.GET("/browser-tests/:id", s.browserTestSession)
	auth.POST("/browser-tests/:id", s.browserTestSession)
	auth.DELETE("/browser-tests/:id", s.browserTestSession)
}

func readBrowserConfig(ctx context.Context, db *ent.Client, gid int) (*browserConfig, error) {
	row, err := db.Resource.Query().Where(resource.KindEQ(browserConfigKind), resource.KeyEQ(itoa(gid))).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var config browserConfig
	if err = decodeResource(row, &config); err != nil {
		return nil, err
	}
	if err = config.Plan.Validate(); err != nil {
		return nil, err
	}
	return &config, nil
}

func (s *Server) browserObservation(ctx context.Context, gid int) (*contracts.BrowserTestObservation, error) {
	raw, ok, err := s.Store.Transient.Get(ctx, "browser-test:"+itoa(gid))
	if err != nil || !ok {
		return nil, err
	}
	var result contracts.BrowserTestObservation
	if err = contracts.DecodeStrict([]byte(raw), &result, 8192); err != nil {
		return nil, err
	}
	if !freshObservation(result.ObservedAt, time.Now(), 90*time.Second) {
		return nil, nil
	}
	return &result, nil
}

func (s *Server) agentBrowserTests(c *gin.Context) {
	var observation contracts.BrowserTestObservation
	if !bindWorker(c, &observation, 8192) {
		return
	}
	if !contracts.BrowserTestIDValid(observation.Boot) || len(observation.Reason) > 200 || !freshObservation(observation.ObservedAt, time.Now(), time.Minute) || len(observation.Sessions) > 16 {
		failure(c, 400, "browser_test_observation_invalid")
		return
	}
	seen := map[string]bool{}
	for _, v := range observation.Sessions {
		if !contracts.BrowserTestIDValid(v.ID) || seen[v.ID] || v.Bytes < 0 || v.Bytes > contracts.BrowserTestMaxBytes {
			failure(c, 400, "browser_test_observation_invalid")
			return
		}
		seen[v.ID] = true
	}
	raw, _ := json.Marshal(observation)
	if err := s.Store.Transient.Set(c.Request.Context(), "browser-test:"+itoa(gateFrom(c).ID), string(raw), 90*time.Second); err != nil {
		failInternal(c, 503, "save_failed", err)
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (s *Server) getBrowserTestConfig(c *gin.Context) {
	g, ok := s.gateParam(c)
	if !ok {
		return
	}
	config, err := readBrowserConfig(c.Request.Context(), s.Store.Ent, g.ID)
	if err != nil {
		failInternal(c, 503, "browser_test_unavailable", err)
		return
	}
	observation, err := s.browserObservation(c.Request.Context(), g.ID)
	if err != nil {
		failInternal(c, 503, "browser_test_unavailable", err)
		return
	}
	var plan *contracts.BrowserTestPlan
	if config != nil {
		plan = &config.Plan
	}
	c.JSON(200, gin.H{"config": plan, "observation": observation})
}

func (s *Server) putBrowserTestConfig(c *gin.Context) {
	g, ok := s.gateParam(c)
	if !ok {
		return
	}
	var req struct {
		Enabled bool   `json:"enabled"`
		CertID  int    `json:"cert_id"`
		Host    string `json:"host"`
	}
	if !bind(c, &req) {
		return
	}
	if req.Enabled && req.Host == "" {
		failure(c, 400, "browser_test_host_required")
		return
	}
	if req.Enabled && req.CertID <= 0 {
		failure(c, 400, "browser_test_cert_required")
		return
	}
	ctx := WithRevisionCause(c.Request.Context(), "browser.test.configure", c.GetInt("user_id"))
	finish := operationStage(ctx, "browser.test.configure", "gate_id", g.ID)
	origin := s.siteOrigin(c)
	err := s.Store.Lock.Do(ctx, "certificate:"+itoa(req.CertID), func(ctx context.Context) error {
		return s.Store.Lock.Do(ctx, "browser-test:"+itoa(g.ID), func(ctx context.Context) error {
			if req.Enabled {
				caps, err := s.Store.Tasks.Capabilities(ctx, g.ID)
				if err != nil {
					return err
				}
				if caps.ConfigFeatures[contracts.ConfigBrowserTest] < 1 || caps.ConfigFeatures[contracts.ConfigFirewall] < 1 {
					return errors.New("VPS 未声明测试服务或防火墙管理能力")
				}
			}
			tx, err := s.Store.Ent.Tx(ctx)
			if err != nil {
				return err
			}
			defer tx.Rollback()
			if err = lockActiveGates(ctx, tx.Client(), g.ID); err != nil {
				return err
			}
			if !req.Enabled {
				if _, err = tx.Resource.Delete().Where(resource.KindIn(browserConfigKind, browserSessionKind), resource.OwnerIDEQ(g.ID)).Exec(ctx); err != nil {
					return err
				}
			} else {
				cert, err := tx.Certificate.Get(ctx, req.CertID)
				if err != nil {
					return err
				}
				if cert.GateID != nil && *cert.GateID != g.ID {
					return errors.New("证书属于其他 VPS")
				}
				if !cert.NotAfter.After(time.Now()) {
					return errors.New("证书已到期")
				}
				config, err := readBrowserConfig(ctx, tx.Client(), g.ID)
				if err != nil {
					return err
				}
				if config == nil {
					own, err := s.gateOwnPortsDB(ctx, tx.Client(), g)
					if err != nil {
						return err
					}
					port, err := pickPort(own, foreignPorts(g, own))
					if err != nil {
						return err
					}
					own[port] = true
					rtc, err := pickPort(own, foreignPorts(g, own))
					if err != nil {
						return err
					}
					pub, key, err := ed25519.GenerateKey(rand.Reader)
					if err != nil {
						return err
					}
					sealed, err := s.seal(base64.RawURLEncoding.EncodeToString(key))
					if err != nil {
						return err
					}
					config = &browserConfig{Plan: contracts.BrowserTestPlan{GateID: g.ID, Port: port, RTCPort: rtc, PublicKey: base64.RawURLEncoding.EncodeToString(pub)}, Key: sealed}
				}
				config.Plan.Host, config.Plan.CertID, config.Plan.Origin = req.Host, int64(req.CertID), origin
				if err = config.Plan.Validate(); err != nil {
					return err
				}
				if cert.Source != certificate.SourcePanel {
					key, err := s.reveal(cert.KeyPem)
					if err != nil {
						return err
					}
					leaf, err := parseCertPair(cert.CertPem, key)
					if err != nil {
						return err
					}
					if err = leaf.VerifyHostname(req.Host); err != nil {
						return errors.New("测试地址与证书不匹配")
					}
				}
				if err = tx.Resource.Create().SetKind(browserConfigKind).SetKey(itoa(g.ID)).SetOwnerID(g.ID).SetData(toMap(config)).OnConflictColumns(resource.FieldKind, resource.FieldKey).UpdateNewValues().Exec(ctx); err != nil {
					return err
				}
				if _, err = tx.Resource.Delete().Where(resource.KindEQ(browserSessionKind), resource.OwnerIDEQ(g.ID)).Exec(ctx); err != nil {
					return err
				}
			}
			if err = advanceOperationRevision(ctx, tx, g.ID); err != nil {
				return err
			}
			return tx.Commit()
		})
	})
	finish(err)
	if err != nil {
		failDetail(c, 422, "browser_test_configuration_invalid", err.Error(), nil)
		return
	}
	s.publishOperationRevision(ctx, g.ID)
	s.audit(c, "browser.test.configure", "gate:"+itoa(g.ID), "ok")
	s.getBrowserTestConfig(c)
}

func browserNodeAccess(ctx context.Context, db *ent.Client, uid, nid int) (*ent.Node, error) {
	u, err := db.User.Get(ctx, uid)
	if err != nil {
		return nil, err
	}
	n, err := db.Node.Get(ctx, nid)
	if err != nil {
		return nil, err
	}
	if !u.Enabled || u.ExpiresAt != nil && !u.ExpiresAt.After(time.Now()) || !n.Enabled || !u.AllNodes && !slices.Contains(u.NodeIds, nid) {
		return nil, errors.New("node access denied")
	}
	return n, nil
}

func (s *Server) createBrowserTest(c *gin.Context, admin bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		failure(c, 400, "invalid_id")
		return
	}
	ctx := WithRevisionCause(c.Request.Context(), "browser.test.create", c.GetInt("user_id"))
	gid, nid := id, 0
	if !admin {
		n, err := browserNodeAccess(ctx, s.Store.Ent, c.GetInt("user_id"), id)
		if err != nil {
			failure(c, 403, "node_forbidden")
			return
		}
		gid, nid = n.GateID, id
	}
	var req struct {
		Purpose string `json:"purpose"`
	}
	if !bind(c, &req) {
		return
	}
	finish := operationStage(ctx, "browser.test.create", "gate_id", gid)
	var response gin.H
	err = s.Store.Lock.Do(ctx, "browser-test:"+itoa(gid), func(ctx context.Context) error {
		tx, err := s.Store.Ent.Tx(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if err = lockActiveGates(ctx, tx.Client(), gid); err != nil {
			return err
		}
		g, err := tx.Gate.Get(ctx, gid)
		if err != nil {
			return err
		}
		if !gateOnline(g) {
			return errors.New("VPS 离线")
		}
		config, err := readBrowserConfig(ctx, tx.Client(), gid)
		if err != nil {
			return err
		}
		if config == nil {
			return errors.New("管理员尚未启用浏览器测试服务")
		}
		observed, err := s.browserObservation(ctx, gid)
		if err != nil {
			return err
		}
		if observed == nil {
			return errors.New("测试服务尚无新鲜回执")
		}
		if !observed.Healthy {
			return errors.New("测试监听已停止，请重新应用测试服务配置")
		}
		rows, err := tx.Resource.Query().Where(resource.KindEQ(browserSessionKind), resource.OwnerIDEQ(gid)).All(ctx)
		if err != nil {
			return err
		}
		active := 0
		for _, row := range rows {
			var prior browserSession
			if err = decodeResource(row, &prior); err != nil {
				return err
			}
			if prior.Claim.Expires.After(time.Now()) && !prior.Cancelled {
				active++
			} else {
				if err = tx.Resource.DeleteOne(row).Exec(ctx); err != nil {
					return err
				}
			}
		}
		if active >= 16 {
			return errors.New("测试会话已达上限，请稍后重试")
		}
		claim := contracts.BrowserTestClaim{ID: randomHex(16), GateID: gid, UserID: c.GetInt("user_id"), Boot: observed.Boot, Purpose: req.Purpose, Bytes: contracts.BrowserTestMaxBytes, Expires: time.Now().Add(contracts.BrowserTestLifetime), TraceID: taskTrace(ctx)}
		secret, err := s.reveal(config.Key)
		if err != nil {
			return err
		}
		key, err := base64.RawURLEncoding.DecodeString(secret)
		if err != nil {
			return err
		}
		token, err := contracts.SignBrowserTest(claim, ed25519.PrivateKey(key))
		if err != nil {
			return err
		}
		sealed, err := s.seal(token)
		if err != nil {
			return err
		}
		data := browserSession{Claim: claim, NodeID: nid, Token: sealed}
		if err = browserSessionAccess(ctx, tx.Client(), c.GetInt("user_id"), data); err != nil {
			return err
		}
		if err = tx.Resource.Create().SetKind(browserSessionKind).SetKey(claim.ID).SetOwnerID(gid).SetData(toMap(data)).Exec(ctx); err != nil {
			return err
		}
		if err = advanceOperationRevision(ctx, tx, gid); err != nil {
			return err
		}
		certificateHash := ""
		if claim.Purpose == "webtransport" {
			cert, err := tx.Certificate.Get(ctx, int(config.Plan.CertID))
			if err != nil {
				return err
			}
			if cert.Source != certificate.SourcePanel {
				block, _ := pem.Decode([]byte(cert.CertPem))
				if block == nil || block.Type != "CERTIFICATE" {
					return errors.New("测试证书格式无效")
				}
				leaf, err := x509.ParseCertificate(block.Bytes)
				if err != nil {
					return err
				}
				certificateHash = contracts.BrowserTestCertificateHash(leaf, time.Now())
			}
		}
		if err = tx.Commit(); err != nil {
			return err
		}
		response = gin.H{"id": claim.ID, "token": token, "endpoint": "https://" + net.JoinHostPort(config.Plan.Host, itoa(config.Plan.Port)), "expires_at": claim.Expires, "purpose": claim.Purpose, "max_bytes": claim.Bytes, "status": "pending"}
		if certificateHash != "" {
			response["certificate_hash"] = certificateHash
		}
		return nil
	})
	finish(err)
	if err != nil {
		failDetail(c, 409, "browser_test_unavailable", err.Error(), nil)
		return
	}
	s.publishOperationRevision(ctx, gid)
	s.audit(c, "browser.test.create", fmt.Sprint(response["id"]), "ok")
	c.Header("Cache-Control", "no-store")
	c.JSON(201, response)
}

func browserSessionAccess(ctx context.Context, db *ent.Client, uid int, session browserSession) error {
	if session.Claim.UserID != uid {
		return errors.New("session access denied")
	}
	u, err := db.User.Get(ctx, uid)
	if err != nil {
		return err
	}
	if !u.Enabled || u.ExpiresAt != nil && !u.ExpiresAt.After(time.Now()) {
		return errors.New("account unavailable")
	}
	if session.NodeID > 0 {
		n, err := browserNodeAccess(ctx, db, uid, session.NodeID)
		if err != nil {
			return err
		}
		if n.GateID != session.Claim.GateID {
			return errors.New("node changed")
		}
		return nil
	}
	if u.Role != user.RoleAdmin {
		return errors.New("administrator required")
	}
	return nil
}

func advanceOperationRevision(ctx context.Context, tx *ent.Tx, gid int) error {
	g, err := tx.Gate.UpdateOneID(gid).AddConfigRevision(1).Save(ctx)
	if err != nil {
		return err
	}
	cause := causeFrom(ctx)
	return tx.Audit.Create().SetActor(cause.Actor).SetAction("config.revision").SetTarget("gate:" + itoa(gid)).SetResult("ok").SetDetail(map[string]any{"old_revision": g.ConfigRevision - 1, "new_revision": g.ConfigRevision, "source": cause.Source, "trace_id": taskTrace(ctx)}).Exec(ctx)
}
func (s *Server) publishOperationRevision(ctx context.Context, gid int) {
	s.waiters.notify(gid)
	_ = s.Store.Notifications.Publish(ctx, "gate:"+itoa(gid))
}

func (s *Server) browserTestSession(c *gin.Context) {
	ctx := WithRevisionCause(c.Request.Context(), "browser.test.finish", c.GetInt("user_id"))
	row, err := s.Store.Ent.Resource.Query().Where(resource.KindEQ(browserSessionKind), resource.KeyEQ(c.Param("id"))).Only(ctx)
	if ent.IsNotFound(err) {
		failure(c, 404, "browser_test_not_found")
		return
	}
	if err != nil {
		failInternal(c, 503, "browser_test_unavailable", err)
		return
	}
	var session browserSession
	if err = decodeResource(row, &session); err != nil {
		failInternal(c, 503, "browser_test_unavailable", err)
		return
	}
	if err = browserSessionAccess(ctx, s.Store.Ent, c.GetInt("user_id"), session); err != nil {
		failure(c, 403, "forbidden")
		return
	}
	if c.Request.Method != "GET" {
		finish := operationStage(ctx, "browser.test.finish", "session_id", row.Key)
		var result *browserResult
		if c.Request.Method == "POST" {
			result = &browserResult{}
			if !bind(c, result) {
				finish(errors.New("invalid result"))
				return
			}
			valid := func(n *float64) bool {
				return n == nil || !math.IsNaN(*n) && !math.IsInf(*n, 0) && *n >= 0 && *n <= 120000
			}
			if !slices.Contains([]string{"passed", "failed", "unavailable"}, result.Status) || len(result.Reason) > 600 || !valid(result.LatencyMS) || !valid(result.DownloadMS) || result.Bytes < 0 || result.Bytes > session.Claim.Bytes {
				finish(errors.New("invalid result"))
				failure(c, 400, "browser_test_result_invalid")
				return
			}
		}
		err = s.Store.Lock.Do(ctx, "browser-test:"+itoa(session.Claim.GateID), func(ctx context.Context) error {
			tx, err := s.Store.Ent.Tx(ctx)
			if err != nil {
				return err
			}
			defer tx.Rollback()
			if err = lockActiveGates(ctx, tx.Client(), session.Claim.GateID); err != nil {
				return err
			}
			current, err := tx.Resource.Get(ctx, row.ID)
			if err != nil {
				return err
			}
			if err = decodeResource(current, &session); err != nil {
				return err
			}
			if err = browserSessionAccess(ctx, tx.Client(), c.GetInt("user_id"), session); err != nil {
				return err
			}
			if result != nil {
				observed, err := s.browserObservation(ctx, session.Claim.GateID)
				if err != nil {
					return err
				}
				if session.Cancelled || !session.Claim.Expires.After(time.Now()) || observed == nil || !observed.Healthy || observed.Boot != session.Claim.Boot {
					return contracts.ErrBrowserTestExpired
				}
				session.Result = result
			} else if session.Cancelled {
				return nil
			}
			session.Cancelled = true
			if err = tx.Resource.UpdateOneID(row.ID).SetData(toMap(session)).Exec(ctx); err != nil {
				return err
			}
			if err = advanceOperationRevision(ctx, tx, session.Claim.GateID); err != nil {
				return err
			}
			return tx.Commit()
		})
		finish(err)
		if errors.Is(err, contracts.ErrBrowserTestExpired) {
			failure(c, 409, "browser_test_expired")
			return
		}
		if err != nil {
			failInternal(c, 503, "save_failed", err)
			return
		}
		s.publishOperationRevision(ctx, session.Claim.GateID)
	}
	observation, err := s.browserObservation(ctx, session.Claim.GateID)
	if err != nil {
		failInternal(c, 503, "browser_test_unavailable", err)
		return
	}
	status := "pending"
	var measured *int64
	if observation != nil && observation.Boot == session.Claim.Boot {
		for _, v := range observation.Sessions {
			if v.ID == row.Key {
				n := v.Bytes
				measured = &n
			}
		}
	}
	if session.Cancelled {
		status = "finished"
	} else if !session.Claim.Expires.After(time.Now()) {
		status = "expired"
	} else if observation != nil {
		if !observation.Healthy || observation.Boot != session.Claim.Boot {
			status = "expired"
		} else if measured != nil {
			status = "ready"
		}
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(200, gin.H{"id": row.Key, "status": status, "result": session.Result, "result_source": "browser", "gate_observed_bytes": measured, "reason": "浏览器上报的测量仅描述本次浏览器到 VPS 的路径，不代表代理协议或运营商限速结论"})
}

func (s *Server) compileBrowserTest(ctx context.Context, g *ent.Gate, cfg *contracts.GateConfig) error {
	config, err := readBrowserConfig(ctx, s.Store.Ent, g.ID)
	if err != nil || config == nil {
		return err
	}
	caps, err := s.Store.Tasks.Capabilities(ctx, g.ID)
	if err != nil {
		return err
	}
	if caps.ConfigFeatures[contracts.ConfigBrowserTest] < 1 {
		return errors.New("browser test capability unavailable")
	}
	plan := config.Plan
	rows, err := s.Store.Ent.Resource.Query().Where(resource.KindEQ(browserSessionKind), resource.OwnerIDEQ(g.ID)).All(ctx)
	if err != nil {
		return err
	}
	for _, row := range rows {
		var session browserSession
		if err = decodeResource(row, &session); err != nil {
			return err
		}
		if session.Cancelled || !session.Claim.Expires.After(time.Now()) {
			continue
		}
		if err = browserSessionAccess(ctx, s.Store.Ent, session.Claim.UserID, session); err != nil {
			continue
		}
		token, err := s.reveal(session.Token)
		if err != nil {
			return err
		}
		plan.Tokens = append(plan.Tokens, token)
	}
	if err = plan.Validate(); err != nil {
		return err
	}
	cfg.BrowserTest = &plan
	found := false
	for _, c := range cfg.Certs {
		if c.ID == plan.CertID {
			found = true
		}
	}
	if !found {
		cert, err := s.Store.Ent.Certificate.Get(ctx, int(plan.CertID))
		if err != nil {
			return err
		}
		ref := contracts.CertRef{ID: plan.CertID, SHA256: cert.Sha256}
		if cert.Source == certificate.SourcePanel {
			ref.SHA256, ref.Panel = "", cert.Domain
		}
		cfg.Certs = append(cfg.Certs, ref)
	}
	if cfg.Firewall == nil {
		return errors.New("browser test requires managed firewall plan")
	}
	cfg.Firewall.Ports = append(cfg.Firewall.Ports, contracts.FirewallPort{Port: plan.Port, Protocol: "tcp"}, contracts.FirewallPort{Port: plan.Port, Protocol: "udp"}, contracts.FirewallPort{Port: plan.RTCPort, Protocol: "udp"})
	return nil
}
