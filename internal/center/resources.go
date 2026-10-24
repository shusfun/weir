package center

import (
	"context"
	"errors"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/resource"
)

// errResourceConflict 表示调用方的版本校验未通过（由 update 回调返回）。
var errResourceConflict = errors.New("resource_conflict")

// updateResource 在同名锁内读取当前值、交给 fn 决定新值并写回（PostgreSQL 用 advisory lock，SQLite 用进程锁）。
// fn 收到 nil 表示记录不存在；返回错误时不写入。
func (s *Server) updateResource(ctx context.Context, kind, key string, fn func(old map[string]any) (map[string]any, error)) error {
	return s.Store.Lock.Do(ctx, "resource:"+kind+":"+key, func(ctx context.Context) error {
		row, err := s.Store.Ent.Resource.Query().Where(resource.KindEQ(kind), resource.KeyEQ(key)).Only(ctx)
		if err != nil && !ent.IsNotFound(err) {
			return err
		}
		var old map[string]any
		if row != nil {
			old = row.Data
		}
		next, err := fn(old)
		if err != nil {
			return err
		}
		if row != nil {
			return row.Update().SetData(next).Exec(ctx)
		}
		return s.Store.Ent.Resource.Create().SetKind(kind).SetKey(key).SetData(next).Exec(ctx)
	})
}
