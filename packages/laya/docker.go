package laya

import (
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// DockerSpec describes one instance. It contains no arbitrary arguments, host
// mounts, commands, environment variables or socket settings.
type DockerSpec struct {
	Name        string `json:"name"`
	Repository  string `json:"repository"`
	Digest      string `json:"digest"`
	ModelVolume string `json:"model_volume"`
	UID         uint32 `json:"uid"`
	GID         uint32 `json:"gid"`
	HostPort    int    `json:"host_port"`
	MemoryBytes uint64 `json:"memory_bytes"`
	CPUMillis   int    `json:"cpu_millis"`
}

type DockerPolicy struct {
	Repositories   []string
	Digests        []string
	MaxMemoryBytes uint64
	MaxCPUMillis   int
}

var containerName = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,62}$`)
var repositoryName = regexp.MustCompile(`^[a-z0-9]+([.-][a-z0-9]+)*(:[0-9]+)?(/[a-z0-9]+([._-][a-z0-9]+)*)+$`)

func (s DockerSpec) Validate(p DockerPolicy) error {
	if s.UID == 0 || s.GID == 0 {
		return errors.New("container requires a non-root identity")
	}
	if !containerName.MatchString(s.Name) || !containerName.MatchString(s.ModelVolume) || !repositoryName.MatchString(s.Repository) || !slices.Contains(p.Repositories, s.Repository) {
		return errors.New("container identity or repository is not allowed")
	}
	digest, err := hex.DecodeString(strings.TrimPrefix(s.Digest, "sha256:"))
	if err != nil || len(digest) != 32 || !strings.HasPrefix(s.Digest, "sha256:") || strings.ToLower(s.Digest) != s.Digest || !slices.Contains(p.Digests, s.Digest) {
		return errors.New("container image digest is not allowed")
	}
	if s.HostPort < 1024 || s.HostPort > 65535 || s.MemoryBytes < 64<<20 || p.MaxMemoryBytes == 0 || s.MemoryBytes > p.MaxMemoryBytes || s.CPUMillis < 100 || p.MaxCPUMillis < 100 || s.CPUMillis > p.MaxCPUMillis {
		return errors.New("container resource limits are invalid")
	}
	return nil
}

// DockerArguments produces only fixed operations. The host executes these in
// its privileged adapter; application processes never need daemon access.
func DockerArguments(operation string, s DockerSpec, p DockerPolicy) ([]string, error) {
	if err := s.Validate(p); err != nil {
		return nil, err
	}
	image := s.Repository + "@" + s.Digest
	switch operation {
	case "pull":
		return []string{"pull", image}, nil
	case "create":
		return []string{"create", "--name", s.Name, "--label", "laya.managed=true", "--pull=never", "--user", fmt.Sprintf("%d:%d", s.UID, s.GID), "--read-only", "--cap-drop=ALL", "--security-opt", "no-new-privileges=true", "--pids-limit", "128", "--memory", strconv.FormatUint(s.MemoryBytes, 10), "--memory-swap", strconv.FormatUint(s.MemoryBytes, 10), "--cpus", fmt.Sprintf("%.3f", float64(s.CPUMillis)/1000), "--publish", fmt.Sprintf("127.0.0.1:%d:11435/tcp", s.HostPort), "--mount", "type=volume,source=" + s.ModelVolume + ",target=/home/ollaya/.ollaya", "--env", "OLLAYA_HOST=0.0.0.0:11435", "--env", "OLLAYA_MODELS=/home/ollaya/.ollaya/models", image, "serve"}, nil
	case "start", "stop":
		return []string{operation, s.Name}, nil
	case "remove":
		return []string{"rm", s.Name}, nil
	case "status":
		return []string{"inspect", "--type=container", s.Name}, nil
	default:
		return nil, errors.New("unsupported container operation")
	}
}
