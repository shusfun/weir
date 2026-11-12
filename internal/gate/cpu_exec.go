package gate

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"time"

	"github.com/shusfun/weir/packages/contracts"
)

func (a *Agent) runCPUTask(ctx context.Context, task contracts.Task) (json.RawMessage, error) {
	switch task.Kind {
	case contracts.TaskLayaClassify:
		return a.layaTask(ctx, task)
	case contracts.TaskExternalConvert:
		return a.networkTask(ctx, task)
	default:
		return nil, errors.New("not a cpu task")
	}
}

func RunCPUExec(taskPath, resultPath string, paths Paths) error {
	if err := JoinCPUGroup(); err != nil {
		return err
	}
	raw, err := os.ReadFile(taskPath)
	if err != nil {
		return err
	}
	var task contracts.Task
	if err = json.Unmarshal(raw, &task); err != nil {
		return err
	}
	agent, err := NewAgent(paths)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	out, err := agent.runCPUTask(ctx, task)
	if err != nil {
		return err
	}
	return os.WriteFile(resultPath, out, 0600)
}
