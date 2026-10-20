package control

// Fixed MiSub GroupManagementModal/useManualNodes mapping. Dissolving a group
// clears only its label. Node identities, credentials and profile references stay.
import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
)

type nodeGroup struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}
type nodeGroupsDocument struct {
	Groups   []nodeGroup `json:"groups"`
	Revision string      `json:"revision"`
}
type groupReader interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func readNodeGroups(ctx context.Context, q groupReader) (nodeGroupsDocument, error) {
	d := nodeGroupsDocument{Groups: []nodeGroup{}}
	rows, e := q.QueryContext(ctx, `SELECT id,COALESCE(data->>'group','') FROM migate.resources WHERE kind='nodes' ORDER BY id`)
	if e != nil {
		return d, e
	}
	counts := map[string]int{}
	members := [][2]any{}
	for rows.Next() {
		var id int
		var name string
		if e = rows.Scan(&id, &name); e != nil {
			rows.Close()
			return d, e
		}
		if name != "" {
			counts[name]++
			members = append(members, [2]any{id, name})
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return d, e
	}
	if len(counts) > 1000 {
		return d, errors.New("group_limit")
	}
	var raw []byte
	e = q.QueryRowContext(ctx, `SELECT data->'order' FROM migate.resources WHERE kind='settings' AND key='node-group-order'`).Scan(&raw)
	order := []string{}
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return d, e
	}
	if e == nil && json.Unmarshal(raw, &order) != nil {
		return d, errors.New("invalid_group_order")
	}
	seen := map[string]bool{}
	for _, name := range order {
		if counts[name] > 0 && !seen[name] {
			seen[name] = true
			d.Groups = append(d.Groups, nodeGroup{name, counts[name]})
		}
	}
	rest := []string{}
	for name := range counts {
		if !seen[name] {
			rest = append(rest, name)
		}
	}
	sort.Strings(rest)
	for _, name := range rest {
		d.Groups = append(d.Groups, nodeGroup{name, counts[name]})
	}
	snapshot, _ := json.Marshal([]any{members, d.Groups})
	d.Revision = digest(string(snapshot))
	return d, nil
}
func (s *Server) getNodeGroups(c *gin.Context) {
	d, e := readNodeGroups(c.Request.Context(), s.DB)
	if e != nil {
		failure(c, 503, "node_groups_unavailable")
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(200, d)
}
func validGroupName(v string) bool { return v == strings.TrimSpace(v) && catalogText(v, 120, true) }
func (s *Server) changeNodeGroups(c *gin.Context) {
	var req struct {
		Operation string   `json:"operation"`
		Name      string   `json:"name"`
		NewName   string   `json:"new_name"`
		Order     []string `json:"order"`
		Revision  string   `json:"revision"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 160<<10)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&req) != nil || decoder.Decode(new(any)) != io.EOF || !catalogChoice(req.Operation, "rename", "dissolve", "order") || len(req.Order) > 1000 {
		failure(c, 400, "invalid_node_group")
		return
	}
	if req.Operation != "order" && (!validGroupName(req.Name) || req.Operation == "rename" && (!validGroupName(req.NewName) || req.NewName == req.Name)) {
		failure(c, 400, "invalid_node_group")
		return
	}
	ctx := c.Request.Context()
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		failure(c, 503, "node_group_save_failed")
		return
	}
	defer tx.Rollback()
	// A short owned-table lock covers inserted/moved members too. No other app's
	// schema is touched; readers continue, DML waits until this small transaction.
	if _, e = tx.ExecContext(ctx, `SET LOCAL lock_timeout='3s'`); e != nil {
		failure(c, 503, "node_group_save_failed")
		return
	}
	if _, e = tx.ExecContext(ctx, `LOCK TABLE migate.resources IN SHARE ROW EXCLUSIVE MODE`); e != nil {
		failure(c, 503, "node_group_save_failed")
		return
	}
	d, e := readNodeGroups(ctx, tx)
	if e != nil {
		failure(c, 503, "node_groups_unavailable")
		return
	}
	if req.Revision != d.Revision {
		failure(c, 409, "node_groups_changed")
		return
	}
	names := map[string]bool{}
	order := []string{}
	for _, v := range d.Groups {
		names[v.Name] = true
		order = append(order, v.Name)
	}
	if req.Operation == "order" {
		if len(req.Order) != len(order) {
			failure(c, 400, "invalid_node_group_order")
			return
		}
		for _, name := range req.Order {
			if !names[name] {
				failure(c, 400, "invalid_node_group_order")
				return
			}
			delete(names, name)
		}
		order = req.Order
	} else {
		if !names[req.Name] {
			failure(c, 404, "node_group_not_found")
			return
		}
		if req.Operation == "rename" && names[req.NewName] {
			failure(c, 409, "node_group_name_exists")
			return
		}
		destination := ""
		if req.Operation == "rename" {
			destination = req.NewName
		}
		encoded, _ := json.Marshal(destination)
		if _, e = tx.ExecContext(ctx, `UPDATE migate.resources SET data=jsonb_set(data,'{group}',$1::jsonb,true) WHERE kind='nodes' AND data->>'group'=$2`, encoded, req.Name); e != nil {
			failure(c, 503, "node_group_save_failed")
			return
		}
		updated := []string{}
		for _, name := range order {
			if name != req.Name {
				updated = append(updated, name)
			} else if destination != "" {
				updated = append(updated, destination)
			}
		}
		order = updated
	}
	raw, _ := json.Marshal(map[string]any{"order": order})
	if _, e = tx.ExecContext(ctx, `INSERT INTO migate.resources(kind,key,data,owner_id,created_at) VALUES('settings','node-group-order',$1,0,NOW()) ON CONFLICT(kind,key) DO UPDATE SET data=EXCLUDED.data`, raw); e != nil {
		failure(c, 503, "node_group_save_failed")
		return
	}
	after, e := readNodeGroups(ctx, tx)
	if e != nil {
		failure(c, 503, "node_groups_unavailable")
		return
	}
	if tx.Commit() != nil {
		failure(c, 503, "node_group_save_failed")
		return
	}
	s.audit(c, "inventory.groups."+req.Operation, "nodes", "ok")
	c.JSON(200, after)
}
