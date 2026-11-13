package center

import (
	"context"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/predicate"
	"github.com/shusfun/weir/internal/store/ent/resource"
)

// Maintenance deliberately leaves excess work for the next tick so a backlog
// cannot monopolize SQLite's single writer or hold a large PostgreSQL transaction.
func (s *Server) pruneResources(ctx context.Context, predicates ...predicate.Resource) error {
	ids, err := s.Store.Ent.Resource.Query().Where(predicates...).Order(ent.Asc(resource.FieldID)).Limit(s.Config.Store.Maintenance.Effective().RecordBatch).IDs(ctx)
	if err != nil || len(ids) == 0 {
		return err
	}
	_, err = s.Store.Ent.Resource.Delete().Where(resource.IDIn(ids...), resource.And(predicates...)).Exec(ctx)
	return err
}
