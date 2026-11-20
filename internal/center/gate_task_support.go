package center

import (
	"context"
	"errors"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/taskqueue"
	"github.com/shusfun/weir/packages/contracts"
)

func (s *Server) gateSupportsTask(ctx context.Context, g *ent.Gate, kind string) (bool, error) {
	caps, err := s.Store.Tasks.Capabilities(ctx, g.ID)
	if err == nil {
		return caps.Tasks[kind] == contracts.TaskSchemaVersion(kind), nil
	}
	if !errors.Is(err, taskqueue.ErrNotFound) {
		return false, err
	}
	return false, nil
}
