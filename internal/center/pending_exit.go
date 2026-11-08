package center

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/chain"
)

// 候选与已生效资源分开保存；密文中包含请求，公开状态不包含出口凭据。
type pendingExit struct {
	ID       string          `json:"id"`
	Kind     string          `json:"kind"`
	Actor    int             `json:"actor"`
	Subject  int             `json:"subject"`
	Admin    bool            `json:"admin"`
	Params   gin.Params      `json:"params"`
	Method   string          `json:"method"`
	Query    string          `json:"query"`
	Body     json.RawMessage `json:"body,omitempty"`
	Binding  string          `json:"binding"`
	Status   string          `json:"status"`
	Revision int             `json:"revision"`
	Reason   string          `json:"reason"`
	Trace    string          `json:"trace"`
	At       time.Time       `json:"at"`
	GateID   int             `json:"gate_id,omitempty"`
	NodeID   int             `json:"node_id,omitempty"`
	ExitID   int             `json:"exit_id,omitempty"`
}

type exitVerificationError struct{ err error }

func (e exitVerificationError) Error() string { return e.err.Error() }
func (e exitVerificationError) Unwrap() error { return e.err }

func (p pendingExit) view() gin.H {
	return gin.H{"id": p.ID, "kind": p.Kind, "user_id": p.Subject, "revision": p.Revision, "status": p.Status, "reason": p.Reason, "trace_id": p.Trace, "at": p.At, "params": p.Params, "gate_id": p.GateID, "node_id": p.NodeID, "exit_id": p.ExitID}
}

func (s *Server) savePendingExit(ctx context.Context, p pendingExit) error {
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	cipher, err := s.seal(string(b))
	if err != nil {
		return err
	}
	return s.Store.KV.Set(ctx, "pending-exit:"+p.ID, cipher, 0)
}

func (s *Server) readPendingExit(ctx context.Context, id string) (pendingExit, error) {
	var p pendingExit
	raw, ok, err := s.Store.KV.Get(ctx, "pending-exit:"+id)
	if err != nil {
		return p, err
	}
	if !ok {
		return p, errors.New("candidate_not_found")
	}
	plain, err := s.reveal(raw)
	if err != nil {
		return p, err
	}
	err = json.Unmarshal([]byte(plain), &p)
	return p, err
}

// 只绑定业务配置和授权；短时凭据、Gate 应用版本与流量采样不改变候选有效性。
func (s *Server) exitChangeBinding(ctx context.Context, p pendingExit) (string, error) {
	actor, err := s.Store.Ent.User.Get(ctx, p.Actor)
	if err != nil {
		return "", err
	}
	u, err := s.Store.Ent.User.Get(ctx, p.Subject)
	if err != nil {
		return "", err
	}
	v := []any{actor.Role, actor.Enabled, u.Role, u.Enabled, u.AllowChain, u.AllNodes, u.NodeIds}
	var req struct {
		GateID int       `json:"gate_id"`
		Entry  int       `json:"entry_node_id"`
		ExitID int       `json:"exit_id"`
		Exit   *ExitSpec `json:"exit"`
	}
	_ = json.Unmarshal(p.Body, &req)
	if p.Kind == "exit.save" {
		var flat ExitSpec
		if err := json.Unmarshal(p.Body, &flat); err != nil {
			return "", err
		}
		req.Exit = &flat
	}
	if req.Exit != nil && req.Exit.Kind == "source" {
		links, e := s.specLinks(ctx, req.Exit.source())
		if e != nil {
			// 字段错误由业务处理器返回；缺少缓存的状态也参与指纹，恢复后旧候选不能生效。
			v = append(v, "source-unavailable", e.Error())
		}
		grants, e := s.grantedProfiles(ctx, p.Subject)
		if e != nil {
			return "", e
		}
		v = append(v, links, grants)
	}
	nodes := map[int]bool{}
	if req.Entry != 0 {
		nodes[req.Entry] = true
	}
	if req.Exit != nil && req.Exit.NodeID != 0 {
		nodes[req.Exit.NodeID] = true
	}
	for _, param := range p.Params {
		switch param.Key {
		case "id":
			if p.Kind == "node.update" {
				n, e := s.Store.Ent.Node.Get(ctx, pendingID(param.Value))
				if e != nil {
					return "", e
				}
				v = append(v, n)
				nodes[n.ID] = true
			}
		case "cid":
			ch, e := s.Store.Ent.Chain.Get(ctx, pendingID(param.Value))
			if e != nil {
				return "", e
			}
			v = append(v, ch)
			nodes[ch.EntryNodeID] = true
		case "eid":
			e, e2 := s.Store.Ent.Exit.Get(ctx, pendingID(param.Value))
			if e2 != nil {
				return "", e2
			}
			v = append(v, e, e.Password, e.Link)
		case "type":
			v = append(v, loadPolicy(u).Revision)
			cs, e := s.policyChains(ctx, u.ID, param.Value)
			if e != nil {
				return "", e
			}
			v = append(v, cs)
			ts, e := s.userTypes(ctx, u)
			if e != nil {
				return "", e
			}
			for _, t := range ts {
				if t.Key == param.Value {
					for _, n := range t.nodes {
						nodes[n.ID] = true
					}
				}
			}
		}
	}
	if req.ExitID != 0 {
		e, err := s.Store.Ent.Exit.Get(ctx, req.ExitID)
		if err != nil {
			return "", err
		}
		v = append(v, e, e.Password, e.Link)
	}
	ids := []int{}
	for id := range nodes {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	for _, id := range ids {
		n, e := s.Store.Ent.Node.Get(ctx, id)
		if e != nil {
			return "", e
		}
		g, e := s.Store.Ent.Gate.Get(ctx, n.GateID)
		if e != nil {
			return "", e
		}
		fp, e := s.probeFingerprint(ctx, n, g)
		if e != nil {
			return "", e
		}
		v = append(v, fp)
	}
	if req.GateID != 0 {
		g, e := s.Store.Ent.Gate.Get(ctx, req.GateID)
		if e != nil {
			return "", e
		}
		v = append(v, g.ID, g.Status, g.PublicHost)
	}
	if strings.HasPrefix(p.Kind, "chain.") {
		cs, e := s.Store.Ent.Chain.Query().Where(chain.UserIDEQ(u.ID)).Order(ent.Asc(chain.FieldID)).All(ctx)
		if e != nil {
			return "", e
		}
		v = append(v, cs)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}

func (s *Server) exitChange(c *gin.Context, kind string, admin bool, h gin.HandlerFunc) {
	if c.GetBool("pending_exit_replay") {
		h(c)
		return
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, (1<<20)+1))
	if err != nil || len(body) > 1<<20 {
		failure(c, 400, "invalid_request")
		return
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	actor := c.MustGet("account").(*ent.User)
	subject := actor.ID
	if raw := c.Param("uid"); raw != "" {
		subject = pendingID(raw)
	}
	if raw := c.Query("user"); raw != "" {
		subject = pendingID(raw)
	}
	if admin && c.Param("eid") != "" {
		resource, e := s.Store.Ent.Exit.Get(c.Request.Context(), pendingID(c.Param("eid")))
		if e == nil && resource.OwnerUserID != nil {
			subject = *resource.OwnerUserID
		}
	}
	// 业务处理器负责资源归属与字段校验；此处不能提前读取其他用户的配置。
	if subject != actor.ID && actor.Role != "admin" {
		s.audit(c, "proxy.denied", "user:"+itoa(subject), "forbidden")
		failure(c, 403, "forbidden")
		return
	}
	p := pendingExit{ID: randomToken(18), Revision: 1, Kind: kind, Actor: actor.ID, Subject: subject, Admin: admin, Params: append(gin.Params{}, c.Params...), Method: c.Request.Method, Query: c.Request.URL.RawQuery, Body: body, Trace: c.GetString("trace_id"), At: time.Now().UTC()}
	var references struct {
		GateID      int `json:"gate_id"`
		EntryNodeID int `json:"entry_node_id"`
	}
	_ = json.Unmarshal(body, &references)
	p.GateID, p.NodeID = references.GateID, references.EntryNodeID
	if kind == "node.update" {
		p.NodeID = pendingID(c.Param("id"))
	}
	if id := pendingID(c.Param("cid")); id > 0 {
		if ch, e := s.Store.Ent.Chain.Get(c.Request.Context(), id); e == nil {
			p.NodeID = ch.EntryNodeID
		}
	}
	if p.NodeID > 0 {
		if n, e := s.Store.Ent.Node.Get(c.Request.Context(), p.NodeID); e == nil {
			p.GateID = n.GateID
		}
	}
	if kind == "exit.save" {
		p.ExitID = pendingID(c.Param("eid"))
		if p.ExitID > 0 {
			if e, err := s.Store.Ent.Exit.Get(c.Request.Context(), p.ExitID); err == nil {
				p.GateID = e.GateID
			}
		}
	}
	err = s.Store.Lock.Do(c.Request.Context(), "exit-changes", func(ctx context.Context) error {
		return s.withExitPolicyLock(ctx, p, func(ctx context.Context) error {
			binding, e := s.exitChangeBinding(ctx, p)
			if e != nil {
				return e
			}
			p.Binding = binding
			c.Set("pending_exit_request", p)
			c.Set("exit_commit_guard", func() bool { now, e := s.exitChangeBinding(ctx, p); return e == nil && now == p.Binding })
			h(c)
			if c.Writer.Status() < 300 && !c.GetBool(dryRunKey) {
				if e := s.supersedePendingExits(ctx, p); e != nil {
					return e
				}
			}
			return nil
		})
	})
	if err != nil {
		if ent.IsNotFound(err) {
			failure(c, 404, "resource_not_found")
		} else {
			failInternal(c, 500, "candidate_failed", err)
		}
	}
}

func (s *Server) deferExitChange(c *gin.Context, cause error) bool {
	var verification exitVerificationError
	if !errors.As(cause, &verification) || c.GetBool("pending_exit_replay") {
		return false
	}
	v, ok := c.Get("pending_exit_request")
	if !ok {
		return false
	}
	p := v.(pendingExit)
	p.Status = "failed"
	p.Reason = redact(cause.Error())
	if missingMihomo(cause) {
		p.Status = "waiting"
		p.Reason = "mihomo 正在下载，完成后自动验证"
	}
	if dryRun(c, dryResult{Preview: []string{"保存待验证出口；验证通过前保留原配置，新资源不会进入订阅"}}) {
		return true
	}
	if !jobCommit(c) {
		return true
	}
	if err := s.savePendingExit(c.Request.Context(), p); err != nil {
		failInternal(c, 500, "candidate_failed", err)
		return true
	}
	s.audit(c, "exit.candidate", "candidate:"+p.ID, p.Status)
	c.JSON(202, gin.H{"pending": p.view()})
	return true
}

func (s *Server) pendingExitRoutes(auth *gin.RouterGroup) {
	auth.GET("/proxy/pending-exits", s.listPendingExits)
	auth.POST("/proxy/pending-exits/:candidate/retry", s.retryPendingExit)
	auth.DELETE("/proxy/pending-exits/:candidate", s.discardPendingExit)
}

func (s *Server) listPendingExits(c *gin.Context) {
	all := c.Query("all") == "true"
	if all && c.MustGet("account").(*ent.User).Role != "admin" {
		s.audit(c, "proxy.denied", "candidates", "forbidden")
		failure(c, 403, "forbidden")
		return
	}
	u, ok := s.proxySubject(c, false)
	if !ok {
		return
	}
	keys, err := s.Store.KV.Keys(c.Request.Context(), "pending-exit:", 10000)
	if err != nil {
		failInternal(c, 503, "query_failed", err)
		return
	}
	items := []gin.H{}
	for _, key := range keys {
		p, e := s.readPendingExit(c.Request.Context(), strings.TrimPrefix(key, "pending-exit:"))
		if e != nil {
			failInternal(c, 503, "query_failed", e)
			return
		}
		if all || p.Subject == u.ID {
			items = append(items, p.view())
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i]["at"].(time.Time).Before(items[j]["at"].(time.Time)) })
	c.JSON(200, gin.H{"items": items})
}

func (s *Server) ownedPendingExit(c *gin.Context) (pendingExit, bool) {
	p, err := s.readPendingExit(c.Request.Context(), c.Param("candidate"))
	if err != nil {
		if err.Error() == "candidate_not_found" {
			failure(c, 404, "candidate_not_found")
		} else {
			failInternal(c, 500, "candidate_failed", err)
		}
		return p, false
	}
	u := c.MustGet("account").(*ent.User)
	if u.ID != p.Subject && u.Role != "admin" {
		s.audit(c, "proxy.denied", "candidate:"+p.ID, "forbidden")
		failure(c, 403, "forbidden")
		return p, false
	}
	return p, true
}
func (s *Server) retryPendingExit(c *gin.Context) {
	p, ok := s.ownedPendingExit(c)
	if !ok {
		return
	}
	var req struct {
		Revision *int `json:"revision"`
	}
	if !bind(c, &req) {
		return
	}
	if req.Revision == nil || *req.Revision != p.Revision {
		failure(c, 409, "candidate_revision_conflict")
		return
	}
	err := s.Store.Lock.Do(c.Request.Context(), "exit-changes", func(ctx context.Context) error {
		current, e := s.readPendingExit(ctx, p.ID)
		if e != nil {
			return e
		}
		if current.Revision != *req.Revision {
			return errors.New("candidate_revision_conflict")
		}
		if current.Status == "applied" || current.Status == "discarded" || current.Status == "stale" {
			return errors.New("candidate_closed")
		}
		if dryRun(c, dryResult{Preview: []string{"重新验证出口，通过后应用候选配置"}}) {
			return nil
		}
		current.Status = "waiting"
		current.Revision++
		return s.savePendingExit(ctx, current)
	})
	if err != nil {
		if err.Error() == "candidate_revision_conflict" || err.Error() == "candidate_closed" {
			failure(c, 409, err.Error())
		} else {
			failInternal(c, 500, "candidate_failed", err)
		}
		return
	}
	if c.GetBool(dryRunKey) {
		return
	}
	s.audit(c, "exit.retry", "candidate:"+p.ID, "ok")
	go s.resumePendingExits(context.WithoutCancel(c.Request.Context()))
	c.JSON(202, gin.H{"id": p.ID, "status": "waiting"})
}
func (s *Server) discardPendingExit(c *gin.Context) {
	p, ok := s.ownedPendingExit(c)
	if !ok {
		return
	}
	var req struct {
		Revision *int `json:"revision"`
	}
	if !bind(c, &req) {
		return
	}
	if req.Revision == nil || *req.Revision != p.Revision {
		failure(c, 409, "candidate_revision_conflict")
		return
	}
	err := s.Store.Lock.Do(c.Request.Context(), "exit-changes", func(ctx context.Context) error {
		current, e := s.readPendingExit(ctx, p.ID)
		if e != nil {
			return e
		}
		if current.Revision != *req.Revision || current.Status == "applied" || current.Status == "discarded" {
			return errors.New("candidate_revision_conflict")
		}
		if dryRun(c, dryResult{Preview: []string{"放弃候选，保留原有出口"}}) {
			return nil
		}
		current.Status = "discarded"
		current.Revision++
		current.Body = nil
		return s.savePendingExit(ctx, current)
	})
	if err != nil {
		if err.Error() == "candidate_revision_conflict" {
			failure(c, 409, "candidate_revision_conflict")
		} else {
			failInternal(c, 500, "candidate_failed", err)
		}
		return
	}
	if c.GetBool(dryRunKey) {
		return
	}
	s.audit(c, "exit.discard", "candidate:"+p.ID, "ok")
	c.JSON(200, gin.H{"ok": true})
}

func (s *Server) resumePendingExits(ctx context.Context) {
	s.state.RLock()
	defer s.state.RUnlock()
	if s.Store == nil {
		return
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		select {
		case <-s.events.stopped():
			cancel()
		case <-ctx.Done():
		}
	}()
	keys, err := s.Store.KV.Keys(ctx, "pending-exit:", 10000)
	if err != nil {
		return
	}
	for _, key := range keys {
		if ctx.Err() != nil {
			return
		}
		err = s.Store.Lock.Do(ctx, "exit-changes", func(ctx context.Context) error {
			p, e := s.readPendingExit(ctx, strings.TrimPrefix(key, "pending-exit:"))
			if e != nil || p.Status != "waiting" {
				return e
			}
			return s.withExitPolicyLock(ctx, p, func(ctx context.Context) error {
				p.Revision++
				binding, e := s.exitChangeBinding(ctx, p)
				if e != nil || binding != p.Binding {
					p.Status = "stale"
					p.Reason = "资源或授权已变化，请放弃后重新保存"
					p.Body = nil
					return s.savePendingExit(ctx, p)
				}
				u, e := s.Store.Ent.User.Get(ctx, p.Actor)
				if e != nil {
					return e
				}
				w := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(w)
				trace := newHTTPTrace("00-" + p.Trace + "-" + randomHex(8) + "-01")
				c.Request = httptest.NewRequest(p.Method, "/internal/exit-candidate?"+p.Query, bytes.NewReader(p.Body)).WithContext(context.WithValue(ctx, httpTraceKey{}, trace))
				c.Request.Header.Set("Content-Type", "application/json")
				c.Params = p.Params
				c.Set("account", u)
				c.Set("user_id", u.ID)
				c.Set("trace_id", trace.ID)
				c.Set("request_id", requestID())
				c.Set("span_id", trace.Span)
				c.Set("pending_exit_replay", true)
				c.Set("exit_commit_guard", func() bool { now, e := s.exitChangeBinding(ctx, p); return e == nil && now == p.Binding })
				if !u.Enabled || p.Admin && u.Role != "admin" {
					failure(c, 403, "forbidden")
				} else {
					switch p.Kind {
					case "node.create":
						s.createNode(c)
					case "node.update":
						s.updateNode(c)
					case "chain.create":
						s.createChain(c, p.Admin)
					case "chain.update":
						s.updateChain(c, p.Admin)
					case "exit.save":
						s.saveExit(c, p.Admin)
					case "type.exit":
						s.putTypeExit(c, p.Admin)
					default:
						failure(c, 400, "invalid_candidate")
					}
				}
				if w.Code >= 200 && w.Code < 300 {
					p.Status = "applied"
					p.Reason = "验证通过，候选已应用"
					p.Body = nil
				} else {
					var result map[string]any
					_ = json.Unmarshal(w.Body.Bytes(), &result)
					p.Status = "failed"
					p.Reason = redact(handlerError(w.Code, result).Error())
					if strings.Contains(w.Body.String(), "mihomo") && strings.Contains(w.Body.String(), "下载") {
						p.Status = "waiting"
					}
				}
				s.audit(c, "exit.candidate", "candidate:"+p.ID, p.Status)
				return s.savePendingExit(ctx, p)
			})
		})
		if err != nil && ctx.Err() == nil {
			slog.Error("恢复出口候选失败", "candidate", strings.TrimPrefix(key, "pending-exit:"), "err", redact(err.Error()))
		}
	}
}

// 类型出口与分组、清单共用同一并发版本，按相同用户锁串行保存。
func (s *Server) withExitPolicyLock(ctx context.Context, p pendingExit, f func(context.Context) error) error {
	if strings.HasPrefix(p.Kind, "type.") {
		return s.Store.Lock.Do(ctx, "proxy-policy:"+itoa(p.Subject), f)
	}
	return f(ctx)
}

func pendingExitScope(p pendingExit) string {
	v := []any{p.Subject, p.Kind, p.Params}
	if strings.HasSuffix(p.Kind, ".create") || p.Kind == "exit.save" && p.Params.ByName("eid") == "" {
		var body map[string]any
		_ = json.Unmarshal(p.Body, &body)
		for _, key := range []string{"gate_id", "entry_node_id", "name", "preset", "port"} {
			v = append(v, body[key])
		}
	}
	b, _ := json.Marshal(v)
	return digest(string(b))
}

func (s *Server) supersedePendingExits(ctx context.Context, current pendingExit) error {
	keys, err := s.Store.KV.Keys(ctx, "pending-exit:", 10000)
	if err != nil {
		return err
	}
	for _, key := range keys {
		p, err := s.readPendingExit(ctx, strings.TrimPrefix(key, "pending-exit:"))
		if err != nil {
			return err
		}
		if p.ID == current.ID || p.Status != "waiting" && p.Status != "failed" || pendingExitScope(p) != pendingExitScope(current) {
			continue
		}
		p.Status, p.Reason, p.Body = "stale", "已有新的修改，旧候选已失效", nil
		p.Revision++
		if err := s.savePendingExit(ctx, p); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) createNode(c *gin.Context) { s.exitChange(c, "node.create", true, s.createNodeNow) }
func (s *Server) updateNode(c *gin.Context) { s.exitChange(c, "node.update", true, s.updateNodeNow) }
func (s *Server) createChain(c *gin.Context, admin bool) {
	s.exitChange(c, "chain.create", admin, func(c *gin.Context) { s.createChainNow(c, admin) })
}
func (s *Server) updateChain(c *gin.Context, admin bool) {
	s.exitChange(c, "chain.update", admin, func(c *gin.Context) { s.updateChainNow(c, admin) })
}
func (s *Server) saveExit(c *gin.Context, admin bool) {
	s.exitChange(c, "exit.save", admin, func(c *gin.Context) { s.saveExitNow(c, admin) })
}
func (s *Server) putTypeExit(c *gin.Context, admin bool) {
	s.exitChange(c, "type.exit", admin, func(c *gin.Context) { s.putTypeExitNow(c, admin) })
}

func pendingID(raw string) int { id, _ := strconv.Atoi(raw); return id }

func (s *Server) deleteTypeExit(c *gin.Context, admin bool) {
	s.exitChange(c, "type.delete", admin, func(c *gin.Context) { s.deleteTypeExitNow(c, admin) })
}
