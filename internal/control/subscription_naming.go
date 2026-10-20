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
	"regexp"
	"strings"
)

type subscriptionNaming struct {
	EnableManualNodes   bool             `json:"enable_manual_nodes"`
	EnableSubscriptions bool             `json:"enable_subscriptions"`
	ManualNodePrefix    string           `json:"manual_node_prefix"`
	PrependGroupName    bool             `json:"prepend_group_name"`
	RegionOverrides     []regionOverride `json:"region_overrides"`
}
type regionOverride struct {
	Pattern string `json:"pattern"`
	Region  string `json:"region"`
	Flags   string `json:"flags"`
}

func emptySubscriptionNaming() subscriptionNaming {
	return subscriptionNaming{ManualNodePrefix: "手动节点", RegionOverrides: []regionOverride{}}
}
func namingRevision(d subscriptionNaming) string {
	raw, _ := json.Marshal(d)
	return digest(string(raw))
}
func decodeSubscriptionNaming(raw []byte) (subscriptionNaming, error) {
	d := emptySubscriptionNaming()
	reader := json.NewDecoder(bytes.NewReader(raw))
	reader.DisallowUnknownFields()
	if len(raw) > 64<<10 || reader.Decode(&d) != nil || reader.Decode(new(any)) != io.EOF || len(d.RegionOverrides) > 64 || len(d.ManualNodePrefix) > 120 || strings.ContainsAny(d.ManualNodePrefix, "\x00\r\n") {
		return d, errors.New("invalid naming settings")
	}
	if d.RegionOverrides == nil {
		d.RegionOverrides = []regionOverride{}
	}
	for _, r := range d.RegionOverrides {
		if _, e := compileRegionOverride(r); e != nil {
			return d, e
		}
	}
	return d, nil
}
func compileRegionOverride(r regionOverride) (*regexp.Regexp, error) {
	if r.Pattern == "" || len(r.Pattern) > 512 || r.Region == "" || len(r.Region) > 60 || strings.ContainsAny(r.Region, "\x00\r\n") || (r.Flags != "" && r.Flags != "i") {
		return nil, errors.New("invalid region override")
	}
	pattern := r.Pattern
	if r.Flags == "i" {
		pattern = "(?i)" + pattern
	}
	return regexp.Compile(pattern)
}
func (s *Server) readSubscriptionNaming(ctx context.Context) (subscriptionNaming, error) {
	var raw []byte
	e := s.DB.QueryRowContext(ctx, `SELECT data FROM migate.resources WHERE kind='settings' AND key='subscription-naming'`).Scan(&raw)
	if errors.Is(e, sql.ErrNoRows) {
		return emptySubscriptionNaming(), nil
	}
	if e != nil {
		return subscriptionNaming{}, e
	}
	return decodeSubscriptionNaming(raw)
}
func (s *Server) getSubscriptionNaming(c *gin.Context) {
	d, e := s.readSubscriptionNaming(c.Request.Context())
	if e != nil {
		failure(c, 503, "subscription_naming_unavailable")
		return
	}
	c.JSON(200, gin.H{"defaults": d, "revision": namingRevision(d)})
}
func (s *Server) putSubscriptionNaming(c *gin.Context) {
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
		failure(c, 400, "invalid_subscription_naming")
		return
	}
	d, e := decodeSubscriptionNaming(req.Defaults)
	if e != nil {
		failure(c, 400, "invalid_subscription_naming")
		return
	}
	if !s.reauth(c, req.Password, req.Code) {
		return
	}
	ctx := c.Request.Context()
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		failure(c, 503, "subscription_naming_save_failed")
		return
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `SET LOCAL lock_timeout='3s'`); e == nil {
		_, e = tx.ExecContext(ctx, `LOCK TABLE migate.resources IN SHARE ROW EXCLUSIVE MODE`)
	}
	if e != nil {
		failure(c, 503, "subscription_naming_save_failed")
		return
	}
	old := emptySubscriptionNaming()
	var raw []byte
	e = tx.QueryRowContext(ctx, `SELECT data FROM migate.resources WHERE kind='settings' AND key='subscription-naming'`).Scan(&raw)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		failure(c, 503, "subscription_naming_unavailable")
		return
	}
	if e == nil {
		old, e = decodeSubscriptionNaming(raw)
		if e != nil {
			failure(c, 503, "subscription_naming_unavailable")
			return
		}
	}
	if req.Revision != namingRevision(old) {
		failure(c, 409, "subscription_naming_changed")
		return
	}
	raw, _ = json.Marshal(d)
	_, e = tx.ExecContext(ctx, `INSERT INTO migate.resources(kind,key,data,owner_id,created_at) VALUES('settings','subscription-naming',$1,0,NOW()) ON CONFLICT(kind,key) DO UPDATE SET data=EXCLUDED.data`, raw)
	if e != nil || tx.Commit() != nil {
		failure(c, 503, "subscription_naming_save_failed")
		return
	}
	s.audit(c, "settings.subscription-naming", "subscription-naming", "ok")
	c.JSON(200, gin.H{"defaults": d, "revision": namingRevision(d)})
}

type profilePrefix struct {
	EnableManualNodes   *bool   `json:"enable_manual_nodes,omitempty"`
	EnableSubscriptions *bool   `json:"enable_subscriptions,omitempty"`
	ManualNodePrefix    *string `json:"manual_node_prefix,omitempty"`
	PrependGroupName    *bool   `json:"prepend_group_name,omitempty"`
}

func decodeProfilePrefix(data map[string]any) (profilePrefix, error) {
	var p profilePrefix
	value, exists := data["prefix_settings"]
	if !exists {
		return p, nil
	}
	raw, e := json.Marshal(value)
	if e != nil || string(raw) == "null" || len(raw) > 2048 {
		return p, errors.New("invalid prefix settings")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&p) != nil {
		return p, errors.New("invalid prefix settings")
	}
	if p.ManualNodePrefix != nil && (len(*p.ManualNodePrefix) > 120 || strings.ContainsAny(*p.ManualNodePrefix, "\x00\r\n")) {
		return p, errors.New("invalid prefix")
	}
	return p, nil
}
func (s *Server) profileNaming(ctx context.Context, data map[string]any) (subscriptionNaming, error) {
	n, e := s.readSubscriptionNaming(ctx)
	if e != nil {
		return n, e
	}
	p, e := decodeProfilePrefix(data)
	if e != nil {
		return n, e
	}
	if p.EnableManualNodes != nil {
		n.EnableManualNodes = *p.EnableManualNodes
	}
	if p.EnableSubscriptions != nil {
		n.EnableSubscriptions = *p.EnableSubscriptions
	}
	if p.PrependGroupName != nil {
		n.PrependGroupName = *p.PrependGroupName
	}
	if p.ManualNodePrefix != nil && strings.TrimSpace(*p.ManualNodePrefix) != "" {
		n.ManualNodePrefix = *p.ManualNodePrefix
	}
	return n, nil
}
func applyInventoryPrefixes(links []string, metadata []pipeline.NodeMetadata, n subscriptionNaming) ([]string, error) {
	if len(metadata) != len(links) {
		return nil, errors.New("prefix metadata mismatch")
	}
	out := append([]string{}, links...)
	for i, m := range metadata {
		var e error
		if m.Manual {
			if n.PrependGroupName {
				out[i], e = pipeline.PrependName(out[i], m.Group)
			}
			if e == nil && n.EnableManualNodes {
				out[i], e = pipeline.PrependName(out[i], n.ManualNodePrefix)
			}
		} else if n.EnableSubscriptions {
			out[i], e = pipeline.PrependName(out[i], m.Provider)
		}
		if e != nil {
			return nil, e
		}
	}
	return out, nil
}
