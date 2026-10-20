package control

import (
	"github.com/gin-gonic/gin"
	"github.com/imzyb/MiGate/internal/subscription"
)

func (s *Server) importInventory(c *gin.Context) {
	var req struct {
		Content     string `json:"content"`
		Preview     bool   `json:"preview"`
		Deduplicate bool   `json:"deduplicate"`
	}
	if !bind(c, &req) {
		return
	}
	links, err := subscription.DecodeInventory([]byte(req.Content))
	if err != nil || len(links) > 500 {
		failure(c, 422, "invalid_inventory")
		return
	}
	parsed, err := subscription.ParseLinks(links)
	if err != nil || len(parsed.Skipped) > 0 {
		failure(c, 422, "invalid_inventory")
		return
	}
	seen := map[string]bool{}
	unique := parsed.Nodes[:0]
	duplicates := 0
	preview := []gin.H{}
	for index, node := range parsed.Nodes {
		identity := subscription.InventoryIdentity(node.RawLink)
		duplicate := seen[identity]
		if duplicate {
			duplicates++
		}
		seen[identity] = true
		if !duplicate || !req.Deduplicate {
			unique = append(unique, node)
			preview = append(preview, gin.H{"index": index + 1, "name": node.Remark, "protocol": node.Protocol, "server": node.Address, "port": node.Port, "duplicate": duplicate})
		}
	}
	if req.Preview {
		c.JSON(200, gin.H{"nodes": preview, "node_count": len(unique), "duplicate_count": duplicates})
		return
	}
	parsed.Nodes = unique
	tx, err := s.Store.Tx(c.Request.Context())
	if err != nil {
		failure(c, 500, "save_failed")
		return
	}
	defer tx.Rollback()
	ids := []int{}
	for _, n := range parsed.Nodes {
		r, err := tx.Resource.Create().SetKind("nodes").SetKey(randomToken(16)).SetData(map[string]any{"name": n.Remark, "url": n.RawLink}).Save(c.Request.Context())
		if err != nil {
			failure(c, 500, "save_failed")
			return
		}
		ids = append(ids, r.ID)
	}
	if err = tx.Commit(); err != nil {
		failure(c, 500, "save_failed")
		return
	}
	s.audit(c, "inventory.import", "nodes", "ok")
	c.JSON(200, gin.H{"ids": ids, "node_count": len(ids)})
}
