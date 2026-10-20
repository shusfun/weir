package control

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	"sort"
	"strings"
)

// Swap only presentation positions. Row locks preserve concurrently edited
// credentials and metadata. The operation never changes profile node order.
func (s *Server) reorderNodes(c *gin.Context) {
	var req struct {
		ID      int `json:"id"`
		OtherID int `json:"other_id"`
	}
	if !bind(c, &req) {
		return
	}
	if req.ID <= 0 || req.OtherID <= 0 || req.ID == req.OtherID {
		failure(c, 400, "invalid_node_order")
		return
	}
	ctx := c.Request.Context()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		failure(c, 503, "save_failed")
		return
	}
	defer tx.Rollback()
	ids := []int{req.ID, req.OtherID}
	sort.Ints(ids)
	data := map[int]map[string]any{}
	for _, id := range ids {
		var raw []byte
		if err = tx.QueryRowContext(ctx, `SELECT data FROM migate.resources WHERE id=$1 AND kind='nodes' FOR UPDATE`, id).Scan(&raw); err != nil {
			failure(c, 404, "not_found")
			return
		}
		var value map[string]any
		if json.Unmarshal(raw, &value) != nil {
			failure(c, 500, "invalid_node_metadata")
			return
		}
		data[id] = value
	}
	positions := map[int]float64{}
	for _, id := range ids {
		value, ok := data[id]["position"].(float64)
		if !ok {
			value = -float64(id)
		}
		positions[id] = value
	}
	for _, id := range ids {
		other := req.ID
		if id == req.ID {
			other = req.OtherID
		}
		data[id]["position"] = positions[other]
		raw, e := json.Marshal(data[id])
		if e != nil {
			failure(c, 500, "save_failed")
			return
		}
		if _, err = tx.ExecContext(ctx, `UPDATE migate.resources SET data=$1 WHERE id=$2 AND kind='nodes'`, raw, id); err != nil {
			failure(c, 500, "save_failed")
			return
		}
	}
	if err = tx.Commit(); err != nil {
		failure(c, 500, "save_failed")
		return
	}
	s.audit(c, "inventory.reorder", "nodes", "ok")
	c.JSON(200, gin.H{"ok": true})
}

func (s *Server) groupNodes(c *gin.Context) {
	var req struct {
		IDs   []int  `json:"ids"`
		Group string `json:"group"`
	}
	if !bind(c, &req) {
		return
	}
	req.Group = strings.TrimSpace(req.Group)
	if len(req.IDs) == 0 || len(req.IDs) > 500 || len(req.Group) > 120 {
		failure(c, 400, "invalid_node_group")
		return
	}
	seen := map[int]bool{}
	for _, id := range req.IDs {
		if id <= 0 || seen[id] {
			failure(c, 400, "invalid_node_group")
			return
		}
		seen[id] = true
	}
	results := []gin.H{}
	for _, id := range req.IDs {
		// jsonb_set updates only the group; stale UI copies cannot replace secrets.
		group, _ := json.Marshal(req.Group)
		result, err := s.DB.ExecContext(c.Request.Context(), `UPDATE migate.resources SET data=jsonb_set(data,'{group}',$1::jsonb,true) WHERE id=$2 AND kind='nodes'`, group, id)
		ok := false
		if err == nil {
			n, e := result.RowsAffected()
			ok = e == nil && n == 1
		}
		entry := gin.H{"id": id, "ok": ok}
		if !ok {
			entry["error"] = "not_found_or_save_failed"
		}
		results = append(results, entry)
	}
	s.audit(c, "inventory.group", "nodes", "reported")
	c.JSON(200, gin.H{"items": results})
}
