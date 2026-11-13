package center

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/shusfun/weir/internal/kernelconf"
	"github.com/shusfun/weir/internal/nodeprobe"
	"github.com/shusfun/weir/internal/store/ent"
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
	Status          string          `json:"status"`
	Why             string          `json:"why,omitempty"`
	TCP             nodeprobe.Check `json:"tcp"`
	UDP             nodeprobe.Check `json:"udp"`
	At              time.Time       `json:"at"`
	JobID           string          `json:"job_id"`
	TraceID         string          `json:"trace_id"`
	Location        string          `json:"location"` // center / colocated
	ExecutionGateID int             `json:"execution_gate_id,omitempty"`
	External        string          `json:"external"` // passed / failed / unknown
	UserID          int             `json:"user_id,omitempty"`
	LinkID          int             `json:"link_id,omitempty"`
	CredentialID    int             `json:"credential_id,omitempty"`
	Binding         string          `json:"-"`
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
	if validTraceHex(token, 48) {
		_, found, err := s.Store.KV.GetDel(c.Request.Context(), "node-probe-grant:"+digest(token))
		if err != nil {
			failure(c, 503, "probe_grant_unavailable")
			return
		}
		if found {
			c.Header("Cache-Control", "no-store")
			c.Data(http.StatusOK, "application/octet-stream", nodeprobe.Payload(token))
			return
		}
	}
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

func (s *Server) issueProbeGrant(ctx context.Context) (string, func(), error) {
	token := randomHex(24)
	key := "node-probe-grant:" + digest(token)
	if err := s.Store.KV.Set(ctx, key, "1", 3*time.Minute); err != nil {
		return "", func() {}, err
	}
	return token, func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		_ = s.Store.KV.Del(cleanup, key)
	}, nil
}

func (s *Server) probeSlots() chan struct{} {
	s.nodeProbes.Lock()
	defer s.nodeProbes.Unlock()
	if s.nodeProbes.slots == nil {
		s.nodeProbes.slots = make(chan struct{}, 1)
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
	var execution struct {
		GateID int `json:"gate_id"`
	}
	if c.Request.ContentLength != 0 && !bindWorker(c, &execution, 4096) {
		return
	}
	if execution.GateID < 0 {
		failure(c, 400, "invalid_gate_id")
		return
	}
	if execution.GateID > 0 {
		g, err := s.Store.Ent.Gate.Get(c.Request.Context(), execution.GateID)
		if err != nil || !gateOnline(g) {
			failure(c, 422, "probe_gate_unavailable")
			return
		}
		supported, err := s.gateSupportsTask(c.Request.Context(), g, contracts.TaskNodeProbe, 0)
		if err != nil {
			failInternal(c, 503, "capabilities_unavailable", err)
			return
		}
		if !supported {
			failure(c, 409, "agent_upgrade_required")
			return
		}
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
	gate, err := s.Store.Ent.Gate.Get(c.Request.Context(), n.GateID)
	if err != nil {
		s.nodeProbes.Unlock()
		failure(c, 404, "gate_not_found")
		return
	}
	binding, err := s.probeFingerprint(c.Request.Context(), n, gate)
	if err != nil {
		s.nodeProbes.Unlock()
		failInternal(c, 503, "probe_unavailable", err)
		return
	}
	if ht.ID == "" {
		ht = newHTTPTrace(request)
	}
	queued := pendingNodeProbe{NodeID: id, Actor: actor, Binding: binding, Trace: ht, Deployed: true, ExecutionGateID: execution.GateID}
	queuedJSON, _ := json.Marshal(queued)
	if err := s.Store.KV.Set(c.Request.Context(), pendingNodeProbePrefix+itoa(id), string(queuedJSON), 0); err != nil {
		s.nodeProbes.Unlock()
		failInternal(c, 503, "probe_queue_unavailable", err)
		return
	}
	s.nodeProbes.jobs[id] = jobID
	j := s.jobs.StartWith(jobSpec{ID: jobID, Kind: "action", Title: "实测节点「" + n.Name + "」", Owner: actor, Trace: ht.ID, CancelMsg: "节点测试已取消"},
		func(ctx context.Context, job JobRun) (map[string]any, error) {
			ctx = context.WithValue(ctx, nodeProbeExecutionKey{}, execution.GateID)
			ctx = context.WithValue(withTrace(ctx, request), httpTraceKey{}, ht)
			ctx = context.WithValue(ctx, jobCtxKey{}, &job)
			s.state.RLock()
			defer s.state.RUnlock()
			initial := nodeTestResult{Status: "running", JobID: jobID, TraceID: ht.ID, At: time.Now().UTC(), Binding: binding, TCP: nodeprobe.Check{Status: "unknown"}, UDP: nodeprobe.Check{Status: "unknown"}, External: "unknown"}
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
				r.Status, r.Why, r.External = "unknown", "测试已取消", "unknown"
				r.TCP, r.UDP = nodeprobe.Check{Status: "unknown"}, nodeprobe.Check{Status: "unknown"}
			}
			r.At = time.Now().UTC()
			r.Binding = binding
			if err := s.storeNodeTest(cleanup, id, r); err != nil {
				r.Status, r.Why, r.External = "unknown", "业务配置已变化或结果保存失败，本次结果未知", "unknown"
				r.TCP, r.UDP = nodeprobe.Check{Status: "unknown"}, nodeprobe.Check{Status: "unknown"}
				slog.Error("node test result", "trace", ht.ID, "node", id, "err", redact(err.Error()))
			} else if err := s.clearPendingNodeProbe(cleanup, pendingNodeProbePrefix+itoa(id), string(queuedJSON)); err != nil {
				slog.Error("node test queue receipt", "trace", ht.ID, "node", id, "err", redact(err.Error()))
			}
			_, auditErr := s.Store.Ent.Audit.Create().SetActor(actor).SetAction("node.test").SetTarget("node:" + itoa(id)).SetResult(r.Status).SetIP(ip).
				SetDetail(map[string]any{"trace_id": ht.ID, "request_id": request, "job_id": r.JobID, "user_id": r.UserID, "link_id": r.LinkID, "location": r.Location, "execution_gate_id": r.ExecutionGateID, "tcp": r.TCP.Status, "udp": r.UDP.Status}).Save(cleanup)
			if auditErr != nil {
				slog.Error("node test audit", "trace", ht.ID, "node", id, "err", redact(auditErr.Error()))
			}
			slog.Info("node test", "trace", ht.ID, "request", request, "job", r.JobID, "node", id, "user", actor, "status", r.Status, "tcp", r.TCP.Status, "udp", r.UDP.Status, "detail", r.Why)
			if ctx.Err() == nil && (r.Status == "failed" || r.Status == "unknown") {
				_, observeErr := s.enqueueObservation(cleanup, observationInput{Point: "node.test", Resource: "node:" + itoa(id), Text: r.Why + "\n" + r.TCP.Why + "\n" + r.UDP.Why, Origin: "center.startDeployedNodeTest"})
				s.tasks.note("observation-producer", observeErr)
			}
			if ctx.Err() == nil && r.Status == "failed" {
				s.triggerOps(cleanup, n.GateID, "node.test", "节点 "+itoa(id)+" 实测失败："+r.Why)
			}
			s.notifyConsole("gate", itoa(n.GateID))
			body := map[string]any{"test": r}
			if ctx.Err() != nil {
				return map[string]any{"status": 200, "body": body}, ctx.Err()
			}
			if r.Status == "failed" {
				return map[string]any{"status": 200, "body": body}, errors.New("节点实测失败")
			}
			return map[string]any{"status": 200, "body": body}, nil
		})
	s.nodeProbes.Unlock()
	c.JSON(202, gin.H{"job": j})
}

func nodeClient(link string) (contracts.NodeTestClient, error) {
	proxy, err := subout.ClashProxy(link)
	if err != nil {
		return contracts.NodeTestClient{}, err
	}
	return contracts.NodeTestClient{Proxies: []map[string]any{proxy}, Target: proxy["name"].(string)}, nil
}

func (s *Server) nodeTestBinding(ctx context.Context, n *ent.Node, g *ent.Gate, selected *ent.Credential) (string, error) {
	if !n.Enabled || g.Status != "active" {
		return "", errors.New("节点已停用或 VPS 已撤销")
	}
	return s.probeFingerprint(ctx, n, g)
}

var externalProbeSupported = func() bool {
	return runtime.GOOS == "linux" && (runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64")
}

// Tests replace execution, not credential selection or result invalidation.
var deployedNodeProbe = nodeprobe.Run
var deployedProbeBinary = func(s *Server, ctx context.Context) (string, kernelconf.Driver, error) {
	bin, version, err := defaultSubCheckBinary(s, ctx, "mihomo")
	if err != nil {
		return "", kernelconf.Driver{}, err
	}
	d, _, err := s.drivers(ctx).Select("mihomo", version)
	return bin, d, err
}

type nodeProbeExecutionKey struct{}

func (s *Server) runDeployedNodeTest(ctx context.Context, id, actor int, job JobRun) (r nodeTestResult, resultErr error) {
	trace, _ := ctx.Value(httpTraceKey{}).(httpTrace)
	r = nodeTestResult{Status: "unknown", JobID: job.e.job.ID, TraceID: trace.ID, Location: "center", External: "unknown", TCP: nodeprobe.Check{Status: "unknown"}, UDP: nodeprobe.Check{Status: "unknown"}}
	job.StepFor("正在等待其他实测完成", 5*time.Minute)
	queue, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	select {
	case s.probeSlots() <- struct{}{}:
		defer func() { <-s.probeSlots() }()
	case <-queue.Done():
		return r, queue.Err()
	}
	resultErr = s.Store.Lock.Do(queue, "internal-node-probes", func(context.Context) error {
		var err error
		r, err = s.runDeployedNodeTestNow(ctx, id, actor, job)
		return err
	})
	return r, resultErr
}

func (s *Server) runDeployedNodeTestNow(ctx context.Context, id, actor int, job JobRun) (r nodeTestResult, resultErr error) {
	executionCtx := ctx
	ht, _ := ctx.Value(httpTraceKey{}).(httpTrace)
	r = nodeTestResult{Status: "unknown", TCP: nodeprobe.Check{Status: "unknown"}, UDP: nodeprobe.Check{Status: "unknown"}, JobID: job.e.job.ID, TraceID: ht.ID, Location: "center", External: "unknown"}
	executorID, _ := ctx.Value(nodeProbeExecutionKey{}).(int)
	if executorID > 0 {
		r.Location = "gate"
		r.ExecutionGateID = executorID
	}
	job.StepFor("正在排队等待节点测试", 5*time.Minute)
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	var bin string
	var driver kernelconf.Driver
	var err error
	if executorID == 0 {
		bin, driver, err = deployedProbeBinary(s, ctx)
		if err != nil {
			return r, err
		}
	}
	origin := s.centerOrigin(ctx)
	if !strings.HasPrefix(origin, "https://") {
		return r, errors.New("中心尚未记录 HTTPS 公共地址，无法准备下载测试端点")
	}
	job.StepFor("正在读取已部署配置并准备短时内部授权", 5*time.Minute)
	n, err := s.Store.Ent.Node.Get(ctx, id)
	if err != nil {
		return r, err
	}
	g, err := s.Store.Ent.Gate.Get(ctx, n.GateID)
	if err != nil {
		return r, err
	}
	if executorID == 0 && colocated(g) {
		r.Location = "colocated"
	}
	if !n.Enabled || g.Status != "active" || g.LastSeenAt == nil || time.Since(*g.LastSeenAt) > 90*time.Second {
		return r, errors.New("节点已停用或 VPS 离线／已撤销")
	}
	if executorID == 0 && colocated(g) {
		return r, errors.New("中心与这台 VPS 在同一台机器上，无法验证外部连接")
	}
	if executorID == 0 && !externalProbeSupported() {
		return r, errors.New("中心不是 Linux amd64／arm64，无法执行外部实测")
	}
	selected, err := s.probeCredential(ctx, n)
	if err != nil {
		return r, err
	}
	r.CredentialID = selected.ID
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
	if !n.Enabled || g.KernelsRunning[n.Kernel] == "" {
		return r, errors.New("节点已停用或服务端内核尚未运行")
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
	token, revoke, err := s.issueProbeGrant(executionCtx)
	if err != nil {
		return r, err
	}
	defer revoke()
	// 等待配置的 ctx 到这里结束。出口查询必须用还活着的 executionCtx，
	// 取消后的查询失败会被当成「未知」，于是照常发 UDP，把「出口不支持」测成超时。
	cancel()
	udp := n.Protocol != "http" && n.Protocol != "shadowtls"
	if supported, known := s.nodeUDP(executionCtx, n); known && !supported {
		udp = false
	}
	var res nodeprobe.Result
	var probeErr error
	probeTarget := strings.TrimRight(origin, "/") + "/api/v1/node-test/download"
	if executorID > 0 {
		executor, e := s.Store.Ent.Gate.Get(executionCtx, executorID)
		if e != nil {
			return r, e
		}
		kernels, drivers, e := s.gateKernels(executionCtx, executor, map[string]bool{"mihomo": true})
		if e != nil {
			return r, e
		}
		request := contracts.NodeProbeTask{Client: client, UDP: udp, Credentials: contracts.FetchSecrets{URL: probeTarget, Authorization: token}, Kernels: kernels, Drivers: drivers}
		job.StepFor("正在指定 VPS 上实测已部署节点", 2*time.Minute)
		raw, _, e := s.dispatchWorkerTask(executionCtx, contracts.TaskNodeProbe, request, contracts.TaskRequirements{TargetGateID: executorID}, []int{executorID})
		if e != nil {
			return r, e
		}
		if e = contracts.DecodeStrict(raw, &res, 64<<10); e != nil {
			return r, e
		}
		res.TCP.Why, res.UDP.Why = probeReasonText(res.TCP.Why), probeReasonText(res.UDP.Why)
	} else {
		res, probeErr = deployedNodeProbe(executionCtx, nodeprobe.Options{Binary: bin, Run: driver.Run, Check: driver.Check, Client: client, UDP: udp, Target: probeTarget, Token: token, Traceparent: ht.parent(), Step: func(step string) { job.StepFor(step, 60*time.Second) }})
	}
	r.TCP, r.UDP = res.TCP, res.UDP
	if supported, known := s.nodeUDP(executionCtx, n); known && !supported {
		r.UDP = nodeprobe.Check{Status: "not_applicable", Why: "出口不支持"}
	}
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
		var reasons []string
		for _, check := range []nodeprobe.Check{r.TCP, r.UDP} {
			if check.Status == "failed" && check.Why != "" {
				reasons = append(reasons, check.Why)
			}
		}
		r.Why = strings.Join(reasons, "；")
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
	if r.Location != "colocated" && executorID != g.ID {
		r.External = r.Status
	}
	if executorID == g.ID {
		r.Why = "在节点所在 VPS 上实测，外部可达性未知。" + r.Why
	}
	return r, nil
}

func (s *Server) storeNodeTest(ctx context.Context, id int, r nodeTestResult) error {
	s.nodeProbes.Lock()
	defer s.nodeProbes.Unlock()
	n, err := s.Store.Ent.Node.Get(ctx, id)
	if err != nil {
		return err
	}
	if r.Binding == "" {
		b, err := json.Marshal(storedNodeTest{Result: r})
		if err != nil {
			return err
		}
		return s.Store.KV.Set(ctx, nodeTestKey(id), string(b), 24*time.Hour)
	}
	g, err := s.Store.Ent.Gate.Get(ctx, n.GateID)
	if err != nil {
		return err
	}
	binding, err := s.nodeTestBinding(ctx, n, g, nil)
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
	binding, err := s.nodeTestBinding(ctx, n, g, nil)
	if err == nil && binding != stored.Binding {
		err = errors.New("节点配置已变化")
	}

	if err != nil {
		r.Status, r.Why, r.External = "unknown", "业务配置已变化，上次实测结果已失效", "unknown"
		r.TCP, r.UDP = nodeprobe.Check{Status: "unknown"}, nodeprobe.Check{Status: "unknown"}
	}
	return &r
}
