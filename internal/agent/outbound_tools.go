package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"time"

	"github.com/imzyb/MiGate/internal/db"
	"github.com/imzyb/MiGate/packages/contracts"
)

func strictOutboundPayload(payload map[string]any, out any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	return d.Decode(out)
}

func (a *Agent) probeCatalog(ctx context.Context, t contracts.Task) (map[string]any, error) {
	var req contracts.CatalogProbe
	if strictOutboundPayload(t.Payload, &req) != nil || len(req.Key) != 64 || !publicProbeIP(net.ParseIP(req.Address)) || req.Port < 1 || req.Port > 65535 {
		return nil, errors.New("invalid_catalog_probe")
	}
	a.mu.Unlock()
	defer a.mu.Lock()
	status, ms := probeTCP(ctx, req.Address, req.Port, net.DefaultResolver.LookupIPAddr, (&net.Dialer{}).DialContext)
	item := map[string]any{"id": 1, "status": status}
	if status == "reachable" {
		item["latency_ms"] = ms
	}
	return map[string]any{"source": "gate_tcp_connect", "observed_at": time.Now().UTC().Format(time.RFC3339Nano), "items": []map[string]any{item}}, nil
}

func (a *Agent) reorderOutbounds(t contracts.Task) error {
	var order contracts.OutboundOrder
	if strictOutboundPayload(t.Payload, &order) != nil {
		return errors.New("invalid_outbound_order")
	}
	if a.State.Versions["outbound:0"] == t.Revision && t.Revision > 0 {
		return nil
	}
	current := []contracts.RoutePosition{}
	for _, out := range a.State.Outbounds {
		if out.Source == db.OutboundSourceSubscription {
			continue
		}
		if a.State.Versions[fmt.Sprintf("outbound:%d", out.ID)] > t.Revision {
			return errors.New("stale_resource_revision")
		}
		current = append(current, contracts.RoutePosition{ID: out.ID, Sort: out.Sort})
	}
	if err := order.Validate(current); err != nil {
		return err
	}
	positions := map[int64]int{}
	for i, out := range order.Items {
		positions[out.ID] = (i + 1) * 10
	}
	if a.State.Versions == nil {
		a.State.Versions = map[string]int64{}
	}
	for i := range a.State.Outbounds {
		out := &a.State.Outbounds[i]
		if out.Source == db.OutboundSourceSubscription {
			continue
		}
		out.Sort = positions[out.ID]
		a.State.Versions[fmt.Sprintf("outbound:%d", out.ID)] = t.Revision
	}
	return nil
}

// Probe is read only: current inventory IDs, no browser supplied dial target,
// no proxy handshake/HTTP request, no privileged helper or core restart.
func (a *Agent) probeOutbounds(ctx context.Context, t contracts.Task) (map[string]any, error) {
	// Called with a.mu held. Never hold it across network waits: quota/expiry
	// enforcement must continue while up to 50 endpoints are being sampled.
	outs := append([]db.Outbound(nil), a.State.Outbounds...)
	a.mu.Unlock()
	defer a.mu.Lock()
	return probeOutboundSnapshot(ctx, t, outs)
}

func probeOutboundSnapshot(ctx context.Context, t contracts.Task, outs []db.Outbound) (map[string]any, error) {
	var probe contracts.OutboundProbe
	if strictOutboundPayload(t.Payload, &probe) != nil || probe.Validate() != nil {
		return nil, errors.New("invalid_outbound_probe")
	}
	byID := map[int64]db.Outbound{}
	for _, out := range outs {
		byID[out.ID] = out
	}
	results := make([]map[string]any, len(probe.Items))
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for i, item := range probe.Items {
		out, ok := byID[item.ID]
		results[i] = map[string]any{"id": item.ID, "status": "inventory_changed"}
		if !ok || db.PoolMemberDigest(out) != item.Digest {
			continue
		}
		wg.Add(1)
		go func(i int, out db.Outbound) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				results[i]["status"] = "unknown"
				return
			}
			status, ms := probeTCP(ctx, out.Address, out.Port, net.DefaultResolver.LookupIPAddr, (&net.Dialer{}).DialContext)
			results[i]["status"] = status
			if status == "reachable" {
				results[i]["latency_ms"] = ms
			}
		}(i, out)
	}
	wg.Wait()
	return map[string]any{"source": "gate_tcp_connect", "observed_at": time.Now().UTC().Format(time.RFC3339Nano), "items": results}, nil
}

func publicProbeIP(ip net.IP) bool {
	a, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	a = a.Unmap()
	if !a.IsGlobalUnicast() || a.IsPrivate() || a.IsLoopback() || a.IsLinkLocalUnicast() {
		return false
	}
	for _, cidr := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001:db8::/32"} {
		if netip.MustParsePrefix(cidr).Contains(a) {
			return false
		}
	}
	return true
}

func probeTCP(ctx context.Context, host string, port int, lookup func(context.Context, string) ([]net.IPAddr, error), dial func(context.Context, string, string) (net.Conn, error)) (string, int64) {
	if port < 1 || port > 65535 {
		return "blocked", 0
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var ips []net.IPAddr
	if ip := net.ParseIP(host); ip != nil {
		ips = []net.IPAddr{{IP: ip}}
	} else {
		var err error
		ips, err = lookup(ctx, host)
		if err != nil || len(ips) == 0 {
			return "unknown", 0
		}
	}
	// Reject mixed private/public DNS results. Pin the validated numeric address.
	for _, ip := range ips {
		if ip.Zone != "" || !publicProbeIP(ip.IP) {
			return "blocked", 0
		}
	}
	start := time.Now()
	conn, err := dial(ctx, "tcp", net.JoinHostPort(ips[0].IP.String(), strconv.Itoa(port)))
	if err != nil {
		return "unreachable", 0
	}
	conn.Close()
	return "reachable", time.Since(start).Milliseconds()
}
