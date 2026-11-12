package laya

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func dockerFixture() (DockerRuntime, dockerInspection) {
	s := DockerSpec{Name: "example-laya", Repository: "ghcr.io/ollaya-dev/ollaya", Digest: "sha256:" + strings.Repeat("a", 64), ModelVolume: "example-models", UID: 1000, GID: 1000, HostPort: 11435, MemoryBytes: 2 << 30, CPUMillis: 1000}
	d := DockerRuntime{Spec: s, Policy: DockerPolicy{Repositories: []string{s.Repository}, Digests: []string{s.Digest}, MaxMemoryBytes: 4 << 30, MaxCPUMillis: 2000}}
	var i dockerInspection
	i.Name = "/" + s.Name
	i.Config.Image = s.Repository + "@" + s.Digest
	i.Config.User = "1000:1000"
	i.Config.Labels = map[string]string{"laya.managed": "true"}
	i.State.Status = "created"
	i.HostConfig.ReadonlyRootfs = true
	i.HostConfig.CapDrop = []string{"ALL"}
	i.HostConfig.SecurityOpt = []string{"no-new-privileges=true"}
	i.HostConfig.Memory = int64(s.MemoryBytes)
	i.HostConfig.MemorySwap = int64(s.MemoryBytes)
	i.HostConfig.NanoCpus = 1_000_000_000
	i.HostConfig.PidsLimit = 128
	i.HostConfig.NetworkMode = "default"
	i.HostConfig.PortBindings = map[string][]struct{ HostIp, HostPort string }{"11435/tcp": {{"127.0.0.1", "11435"}}}
	i.Mounts = []struct {
		Type, Name, Destination string
		RW                      bool
	}{{"volume", s.ModelVolume, "/home/ollaya/.ollaya", true}}
	return d, i
}

func TestDockerRuntimeLifecycleValidatesBeforeMutation(t *testing.T) {
	d, inspection := dockerFixture()
	exists := false
	var operations []string
	d.Execute = func(ctx context.Context, args []string) ([]byte, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		operations = append(operations, args[0])
		switch args[0] {
		case "inspect":
			if !exists {
				return nil, ErrContainerMissing
			}
			return json.Marshal([]dockerInspection{inspection})
		case "create":
			exists = true
		case "start":
			inspection.State.Status = "running"
			inspection.State.Running = true
		case "stop":
			inspection.State.Status = "exited"
			inspection.State.Running = false
		case "rm":
			exists = false
		}
		return nil, nil
	}
	status, err := d.Ensure(t.Context())
	if err != nil || status.State != StateRunning || !reflect.DeepEqual(operations, []string{"inspect", "pull", "create", "inspect", "start", "inspect"}) {
		t.Fatalf("ensure: %+v %v %v", status, err, operations)
	}
	operations = nil
	if _, err = d.Ensure(t.Context()); err != nil || !reflect.DeepEqual(operations, []string{"inspect"}) {
		t.Fatalf("idempotent: %v %v", operations, err)
	}
	inspection.Config.Image = "unapproved:latest"
	operations = nil
	if err = d.Remove(t.Context()); err == nil || !reflect.DeepEqual(operations, []string{"inspect"}) {
		t.Fatalf("unapproved remove: %v %v", operations, err)
	}
	inspection.Config.Image = d.Spec.Repository + "@" + d.Spec.Digest
	if err = d.Remove(t.Context()); err != nil || exists {
		t.Fatalf("remove: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err = d.Ensure(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
}

func TestDockerRuntimeRejectsWeakenedActualConfiguration(t *testing.T) {
	for name, mutate := range map[string]func(*dockerInspection){
		"root":                 func(i *dockerInspection) { i.Config.User = "0:0" },
		"wrong_image":          func(i *dockerInspection) { i.Config.Image = "other@sha256:" + strings.Repeat("a", 64) },
		"writable_root":        func(i *dockerInspection) { i.HostConfig.ReadonlyRootfs = false },
		"cap_add":              func(i *dockerInspection) { i.HostConfig.CapAdd = []string{"SYS_ADMIN"} },
		"cap_drop":             func(i *dockerInspection) { i.HostConfig.CapDrop = nil },
		"privileged":           func(i *dockerInspection) { i.HostConfig.Privileged = true },
		"privilege_escalation": func(i *dockerInspection) { i.HostConfig.SecurityOpt = nil },
		"public_port":          func(i *dockerInspection) { i.HostConfig.PortBindings["11435/tcp"][0].HostIp = "0.0.0.0" },
		"extra_port":           func(i *dockerInspection) { i.HostConfig.PortBindings["80/tcp"] = nil },
		"host_network":         func(i *dockerInspection) { i.HostConfig.NetworkMode = "host" },
		"host_pid":             func(i *dockerInspection) { i.HostConfig.PidMode = "host" },
		"host_bind":            func(i *dockerInspection) { i.HostConfig.Binds = []string{"/:/host"} },
		"sensitive_mount":      func(i *dockerInspection) { i.Mounts[0].Destination = "/var/run/docker.sock" },
		"unlimited_memory":     func(i *dockerInspection) { i.HostConfig.Memory = 0 },
		"unlimited_cpu":        func(i *dockerInspection) { i.HostConfig.NanoCpus = 0 },
		"device_access":        func(i *dockerInspection) { i.HostConfig.DeviceRequests = []json.RawMessage{json.RawMessage(`{}`)} },
	} {
		t.Run(name, func(t *testing.T) {
			d, i := dockerFixture()
			mutate(&i)
			calls := 0
			d.Execute = func(_ context.Context, args []string) ([]byte, error) {
				calls++
				if args[0] != "inspect" {
					t.Fatal("mutated unapproved container")
				}
				return json.Marshal([]dockerInspection{i})
			}
			status, err := d.Ensure(t.Context())
			if err == nil || calls != 1 || status.State != StateUnknown || status.ErrorCode != "container_policy_mismatch" {
				t.Fatalf("status: %+v %v", status, err)
			}
		})
	}
}
