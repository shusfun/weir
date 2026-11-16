package center

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/chain"
	"github.com/shusfun/weir/internal/store/ent/gate"
	"github.com/shusfun/weir/internal/store/ent/resource"
	"github.com/shusfun/weir/internal/store/ent/subgrant"
	"github.com/shusfun/weir/internal/store/ent/subnode"
	"github.com/shusfun/weir/internal/subscription"
)

const catalogSourceKind = "catalog-source-node"
const catalogGrantKind = "catalog-grants"
const catalogRefreshKind = "catalog-refresh"

type objectState struct {
	Status     string     `json:"status"`
	Label      string     `json:"label,omitempty"`
	Reason     string     `json:"reason"`
	ObservedAt *time.Time `json:"observed_at"`
	Next       []string   `json:"next"`
}

type catalogNode struct {
	Ref        string      `json:"ref"`
	Kind       string      `json:"kind"`
	ID         int         `json:"id"`
	SourceID   int         `json:"source_id,omitempty"`
	GateID     int         `json:"gate_id,omitempty"`
	OwnerID    int         `json:"owner_id,omitempty"`
	Name       string      `json:"name"`
	Protocol   string      `json:"protocol"`
	Selected   bool        `json:"selected"`
	Authorized bool        `json:"authorized"`
	ChainEntry bool        `json:"chain_entry"`
	State      objectState `json:"state"`
	URI        string      `json:"-"`
}

func nodeCatalogRevision(items []catalogNode) string {
	type access struct {
		Ref                              string
		Owner                            int
		Selected, Authorized, ChainEntry bool
	}
	rows := make([]access, 0, len(items))
	for _, item := range items {
		rows = append(rows, access{item.Ref, item.OwnerID, item.Selected, item.Authorized, item.ChainEntry})
	}
	return version(rows)
}

type sourceCatalogRecord struct {
	SourceID   int       `json:"source_id"`
	Identity   string    `json:"identity"`
	Name       string    `json:"name"`
	Protocol   string    `json:"protocol"`
	Ciphertext string    `json:"ciphertext"`
	Selected   bool      `json:"selected"`
	Available  bool      `json:"available"`
	ObservedAt time.Time `json:"observed_at"`
}

func catalogRecord(row *ent.Resource) (record sourceCatalogRecord, err error) {
	data, err := json.Marshal(row.Data)
	if err == nil {
		err = json.Unmarshal(data, &record)
	}
	return
}

// Source identities include credentials and transport/security parameters, but
// exclude display names. A missing source member keeps its ID and selection.
func (s *Server) syncSourceCatalog(ctx context.Context, source int, raw []string) (err error) {
	finish := operationStage(ctx, "node.catalog.refresh", "source_id", source)
	defer func() { finish(err) }()
	tx, err := s.Store.Ent.Tx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.Resource.Query().Where(resource.KindEQ(catalogSourceKind), resource.OwnerIDEQ(source)).All(ctx)
	if err != nil {
		return err
	}
	profiles, err := tx.SubProfile.Query().All(ctx)
	if err != nil {
		return err
	}
	selected := false
	for _, profile := range profiles {
		if profile.AllSources {
			selected = true
			break
		}
		for _, member := range profileMembers(profile) {
			if member.Kind == "source" && member.ID == source {
				selected = true
			}
		}
	}
	old := map[string]*ent.Resource{}
	for _, row := range rows {
		old[row.Key] = row
	}
	seen := map[string]bool{}
	now := time.Now().UTC()
	for _, uri := range raw {
		parsed, e := subscription.ParseLink(uri)
		if e != nil {
			continue
		}
		identity := subscription.InventoryIdentity(uri)
		key := itoa(source) + ":" + identity
		if seen[key] {
			continue
		}
		seen[key] = true
		cipher, e := s.Crypto.Encrypt(uri)
		if e != nil {
			return e
		}
		record := sourceCatalogRecord{SourceID: source, Identity: identity, Name: parsed.Remark, Protocol: parsed.Protocol, Ciphertext: cipher, Available: true, Selected: selected, ObservedAt: now}
		if row := old[key]; row != nil {
			prior, e := catalogRecord(row)
			if e != nil {
				return e
			}
			record.Selected = prior.Selected
			if e = row.Update().SetData(toMap(record)).Exec(ctx); e != nil {
				return e
			}
		} else {
			if _, e = tx.Resource.Create().SetKind(catalogSourceKind).SetKey(key).SetOwnerID(source).SetData(toMap(record)).Save(ctx); e != nil {
				return e
			}
		}
	}
	for key, row := range old {
		if seen[key] {
			continue
		}
		record, e := catalogRecord(row)
		if e != nil {
			return e
		}
		record.Available, record.ObservedAt = false, now
		if e = row.Update().SetData(toMap(record)).Exec(ctx); e != nil {
			return e
		}
	}
	return tx.Commit()
}

// A later refresh must not resurrect nodes removed by an earlier completed fetch.
func (s *Server) catalogRefreshStarted(ctx context.Context, source int) (time.Time, bool, error) {
	row, err := s.Store.Ent.Resource.Query().Where(resource.KindEQ(catalogRefreshKind), resource.KeyEQ(itoa(source))).Only(ctx)
	if ent.IsNotFound(err) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	text, _ := row.Data["started"].(string)
	at, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		return time.Time{}, false, err
	}
	return at, true, nil
}

func (s *Server) saveCatalogRefreshStarted(ctx context.Context, source int, started time.Time) error {
	return s.Store.Ent.Resource.Create().SetKind(catalogRefreshKind).SetKey(itoa(source)).SetOwnerID(source).SetData(map[string]any{"started": started.UTC().Format(time.RFC3339Nano)}).OnConflictColumns(resource.FieldKind, resource.FieldKey).UpdateNewValues().Exec(ctx)
}

func (s *Server) catalogRoutes(auth, admin *gin.RouterGroup) {
	admin.GET("/node-catalog", func(c *gin.Context) { s.getCatalog(c, true, false) })
	admin.GET("/users/:uid/node-catalog", func(c *gin.Context) { s.getCatalog(c, true, true) })
	auth.GET("/me/node-catalog", func(c *gin.Context) { s.getCatalog(c, false, true) })
	admin.PUT("/users/:uid/node-catalog/grants", s.putCatalogGrants)
	admin.PUT("/node-catalog/source/:id/selection", s.selectCatalogSource)
}

func (s *Server) catalogGrants(ctx context.Context, uid int) (map[string]bool, error) {
	out := map[string]bool{}
	row, err := s.Store.Ent.Resource.Query().Where(resource.KindEQ(catalogGrantKind), resource.KeyEQ(itoa(uid))).Only(ctx)
	if ent.IsNotFound(err) {
		return s.legacyCatalogGrants(ctx, uid)
	}
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(row.Data["refs"])
	if err != nil {
		return nil, err
	}
	var refs []string
	if err = json.Unmarshal(encoded, &refs); err != nil {
		return nil, err
	}
	for _, ref := range refs {
		out[ref] = true
	}
	return out, nil
}

// Authorization uses transactional catalog rows only. Rendering node status
// also reads observation caches outside that transaction and can exhaust a
// single-connection SQLite pool while a link is being saved.
func (s *Server) catalogLinkSelections(ctx context.Context, uid int, requested []string) ([]string, error) {
	refs := []string{}
	grants, err := s.catalogGrants(ctx, uid)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, ref := range requested {
		if !grants[ref] {
			return nil, errors.New("node_not_allowed")
		}
		kind, rawID, _ := strings.Cut(ref, ":")
		id, err := strconv.Atoi(rawID)
		if err != nil || id <= 0 || itoa(id) != rawID {
			return nil, errors.New("node_not_allowed")
		}
		allowed := false
		switch kind {
		case "source":
			row, err := s.Store.Ent.Resource.Query().Where(resource.IDEQ(id), resource.KindEQ(catalogSourceKind)).Only(ctx)
			if err != nil && !ent.IsNotFound(err) {
				return nil, err
			}
			if row != nil {
				record, err := catalogRecord(row)
				if err != nil {
					return nil, err
				}
				allowed = record.Selected
			}
		case "manual":
			allowed, err = s.Store.Ent.SubNode.Query().Where(subnode.IDEQ(id)).Exist(ctx)
			if err != nil {
				return nil, err
			}
		}
		if !allowed {
			return nil, errors.New("node_not_allowed")
		}
		if !seen[ref] {
			refs, seen[ref] = append(refs, ref), true
		}
	}
	return refs, nil
}

func (s *Server) nodeCatalog(ctx context.Context, subject *ent.User, all bool) ([]catalogNode, error) {
	items := []catalogNode{}
	grants := map[string]bool{}
	if subject != nil {
		var err error
		grants, err = s.catalogGrants(ctx, subject.ID)
		if err != nil {
			return nil, err
		}
	}
	nodes, err := s.Store.Ent.Node.Query().All(ctx)
	if err != nil {
		return nil, err
	}
	gates, err := s.Store.Ent.Gate.Query().All(ctx)
	if err != nil {
		return nil, err
	}
	devices := map[int]*ent.Gate{}
	for _, g := range gates {
		devices[g.ID] = g
	}
	for _, n := range nodes {
		authorized := subject != nil && (subject.AllNodes || containsInt(subject.NodeIds, n.ID))
		if !all && !authorized {
			continue
		}
		g := devices[n.GateID]
		usable := g != nil && g.Status != gate.StatusRevoked
		state := s.nodeObjectState(ctx, n, g)
		items = append(items, catalogNode{Ref: "node:" + itoa(n.ID), Kind: "node", ID: n.ID, GateID: n.GateID, Name: n.Name, Protocol: n.Protocol, Selected: true, Authorized: authorized, ChainEntry: authorized && n.Enabled && usable, State: state})
	}
	rows, err := s.Store.Ent.Resource.Query().Where(resource.KindEQ(catalogSourceKind)).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		r, e := catalogRecord(row)
		if e != nil {
			return nil, e
		}
		ref := "source:" + itoa(row.ID)
		authorized := grants[ref]
		if !all && (!authorized || !r.Selected) {
			continue
		}
		state := objectState{Status: "unknown", Reason: "机场直用路径与用量未观测", ObservedAt: &r.ObservedAt, Next: []string{"select"}}
		available := r.Available
		if src, e := s.Store.Ent.SubSource.Get(ctx, r.SourceID); ent.IsNotFound(e) || e == nil && !src.Enabled {
			available = false
		} else if e != nil {
			return nil, e
		}
		if !available {
			state.Status, state.Reason = "unavailable", "订阅源已停用、删除或不再包含此节点；原引用已保留"
		}
		uri, e := s.Crypto.Decrypt(r.Ciphertext)
		if e != nil {
			return nil, e
		}
		items = append(items, catalogNode{Ref: ref, Kind: "source", ID: row.ID, SourceID: r.SourceID, Name: r.Name, Protocol: r.Protocol, Selected: r.Selected, Authorized: authorized, State: state, URI: uri})
	}
	manuals, err := s.Store.Ent.SubNode.Query().Order(ent.Asc(subnode.FieldID)).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, n := range manuals {
		ref := "manual:" + itoa(n.ID)
		authorized := grants[ref]
		if !all && !authorized {
			continue
		}
		state := objectState{Status: "unknown", Reason: "尚无连接实测结果", Next: []string{"test"}}
		if !n.Enabled {
			state.Status, state.Reason = "disabled", "手动节点已停用"
		}
		protocol := ""
		if parsed, e := subscription.ParseLink(n.URL); e == nil {
			protocol = parsed.Protocol
		} else {
			state.Status, state.Reason = "unavailable", "节点参数无法解析"
		}
		items = append(items, catalogNode{Ref: ref, Kind: "manual", ID: n.ID, Name: n.Name, Protocol: protocol, Selected: true, Authorized: authorized, State: state, URI: n.URL})
	}
	query := s.Store.Ent.Chain.Query()
	if subject != nil {
		query.Where(chain.UserIDEQ(subject.ID))
	} else if !all {
		return items, nil
	}
	chains, err := query.All(ctx)
	if err != nil {
		return nil, err
	}
	for _, n := range chains {
		state := objectState{Status: "unknown", Reason: "链式路径尚未完整观测", Next: []string{"test"}}
		if !n.Enabled {
			state.Status, state.Reason = "disabled", "链式节点已停用"
		}
		items = append(items, catalogNode{Ref: "chain:" + itoa(n.ID), Kind: "chain", ID: n.ID, OwnerID: n.UserID, Name: n.Name, Selected: true, Authorized: subject != nil && subject.ID == n.UserID, State: state})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Ref < items[j].Ref })
	return items, nil
}

func (s *Server) getCatalog(c *gin.Context, admin, hasSubject bool) {
	var subject *ent.User
	if hasSubject {
		var ok bool
		subject, ok = s.subject(c, admin)
		if !ok {
			return
		}
	}
	items, err := s.nodeCatalog(c.Request.Context(), subject, admin)
	if err != nil {
		failInternal(c, 503, "query_failed", err)
		return
	}
	c.JSON(200, gin.H{"items": items, "revision": nodeCatalogRevision(items)})
}

func (s *Server) selectCatalogSource(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	var req struct {
		Selected bool `json:"selected"`
	}
	if !bind(c, &req) {
		return
	}
	ctx := c.Request.Context()
	row, err := s.Store.Ent.Resource.Get(ctx, id)
	if err != nil || row.Kind != catalogSourceKind {
		failure(c, 404, "node_not_found")
		return
	}
	err = s.Store.Lock.Do(ctx, "subscription-source:"+itoa(row.OwnerID), func(ctx context.Context) error {
		current, e := s.Store.Ent.Resource.Get(ctx, id)
		if e != nil {
			return e
		}
		r, e := catalogRecord(current)
		if e != nil {
			return e
		}
		r.Selected = req.Selected
		return current.Update().SetData(toMap(r)).Exec(ctx)
	})
	if err != nil {
		failInternal(c, 503, "save_failed", err)
		return
	}
	s.audit(c, "node.catalog.select", "source:"+itoa(id), "ok")
	c.Status(204)
}

func (s *Server) putCatalogGrants(c *gin.Context) {
	u, ok := s.subject(c, true)
	if !ok {
		return
	}
	var req struct {
		Refs     []string `json:"refs"`
		Revision string   `json:"revision"`
	}
	if !bind(c, &req) {
		return
	}
	ctx := c.Request.Context()
	finish := operationStage(ctx, "node.authorize", "user_id", u.ID)
	err := s.Store.Lock.Do(ctx, "cred-sync", func(ctx context.Context) error {
		var e error
		u, e = s.Store.Ent.User.Get(ctx, u.ID)
		if e != nil {
			return e
		}
		items, e := s.nodeCatalog(ctx, u, true)
		if e != nil {
			return e
		}
		if req.Revision == "" || req.Revision != nodeCatalogRevision(items) {
			return errors.New("catalog_changed")
		}
		allowed := map[string]catalogNode{}
		for _, item := range items {
			allowed[item.Ref] = item
		}
		native := []int{}
		refs := []string{}
		seen := map[string]bool{}
		for _, ref := range req.Refs {
			item, exists := allowed[ref]
			if !exists || !item.Selected || item.Kind == "chain" && item.OwnerID != u.ID {
				return errors.New("node_not_allowed")
			}
			if seen[ref] {
				continue
			}
			seen[ref] = true
			if item.Kind == "node" {
				native = append(native, item.ID)
			} else if item.Kind != "chain" {
				refs = append(refs, ref)
			}
		}
		if u.Role == "admin" {
			for _, item := range items {
				if item.Kind == "node" && !seen[item.Ref] {
					return errors.New("admin_all_nodes")
				}
			}
		}
		tx, e := s.Store.Ent.Tx(ctx)
		if e != nil {
			return e
		}
		defer tx.Rollback()
		if e = tx.User.UpdateOneID(u.ID).SetAllNodes(u.Role == "admin").SetNodeIds(native).Exec(ctx); e != nil {
			return e
		}
		if e = tx.Resource.Create().SetKind(catalogGrantKind).SetKey(itoa(u.ID)).SetOwnerID(u.ID).SetData(map[string]any{"refs": refs, "trace_id": taskTrace(ctx)}).OnConflictColumns(resource.FieldKind, resource.FieldKey).UpdateNewValues().Exec(ctx); e != nil {
			return e
		}
		return tx.Commit()
	})
	finish(err)
	if err != nil {
		failDetail(c, 409, "catalog_grant_failed", err.Error(), nil)
		return
	}
	if err = s.SyncUser(ctx, u.ID); err != nil {
		failInternal(c, 503, "sync_failed", err)
		return
	}
	s.audit(c, "node.authorize", "user:"+itoa(u.ID), "ok")
	c.Status(204)
}

// Existing group grants stay effective until the administrator replaces them
// with an explicit catalog selection. No link token or object ID is migrated.
func (s *Server) legacyCatalogGrants(ctx context.Context, uid int) (map[string]bool, error) {
	out := map[string]bool{}
	grants, err := s.Store.Ent.SubGrant.Query().Where(subgrant.UserIDEQ(uid)).All(ctx)
	if err != nil {
		return nil, err
	}
	sources, manuals, visited := map[int]bool{}, map[int]bool{}, map[int]bool{}
	all := false
	var visit func(int) error
	visit = func(id int) error {
		if visited[id] {
			return nil
		}
		visited[id] = true
		p, e := s.Store.Ent.SubProfile.Get(ctx, id)
		if ent.IsNotFound(e) {
			return nil
		}
		if e != nil {
			return e
		}
		if !p.Enabled || p.ExpiresAt != nil && !time.Now().Before(*p.ExpiresAt) {
			return nil
		}
		all = all || p.AllSources
		for _, m := range profileMembers(p) {
			switch m.Kind {
			case "source":
				sources[m.ID] = true
			case "node":
				manuals[m.ID] = true
			case "profile":
				if e = visit(m.ID); e != nil {
					return e
				}
			}
		}
		return nil
	}
	for _, grant := range grants {
		if err = visit(grant.ProfileID); err != nil {
			return nil, err
		}
	}
	rows, err := s.Store.Ent.Resource.Query().Where(resource.KindEQ(catalogSourceKind)).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if all || sources[row.OwnerID] {
			out["source:"+itoa(row.ID)] = true
		}
	}
	if all {
		nodes, e := s.Store.Ent.SubNode.Query().All(ctx)
		if e != nil {
			return nil, e
		}
		for _, n := range nodes {
			manuals[n.ID] = true
		}
	}
	for id := range manuals {
		out["manual:"+itoa(id)] = true
	}
	return out, nil
}

type catalogSubjectKey struct{}

func (s *Server) filterCatalogGroup(ctx context.Context, list string) (string, error) {
	u, ok := ctx.Value(catalogSubjectKey{}).(*ent.User)
	if !ok {
		return list, nil
	}
	explicit, err := s.Store.Ent.Resource.Query().Where(resource.KindEQ(catalogGrantKind), resource.KeyEQ(itoa(u.ID))).Exist(ctx)
	if err != nil {
		return "", err
	}
	if !explicit {
		return list, nil
	}
	items, err := s.nodeCatalog(ctx, u, false)
	if err != nil {
		return "", err
	}
	identities := map[string]bool{}
	for _, item := range items {
		if item.URI != "" && item.Selected && item.Authorized && item.State.Status != "unavailable" && item.State.Status != "disabled" {
			identities[subscription.InventoryIdentity(item.URI)] = true
		}
	}
	out := []string{}
	for _, uri := range strings.Split(list, "\n") {
		if identities[subscription.InventoryIdentity(uri)] {
			out = append(out, uri)
		}
	}
	if len(out) == 0 {
		return "", errors.New("subscription_no_authorized_nodes")
	}
	return strings.Join(out, "\n"), nil
}

// Explicit link selections intersect the current grant set on every download.
func (s *Server) catalogEntries(ctx context.Context, u *ent.User, l *ent.Link) ([]Entry, error) {
	items, err := s.nodeCatalog(ctx, u, false)
	if err != nil {
		return nil, err
	}
	out := []Entry{}
	selection := map[string]bool{}
	if !l.AllNodes {
		row, e := s.Store.Ent.Resource.Query().Where(resource.KindEQ("catalog-link"), resource.KeyEQ(strconv.Itoa(l.ID)), resource.OwnerIDEQ(u.ID)).Only(ctx)
		if ent.IsNotFound(e) {
			return out, nil
		}
		if e != nil {
			return nil, e
		}
		var refs []string
		b, _ := json.Marshal(row.Data["refs"])
		if e = json.Unmarshal(b, &refs); e != nil {
			return nil, e
		}
		for _, ref := range refs {
			selection[ref] = true
		}
	}
	for _, item := range items {
		if item.Kind != "source" && item.Kind != "manual" || !item.Authorized || !item.Selected || item.State.Status == "unavailable" || item.State.Status == "disabled" || !l.AllNodes && !selection[item.Ref] {
			continue
		}
		parsed, e := subscription.ParseLink(item.URI)
		if e != nil {
			return nil, e
		}
		out = append(out, Entry{Ref: item.Ref, Name: firstNonEmpty(item.Name, parsed.Remark), Protocol: parsed.Protocol, Type: "external:" + strings.ToLower(parsed.Protocol), Host: parsed.Address, Port: parsed.Port, URI: item.URI})
	}
	return out, nil
}
