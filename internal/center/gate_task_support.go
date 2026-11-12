package center

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/packages/contracts"
)

// Only workers without a capability document use the rollout-era heartbeat.
// An explicit document is authoritative, including a missing task entry.
func (s *Server) gateSupportsTask(ctx context.Context, g *ent.Gate, kind string, legacyMinimum int) (bool, error) {
	caps, err := s.Store.Tasks.Capabilities(ctx, g.ID)
	if err == nil {
		return caps.Tasks[kind] == contracts.TaskVersion, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	version, err := strconv.Atoi(fmt.Sprint(g.Host["node_test_version"]))
	return err == nil && legacyMinimum > 0 && version >= legacyMinimum, nil
}
