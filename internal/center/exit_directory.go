package center

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/shusfun/weir/internal/store/ent"
)

// A directory entry is a view over deployments, never a replacement for their
// IDs. Comparison includes credentials and security settings; no secret-derived
// fingerprint is sent to the client.
func (s *Server) exitDirectory(ctx context.Context, exits []*ent.Exit, views []gin.H) ([]gin.H, error) {
	groups := []gin.H{}
	identities := map[string]int{}
	endpoints := map[string][]int{}
	for i, e := range exits {
		if e.Kind == "freedom" {
			continue
		}
		spec, err := s.exitSpec(e)
		if err != nil {
			return nil, err
		}
		owner := "system"
		if e.OwnerUserID != nil {
			owner = "user:" + itoa(*e.OwnerUserID)
		}
		host := strings.ToLower(strings.TrimSuffix(strings.Trim(spec.Address, "[]"), "."))
		if ip := net.ParseIP(host); ip != nil {
			host = ip.String()
		}
		endpoint := owner + "/" + host + "/" + itoa(spec.Port)
		// Sources and node references do not identify a remote endpoint by address.
		if host == "" {
			endpoint += "/" + spec.Kind + "/" + itoa(spec.NodeID) + "/" + itoa(spec.SourceID) + "/" + itoa(spec.ProfileID)
		}
		spec.Remark, spec.Address = "", host
		var comparable any = spec
		if spec.Kind == "link" {
			// Parse into the actual outbound so display names and URL spelling do
			// not split otherwise identical deployments. Keep unsupported legacy
			// links separate by their complete original parameters.
			if outbound, _, err := parseConfirmedExitLink(spec.Link, spec.InsecureIdentities); err == nil {
				outbound.Address = host
				comparable = outbound
			}
		}
		raw, err := json.Marshal(comparable)
		if err != nil {
			return nil, err
		}
		identity := owner + "/" + string(raw)
		index, exists := identities[identity]
		if !exists {
			index = len(groups)
			identities[identity] = index
			groups = append(groups, gin.H{"id": e.ID, "owner_user_id": e.OwnerUserID,
				"address": spec.Address, "port": spec.Port, "kind": spec.Kind,
				"conflict": false, "reason": "", "deployments": []gin.H{}})
			endpoints[endpoint] = append(endpoints[endpoint], index)
		}
		group := groups[index]
		group["deployments"] = append(group["deployments"].([]gin.H), views[i])
	}
	for _, indices := range endpoints {
		if len(indices) < 2 {
			continue
		}
		for _, i := range indices {
			groups[i]["conflict"] = true
			groups[i]["reason"] = "同一地址存在不同的协议、凭据或安全参数，已保留为独立代理"
		}
	}
	return groups, nil
}

func (s *Server) revealExitCredentials(c *gin.Context) {
	ctx := c.Request.Context()
	var outcome error
	finish := operationStage(ctx, "proxy.credentials.reveal", "actor", c.GetInt("user_id"))
	defer func() { finish(outcome) }()
	e, ok := s.exitParam(c, true)
	if !ok {
		outcome = errors.New("exit_not_found")
		return
	}
	var req struct {
		Confirm bool `json:"confirm"`
	}
	if !bind(c, &req) || !req.Confirm {
		outcome = errors.New("confirmation_required")
		if !c.IsAborted() {
			failure(c, 400, "confirmation_required")
		}
		return
	}
	spec, err := s.exitSpec(e)
	if err != nil {
		outcome = err
		failInternal(c, 500, "decrypt_failed", err)
		return
	}
	// Revealing credentials requires a durable audit. A best-effort audit would
	// let a storage outage silently bypass the disclosure record.
	_, err = s.Store.Ent.Audit.Create().SetActor(c.GetInt("user_id")).
		SetAction("exit.credentials.reveal").SetTarget(exitOwner(e.ID)).SetResult("ok").
		SetIP(c.ClientIP()).SetDetail(traceDetail(c)).Save(ctx)
	if err != nil {
		outcome = err
		failInternal(c, 503, "audit_unavailable", err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(200, gin.H{"password": spec.Password, "link": spec.Link})
}
