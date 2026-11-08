package center

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/credential"
)

const probeCredentialTTL = 10 * time.Minute

// 内部测试授权不依赖用户、订阅或额度。数据库只保存加密秘密，Gate 用到期时间在本地撤销。
func (s *Server) issueProbeCredential(ctx context.Context, n *ent.Node) (*ent.Credential, func(), error) {
	id, password := newSecret(n.Protocol)
	sealedID, err := s.Crypto.Encrypt(id)
	if err != nil {
		return nil, nil, err
	}
	sealedPassword, err := s.Crypto.Encrypt(password)
	if err != nil {
		return nil, nil, err
	}
	expires := time.Now().Add(probeCredentialTTL)
	c, err := s.Store.Ent.Credential.Create().SetNodeID(n.ID).SetInternal(true).SetPurpose("probe").SetExpiresAt(expires).
		SetStatsKey("probe-" + randomHex(16)).SetUUID(sealedID).SetPassword(sealedPassword).Save(ctx)
	if err != nil {
		return nil, nil, err
	}
	revoke := func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if err := s.Store.Ent.Credential.DeleteOneID(c.ID).Exec(cleanup); err != nil && !ent.IsNotFound(err) {
			slog.Error("回收内部测试凭据失败", "node", n.ID, "err", redact(err.Error()))
		}
		if err := s.bumpGates(cleanup, []int{n.GateID}); err != nil {
			slog.Error("通知撤销内部测试凭据失败", "node", n.ID, "err", redact(err.Error()))
		}
	}
	if err = s.bumpGates(ctx, []int{n.GateID}); err != nil {
		revoke()
		return nil, nil, err
	}
	plain := *c
	plain.UUID, plain.Password = id, password
	return &plain, revoke, nil
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

func (s *Server) cleanupProbeCredentials(ctx context.Context, now time.Time, all bool) error {
	q := s.Store.Ent.Credential.Query().Where(credential.Or(credential.PurposeEQ("probe"), credential.ProbeParentIDNotNil()))
	if !all {
		q = q.Where(credential.Or(credential.ProbeParentIDNotNil(), credential.ExpiresAtIsNil(), credential.ExpiresAtLTE(now)))
	}
	rows, err := q.All(ctx)
	if err != nil {
		return err
	}
	var gates []int
	for _, c := range rows {
		if n, e := s.Store.Ent.Node.Get(ctx, c.NodeID); e == nil {
			gates = append(gates, n.GateID)
		}
		if e := s.Store.Ent.Credential.DeleteOne(c).Exec(ctx); e != nil && !ent.IsNotFound(e) {
			return e
		}
	}
	if len(gates) > 0 {
		return s.bumpGates(ctx, dedupe(gates))
	}
	return nil
}

// 只绑定业务配置；测试凭据发放与回收引起的配置版本变化不使结果失效。
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
