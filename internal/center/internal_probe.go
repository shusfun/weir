package center

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/credential"
)

// 每个节点一条长期内部实测凭据：加密保存，不过期，不计入用户、订阅和额度。
// 领取和读取不推进配置版本。协议变更和管理员轮换才更换秘密。

func (s *Server) probeCredential(ctx context.Context, n *ent.Node) (*ent.Credential, error) {
	row, err := s.standingProbe(ctx, n.ID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, errors.New("节点还没有内部实测凭据")
	}
	return s.revealProbe(row)
}

func (s *Server) ensureProbeCredential(ctx context.Context, n *ent.Node) (*ent.Credential, error) {
	var row *ent.Credential
	err := s.Store.Lock.Do(ctx, "probe:"+itoa(n.ID), func(ctx context.Context) error {
		existing, err := s.standingProbe(ctx, n.ID)
		if err != nil {
			return err
		}
		if existing != nil {
			row = existing
			return nil
		}
		row, err = s.insertProbe(ctx, n)
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.revealProbe(row)
}

func (s *Server) rotateProbeCredential(ctx context.Context, n *ent.Node) (*ent.Credential, error) {
	var row *ent.Credential
	err := s.Store.Lock.Do(ctx, "probe:"+itoa(n.ID), func(ctx context.Context) error {
		existing, err := s.standingProbe(ctx, n.ID)
		if err != nil {
			return err
		}
		if existing == nil {
			row, err = s.insertProbe(ctx, n)
			return err
		}
		row, err = s.rewriteProbe(ctx, existing, n.Protocol)
		return err
	})
	if err != nil {
		return nil, err
	}
	if _, err = s.bumpGate(ctx, n.GateID); err != nil {
		return nil, err
	}
	return s.revealProbe(row)
}

func (s *Server) rewriteProbeSecret(ctx context.Context, n *ent.Node) error {
	return s.Store.Lock.Do(ctx, "probe:"+itoa(n.ID), func(ctx context.Context) error {
		existing, err := s.standingProbe(ctx, n.ID)
		if err != nil {
			return err
		}
		if existing == nil {
			_, err = s.insertProbe(ctx, n)
			return err
		}
		_, err = s.rewriteProbe(ctx, existing, n.Protocol)
		return err
	})
}

func (s *Server) standingProbe(ctx context.Context, nodeID int) (*ent.Credential, error) {
	rows, err := s.Store.Ent.Credential.Query().Where(
		credential.NodeIDEQ(nodeID),
		credential.PurposeEQ("probe"),
		credential.ProbeParentIDIsNil(),
		credential.ExpiresAtIsNil(),
	).All(ctx)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	keep := rows[0]
	for _, row := range rows[1:] {
		if row.ID < keep.ID {
			keep = row
		}
	}
	for _, row := range rows {
		if row.ID == keep.ID {
			continue
		}
		if err = s.Store.Ent.Credential.DeleteOneID(row.ID).Exec(ctx); err != nil && !ent.IsNotFound(err) {
			return nil, err
		}
	}
	return keep, nil
}

func (s *Server) insertProbe(ctx context.Context, n *ent.Node) (*ent.Credential, error) {
	id, password := newSecret(n.Protocol)
	sealedID, err := s.Crypto.Encrypt(id)
	if err != nil {
		return nil, err
	}
	sealedPassword, err := s.Crypto.Encrypt(password)
	if err != nil {
		return nil, err
	}
	return s.Store.Ent.Credential.Create().
		SetNodeID(n.ID).SetInternal(true).SetPurpose("probe").
		SetStatsKey("probe-" + randomHex(16)).
		SetUUID(sealedID).SetPassword(sealedPassword).
		Save(ctx)
}

func (s *Server) rewriteProbe(ctx context.Context, row *ent.Credential, protocol string) (*ent.Credential, error) {
	id, password := newSecret(protocol)
	sealedID, err := s.Crypto.Encrypt(id)
	if err != nil {
		return nil, err
	}
	sealedPassword, err := s.Crypto.Encrypt(password)
	if err != nil {
		return nil, err
	}
	return row.Update().SetUUID(sealedID).SetPassword(sealedPassword).Save(ctx)
}

func (s *Server) revealProbe(row *ent.Credential) (*ent.Credential, error) {
	if row == nil {
		return nil, errors.New("节点还没有内部实测凭据")
	}
	plain := *row
	var err error
	if plain.UUID, err = s.reveal(row.UUID); err != nil {
		return nil, err
	}
	if plain.Password, err = s.reveal(row.Password); err != nil {
		return nil, err
	}
	return &plain, nil
}

// migrateProbeCredentials 给已有节点补长期凭据，并清掉过期的临时凭据。
// 新凭据要进入内核配置，所以只在这里推进一次版本。
func (s *Server) migrateProbeCredentials(ctx context.Context) error {
	if err := s.cleanupProbeCredentials(ctx, time.Now(), false); err != nil {
		return err
	}
	nodes, err := s.Store.Ent.Node.Query().All(ctx)
	if err != nil {
		return err
	}
	var gates []int
	for _, n := range nodes {
		existed, err := s.standingProbe(ctx, n.ID)
		if err != nil {
			return err
		}
		if existed != nil {
			continue
		}
		if _, err = s.ensureProbeCredential(ctx, n); err != nil {
			return err
		}
		gates = append(gates, n.GateID)
	}
	return s.bumpGates(WithRevisionCause(ctx, "probe.migrate", 0), dedupe(gates))
}

// cleanupProbeCredentials 只删除过期的临时实测凭据和 WireGuard 检测子凭据。
// 长期凭据没有到期时间，不在这里删除，也不推进配置版本。
func (s *Server) cleanupProbeCredentials(ctx context.Context, now time.Time, _ bool) error {
	rows, err := s.Store.Ent.Credential.Query().Where(credential.Or(
		credential.ProbeParentIDNotNil(),
		credential.And(credential.PurposeEQ("probe"), credential.ExpiresAtNotNil(), credential.ExpiresAtLTE(now)),
	)).All(ctx)
	if err != nil {
		return err
	}
	for _, c := range rows {
		if c.Purpose == "probe" && c.ExpiresAt == nil && c.ProbeParentID == nil {
			continue
		}
		if err = s.Store.Ent.Credential.DeleteOne(c).Exec(ctx); err != nil && !ent.IsNotFound(err) {
			return err
		}
	}
	return nil
}

func (s *Server) waitProbeApplied(ctx context.Context, n *ent.Node) (*ent.Gate, error) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		g, err := s.Store.Ent.Gate.Get(ctx, n.GateID)
		if err != nil {
			return nil, err
		}
		if g.Status != "active" {
			return nil, errors.New("VPS 已撤销")
		}
		if g.FailedRevision >= g.ConfigRevision && g.FailedRevision > 0 {
			return nil, errors.New("VPS 配置应用失败：" + g.LastError)
		}
		if g.AppliedRevision >= g.ConfigRevision {
			return g, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (s *Server) probeFingerprint(ctx context.Context, n *ent.Node, g *ent.Gate) (string, error) {
	return s.probePathFingerprint(ctx, n, g, map[int]bool{})
}

func (s *Server) probePathFingerprint(ctx context.Context, n *ent.Node, g *ent.Gate, seen map[int]bool) (string, error) {
	if seen[n.ID] || len(seen) > 8 {
		return "", errors.New("节点出口形成循环")
	}
	seen[n.ID] = true
	var cert, exit any
	if n.CertID != nil {
		c, err := s.Store.Ent.Certificate.Get(ctx, *n.CertID)
		if err != nil {
			return "", err
		}
		cert = []any{c.CertPem, c.Sha256}
	}
	if n.ExitID != 0 {
		e, err := s.Store.Ent.Exit.Get(ctx, n.ExitID)
		if err != nil {
			return "", err
		}
		path := []any{e, e.Password, e.Link}
		if e.ExitNodeID != nil {
			downstream, err := s.Store.Ent.Node.Get(ctx, *e.ExitNodeID)
			if err != nil {
				return "", err
			}
			downGate, err := s.Store.Ent.Gate.Get(ctx, downstream.GateID)
			if err != nil {
				return "", err
			}
			fingerprint, err := s.probePathFingerprint(ctx, downstream, downGate, seen)
			if err != nil {
				return "", err
			}
			path = append(path, fingerprint)
		}
		if e.Kind == "source" {
			links, err := s.specLinks(ctx, specOf(e.Source))
			if err != nil {
				return "", err
			}
			path = append(path, links)
		}
		exit = path
	}
	b, err := json.Marshal([]any{n.ID, n.Enabled, n.Kernel, n.Protocol, n.Network, n.Security, n.Port, n.Settings, n.CertID, cert, exit, g.ID, g.Status, g.PublicHost, g.CenterURL})
	if err != nil {
		return "", err
	}
	return digest(string(b)), nil
}
