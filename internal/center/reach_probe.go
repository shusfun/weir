package center

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/shusfun/weir/internal/nodeprobe"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/packages/contracts"
)

var batchNodeProbe = nodeprobe.Batch

func (s *Server) checkReachabilities(ctx context.Context, ids []int) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	select {
	case s.probeSlots() <- struct{}{}:
		defer func() { <-s.probeSlots() }()
	case <-ctx.Done():
		return
	}
	if err := s.Store.Lock.Do(ctx, "internal-node-probes", func(ctx context.Context) error {
		s.checkReachabilitiesNow(ctx, ids)
		return ctx.Err()
	}); err != nil && ctx.Err() == nil {
		slog.Error("外部检查排队失败", "err", redact(err.Error()))
	}
}

func (s *Server) checkReachabilitiesNow(ctx context.Context, ids []int) {
	trace, _ := ctx.Value(httpTraceKey{}).(httpTrace)
	if trace.ID == "" {
		trace = newHTTPTrace("")
		ctx = context.WithValue(ctx, httpTraceKey{}, trace)
	}
	type candidate struct {
		n      *ent.Node
		g      *ent.Gate
		result reachResult
		proxy  map[string]any
	}
	var candidates []candidate
	var proxies []map[string]any
	bin, driver, binaryErr := deployedProbeBinary(s, ctx)
	for _, id := range ids {
		n, err := s.Store.Ent.Node.Get(ctx, id)
		if err != nil || !n.Enabled {
			continue
		}
		g, err := s.Store.Ent.Gate.Get(ctx, n.GateID)
		if err != nil {
			continue
		}
		binding, err := s.probeFingerprint(ctx, n, g)
		if err != nil {
			continue
		}
		r := reachResult{Status: "unknown", At: time.Now().UTC(), Revision: g.ConfigRevision, Host: g.PublicHost, Port: n.Port, Binding: binding, TraceID: trace.ID}
		c := candidate{n: n, g: g, result: r}
		switch {
		case colocated(g):
			c.result.Why = "中心与这台 VPS 在同一台机器上，无法验证外部连接"
		case !externalProbeSupported():
			c.result.Why = "中心不是 Linux amd64／arm64，无法执行外部实测"
		case binaryErr != nil:
			c.result.Why = "mihomo 未就绪：" + safeCheckOutput(binaryErr.Error())
			s.autoKernelFetch(ctx, "mihomo", s.newestAnyArch(ctx, "mihomo"))
		case g.PublicHost == "":
			c.result.Why = "VPS 没有公网地址"
		case g.Status != "active" || g.LastSeenAt == nil || time.Since(*g.LastSeenAt) > 90*time.Second:
			c.result.Why = "VPS 离线或已撤销"
		default:
			credential, err := s.probeCredential(ctx, n)
			if err != nil {
				c.result.Why = "无法准备内部测试授权"
				break
			}
			var cert *ent.Certificate
			if n.CertID != nil {
				cert, err = s.Store.Ent.Certificate.Get(ctx, *n.CertID)
				if err != nil {
					c.result.Why = "无法读取节点证书"
					break
				}
			}
			link, err := shareLink(n, g.PublicHost, credential, "probe-"+itoa(n.ID), cert)
			if err != nil {
				c.result.Why = safeCheckOutput(err.Error())
				break
			}
			client, err := nodeClient(link)
			if err != nil {
				c.result.Why = safeCheckOutput(err.Error())
				break
			}
			c.proxy = client.Proxies[0]
		}
		candidates = append(candidates, c)
	}
	gateErrors := map[int]error{}
	for i := range candidates {
		c := &candidates[i]
		if c.proxy == nil {
			continue
		}
		if _, checked := gateErrors[c.g.ID]; !checked {
			_, gateErrors[c.g.ID] = s.waitProbeApplied(ctx, c.n)
		}
		if err := gateErrors[c.g.ID]; err != nil {
			c.result.Why = safeCheckOutput(err.Error())
			c.proxy = nil
			continue
		}
		proxies = append(proxies, c.proxy)
	}
	results, err := batchNodeProbe(ctx, nodeprobe.BatchOptions{Binary: bin, Run: driver.Run, Check: driver.Check, Proxies: proxies, Target: contracts.GroupProbeURL})
	for _, c := range candidates {
		r := c.result
		if c.proxy != nil {
			if err != nil {
				r.Why = safeCheckOutput(err.Error())
			} else {
				check, ok := results[fmt.Sprint(c.proxy["name"])]
				if !ok {
					r.Why = "mihomo 没有返回此节点的结果"
				} else {
					r.Why, r.LatencyMS = safeCheckOutput(check.Why), int(check.LatencyMS)
					switch check.Status {
					case "passed":
						r.Status = "ok"
					case "failed":
						r.Status = "failed"
					}
				}
			}
		}
		current, nerr := s.Store.Ent.Node.Get(ctx, c.n.ID)
		g, gerr := s.Store.Ent.Gate.Get(ctx, c.g.ID)
		if nerr != nil || gerr != nil {
			continue
		}
		binding, ferr := s.probeFingerprint(ctx, current, g)
		if ferr != nil || binding != r.Binding {
			continue
		}
		b, _ := json.Marshal(r)
		if saveErr := s.Store.KV.Set(ctx, reachKey(c.n.ID), string(b), 24*time.Hour); saveErr != nil {
			slog.Error("node reach save", "trace", trace.ID, "node", c.n.ID, "err", redact(saveErr.Error()))
			continue
		}
		slog.Info("node reach", "trace", trace.ID, "node", c.n.ID, "status", r.Status, "detail", r.Why)
		if _, auditErr := s.Store.Ent.Audit.Create().SetActor(0).SetAction("node.reach").SetTarget("node:" + itoa(c.n.ID)).SetResult(r.Status).SetDetail(map[string]any{"trace_id": trace.ID, "automatic": true}).Save(ctx); auditErr != nil {
			slog.Error("外部实测审计保存失败", "trace", trace.ID, "node", c.n.ID, "err", redact(auditErr.Error()))
		}
	}
}
