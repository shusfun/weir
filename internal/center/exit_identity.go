package center

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/shusfun/weir/internal/subscription"
)

func validSourceIdentity(id string) bool {
	b, err := hex.DecodeString(id)
	return err == nil && len(b) == 32 && id == strings.ToLower(id)
}

func confirmedIdentity(raw string, confirmations []string) bool {
	id := subscription.InventoryIdentity(raw)
	for _, confirmed := range confirmations {
		if confirmed == id {
			return true
		}
	}
	return false
}

func validConfirmations(raw string, confirmations []string) []string {
	if confirmedIdentity(raw, confirmations) {
		return []string{subscription.InventoryIdentity(raw)}
	}
	return nil
}

type sourceNodeChoice struct {
	Identity             string `json:"identity"`
	Name                 string `json:"name"`
	RequiresConfirmation bool   `json:"requires_confirmation"`
}

func sourceNodeChoices(links []string) []sourceNodeChoice {
	out := []sourceNodeChoice{}
	seen := map[string]bool{}
	parsed, _ := subscription.ParseLinks(links)
	for _, n := range parsed.Nodes {
		id := subscription.InventoryIdentity(n.RawLink)
		if seen[id] {
			continue
		}
		if _, err := confirmedLinkOutbound(n, []string{id}); err != nil {
			continue
		}
		var settings subscription.Settings
		_ = json.Unmarshal([]byte(n.SettingsJSON), &settings)
		out = append(out, sourceNodeChoice{Identity: id, Name: n.Remark, RequiresConfirmation: settings.AllowInsecure})
		seen[id] = true
	}
	return out
}

func (s *Server) previewExitLink(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var err error
	end := operationStage(c.Request.Context(), "proxy.link.inspect")
	defer func() { end(err) }()
	var body struct {
		Link string `json:"link"`
	}
	if err = c.ShouldBindJSON(&body); err != nil {
		failure(c, 400, "invalid_link")
		return
	}
	id := subscription.InventoryIdentity(body.Link)
	o, _, err := parseConfirmedExitLink(body.Link, []string{id})
	if err != nil {
		failDetail(c, 422, "invalid_link", err.Error(), nil)
		return
	}
	n, _ := subscription.ParseLink(strings.TrimSpace(body.Link))
	var settings subscription.Settings
	_ = json.Unmarshal([]byte(n.SettingsJSON), &settings)
	c.JSON(200, gin.H{"identity": id, "protocol": o.Protocol, "requires_confirmation": settings.AllowInsecure})
}

// Display names are accepted only to bind a legacy selection once. Subsequent
// refreshes must not silently substitute a different server or credential.
func (s *Server) bindSourceIdentity(ctx context.Context, sp sourceSpec) (sourceSpec, error) {
	if err := sp.check(); err != nil {
		return sp, err
	}
	if sp.Mode != "node" || sp.Identity != "" {
		return sp, nil
	}
	links, err := s.specLinks(ctx, sp)
	if err != nil {
		return sp, err
	}
	parsed, _ := subscription.ParseLinks(links)
	for _, n := range parsed.Nodes {
		if n.Remark != sp.Match {
			continue
		}
		id := subscription.InventoryIdentity(n.RawLink)
		if sp.Identity != "" && sp.Identity != id {
			return sp, errors.New("同名节点不唯一，请重新选择具体节点")
		}
		sp.Identity = id
	}
	if sp.Identity == "" {
		return sp, errors.New("选中的节点已不在来源中，请重新选择")
	}
	return sp, nil
}

func (s *Server) bindStoredSourceIdentity(ctx context.Context, owner string, gid int, sp sourceSpec) (bound sourceSpec, err error) {
	end := operationStage(ctx, "proxy.source.identity", "gate_id", gid)
	defer func() { end(err) }()
	bound, err = s.bindSourceIdentity(ctx, sp)
	if err != nil || sp.Mode != "node" || sp.Identity != "" {
		return bound, err
	}
	if prev, ok := s.loadExit(ctx, owner); ok {
		links, e := s.specLinks(ctx, bound)
		if e != nil {
			return bound, e
		}
		matched := false
		for _, raw := range links {
			if subscription.InventoryIdentity(raw) != bound.Identity {
				continue
			}
			n, e := subscription.ParseLink(raw)
			if e != nil {
				continue
			}
			o, e := confirmedLinkOutbound(n, bound.InsecureIdentities)
			if e != nil {
				continue
			}
			old := prev.Outbound
			old.ID, old.Tag = 0, ""
			// Probe annotations are observations, not connection identity.
			delete(old.Settings, "udp")
			delete(o.Settings, "udp")
			a, _ := json.Marshal(old)
			b, _ := json.Marshal(o)
			matched = string(a) == string(b)
			if matched {
				break
			}
		}
		if !matched {
			return bound, errors.New("旧节点的连接参数已变化，请重新选择；保留上次可用出口")
		}
	}
	id := atoiOr(strings.TrimPrefix(owner, "exit:"))
	if id <= 0 {
		return bound, errors.New("出口身份无效")
	}
	tx, e := s.Store.Ent.Tx(ctx)
	if e != nil {
		return bound, e
	}
	defer tx.Rollback()
	if e = lockActiveGates(ctx, tx.Client(), gid); e != nil {
		return bound, e
	}
	current, e := tx.Exit.Get(ctx, id)
	if e != nil {
		return bound, e
	}
	if current.GateID != gid || !reflect.DeepEqual(specOf(current.Source), sp) {
		return bound, errors.New("出口已变化，请刷新后重试")
	}
	if e = tx.Exit.UpdateOneID(id).SetSource(bound.toMap()).Exec(ctx); e != nil {
		return bound, e
	}
	return bound, tx.Commit()
}
