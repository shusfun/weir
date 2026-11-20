package taskqueue_test

import (
	"errors"
	"testing"

	"github.com/shusfun/weir/internal/taskqueue"
	"github.com/shusfun/weir/packages/contracts"
)

func TestInvalidCapabilityCacheCanBeReplaced(t *testing.T) {
	for _, raw := range []string{
		`{"protocol":2,"version":"test","arch":"amd64","tasks":{"subscription.convert":1}}`,
		`{"protocol":2,"version":"test","arch":"amd64","tasks":{"core.install":99}}`,
		`{"protocol":2`,
	} {
		t.Run(raw, func(t *testing.T) {
			q := queue(t)
			ctx := t.Context()
			if err := q.Ent.WorkerCapability.Create().SetID(1).SetHash("invalid").SetDocument(raw).Exec(ctx); err != nil {
				t.Fatal(err)
			}
			if c, err := q.Capabilities(ctx, 1); !errors.Is(err, taskqueue.ErrInvalidCapabilities) || c.Tasks != nil || c.Protocol != 0 {
				t.Fatalf("invalid cache returned trusted fields: %+v %v", c, err)
			}
			if _, err := q.WorkerInventory(ctx); !errors.Is(err, taskqueue.ErrInvalidCapabilities) {
				t.Fatalf("invalid inventory: %v", err)
			}
			if err := q.SetCapabilities(ctx, 1, capabilities(contracts.TaskCoreInstall)); err != nil {
				t.Fatal(err)
			}
			if c, err := q.Capabilities(ctx, 1); err != nil || c.Tasks[contracts.TaskCoreInstall] != contracts.TaskVersion {
				t.Fatalf("replacement: %+v %v", c, err)
			}
			if inventory, err := q.WorkerInventory(ctx); err != nil || len(inventory) != 1 {
				t.Fatalf("inventory after replacement: %+v %v", inventory, err)
			}
		})
	}
}

func TestCapabilityStorageFailureIsNotInvalidCache(t *testing.T) {
	s := openStore(t)
	q := s.Tasks
	if _, err := s.DB.ExecContext(t.Context(), `DROP TABLE worker_capabilities`); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Capabilities(t.Context(), 1); err == nil || errors.Is(err, taskqueue.ErrInvalidCapabilities) {
		t.Fatalf("storage failure misclassified: %v", err)
	}
}
