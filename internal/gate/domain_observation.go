package gate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/shusfun/weir/internal/buildinfo"
	"github.com/shusfun/weir/internal/kernelconf"
	"github.com/shusfun/weir/internal/releasefiles"
	"github.com/shusfun/weir/packages/contracts"
)

const maxObservedDomains = 4000
const maxObservedIdentities = 50000

type nativeConnection struct {
	ID       string `json:"id"`
	Start    string `json:"start"`
	Upload   int64  `json:"upload"`
	Download int64  `json:"download"`
	Metadata struct {
		Host          string `json:"host"`
		DestinationIP string `json:"destinationIP"`
		InboundName   string `json:"inboundName"`
		InboundUser   string `json:"inboundUser"`
		Type          string `json:"type"`
	} `json:"metadata"`
}

type connectionBaseline struct {
	Counter Counter
	Binding string
}

// Native snapshots and identity sets are deliberately memory-only. A process
// restart starts with a baseline, sacrificing coverage instead of replaying bytes.
type domainCollector struct {
	Prefixes  map[int]map[netip.Addr]bool
	Fetched   time.Time
	Retry     time.Time
	Snapshots map[string]map[string]connectionBaseline
	Secrets   map[string]string
	Users     map[string]map[string]bool
	UserCount int
}

func (c nativeConnection) inboundName(engine string) string {
	if c.Metadata.InboundName != "" {
		return c.Metadata.InboundName
	}
	// sing-box's Clash API encodes the inbound tag after its inbound type.
	// Only accept the known native form; arbitrary suffix matching can attribute
	// a connection to the wrong node.
	if engine == "sing-box" {
		kind, tag, ok := strings.Cut(c.Metadata.Type, "/")
		if ok && tag != "" && !strings.Contains(tag, "/") {
			switch kind {
			case "socks", "http", "mixed", "shadowsocks", "vmess", "vless", "trojan", "hysteria2", "tuic", "anytls", "shadowtls":
				return tag
			}
		}
	}
	return ""
}

func (d *domainCollector) addIdentity(key, user string) bool {
	if d.Users == nil {
		d.Users = map[string]map[string]bool{}
	}
	if d.Users[key] == nil {
		d.Users[key] = map[string]bool{}
	}
	if d.Users[key][user] {
		return true
	}
	if d.UserCount >= maxObservedIdentities {
		return false
	}
	d.Users[key][user] = true
	d.UserCount++
	return true
}

func (d *domainCollector) contains(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsGlobalUnicast() || addr.IsPrivate() {
		return false
	}
	for bits, set := range d.Prefixes {
		if bits > addr.BitLen() {
			continue
		}
		if set[netip.PrefixFrom(addr, bits).Masked().Addr()] {
			return true
		}
	}
	return false
}

func (d *domainCollector) setNetworks(data contracts.CountryNetworks, now time.Time) error {
	if len(data.Prefixes) == 0 || len(data.Prefixes) > 200000 || data.Fetched.IsZero() || now.Sub(data.Fetched) > 45*24*time.Hour || data.Fetched.After(now.Add(5*time.Minute)) {
		return errors.New("country_data_unavailable")
	}
	prefixes := map[int]map[netip.Addr]bool{}
	for _, p := range data.Prefixes {
		if !p.IsValid() || p.Bits() < 8 {
			return errors.New("invalid_country_data")
		}
		p = p.Masked()
		if prefixes[p.Bits()] == nil {
			prefixes[p.Bits()] = map[netip.Addr]bool{}
		}
		prefixes[p.Bits()][p.Addr()] = true
	}
	d.Prefixes, d.Fetched = prefixes, data.Fetched
	return nil
}

// The client never uses proxies or follows redirects, including redirects to a
// different loopback API. Native error bodies are not logged or persisted.
var nativeObservationHTTP = &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: time.Second}).DialContext}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

func readNativeConnections(ctx context.Context, plan contracts.ConnectionObserver) ([]nativeConnection, error) {
	if plan.Port < 1 || plan.Port > 65535 || len(plan.Secret) < 32 {
		return nil, errors.New("observer_configuration_invalid")
	}
	req, err := http.NewRequestWithContext(ctx, "GET", "http://127.0.0.1:"+strconv.Itoa(plan.Port)+"/connections", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+plan.Secret)
	resp, err := nativeObservationHTTP.Do(req)
	if err != nil {
		return nil, errors.New("native_api_unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, errors.New("native_api_rejected")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (8<<20)+1))
	if err != nil || len(data) > 8<<20 {
		return nil, errors.New("snapshot_limit")
	}
	var doc struct {
		Connections json.RawMessage `json:"connections"`
	}
	if json.Unmarshal(data, &doc) != nil || len(doc.Connections) == 0 {
		return nil, errors.New("native_snapshot_invalid")
	}
	var connections []nativeConnection
	if json.Unmarshal(doc.Connections, &connections) != nil {
		return nil, errors.New("native_snapshot_invalid")
	}
	if len(connections) > 10000 {
		return nil, errors.New("snapshot_limit")
	}
	return connections, nil
}

func domainObservationKey(engine, day, domain string, node int64) string {
	return engine + ":" + day + ":" + domain + ":" + strconv.FormatInt(node, 10)
}

func (d *domainCollector) absorb(st *State, cfg contracts.GateConfig, engine string, plan contracts.ConnectionObserver, connections []nativeConnection, now time.Time) {
	if d.Snapshots == nil {
		d.Snapshots = map[string]map[string]connectionBaseline{}
		d.Secrets = map[string]string{}
		d.Users = map[string]map[string]bool{}
	}
	old, warm := d.Snapshots[engine]
	if d.Secrets[engine] != plan.Secret {
		warm = false
	}
	next := map[string]connectionBaseline{}
	if st.DomainCoverage == nil {
		st.DomainCoverage = map[string]contracts.DomainCoverage{}
	}
	if st.Domains == nil {
		st.Domains = map[string]contracts.DomainObservation{}
	}
	day := now.UTC().Format("2006-01-02")
	for key, sample := range st.Domains {
		if sample.Day < now.AddDate(0, 0, -29).Format("2006-01-02") {
			delete(st.Domains, key)
		}
	}
	for key := range d.Users {
		if _, ok := st.Domains[key]; !ok {
			d.UserCount -= len(d.Users[key])
			delete(d.Users, key)
		}
	}
	limited := false
	metadata := false
	nativeNames := kernelconf.NativeInboundNames(cfg, engine)
	for _, c := range connections {
		if c.ID == "" || len(c.ID) > 256 || c.Upload < 0 || c.Download < 0 || c.Upload > math.MaxInt64-c.Download {
			continue
		}
		// Prefer the consumer. An Xray helper is the only observation point for
		// that path; its original person and node remain unknown.
		inboundName := c.inboundName(engine)
		node := nativeNames[inboundName]
		helper := false
		if node == 0 {
			for i, b := range cfg.Bridges {
				if b.Kernel == engine && b.FromKernel == "xray" && inboundName == fmt.Sprintf("weir-bridge-%d", i+1) {
					helper = true
					break
				}
			}
			if !helper {
				continue
			}
		}
		internal := false
		for _, cred := range cfg.Credentials {
			if cred.InboundID == node && cred.StatsKey == c.Metadata.InboundUser && cred.Internal {
				internal = true
				break
			}
		}
		if internal {
			continue
		}
		if c.Metadata.Host != "" && c.Metadata.DestinationIP != "" {
			metadata = true
		}
		domain := contracts.ObservedDomain(c.Metadata.Host)
		ip, err := netip.ParseAddr(c.Metadata.DestinationIP)
		if domain == "" || err != nil || !d.contains(ip) {
			continue
		}
		binding := domain + "|" + c.Metadata.DestinationIP + "|" + c.Start + "|" + strconv.FormatInt(node, 10)
		cur := connectionBaseline{Counter: Counter{Up: c.Upload, Down: c.Download}, Binding: binding}
		next[c.ID] = cur
		if !warm {
			continue
		}
		up, down := c.Upload, c.Download
		if prior, ok := old[c.ID]; ok && prior.Binding == binding {
			if up < prior.Counter.Up || down < prior.Counter.Down {
				continue
			}
			up -= prior.Counter.Up
			down -= prior.Counter.Down
		}
		key := domainObservationKey(engine, day, domain, node)
		sample, exists := st.Domains[key]
		if !exists {
			if len(st.Domains) >= maxObservedDomains {
				limited = true
				continue
			}
			sample = contracts.DomainObservation{Engine: engine, Day: day, Domain: domain, NodeID: node}
		}
		if sample.Bytes > math.MaxInt64-up-down {
			limited = true
			continue
		}
		sample.Bytes += up + down
		sample.ObservedAt = now
		for _, cred := range cfg.Credentials {
			if c.Metadata.InboundUser == "" || cred.Internal || cred.UserKey == "" || cred.InboundID != node {
				continue
			}
			if c.Metadata.InboundUser != cred.StatsKey {
				continue
			}
			if !d.addIdentity(key, cred.UserKey) {
				limited = true
			}
			count := len(d.Users[key])
			if sample.Users == nil || count > *sample.Users {
				sample.Users = &count
			}
		}
		st.Domains[key] = sample
	}
	d.Snapshots[engine], d.Secrets[engine] = next, plan.Secret
	reason := "connection_snapshots"
	if !warm {
		reason = "restart_baseline"
	}
	if len(connections) > 0 && !metadata {
		reason = "metadata_unavailable"
	}
	if limited {
		reason = "domain_limit"
	}
	st.DomainCoverage[engine] = contracts.DomainCoverage{Engine: engine, Status: "partial", Reason: reason, ObservedAt: now}
}

func (a *Agent) collectDomains(ctx context.Context, cfg contracts.GateConfig) {
	for engine := range a.st.DomainCoverage {
		if _, ok := cfg.Observers[engine]; !ok {
			delete(a.st.DomainCoverage, engine)
		}
	}
	for engine := range a.domains.Snapshots {
		if _, ok := cfg.Observers[engine]; !ok {
			delete(a.domains.Snapshots, engine)
			delete(a.domains.Secrets, engine)
		}
	}
	if len(cfg.Observers) == 0 {
		return
	}
	now := time.Now().UTC()
	if a.st.DomainCoverage == nil {
		a.st.DomainCoverage = map[string]contracts.DomainCoverage{}
	}
	if now.After(a.domains.Retry) {
		requestCtx := releasefiles.WithTrace(ctx, "")
		started := time.Now()
		a.domains.Retry = now.Add(10 * time.Minute)
		var data contracts.CountryNetworks
		status, err := a.center.call(requestCtx, "GET", "/agent/country-networks", nil, &data)
		stage, outcome := "receive", "failed"
		if err == nil {
			stage = "apply"
			err = a.domains.setNetworks(data, now)
			if err == nil {
				outcome = "succeeded"
				a.domains.Retry = now.Add(6 * time.Hour)
			}
		}
		reason := ""
		if err != nil {
			reason = "request_failed"
			switch {
			case stage == "apply":
				reason = "invalid_snapshot"
			case errors.Is(err, context.DeadlineExceeded):
				reason = "timeout"
			case errors.Is(err, context.Canceled):
				reason = "cancelled"
			case status >= 400:
				reason = "http_error"
			}
		}
		log.Printf("operation=traffic.domains.country-data.gate trace=%s stage=%s outcome=%s reason=%s http_status=%d snapshot_version=%s duration_ms=%d", releasefiles.Trace(requestCtx), stage, outcome, reason, status, data.Fetched.UTC().Format(time.RFC3339Nano), time.Since(started).Milliseconds())
	}
	for engine, plan := range cfg.Observers {
		started := time.Now()
		err := error(nil)
		if len(a.domains.Prefixes) == 0 || now.Sub(a.domains.Fetched) > 45*24*time.Hour {
			err = errors.New("country_data_unavailable")
		} else {
			var connections []nativeConnection
			connections, err = readNativeConnections(ctx, plan)
			if err == nil {
				a.domains.absorb(&a.st, cfg, engine, plan, connections, now)
			}
		}
		if err != nil {
			a.st.DomainCoverage[engine] = contracts.DomainCoverage{Engine: engine, Status: "unavailable", Reason: err.Error(), ObservedAt: now}
		}
		outcome := "succeeded"
		reason := ""
		if err != nil {
			outcome = "failed"
			reason = err.Error()
		}
		log.Printf("operation=traffic.domains.observe trace=%s engine=%s outcome=%s reason=%s duration_ms=%d code.file.path=internal/gate/domain_observation.go vcs.ref.head.revision=%s service.version=%s", releasefiles.Trace(ctx), engine, outcome, reason, time.Since(started).Milliseconds(), buildinfo.Revision, buildinfo.Version)
	}
}

func (st *State) domainReport(rep *contracts.MeterReport) {
	for _, sample := range st.Domains {
		copy := sample
		if sample.Users != nil {
			users := *sample.Users
			copy.Users = &users
		}
		rep.Domains = append(rep.Domains, copy)
	}
	for _, coverage := range st.DomainCoverage {
		rep.DomainCoverage = append(rep.DomainCoverage, coverage)
	}
	sort.Slice(rep.Domains, func(i, j int) bool {
		a, b := rep.Domains[i], rep.Domains[j]
		return domainObservationKey(a.Engine, a.Day, a.Domain, a.NodeID) < domainObservationKey(b.Engine, b.Day, b.Domain, b.NodeID)
	})
	sort.Slice(rep.DomainCoverage, func(i, j int) bool { return rep.DomainCoverage[i].Engine < rep.DomainCoverage[j].Engine })
}
