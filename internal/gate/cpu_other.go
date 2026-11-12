//go:build !linux

package gate

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/shusfun/weir/packages/contracts"
)

func isolateCPU(context.Context, contracts.WorkerBudget) error {
	return errors.New("cpu_cgroup_unavailable")
}

func runCPUChild(context.Context, Paths, contracts.WorkerBudget, contracts.Task) (json.RawMessage, error) {
	return nil, &contracts.TaskError{Code: "cpu_cgroup_unavailable", Origin: "gate.runCPUChild"}
}

func JoinCPUGroup() error { return errors.New("cpu_cgroup_unavailable") }
