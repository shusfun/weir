package center

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/shusfun/weir/internal/kernelconf"
	"github.com/shusfun/weir/internal/nodeprobe"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/credential"
	"github.com/shusfun/weir/internal/subout"
	"github.com/shusfun/weir/packages/contracts"
)

type nodeProbeState struct {
	sync.Mutex
	slots   chan struct{}
	targets map[string]time.Time
	jobs    map[int]string
}

type nodeTestResult struct {
	Status       string          `json:"status"`
	Why          string          `json:"why,omitempty"`
	TCP          nodeprobe.Check `json:"tcp"`
	UDP          nodeprobe.Check `json:"udp"`
	At           time.Time       `json:"at"`
	JobID        string          `json:"job_id"`
	TraceID      string          `json:"trace_id"`
	Location     string          `json:"location"` // center / colocated
	External     string          `json:"external"` // passed / failed / unknown
	UserID       int             `json:"user_id,omitempty"`
	LinkID       int             `json:"link_id,omitempty"`
	CredentialID int             `json:"credential_id,omitempty"`
	Binding      string          `json:"-"`
}

type storedNodeTest struct {
	Result  nodeTestResult `json:"result"`
	Binding string         `json:"binding"`
}

func nodeTestJobID(id int) string { return "node-test:" + itoa(id) }
func nodeTestKey(id int) string   { return "test:node:" + itoa(id) }

func (s *Server) nodeTestJob(id int) (Job, bool) {
	s.nodeProbes.Lock()
	jobID := s.nodeProbes.jobs[id]
	s.nodeProbes.Unlock()
	return s.jobs.Get(jobID)
}

// nodeTestDownload accepts a single bounded download per short-lived grant.
// A fixed path keeps the bearer credential out of proxy access logs.
func (s *Server) nodeTestDownload(c *gin.Context) {
	token := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
	s.nodeProbes.Lock()
	until, ok := s.nodeProbes.targets[token]
	delete(s.nodeProbes.targets, token)
	s.nodeProbes.Unlock()
	if !ok || time.Now().After(until) {
		failure(c, 404, "not_found")
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "application/octet-stream", nodeprobe.Payload(token))
}

func (s *Server) probeGrant() (string, func()) {
	token := randomHex(24)
	s.nodeProbes.Lock()
	if s.nodeProbes.targets == nil {
		s.nodeProbes.targets = map[string]time.Time{}
	}
	for key, until := range s.nodeProbes.targets {
		if time.Now().After(until) {
			delete(s.nodeProbes.targets, key)
		}
	}
	s.nodeProbes.targets[token] = time.Now().Add(90 * time.Second)
	s.nodeProbes.Unlock()
	return token, func() { s.nodeProbes.Lock(); delete(s.nodeProbes.targets, token); s.nodeProbes.Unlock() }
}

func (s *Server) probeSlots() chan struct{} {
	s.nodeProbes.Lock()
	defer s.nodeProbes.Unlock()
	if s.nodeProbes.slots == nil {
		s.nodeProbes.slots = make(chan struct{}, 2)
	}
	return s.nodeProbes.slots
}

func (s *Server) startDeployedNodeTest(c *gin.Context) {
	n, ok := s.nodeParam(c)
	if !ok {
		return
	}
	if !n.Enabled {
		failDetail(c, 422, "node_test_failed", "节点已停用", nil)
		return
	}
	id, actor, ip := n.ID, c.GetInt("user_id"), c.ClientIP()
	ht, _ := c.Request.Context().Value(httpTraceKey{}).(httpTrace)
	request := c.GetString("request_id")
	s.nodeProbes.Lock()
	if current, ok := s.jobs.Get(s.nodeProbes.jobs[id]); ok && current.Status == jobRunning {
		s.nodeProbes.Unlock()
		c.JSON(202, gin.H{"job": current})
		return
	}
	if s.nodeProbes.jobs == nil {
		s.nodeProbes.jobs = map[int]string{}
	}
	jobID := nodeTestJobID(id) + ":" + randomHex(6)
	s.nodeProbes.jobs[id] = jobID
	j := s.jobs.StartWith(jobSpec{ID: jobID, Kind: "action", Title: "实测节点「" + n.Name + "」", Owner: actor, Trace: ht.ID, CancelMsg: "节点测试已取消"},
		func(ctx context.Context, job JobRun) (map[string]any, error) {
			ctx = context.WithValue(withTrace(ctx, request), httpTraceKey{}, ht)
			ctx = context.WithValue(ctx, jobCtxKey{}, &job)
			s.state.RLock()
			defer s.state.RUnlock()
			initial := nodeTestResult{Status: "running", JobID: jobID, TraceID: ht.ID, At: time.Now().UTC(), TCP: nodeprobe.Check{Status: "unknown"}, UDP: nodeprobe.Check{Status: "unknown"}, External: "unknown"}
			if err := s.storeNodeTest(ctx, id, initial); err != nil {
				return nil, err
			}
			r, err := s.runDeployedNodeTest(ctx, id, actor, job)
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
			defer cancel()
			if err != nil {
				r.Why = safeCheckOutput(err.Error())
			}
			if ctx.Err() != nil {
				r.Status, r.Why = "unknown", "测试已取消"
			}
			r.At = time.Now().UTC()
			if err := s.storeNodeTest(cleanup, id, r); err != nil {
				r.Status, r.Why, r.External, r.Binding = "unknown", "配置或凭据已变化，本次结果已失效", "unknown", ""
				r.TCP, r.UDP = nodeprobe.Check{Status: "unknown"}, nodeprobe.Check{Status: "unknown"}
				if saveErr := s.storeNodeTest(cleanup, id, r); saveErr != nil {
					slog.Error("node test result", "trace", ht.ID, "node", id, "err", redact(saveErr.Error()))
				}
			}
			_, auditErr := s.Store.Ent.Audit.Create().SetActor(actor).SetAction("node.test").SetTarget("node:" + itoa(id)).SetResult(r.Status).SetIP(ip).
				SetDetail(map[string]any{"trace_id": ht.ID, "request_id": request, "job_id": r.JobID, "user_id": r.UserID, "link_id": r.LinkID, "tcp": r.TCP.Status, "udp": r.UDP.Status}).Save(cleanup)
			if auditErr != nil {
				slog.Error("node test audit", "trace", ht.ID, "node", id, "err", redact(auditErr.Error()))
			}
			slog.Info("node test", "trace", ht.ID, "request", request, "job", r.JobID, "node", id, "user", actor, "status", r.Status, "tcp", r.TCP.Status, "udp", r.UDP.Status, "detail", r.Why)
			s.notifyConsole("gate", itoa(n.GateID))
			body := map[string]any{"test": r}
			if r.Status == "failed" {
				return map[string]any{"status": 200, "body": body}, errors.New("节点实测失败")
			}
			return map[string]any{"status": 200, "body": body}, nil
		})
	s.nodeProbes.Unlock()
	c.JSON(202, gin.H{"job": j})
}

// selectNodeTestCredential only chooses normal node access, never internal hops or user chains.
func (s *Server) selectNodeTestCredential(ctx context.Context, n *ent.Node, g *ent.Gate, actor, exact int) (*ent.Credential, error) {
	cfg := contracts.GateConfig{Budgets: map[string]contracts.GateBudget{}}
	if _, err := s.addCredentials(ctx, g, &cfg, []int{n.ID}); err != nil {
		return nil, err
	}
	active := map[int]bool{}
	for _, c := range cfg.Credentials {
		b := cfg.Budgets[c.UserKey]
		if !c.Internal && c.InWindow(time.Now()) && (b.Unlimited || b.Allocated > b.Used) {
			active[int(c.ID)] = true
		}
	}
	q := s.Store.Ent.Credential.Query().Where(credential.NodeIDEQ(n.ID), credential.InternalEQ(false), credential.ChainIDIsNil(), credential.ProbeParentIDIsNil(), credential.UserIDNotNil(), credential.LinkIDNotNil())
	if exact != 0 {
		q = q.Where(credential.IDEQ(exact))
	}
	creds, err := q.Order(ent.Asc(credential.FieldID)).All(ctx)
	if err != nil {
		return nil, err
	}
	var first *ent.Credential
	for _, c := range creds {
		if !active[c.ID] {
			continue
		}
		u, err := s.Store.Ent.User.Get(ctx, *c.UserID)
		if err != nil {
			return nil, err
		}
		allowed, err := s.userNodeIDs(ctx, u)
		if err != nil {
			return nil, err
		}
		if allowed[n.ID] == nil || n.Protocol == "wireguard" && u.Quota != 0 {
			continue
		}
		l, err := s.Store.Ent.Link.Get(ctx, *c.LinkID)
		if err != nil {
			return nil, err
		}
		if l.UserID != u.ID || !l.AllNodes && !contains(l.NodeIds, n.ID) {
			continue
		}
		if first == nil {
			first = c
		}
		if *c.UserID == actor {
			return c, nil
		}
	}
	if first != nil {
		return first, nil
	}
	return nil, errors.New("没有有效普通节点凭据，请检查用户授权、订阅有效期和可用额度")
}

func sameCredentialOwner(a, b *ent.Credential) bool {
	return a.UserID != nil && b.UserID != nil && *a.UserID == *b.UserID && a.LinkID != nil && b.LinkID != nil && *a.LinkID == *b.LinkID && a.NodeID == b.NodeID && a.ChainID == nil && b.ChainID == nil && !a.Internal && !b.Internal
}

func (s *Server) wireGuardProbeCredential(ctx context.Context, parent *ent.Credential) (*ent.Credential, error) {
	var out *ent.Credential
	created := false
	err := s.Store.Lock.Do(ctx, "cred-sync", func(ctx context.Context) error {
		var err error
		current, err := s.Store.Ent.Credential.Get(ctx, parent.ID)
		if err != nil {
			return err
		}
		if !sameCredentialOwner(current, parent) || current.ProbeParentID != nil {
			return errors.New("凭据已变化")
		}
		out, err = s.Store.Ent.Credential.Query().Where(credential.ProbeParentIDEQ(parent.ID)).Only(ctx)
		if err == nil && sameCredentialOwner(out, current) {
			return nil
		}
		if err != nil && !ent.IsNotFound(err) {
			return err
		}
		_, password := newSecret("wireguard")
		out, err = s.Store.Ent.Credential.Create().SetNodeID(parent.NodeID).SetUserID(*parent.UserID).SetLinkID(*parent.LinkID).
			SetProbeParentID(parent.ID).SetStatsKey(newStatsKey()).SetPassword(password).Save(ctx)
		created = err == nil
		return err
	})
	if err == nil && created {
		n, e := s.Store.Ent.Node.Get(ctx, parent.NodeID)
		if e != nil {
			return nil, e
		}
		err = s.bumpGates(ctx, []int{n.GateID})
	}
	return out, err
}

func nodeClient(link string) (contracts.NodeTestClient, error) {
	body, _, err := subout.Render([]string{link}, "singbox")
	if err != nil {
		return contracts.NodeTestClient{}, err
	}
	var doc contracts.NodeTestClient
	if err = json.Unmarshal(body, &doc); err != nil {
		return doc, err
	}
	var outs []map[string]any
	for _, o := range doc.Outbounds {
		switch o["type"] {
		case "selector", "urltest", "direct", "block", "dns":
			continue
		}
		outs = append(outs, o)
		if o["type"] != "shadowtls" {
			doc.Target, _ = o["tag"].(string)
		}
	}
	doc.Outbounds = outs
	for _, ep := range doc.Endpoints {
		doc.Target, _ = ep["tag"].(string)
	}
	if doc.Target == "" {
		return doc, errors.New("客户端配置为空")
	}
	return doc, nil
}

func (s *Server) nodeTestBinding(ctx context.Context, n *ent.Node, g *ent.Gate, selected *ent.Credential) (string, error) {
	if !n.Enabled || g.Status != "active" || g.AppliedRevision < g.ConfigRevision {
		return "", errors.New("节点配置已变化")
	}
	parent := selected
	if selected.ProbeParentID != nil {
		var err error
		parent, err = s.Store.Ent.Credential.Get(ctx, *selected.ProbeParentID)
		if err != nil || !sameCredentialOwner(selected, parent) {
			return "", errors.New("检测凭据已撤销")
		}
	}
	if _, err := s.selectNodeTestCredential(ctx, n, g, 0, parent.ID); err != nil {
		return "", err
	}
	cert := ""
	if n.CertID != nil {
		c, err := s.Store.Ent.Certificate.Get(ctx, *n.CertID)
		if err != nil {
			return "", err
		}
		cert = c.CertPem
	}
	b, err := json.Marshal([]any{n.ID, n.Protocol, n.Network, n.Security, n.Port, n.Settings, n.CertID, cert, n.ExitID, g.ConfigRevision, g.PublicHost, selected.ID, selected.UUID, selected.Password, parent.ID, parent.UUID, parent.Password, parent.UserID, parent.LinkID})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(b)), nil
}

// Tests replace execution, not credential selection or result invalidation.
var deployedNodeProbe = nodeprobe.Run
var deployedProbeBinary = func(s *Server, ctx context.Context) (string, kernelconf.Driver, error) {
	bin, version, err := defaultSubCheckBinary(s, ctx, "sing-box")
	if err != nil {
		return "", kernelconf.Driver{}, err
	}
	d, _, err := s.drivers(ctx).Select("sing-box", version)
	return bin, d, err
}

func (s *Server) runDeployedNodeTest(ctx context.Context, id, actor int, job JobRun) (r nodeTestResult, resultErr error) {
	executionCtx := ctx
	ht, _ := ctx.Value(httpTraceKey{}).(httpTrace)
	r = nodeTestResult{Status: "unknown", TCP: nodeprobe.Check{Status: "unknown"}, UDP: nodeprobe.Check{Status: "unknown"}, JobID: job.e.job.ID, TraceID: ht.ID, Location: "center", External: "unknown"}
	job.StepFor("正在排队等待节点测试", 5*time.Minute)
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	slots := s.probeSlots()
	select {
	case slots <- struct{}{}:
		defer func() { <-slots }()
	case <-ctx.Done():
		return r, ctx.Err()
	}
	bin, driver, err := deployedProbeBinary(s, ctx)
	if err != nil {
		return r, err
	}
	origin := s.centerOrigin(ctx)
	if !strings.HasPrefix(origin, "https://") {
		return r, errors.New("中心尚未记录 HTTPS 公共地址，无法准备下载测试端点")
	}
	job.StepFor("正在读取已部署配置与有效用户凭据", 5*time.Minute)
	n, err := s.Store.Ent.Node.Get(ctx, id)
	if err != nil {
		return r, err
	}
	g, err := s.Store.Ent.Gate.Get(ctx, n.GateID)
	if err != nil {
		return r, err
	}
	if colocated(g) {
		r.Location = "colocated"
	}
	if !n.Enabled || g.Status != "active" || g.LastSeenAt == nil || time.Since(*g.LastSeenAt) > 90*time.Second {
		return r, errors.New("节点已停用或 VPS 离线／已撤销")
	}
	parent, err := s.selectNodeTestCredential(ctx, n, g, actor, 0)
	if err != nil {
		return r, err
	}
	selected := parent
	if n.Protocol == "wireguard" {
		selected, err = s.wireGuardProbeCredential(ctx, parent)
		if err != nil {
			return r, err
		}
	}
	r.UserID, r.LinkID, r.CredentialID = *selected.UserID, *selected.LinkID, selected.ID
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		g, err = s.Store.Ent.Gate.Get(ctx, n.GateID)
		if err != nil {
			return r, err
		}
		if g.FailedRevision >= g.ConfigRevision && g.FailedRevision > 0 {
			return r, errors.New("VPS 配置应用失败：" + g.LastError)
		}
		if g.AppliedRevision >= g.ConfigRevision {
			break
		}
		job.StepFor("正在等待 VPS 应用配置", 5*time.Minute)
		select {
		case <-ctx.Done():
			return r, ctx.Err()
		case <-ticker.C:
		}
	}
	n, err = s.Store.Ent.Node.Get(ctx, id)
	if err != nil {
		return r, err
	}
	if state, why := s.nodeReadiness(ctx, n, g); state != "ready" {
		return r, errors.New(why)
	}
	selected, err = s.Store.Ent.Credential.Get(ctx, selected.ID)
	if err != nil {
		return r, err
	}
	r.Binding, err = s.nodeTestBinding(ctx, n, g, selected)
	if err != nil {
		return r, err
	}
	var cert *ent.Certificate
	if n.CertID != nil {
		cert, err = s.Store.Ent.Certificate.Get(ctx, *n.CertID)
		if err != nil {
			return r, err
		}
	}
	link, err := shareLink(n, g.PublicHost, selected, "node-test", cert)
	if err != nil {
		return r, err
	}
	client, err := nodeClient(link)
	if err != nil {
		return r, err
	}
	token, revoke := s.probeGrant()
	defer revoke()
	cancel()
	udp := n.Protocol != "http" && n.Protocol != "shadowtls"
	res, probeErr := deployedNodeProbe(executionCtx, nodeprobe.Options{Binary: bin, Run: driver.Run, Check: driver.Check, Client: client, UDP: udp, Target: strings.TrimRight(origin, "/") + "/api/v1/node-test/download", Token: token, Traceparent: ht.parent(), Step: func(step string) { job.StepFor(step, 60*time.Second) }})
	r.TCP, r.UDP = res.TCP, res.UDP
	clean := func(msg string) string {
		for _, secret := range []string{selected.UUID, selected.Password, token, nodeSetting(n.Settings, "ss_password")} {
			if secret != "" {
				msg = strings.ReplaceAll(msg, secret, "[redacted]")
			}
		}
		return safeCheckOutput(msg)
	}
	r.TCP.Why, r.UDP.Why = clean(r.TCP.Why), clean(r.UDP.Why)
	switch {
	case r.TCP.Status == "failed" || r.UDP.Status == "failed":
		r.Status = "failed"
		r.Why = strings.TrimSpace(strings.Join([]string{r.TCP.Why, r.UDP.Why}, " "))
		if probeErr != nil && clean(probeErr.Error()) != "" {
			r.Why += "；" + clean(probeErr.Error())
		}
	case r.TCP.Status == "passed" && (r.UDP.Status == "passed" || r.UDP.Status == "not_applicable"):
		r.Status = "passed"
	default:
		if probeErr != nil {
			r.Why = clean(probeErr.Error())
		}
	}
	if r.Location != "colocated" {
		r.External = r.Status
	}
	return r, nil
}

func (s *Server) storeNodeTest(ctx context.Context, id int, r nodeTestResult) error {
	if r.Binding == "" {
		b, err := json.Marshal(storedNodeTest{Result: r})
		if err != nil {
			return err
		}
		return s.Store.KV.Set(ctx, nodeTestKey(id), string(b), 24*time.Hour)
	}
	n, err := s.Store.Ent.Node.Get(ctx, id)
	if err != nil {
		return err
	}
	g, err := s.Store.Ent.Gate.Get(ctx, n.GateID)
	if err != nil {
		return err
	}
	c, err := s.Store.Ent.Credential.Get(ctx, r.CredentialID)
	if err != nil {
		return err
	}
	binding, err := s.nodeTestBinding(ctx, n, g, c)
	if err != nil {
		return err
	}
	if binding != r.Binding {
		return errors.New("节点配置已变化")
	}
	b, err := json.Marshal(storedNodeTest{Result: r, Binding: r.Binding})
	if err != nil {
		return err
	}
	return s.Store.KV.Set(ctx, nodeTestKey(id), string(b), 24*time.Hour)
}

func (s *Server) loadNodeTest(ctx context.Context, n *ent.Node) *nodeTestResult {
	if job, ok := s.nodeTestJob(n.ID); ok && job.Status == jobRunning {
		return &nodeTestResult{Status: "running", Why: job.Step, At: job.Started, JobID: job.ID, TraceID: job.Trace, TCP: nodeprobe.Check{Status: "unknown"}, UDP: nodeprobe.Check{Status: "unknown"}, External: "unknown"}
	}
	raw, ok, _ := s.Store.KV.Get(ctx, nodeTestKey(n.ID))
	var stored storedNodeTest
	if !ok || json.Unmarshal([]byte(raw), &stored) != nil {
		return nil
	}
	r := stored.Result
	if r.Status == "running" {
		r.Status, r.Why = "unknown", "中心重启过或任务记录已丢失，请重新测试"
	}
	if stored.Binding == "" {
		return &r
	}
	g, err := s.Store.Ent.Gate.Get(ctx, n.GateID)
	if err != nil {
		return nil
	}
	c, err := s.Store.Ent.Credential.Get(ctx, r.CredentialID)
	if err == nil {
		var binding string
		binding, err = s.nodeTestBinding(ctx, n, g, c)
		if binding != stored.Binding {
			err = errors.New("节点配置已变化")
		}
	}
	if err != nil {
		r.Status, r.Why, r.External = "unknown", "配置或凭据已变化，上次实测结果已失效", "unknown"
		r.TCP, r.UDP = nodeprobe.Check{Status: "unknown"}, nodeprobe.Check{Status: "unknown"}
	}
	return &r
}
