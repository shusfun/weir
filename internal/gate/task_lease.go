package gate

import (
	"context"
	"time"

	"github.com/shusfun/weir/packages/contracts"
)

// Queueing and downloads may outlive the initial lease check. Effects must
// revalidate against the center immediately before crossing the helper boundary.
func (a *Agent) checkExecutionLease(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	lease, ok := ctx.Value(taskLeaseKey{}).(taskLease)
	if !ok {
		return nil
	}
	check, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err := a.center.call(check, "POST", "/agent/tasks/"+lease.id+"/renew", contracts.TaskUpdate{Attempt: lease.attempt, LeaseToken: lease.token}, nil)
	if err != nil {
		return &contracts.TaskError{Code: "task_lease_lost", Origin: "gate.checkExecutionLease"}
	}
	return nil
}
