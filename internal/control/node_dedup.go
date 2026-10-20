package control

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/imzyb/MiGate/internal/subscription"
	"strings"
	"unicode/utf16"
)

type dedupNode struct {
	ID   int            `json:"id"`
	Data map[string]any `json:"data"`
}
type dedupChoice struct {
	Keep       int    `json:"keep_id"`
	KeepName   string `json:"keep_name"`
	Remove     int    `json:"remove_id"`
	RemoveName string `json:"remove_name"`
}

func nodePriority(n dedupNode) int {
	score := 0
	if n.Data["enabled"] == true {
		score += 8
	}
	if strings.TrimSpace(text(n.Data["name"])) != "" {
		score += 4
	}
	if strings.TrimSpace(text(n.Data["group"])) != "" {
		score += 2
	}
	if text(n.Data["colorTag"]) != "" {
		score++
	}
	if strings.TrimSpace(text(n.Data["notes"])) != "" {
		score++
	}
	return score
}
func dedupPlan(nodes []dedupNode) ([]dedupChoice, int) {
	buckets := map[string][]dedupNode{}
	keys := []string{}
	invalid := 0
	for _, n := range nodes {
		raw := text(n.Data["url"])
		if _, err := subscription.ParseLink(raw); err != nil {
			invalid++
			continue
		}
		key := subscription.InventoryIdentity(raw)
		if _, ok := buckets[key]; !ok {
			keys = append(keys, key)
		}
		buckets[key] = append(buckets[key], n)
	}
	result := []dedupChoice{}
	for _, key := range keys {
		nodes := buckets[key]
		best := nodes[0]
		for _, n := range nodes[1:] {
			if nodePriority(n) > nodePriority(best) || nodePriority(n) == nodePriority(best) && len(utf16.Encode([]rune(text(n.Data["name"])))) > len(utf16.Encode([]rune(text(best.Data["name"])))) {
				best = n
			}
		}
		for _, n := range nodes {
			if n.ID != best.ID {
				result = append(result, dedupChoice{best.ID, text(best.Data["name"]), n.ID, text(n.Data["name"])})
			}
		}
	}
	return result, invalid
}
func (s *Server) deduplicateNodes(c *gin.Context) {
	var req struct {
		Preview  bool   `json:"preview"`
		Snapshot string `json:"snapshot"`
	}
	if !bind(c, &req) {
		return
	}
	ctx := c.Request.Context()
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		failure(c, 503, "save_failed")
		return
	}
	defer tx.Rollback()
	query := `SELECT id, kind, data FROM migate.resources WHERE kind IN ('nodes','profiles') ORDER BY id LIMIT 50001`
	if !req.Preview {
		query += " FOR UPDATE"
	}
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		failure(c, 503, "query_failed")
		return
	}
	nodes := []dedupNode{}
	profiles := []dedupNode{}
	count := 0
	for rows.Next() {
		var id int
		var kind string
		var raw []byte
		if err = rows.Scan(&id, &kind, &raw); err != nil {
			break
		}
		var data map[string]any
		if err = json.Unmarshal(raw, &data); err != nil {
			break
		}
		n := dedupNode{id, data}
		if kind == "nodes" {
			nodes = append(nodes, n)
		} else {
			profiles = append(profiles, n)
		}
		count++
	}
	readErr := rows.Err()
	rows.Close()
	if err != nil || readErr != nil {
		failure(c, 503, "query_failed")
		return
	}
	if count > 50000 {
		failure(c, 422, "inventory_limit")
		return
	}
	raw, _ := json.Marshal(nodes)
	snapshot := digest(string(raw))
	plan, invalid := dedupPlan(nodes)
	if req.Preview {
		c.JSON(200, gin.H{"snapshot": snapshot, "items": plan, "node_count": len(nodes), "invalid_count": invalid})
		return
	}
	if req.Snapshot == "" || req.Snapshot != snapshot {
		failure(c, 409, "inventory_changed_preview_again")
		return
	}
	replacements := map[int]int{}
	for _, choice := range plan {
		replacements[choice.Remove] = choice.Keep
	}
	changed := 0
	for _, profile := range profiles {
		ids, ok := profile.Data["node_ids"].([]any)
		if !ok {
			continue
		}
		next := []any{}
		seen := map[int]bool{}
		touched := false
		for _, value := range ids {
			id := int(number(value))
			if replacement, ok := replacements[id]; ok {
				id = replacement
				touched = true
			}
			if !seen[id] {
				seen[id] = true
				next = append(next, id)
			}
		}
		if !touched {
			continue
		}
		profile.Data["node_ids"] = next
		body, _ := json.Marshal(profile.Data)
		if _, err = tx.ExecContext(ctx, `UPDATE migate.resources SET data=$1 WHERE id=$2 AND kind='profiles'`, body, profile.ID); err != nil {
			failure(c, 503, "save_failed")
			return
		}
		changed++
	}
	for _, choice := range plan {
		if _, err = tx.ExecContext(ctx, `DELETE FROM migate.resources WHERE id=$1 AND kind='nodes'`, choice.Remove); err != nil {
			failure(c, 503, "save_failed")
			return
		}
	}
	if err = tx.Commit(); err != nil {
		failure(c, 409, "inventory_changed_preview_again")
		return
	}
	s.audit(c, "inventory.dedup", fmt.Sprintf("nodes:%d,profiles:%d", len(plan), changed), "ok")
	c.JSON(200, gin.H{"removed": len(plan), "profiles_updated": changed, "invalid_count": invalid})
}
