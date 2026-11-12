package laya

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

var ErrContainerMissing = errors.New("managed container does not exist")

// DockerExecute belongs in a privileged host adapter. It must bound stdout and
// normalize daemon errors; raw stderr can contain host configuration.
type DockerExecute func(context.Context, []string) ([]byte, error)

type DockerRuntime struct {
	Spec     DockerSpec
	Policy   DockerPolicy
	Execute  DockerExecute
	Observer Observer
}

type DockerStatus struct {
	State     RuntimeState `json:"state"`
	Endpoint  string       `json:"endpoint,omitempty"`
	Image     string       `json:"image,omitempty"`
	ErrorCode string       `json:"error_code,omitempty"`
}

type dockerInspection struct {
	Name   string
	Config struct {
		Image  string
		User   string
		Labels map[string]string
	}
	State struct {
		Status   string
		Running  bool
		ExitCode int
	}
	HostConfig struct {
		ReadonlyRootfs                          bool
		Privileged                              bool
		CapAdd, CapDrop, SecurityOpt            []string
		Binds, VolumesFrom                      []string
		Devices                                 []json.RawMessage
		DeviceRequests                          []json.RawMessage
		NetworkMode, PidMode, IpcMode, Runtime  string
		Memory, MemorySwap, NanoCpus, PidsLimit int64
		PortBindings                            map[string][]struct{ HostIp, HostPort string }
	}
	Mounts []struct {
		Type, Name, Destination string
		RW                      bool
	}
}

func (d DockerRuntime) command(ctx context.Context, operation string) ([]byte, error) {
	if d.Execute == nil {
		return nil, errors.New("privileged Docker adapter is required")
	}
	args, err := DockerArguments(operation, d.Spec, d.Policy)
	if err != nil {
		return nil, err
	}
	return d.Execute(ctx, args)
}

// Status validates the actual container before exposing its state. A container
// with the same name but a different image or weaker isolation is never adopted.
func (d DockerRuntime) Status(ctx context.Context) (status DockerStatus, err error) {
	started := time.Now().UTC()
	defer func() { emit(d.Observer, ctx, "laya.docker.status", started, err) }()
	status.State = StateUnknown
	b, err := d.command(ctx, "status")
	if err != nil {
		status.ErrorCode = "container_unavailable"
		return status, err
	}
	if len(b) > 1<<20 {
		return status, errors.New("container status exceeds limit")
	}
	var rows []dockerInspection
	if json.Unmarshal(b, &rows) != nil || len(rows) != 1 {
		return status, errors.New("invalid container status")
	}
	if err = d.validateInspection(rows[0]); err != nil {
		status.ErrorCode = "container_policy_mismatch"
		return status, err
	}
	status.Image = rows[0].Config.Image
	status.Endpoint = fmt.Sprintf("http://127.0.0.1:%d", d.Spec.HostPort)
	switch rows[0].State.Status {
	case "running":
		if rows[0].State.Running {
			status.State = StateRunning
		}
	case "created", "exited":
		status.State = StateStopped
		if rows[0].State.ExitCode != 0 {
			status.State, status.ErrorCode = StateFailed, "container_exited"
		}
	case "restarting":
		status.State = StateStarting
	case "dead":
		status.State, status.ErrorCode = StateFailed, "container_dead"
	}
	return status, nil
}

func (d DockerRuntime) validateInspection(i dockerInspection) error {
	s := d.Spec
	h := i.HostConfig
	if strings.TrimPrefix(i.Name, "/") != s.Name || i.Config.Image != s.Repository+"@"+s.Digest || i.Config.User != fmt.Sprintf("%d:%d", s.UID, s.GID) || i.Config.Labels["laya.managed"] != "true" || !h.ReadonlyRootfs || h.Privileged || len(h.CapAdd) != 0 || !slices.Contains(h.CapDrop, "ALL") || !slices.Contains(h.SecurityOpt, "no-new-privileges=true") || len(h.Binds) != 0 || len(h.VolumesFrom) != 0 || len(h.Devices) != 0 || len(h.DeviceRequests) != 0 || h.Memory != int64(s.MemoryBytes) || h.MemorySwap != int64(s.MemoryBytes) || h.NanoCpus != int64(s.CPUMillis)*1_000_000 || h.PidsLimit != 128 {
		return errors.New("container isolation does not match approved policy")
	}
	if h.PidMode != "" || (h.IpcMode != "" && h.IpcMode != "private") || (h.NetworkMode != "bridge" && h.NetworkMode != "default") || (h.Runtime != "" && h.Runtime != "runc") {
		return errors.New("container namespace does not match approved policy")
	}
	if len(i.Mounts) != 1 || i.Mounts[0].Type != "volume" || i.Mounts[0].Name != s.ModelVolume || i.Mounts[0].Destination != "/home/ollaya/.ollaya" || !i.Mounts[0].RW {
		return errors.New("container mounts do not match approved policy")
	}
	bindings := h.PortBindings["11435/tcp"]
	if len(h.PortBindings) != 1 || len(bindings) != 1 || bindings[0].HostIp != "127.0.0.1" || bindings[0].HostPort != strconv.Itoa(s.HostPort) {
		return errors.New("container must only publish the approved loopback port")
	}
	return nil
}

// Ensure creates a missing container from the pinned image or starts an
// existing approved one. It never replaces an unrelated or changed container.
func (d DockerRuntime) Ensure(ctx context.Context) (status DockerStatus, err error) {
	started := time.Now().UTC()
	defer func() { emit(d.Observer, ctx, "laya.docker.ensure", started, err) }()
	status, err = d.Status(ctx)
	if errors.Is(err, ErrContainerMissing) {
		if _, err = d.command(ctx, "pull"); err != nil {
			return status, err
		}
		if _, err = d.command(ctx, "create"); err != nil {
			return status, err
		}
		status, err = d.Status(ctx)
	}
	if err != nil || status.State == StateRunning {
		return status, err
	}
	if status.State != StateStopped && status.State != StateFailed {
		return status, errors.New("container is not ready to start")
	}
	if _, err = d.command(ctx, "start"); err != nil {
		return status, err
	}
	status, err = d.Status(ctx)
	if err == nil && status.State != StateRunning && status.State != StateStarting {
		err = errors.New("container did not enter a running state")
	}
	return status, err
}

func (d DockerRuntime) Stop(ctx context.Context) (err error) {
	started := time.Now().UTC()
	defer func() { emit(d.Observer, ctx, "laya.docker.stop", started, err) }()
	status, err := d.Status(ctx)
	if errors.Is(err, ErrContainerMissing) {
		return nil
	}
	if err != nil {
		return err
	}
	if status.State == StateRunning || status.State == StateStarting {
		_, err = d.command(ctx, "stop")
	}
	return err
}

// Remove preserves the model volume so a later approved deployment can reuse it.
func (d DockerRuntime) Remove(ctx context.Context) (err error) {
	started := time.Now().UTC()
	defer func() { emit(d.Observer, ctx, "laya.docker.remove", started, err) }()
	if err = d.Stop(ctx); err != nil {
		return err
	}
	if _, err = d.Status(ctx); errors.Is(err, ErrContainerMissing) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = d.command(ctx, "remove")
	return err
}
