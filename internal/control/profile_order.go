package control

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
)

type profileOrderItem struct {
	ID       int     `json:"id"`
	Name     string  `json:"name"`
	Position float64 `json:"position"`
}
type profileOrderDocument struct {
	Items    []profileOrderItem `json:"items"`
	Revision string             `json:"revision"`
}

func readProfileOrder(ctx context.Context, q groupReader) (profileOrderDocument, error) {
	d := profileOrderDocument{Items: []profileOrderItem{}}
	rows, e := q.QueryContext(ctx, `SELECT id,COALESCE(data->>'name',''),COALESCE(CASE WHEN jsonb_typeof(data->'position')='number' THEN (data->>'position')::float8 END,-id) FROM migate.resources WHERE kind='profiles' ORDER BY 3,id DESC LIMIT 2001`)
	if e != nil {
		return d, e
	}
	defer rows.Close()
	snapshot := [][2]any{}
	for rows.Next() {
		var item profileOrderItem
		if e = rows.Scan(&item.ID, &item.Name, &item.Position); e != nil {
			return d, e
		}
		d.Items = append(d.Items, item)
		snapshot = append(snapshot, [2]any{item.ID, item.Position})
	}
	if rows.Err() != nil {
		return d, rows.Err()
	}
	if len(d.Items) > 2000 {
		return d, sql.ErrNoRows
	}
	raw, _ := json.Marshal(snapshot)
	d.Revision = digest(string(raw))
	return d, nil
}
func (s *Server) profileOrder(c *gin.Context) {
	d, e := readProfileOrder(c.Request.Context(), s.DB)
	if e != nil {
		failure(c, 503, "profile_order_unavailable")
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(200, d)
}
func (s *Server) saveProfileOrder(c *gin.Context) {
	var req struct {
		IDs      []int  `json:"ids"`
		Revision string `json:"revision"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&req) != nil || decoder.Decode(new(any)) != io.EOF || len(req.IDs) > 2000 {
		failure(c, 400, "invalid_profile_order")
		return
	}
	ctx := c.Request.Context()
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		failure(c, 503, "profile_order_save_failed")
		return
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `SET LOCAL lock_timeout='3s'`); e != nil {
		failure(c, 503, "profile_order_save_failed")
		return
	}
	if _, e = tx.ExecContext(ctx, `LOCK TABLE migate.resources IN SHARE ROW EXCLUSIVE MODE`); e != nil {
		failure(c, 503, "profile_order_save_failed")
		return
	}
	d, e := readProfileOrder(ctx, tx)
	if e != nil {
		failure(c, 503, "profile_order_unavailable")
		return
	}
	if req.Revision != d.Revision {
		failure(c, 409, "profile_order_changed")
		return
	}
	expected := map[int]bool{}
	for _, p := range d.Items {
		expected[p.ID] = true
	}
	if len(req.IDs) != len(expected) {
		failure(c, 400, "invalid_profile_order")
		return
	}
	for _, id := range req.IDs {
		if !expected[id] {
			failure(c, 400, "invalid_profile_order")
			return
		}
		delete(expected, id)
	}
	for i, id := range req.IDs {
		if _, e = tx.ExecContext(ctx, `UPDATE migate.resources SET data=jsonb_set(data,'{position}',to_jsonb($1::int),true) WHERE id=$2 AND kind='profiles'`, i, id); e != nil {
			failure(c, 503, "profile_order_save_failed")
			return
		}
	}
	next, e := readProfileOrder(ctx, tx)
	if e != nil || tx.Commit() != nil {
		failure(c, 503, "profile_order_save_failed")
		return
	}
	s.audit(c, "inventory.profiles.order", "profiles", "ok")
	c.JSON(200, next)
}
