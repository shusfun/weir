package center

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/shusfun/weir/internal/kernelconf"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/credential"
	entexit "github.com/shusfun/weir/internal/store/ent/exit"
	"github.com/shusfun/weir/internal/store/ent/resource"
	"github.com/shusfun/weir/packages/contracts"
)

const uotResourceKind = "gate-uot"
const uotMethod = "2022-blake3-aes-128-gcm"
const uotInboundBase int64 = 1 << 42

type uotEndpoint struct {
	ReceiverRevision int64     `json:"receiver_revision,omitempty"`
	Ready            bool      `json:"ready,omitempty"`
	Probe            bool      `json:"probe,omitempty"`
	Expires          time.Time `json:"expires,omitempty"`
	CredentialID     int       `json:"credential_id,omitempty"`
	ExitID           int       `json:"exit_id"`
	NodeID           int       `json:"node_id"`
	EntryID          int       `json:"entry_id"`
	GateID           int       `json:"gate_id"`
	Port             int       `json:"port"`
	ServerKey        string    `json:"server_key"`
	UserKey          string    `json:"user_key"`
}

func readUoT(ctx context.Context, db *ent.Client, eid int) (*uotEndpoint, error) {
	row, err := db.Resource.Query().Where(resource.KindEQ(uotResourceKind), resource.KeyEQ(itoa(eid))).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return decodeUoT(row)
}

func decodeUoT(row *ent.Resource) (*uotEndpoint, error) {
	var p uotEndpoint
	if err := decodeResource(row, &p); err != nil {
		return nil, err
	}
	if p.ExitID <= 0 || row.Key != itoa(p.ExitID) || row.OwnerID != p.GateID || p.NodeID <= 0 || p.EntryID <= 0 || p.GateID <= 0 || p.EntryID == p.GateID || p.Port < 1024 || p.Port > 65535 || p.ServerKey == "" || p.UserKey == "" || p.Probe && (p.CredentialID <= 0 || p.Expires.IsZero()) {
		return nil, errors.New("invalid internal UDP transport")
	}
	return &p, nil
}

func (s *Server) uotCapability(ctx context.Context, gid int) bool {
	caps, err := s.Store.Tasks.Capabilities(ctx, gid)
	return err == nil && caps.ConfigFeatures[contracts.ConfigUoT] >= 1 && caps.ConfigFeatures[contracts.ConfigOutboundBridges] >= 1
}

func (s *Server) uotKeys(p *uotEndpoint) (string, string, error) {
	server, err := s.reveal(p.ServerKey)
	if err != nil {
		return "", "", err
	}
	user, err := s.reveal(p.UserKey)
	if err != nil {
		return "", "", err
	}
	for _, v := range []string{server, user} {
		raw, err := base64.StdEncoding.DecodeString(v)
		if err != nil || len(raw) != 16 {
			return "", "", errors.New("invalid internal transport key")
		}
	}
	return server, user, nil
}

// Each exit has a separate internal credential and listening port. Node IDs and
// public credentials are untouched; transport rotation never changes subscriptions.
func (s *Server) syncUoT(ctx context.Context, exits []*ent.Exit) ([]int, error) {
	active := map[int]*ent.Exit{}
	for _, e := range exits {
		active[e.ID] = e
	}
	rows, err := s.Store.Ent.Resource.Query().Where(resource.KindEQ(uotResourceKind)).All(ctx)
	if err != nil {
		return nil, err
	}
	touched := []int{}
	for _, row := range rows {
		p, err := decodeUoT(row)
		if err != nil {
			return nil, err
		}
		if p.Probe && p.Expires.After(time.Now()) {
			continue
		}
		e := active[p.ExitID]
		if e != nil && e.ExitNodeID != nil && *e.ExitNodeID == p.NodeID && (exitGateID(e) == p.EntryID || s.exitBound(ctx, e.ID, p.EntryID)) {
			continue
		}
		if err = s.Store.Ent.Resource.DeleteOne(row).Exec(ctx); err != nil {
			return nil, err
		}
		if _, err = s.Store.Ent.Gate.Get(ctx, p.GateID); err == nil {
			touched = append(touched, p.GateID)
		} else if !ent.IsNotFound(err) {
			return nil, err
		}
	}
	for _, e := range exits {
		if e.ExitNodeID == nil {
			continue
		}
		n, err := s.Store.Ent.Node.Get(ctx, *e.ExitNodeID)
		if err != nil {
			return nil, err
		}
		entryID := exitGateID(e)
		if entryID == 0 {
			gates, err := s.referencingGates(ctx, e.ID)
			if err != nil {
				return nil, err
			}
			for _, gid := range gates {
				if s.exitBound(ctx, e.ID, gid) {
					entryID = gid
					break
				}
			}
		}
		if entryID == 0 || !n.Enabled || !s.uotCapability(ctx, entryID) || !s.uotCapability(ctx, n.GateID) {
			continue
		}
		p, err := readUoT(ctx, s.Store.Ent, e.ID)
		if err != nil {
			return nil, err
		}
		if p != nil {
			if p.ReceiverRevision == 0 {
				g, err := s.Store.Ent.Gate.Get(ctx, p.GateID)
				if err != nil {
					return nil, err
				}
				p.ReceiverRevision = g.ConfigRevision + 1
				if _, err = s.Store.Ent.Resource.Update().Where(resource.KindEQ(uotResourceKind), resource.KeyEQ(itoa(p.ExitID))).SetData(toMap(p)).Save(ctx); err != nil {
					return nil, err
				}
				touched = append(touched, p.EntryID, p.GateID)
			}
			continue
		}
		tx, err := s.Store.Ent.Tx(ctx)
		if err != nil {
			return nil, err
		}
		err = func() error {
			defer tx.Rollback()
			if err := lockActiveGates(ctx, tx.Client(), entryID, n.GateID); err != nil {
				return err
			}
			if existing, err := readUoT(ctx, tx.Client(), e.ID); err != nil {
				return err
			} else if existing != nil {
				return tx.Commit()
			}
			g, err := tx.Gate.Get(ctx, n.GateID)
			if err != nil {
				return err
			}
			own, err := s.gateOwnPortsDB(ctx, tx.Client(), g)
			if err != nil {
				return err
			}
			port, err := pickPort(own, foreignPorts(g, own))
			if err != nil {
				return err
			}
			key := func() (string, error) {
				raw := make([]byte, 16)
				if _, err := rand.Read(raw); err != nil {
					return "", err
				}
				return s.seal(base64.StdEncoding.EncodeToString(raw))
			}
			server, err := key()
			if err != nil {
				return err
			}
			user, err := key()
			if err != nil {
				return err
			}
			p := uotEndpoint{ExitID: e.ID, NodeID: n.ID, EntryID: entryID, GateID: n.GateID, Port: port, ServerKey: server, UserKey: user, ReceiverRevision: g.ConfigRevision + 1}
			if err = tx.Resource.Create().SetKind(uotResourceKind).SetKey(itoa(e.ID)).SetOwnerID(n.GateID).SetData(toMap(p)).Exec(ctx); err != nil {
				return err
			}
			return tx.Commit()
		}()
		if err != nil {
			return nil, err
		}
		touched = append(touched, entryID, n.GateID)
	}
	slices.Sort(touched)
	return slices.Compact(touched), nil
}

func (s *Server) uotOutbound(ctx context.Context, e *ent.Exit) (contracts.Outbound, error) {
	p, err := readUoT(ctx, s.Store.Ent, e.ID)
	if err != nil {
		return contracts.Outbound{}, err
	}
	if p == nil || e.ExitNodeID == nil || p.NodeID != *e.ExitNodeID || !s.exitServes(ctx, e, p.EntryID) {
		return contracts.Outbound{}, errors.New("自有 VPS 的加密 UDP 中转尚未准备")
	}
	if !p.Ready {
		return contracts.Outbound{}, errors.New("等待出口 VPS 确认加密 UDP 中转配置生效")
	}
	if !s.uotCapability(ctx, p.EntryID) || !s.uotCapability(ctx, p.GateID) {
		return contracts.Outbound{}, errors.New("链路两端必须支持原生 UDP over TCP")
	}
	n, err := s.Store.Ent.Node.Get(ctx, p.NodeID)
	if err != nil || !n.Enabled {
		return contracts.Outbound{}, errors.New("出口节点不可用")
	}
	g, err := s.Store.Ent.Gate.Get(ctx, p.GateID)
	if err != nil {
		return contracts.Outbound{}, err
	}
	if g.PublicHost == "" || string(g.Status) != "active" {
		return contracts.Outbound{}, errors.New("出口 VPS 不可用")
	}
	server, user, err := s.uotKeys(p)
	if err != nil {
		return contracts.Outbound{}, err
	}
	return contracts.Outbound{ID: int64(e.ID), Tag: e.Tag, Protocol: "shadowsocks", Address: g.PublicHost, Port: p.Port, Username: uotMethod, Password: server + ":" + user, Settings: map[string]any{"udp_over_tcp": true}}, nil
}

// Receiver acknowledgement unlocks only its own dependent entries. Until then
// compileRoutes removes user credentials or installs a blackhole outbound.
func (s *Server) acknowledgeUoT(ctx context.Context, gid int, applied int64) (resultErr error) {
	finish := operationStage(ctx, "proxy.uot.receiver-applied", "gate_id", gid, "revision", applied)
	defer func() { finish(resultErr) }()
	tx, err := s.Store.Ent.Tx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = lockActiveGates(ctx, tx.Client(), gid); err != nil {
		return err
	}
	rows, err := tx.Resource.Query().Where(resource.KindEQ(uotResourceKind), resource.OwnerIDEQ(gid)).All(ctx)
	if err != nil {
		return err
	}
	entries := map[int]bool{}
	for _, row := range rows {
		p, err := decodeUoT(row)
		if err != nil {
			return err
		}
		if p.Probe || p.Ready || p.ReceiverRevision <= 0 || applied < p.ReceiverRevision {
			continue
		}
		p.Ready = true
		if err = tx.Resource.UpdateOneID(row.ID).SetData(toMap(p)).Exec(ctx); err != nil {
			return err
		}
		entries[p.EntryID] = true
	}
	for id := range entries {
		if err = advanceOperationRevision(ctx, tx, id); err != nil {
			return err
		}
	}
	if len(entries) > 0 {
		if err = tx.Audit.Create().SetAction("proxy.uot.receiver-applied").SetTarget(itoa(gid)).SetResult("ok").SetDetail(map[string]any{"revision": applied, "trace_id": taskTrace(ctx)}).Exec(ctx); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	for id := range entries {
		s.publishOperationRevision(ctx, id)
	}
	return nil
}

// Receiver rules inherit the target node's actual routing, including selective
// rules. The existing internal credential remains the single accounting identity.
func (s *Server) compileUoTReceivers(ctx context.Context, g *ent.Gate, cfg *contracts.GateConfig, need map[string]bool) error {
	rows, err := s.Store.Ent.Resource.Query().Where(resource.KindEQ(uotResourceKind), resource.OwnerIDEQ(g.ID)).Order(ent.Asc(resource.FieldID)).All(ctx)
	if err != nil {
		return err
	}
	original := append([]contracts.Route(nil), cfg.Routes...)
	for _, row := range rows {
		p, err := decodeUoT(row)
		if err != nil {
			return err
		}
		if p.Probe && !p.Expires.After(time.Now()) {
			continue
		}
		if !p.Probe {
			e, err := s.Store.Ent.Exit.Get(ctx, p.ExitID)
			if ent.IsNotFound(err) {
				continue
			}
			if err != nil {
				return err
			}
			if !e.Enabled || e.Kind != entexit.KindNode || e.ExitNodeID == nil || *e.ExitNodeID != p.NodeID || !s.exitServes(ctx, e, p.EntryID) {
				continue
			}
		}
		if !s.uotCapability(ctx, g.ID) || !s.uotCapability(ctx, p.EntryID) {
			continue
		}
		var source *contracts.Inbound
		for i := range cfg.Inbounds {
			if cfg.Inbounds[i].ID == int64(p.NodeID) {
				source = &cfg.Inbounds[i]
				break
			}
		}
		if source == nil {
			continue
		}
		_, drivers, err := s.gateKernels(ctx, g, map[string]bool{"sing-box": true})
		if err != nil {
			return err
		}
		driver, err := kernelconf.ParseDriver(drivers["sing-box"])
		if err != nil {
			return err
		}
		if !driver.Has("udp-over-tcp") {
			return errors.New("sing-box 驱动缺少原生 UDP over TCP")
		}
		query := s.Store.Ent.Credential.Query().Where(credential.NodeIDEQ(p.NodeID), credential.InternalEQ(true))
		if p.Probe {
			query = query.Where(credential.IDEQ(p.CredentialID))
		} else {
			query = query.Where(credential.StatsKeyEQ(exitCredentialKey(p.ExitID)))
		}
		cred, err := query.Only(ctx)
		if err != nil {
			return err
		}
		server, user, err := s.uotKeys(p)
		if err != nil {
			return err
		}
		id := uotInboundBase + int64(p.ExitID)
		tag := fmt.Sprintf("weir-uot-%d", p.ExitID)
		cfg.Inbounds = append(cfg.Inbounds, contracts.Inbound{ID: id, Tag: tag, Kernel: "sing-box", Protocol: "shadowsocks", Port: p.Port, Network: "tcp", Settings: map[string]any{"ss_method": uotMethod, "ss_password": server, "tcp_only": true, "reject_udp": source.Settings["reject_udp"] == true}})
		kept := cfg.Credentials[:0]
		for _, c := range cfg.Credentials {
			if p.Probe || c.ID != int64(cred.ID) {
				kept = append(kept, c)
			}
		}
		cfg.Credentials = kept
		statsKey, credentialID := cred.StatsKey, int64(cred.ID)
		var expires *time.Time
		if p.Probe {
			statsKey = fmt.Sprintf("uot-probe-%d", p.ExitID)
			credentialID = id
			expires = &p.Expires
		}
		cfg.Credentials = append(cfg.Credentials, contracts.Credential{ID: credentialID, InboundID: id, StatsKey: statsKey, UserKey: "internal", Password: user, Internal: true, ExpiresAt: expires})
		for _, r := range original {
			if r.InboundTag == source.Tag {
				r.ID = int64(len(cfg.Routes) + 1)
				r.InboundTag = tag
				cfg.Routes = append(cfg.Routes, r)
			}
		}
		need["sing-box"] = true
	}
	return nil
}

// A preview gets its own expiring transport and never changes a saved exit.
func (s *Server) probeUoT(ctx context.Context, entry *ent.Gate, nid int, publicOnly bool) (result *exitProbe, resultErr error) {
	finish := operationStage(ctx, "proxy.uot.probe", "gate_id", entry.ID, "node_id", nid)
	defer func() { finish(resultErr) }()
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	n, err := s.Store.Ent.Node.Get(ctx, nid)
	if err != nil {
		return nil, err
	}
	cred, err := s.probeCredential(ctx, n)
	if err != nil {
		return nil, err
	}
	tx, err := s.Store.Ent.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = lockActiveGates(ctx, tx.Client(), entry.ID, n.GateID); err != nil {
		return nil, err
	}
	target, err := tx.Gate.Get(ctx, n.GateID)
	if err != nil {
		return nil, err
	}
	own, err := s.gateOwnPortsDB(ctx, tx.Client(), target)
	if err != nil {
		return nil, err
	}
	port, err := pickPort(own, foreignPorts(target, own))
	if err != nil {
		return nil, err
	}
	raw := make([]byte, 40)
	if _, err = rand.Read(raw); err != nil {
		return nil, err
	}
	// Probe IDs occupy a disjoint range from database exit IDs.
	id := int(1<<40) + int(binary.BigEndian.Uint64(raw[:8])&((1<<39)-1))
	server, user := base64.StdEncoding.EncodeToString(raw[8:24]), base64.StdEncoding.EncodeToString(raw[24:40])
	sk, err := s.seal(server)
	if err != nil {
		return nil, err
	}
	uk, err := s.seal(user)
	if err != nil {
		return nil, err
	}
	p := uotEndpoint{ExitID: id, NodeID: n.ID, EntryID: entry.ID, GateID: n.GateID, Port: port, ServerKey: sk, UserKey: uk, Probe: true, Expires: time.Now().Add(3 * time.Minute), CredentialID: cred.ID}
	row, err := tx.Resource.Create().SetKind(uotResourceKind).SetKey(itoa(id)).SetOwnerID(n.GateID).SetData(toMap(p)).Save(ctx)
	if err != nil {
		return nil, err
	}
	if err = advanceOperationRevision(ctx, tx, n.GateID); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	s.publishOperationRevision(ctx, n.GateID)
	defer func() {
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer stop()
		finish := operationStage(cleanup, "proxy.uot.cleanup", "gate_id", n.GateID)
		err := func() error {
			tx, err := s.Store.Ent.Tx(cleanup)
			if err != nil {
				return err
			}
			defer tx.Rollback()
			if err = tx.Resource.DeleteOneID(row.ID).Exec(cleanup); ent.IsNotFound(err) {
				return nil
			} else if err != nil {
				return err
			}
			if err = advanceOperationRevision(cleanup, tx, n.GateID); err != nil {
				return err
			}
			return tx.Commit()
		}()
		finish(err)
		if err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("临时中转清理失败：%w", err))
			return
		}
		s.publishOperationRevision(cleanup, n.GateID)
	}()
	if _, err = s.waitProbeApplied(ctx, n); err != nil {
		return nil, err
	}
	kernels, drivers, err := s.gateKernels(ctx, entry, map[string]bool{"sing-box": true})
	if err != nil {
		return nil, err
	}
	ob := contracts.Outbound{ID: 1, Tag: "probe-exit", Protocol: "shadowsocks", Address: target.PublicHost, Port: port, Username: uotMethod, Password: server + ":" + user, Settings: map[string]any{"udp_over_tcp": true}}
	payload := map[string]any{"address": ob.Address, "port": ob.Port, "public_only": publicOnly, "target": s.probeTarget(ctx), "native": toMap(ob), "kernels": kernels, "drivers": drivers}
	if download := s.probeDownload(ctx); download != "" {
		payload["download"] = download
	}
	finishConnect := operationStage(ctx, "exit.probe.connect", "gate.id", entry.ID, "echo", probeHost(s.probeTarget(ctx)))
	response, err := s.gateProbe(ctx, entry, contracts.ActionChainProbe, payload, 3*time.Minute)
	finishConnect(err)
	if err != nil {
		return nil, err
	}
	probe := &exitProbe{OK: true, Mode: "proxy", At: time.Now().UTC(), Note: "已通过加密 TCP 内部链路实测；UDP 往返需独立测试"}
	applyProbeFields(probe, response)
	s.finishProbe(ctx, probe)
	return probe, nil
}
