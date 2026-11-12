package dsh

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Job runs the official native harness with a host-owned restricted profile.
// The host must validate Patch, isolate Paths.Dir and supply the runtime's
// identity and credentials outside the model conversation.
type Job struct {
	Paths         Paths
	Namespace     string
	Patch         []byte
	Compatibility Compatibility
	Timeout       time.Duration
}

func (job Job) Run(ctx context.Context, task string) (result Result, err error) {
	if err = ctx.Err(); err != nil {
		return Result{State: jobContextState(ctx)}, err
	}
	decision, err := CheckCompatibility(job.Compatibility)
	if err != nil {
		return Result{State: "failed"}, err
	}
	if !decision.EnableBackground {
		return Result{State: "unknown"}, errors.New("background tasks disabled by compatibility policy")
	}
	if !ValidNamespace(job.Namespace) || len(job.Namespace) > 39 || len(job.Patch) == 0 || len(job.Patch) > 1<<20 || len(task) == 0 || len(task) > 8000 {
		return Result{State: "failed"}, errors.New("invalid background job configuration")
	}
	if err = Supported(); err != nil {
		return Result{State: "unknown"}, err
	}
	timeout := job.Timeout
	if timeout == 0 {
		timeout = 5 * time.Minute
	}
	if timeout < time.Second || timeout > 30*time.Minute {
		return Result{State: "failed"}, errors.New("invalid background job timeout")
	}
	run, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	profile := job.Namespace + "-analyst"
	unlock, err := lockJobProfile(run, job.Paths.Dir, profile)
	if err != nil {
		return Result{State: jobContextState(run)}, err
	}
	defer unlock()
	defer func() {
		cleanupErr := os.Remove(filepath.Join(job.Paths.Home(), "profiles", profile, "cordis.patch.yml"))
		if cleanupErr != nil && !errors.Is(cleanupErr, os.ErrNotExist) && err == nil {
			result.State, result.Completed = "failed", false
			err = errors.New("background profile cleanup failed")
		}
	}()
	if err = EnsureProfile(run, job.Paths, profile, "headless", job.Patch); err != nil {
		return Result{State: "failed"}, err
	}
	return RunHeadless(run, job.Paths, profile, task)
}

func jobContextState(ctx context.Context) string {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return "cancelled"
	}
	return "failed"
}

// RecoverBackgroundProfiles removes ephemeral credentials from interrupted
// background profiles while keeping sessions, keys and other user state.
// Call it before accepting work after a supervisor restart.
func RecoverBackgroundProfiles(ctx context.Context, p Paths) error {
	if err := Supported(); err != nil {
		return err
	}
	root := filepath.Join(p.Home(), "profiles")
	profiles, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, profile := range profiles {
		name := profile.Name()
		if !profile.IsDir() || !ValidNamespace(name) || !strings.HasSuffix(name, "-analyst") {
			continue
		}
		unlock, err := lockJobProfile(ctx, p.Dir, name)
		if err != nil {
			return err
		}
		err = os.Remove(filepath.Join(root, name, "cordis.patch.yml"))
		unlock()
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return errors.New("background profile recovery failed")
		}
	}
	return nil
}
