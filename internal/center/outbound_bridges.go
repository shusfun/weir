package center

import (
	"context"
	"errors"
	"fmt"

	"github.com/shusfun/weir/internal/kernelconf"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/resource"
	"github.com/shusfun/weir/packages/contracts"
)

const outboundBridgeKind = "gate-outbound-bridges"

func (s *Server) bridgeSupport(ctx context.Context, g *ent.Gate) bool {
	caps, err := s.Store.Tasks.Capabilities(ctx, g.ID)
	return err == nil && caps.ConfigFeatures[contracts.ConfigOutboundBridges] >= 1
}

// outboundExecutor selects a pinned driver, not an advertised protocol name.
// Binary preflight and the real Gate probe remain mandatory before activation.
func (s *Server) outboundExecutor(ctx context.Context, g *ent.Gate, from string, o contracts.Outbound) (string, error) {
	_, profiles, err := s.gateKernels(ctx, g, map[string]bool{from: true})
	if err != nil {
		return "", err
	}
	d, err := kernelconf.ParseDriver(profiles[from])
	if err != nil {
		return "", err
	}
	if kernelconf.NativeOutbound(d, o) {
		return from, nil
	}
	if !s.bridgeSupport(ctx, g) {
		return "", errors.New("此 VPS 尚不支持跨内核出站，请先更新 Gate")
	}
	if from != "sing-box" {
		_, profiles, err = s.gateKernels(ctx, g, map[string]bool{"sing-box": true})
		if err != nil {
			return "", err
		}
		d, err = kernelconf.ParseDriver(profiles["sing-box"])
		if err != nil {
			return "", err
		}
		if d.Has("hosted-outbounds") && kernelconf.NativeOutbound(d, o) {
			return "sing-box", nil
		}
	}
	_, profiles, err = s.gateKernels(ctx, g, map[string]bool{"mihomo": true})
	if err != nil {
		return "", err
	}
	d, err = kernelconf.ParseDriver(profiles["mihomo"])
	if err != nil {
		return "", err
	}
	if kernelconf.ClientOutbound(d, o) {
		return "mihomo", nil
	}
	return "", fmt.Errorf("%s 的原生内核与辅助内核均不能表达 %s 的参数", from, o.Protocol)
}

func (s *Server) loadOutboundBridges(ctx context.Context, db *ent.Client, gid int) (*ent.Resource, []contracts.OutboundBridge, error) {
	row, err := db.Resource.Query().Where(resource.KindEQ(outboundBridgeKind), resource.KeyEQ(itoa(gid))).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var data struct {
		Items []contracts.OutboundBridge `json:"items"`
	}
	if err = decodeResource(row, &data); err != nil {
		return nil, nil, err
	}
	for i := range data.Items {
		if data.Items[i].Password, err = s.reveal(data.Items[i].Password); err != nil {
			return nil, nil, err
		}
	}
	return row, data.Items, nil
}

func (s *Server) saveOutboundBridges(ctx context.Context, db *ent.Client, gid int, row *ent.Resource, items []contracts.OutboundBridge) error {
	sealed := append([]contracts.OutboundBridge(nil), items...)
	for i := range sealed {
		var err error
		if sealed[i].Password, err = s.seal(sealed[i].Password); err != nil {
			return err
		}
	}
	data := toMap(struct {
		Items []contracts.OutboundBridge `json:"items"`
	}{sealed})
	if row != nil {
		return db.Resource.UpdateOneID(row.ID).SetData(data).Exec(ctx)
	}
	return db.Resource.Create().SetKind(outboundBridgeKind).SetKey(itoa(gid)).SetOwnerID(gid).SetData(data).Exec(ctx)
}

// allocateOutboundBridges keeps loopback ports and authentication stable across
// recompiles. The transaction fences identity deletion and concurrent allocation.
func (s *Server) allocateOutboundBridges(ctx context.Context, g *ent.Gate, requested []contracts.OutboundBridge) ([]contracts.OutboundBridge, error) {
	if len(requested) == 0 {
		return nil, nil
	}
	tx, err := s.Store.Ent.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = lockActiveGates(ctx, tx.Client(), g.ID); err != nil {
		return nil, err
	}
	row, saved, err := s.loadOutboundBridges(ctx, tx.Client(), g.ID)
	if err != nil {
		return nil, err
	}
	// Read the public ports in this transaction: a new node must not claim a
	// port concurrently with allocation of an internal listener.
	current, err := tx.Gate.Get(ctx, g.ID)
	if err != nil {
		return nil, err
	}
	own, err := s.gateOwnPortsDB(ctx, tx.Client(), current)
	if err != nil {
		return nil, err
	}
	foreign := foreignPorts(current, own)
	changed := false
	var selected []contracts.OutboundBridge
	for _, p := range requested {
		found := false
		for _, b := range saved {
			if b.FromKernel == p.FromKernel && b.OutboundTag == p.OutboundTag && b.Kernel == p.Kernel {
				selected, found = append(selected, b), true
				break
			}
		}
		if found {
			continue
		}
		if p.Port, err = pickPort(own, foreign); err != nil {
			return nil, err
		}
		p.Username, p.Password = randomToken(12), randomToken(32)
		own[p.Port] = true
		saved, selected, changed = append(saved, p), append(selected, p), true
	}
	if changed {
		if err = s.saveOutboundBridges(ctx, tx.Client(), g.ID, row, saved); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return selected, nil
}

func (s *Server) compileOutboundBridges(ctx context.Context, g *ent.Gate, cfg *contracts.GateConfig, need map[string]bool) (err error) {
	finish := operationStage(ctx, "proxy.execution.plan", "gate_id", g.ID)
	defer func() { finish(err) }()
	outs, err := kernelconf.ExpandedOutbounds(*cfg)
	if err != nil {
		return err
	}
	var requested []contracts.OutboundBridge
	for _, from := range []string{"xray", "sing-box"} {
		if !need[from] {
			continue
		}
		for _, o := range outs {
			if o.Protocol == "blackhole" {
				continue
			}
			executor, err := s.outboundExecutor(ctx, g, from, o)
			if err != nil {
				return err
			}
			if executor != from {
				requested = append(requested, contracts.OutboundBridge{FromKernel: from, OutboundTag: o.Tag, Kernel: executor})
			}
		}
	}
	if cfg.Bridges, err = s.allocateOutboundBridges(ctx, g, requested); err != nil {
		return err
	}
	for _, p := range cfg.Bridges {
		need[p.Kernel] = true
	}
	return nil
}
