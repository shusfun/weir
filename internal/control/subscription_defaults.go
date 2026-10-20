package control

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	pipeline "github.com/imzyb/MiGate/services/subscriptions"
	"io"
	"net/http"
)

type subscriptionDefaults struct {
	Operators []pipeline.Operator `json:"operators"`
}

func defaultsRevision(d subscriptionDefaults) string {
	raw, _ := json.Marshal(d)
	return digest(string(raw))
}
func decodeSubscriptionDefaults(raw []byte) (subscriptionDefaults, error) {
	d := subscriptionDefaults{}
	reader := json.NewDecoder(bytes.NewReader(raw))
	reader.DisallowUnknownFields()
	if len(raw) > 128<<10 || reader.Decode(&d) != nil || reader.Decode(new(any)) != io.EOF || len(d.Operators) > 100 {
		return d, errors.New("invalid subscription defaults")
	}
	if d.Operators == nil {
		d.Operators = []pipeline.Operator{}
	}
	_, e := pipeline.Run(nil, d.Operators, "base64")
	return d, e
}
func (s *Server) readSubscriptionDefaults(ctx context.Context) (subscriptionDefaults, error) {
	var raw []byte
	e := s.DB.QueryRowContext(ctx, `SELECT data FROM migate.resources WHERE kind='settings' AND key='subscription-defaults'`).Scan(&raw)
	if errors.Is(e, sql.ErrNoRows) {
		return subscriptionDefaults{[]pipeline.Operator{}}, nil
	}
	if e != nil {
		return subscriptionDefaults{}, e
	}
	return decodeSubscriptionDefaults(raw)
}
func (s *Server) getSubscriptionDefaults(c *gin.Context) {
	d, e := s.readSubscriptionDefaults(c.Request.Context())
	if e != nil {
		failure(c, 503, "subscription_defaults_unavailable")
		return
	}
	c.JSON(200, gin.H{"defaults": d, "revision": defaultsRevision(d)})
}
func (s *Server) putSubscriptionDefaults(c *gin.Context) {
	var req struct {
		Defaults json.RawMessage `json:"defaults"`
		Revision string          `json:"revision"`
		Password string          `json:"password"`
		Code     string          `json:"code"`
		Confirm  bool            `json:"confirm"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 144<<10)
	reader := json.NewDecoder(c.Request.Body)
	reader.DisallowUnknownFields()
	if reader.Decode(&req) != nil || reader.Decode(new(any)) != io.EOF || !req.Confirm {
		failure(c, 400, "invalid_subscription_defaults")
		return
	}
	d, e := decodeSubscriptionDefaults(req.Defaults)
	if e != nil {
		failure(c, 400, "invalid_subscription_defaults")
		return
	}
	if !s.reauth(c, req.Password, req.Code) {
		return
	}
	ctx := c.Request.Context()
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		failure(c, 503, "subscription_defaults_save_failed")
		return
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `SET LOCAL lock_timeout='3s'`); e == nil {
		_, e = tx.ExecContext(ctx, `LOCK TABLE migate.resources IN SHARE ROW EXCLUSIVE MODE`)
	}
	if e != nil {
		failure(c, 503, "subscription_defaults_save_failed")
		return
	}
	old := subscriptionDefaults{[]pipeline.Operator{}}
	var raw []byte
	e = tx.QueryRowContext(ctx, `SELECT data FROM migate.resources WHERE kind='settings' AND key='subscription-defaults'`).Scan(&raw)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		failure(c, 503, "subscription_defaults_unavailable")
		return
	}
	if e == nil {
		old, e = decodeSubscriptionDefaults(raw)
		if e != nil {
			failure(c, 503, "subscription_defaults_unavailable")
			return
		}
	}
	if req.Revision != defaultsRevision(old) {
		failure(c, 409, "subscription_defaults_changed")
		return
	}
	raw, _ = json.Marshal(d)
	_, e = tx.ExecContext(ctx, `INSERT INTO migate.resources(kind,key,data,owner_id,created_at) VALUES('settings','subscription-defaults',$1,0,NOW()) ON CONFLICT(kind,key) DO UPDATE SET data=EXCLUDED.data`, raw)
	if e != nil || tx.Commit() != nil {
		failure(c, 503, "subscription_defaults_save_failed")
		return
	}
	s.audit(c, "settings.subscription-defaults", "subscription-defaults", "ok")
	c.JSON(200, gin.H{"defaults": d, "revision": defaultsRevision(d)})
}
