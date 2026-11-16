package center

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/link"
	"github.com/shusfun/weir/internal/store/ent/resource"
)

func linkSaveFailure(c *gin.Context, err error) {
	switch err.Error() {
	case "link_limit":
		failure(c, 409, "link_limit")
	case "node_not_allowed", "chain_not_allowed", "invalid_name", "invalid_link_window", "link_window_order", "link_window_exceeds_plan":
		failure(c, 400, err.Error())
	default:
		failInternal(c, 500, "save_failed", err)
	}
}

func (s *Server) linkCatalogRefs(ctx context.Context, uid, lid int) ([]string, error) {
	refs := []string{}
	row, err := s.Store.Ent.Resource.Query().Where(resource.KindEQ("catalog-link"), resource.KeyEQ(itoa(lid)), resource.OwnerIDEQ(uid)).Only(ctx)
	if ent.IsNotFound(err) {
		return refs, nil
	}
	if err != nil {
		return nil, err
	}
	b, err := json.Marshal(row.Data["refs"])
	if err == nil {
		err = json.Unmarshal(b, &refs)
	}
	return refs, err
}

// Link configuration and external selections commit together. The same lock
// serializes plan renewals so editing a window never restores a stale deadline.
func (s *Server) saveLink(ctx context.Context, uid, lid int, req linkRequest) (saved *ent.Link, err error) {
	finish := operationStage(ctx, "subscription.link.save", "user_id", uid, "link_id", lid)
	defer func() { finish(err) }()
	err = s.Store.Lock.Do(ctx, "cred-sync", func(ctx context.Context) error {
		tx, e := s.Store.Ent.Tx(ctx)
		if e != nil {
			return e
		}
		defer tx.Rollback()
		storeCopy := *s.Store
		storeCopy.Ent = tx.Client()
		service := &Server{Store: &storeCopy, Crypto: s.Crypto}
		u, e := tx.User.Get(ctx, uid)
		if e != nil {
			return e
		}
		if lid == 0 {
			n, e := tx.Link.Query().Where(link.UserIDEQ(uid)).Count(ctx)
			if e != nil {
				return e
			}
			if n >= maxLinks {
				return errors.New("link_limit")
			}
			create := tx.Link.Create().SetUserID(uid).SetToken(linkToken())
			if e = service.applyLink(ctx, u, nil, create.Mutation(), req); e != nil {
				return e
			}
			saved, e = create.Save(ctx)
		} else {
			cur, lookup := tx.Link.Query().Where(link.IDEQ(lid), link.UserIDEQ(uid)).Only(ctx)
			if lookup != nil {
				return lookup
			}
			upd := cur.Update()
			if e = service.applyLink(ctx, u, cur, upd.Mutation(), req); e != nil {
				return e
			}
			saved, e = upd.Save(ctx)
		}
		if e != nil {
			return e
		}
		if req.CatalogRefs != nil {
			refs, e := service.catalogLinkSelections(ctx, uid, *req.CatalogRefs)
			if e != nil {
				return e
			}
			if e = tx.Resource.Create().SetKind("catalog-link").SetKey(itoa(saved.ID)).SetOwnerID(uid).SetData(map[string]any{"refs": refs}).OnConflictColumns(resource.FieldKind, resource.FieldKey).UpdateNewValues().Exec(ctx); e != nil {
				return e
			}
		}
		if e = tx.Commit(); e != nil {
			return e
		}
		saved.Unwrap()
		return nil
	})
	return
}
