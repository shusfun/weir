package laya

import (
	"strings"
	"testing"
)

func TestDockerPolicyRequiresPinnedImageAndRestrictedExecution(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	s := DockerSpec{Name: "example-laya", Repository: "ghcr.io/ollaya-dev/ollaya", Digest: digest, ModelVolume: "example-laya-models", UID: 1000, GID: 1000, HostPort: 11435, MemoryBytes: 2 << 30, CPUMillis: 1000}
	p := DockerPolicy{Repositories: []string{s.Repository}, Digests: []string{digest}, MaxMemoryBytes: 4 << 30, MaxCPUMillis: 2000}
	args, err := DockerArguments("create", s, p)
	if err != nil {
		t.Fatal(err)
	}
	custom := s
	custom.UID, custom.GID = 2100, 2200
	customArgs, err := DockerArguments("create", custom, p)
	if err != nil || !strings.Contains(strings.Join(customArgs, " "), "--user 2100:2200") {
		t.Fatalf("caller identity ignored: %v %v", customArgs, err)
	}
	custom.UID = 0
	if _, err := DockerArguments("create", custom, p); err == nil {
		t.Fatal("root accepted")
	}
	joined := strings.Join(args, " ")
	for _, required := range []string{"--user 1000:1000", "--read-only", "--cap-drop=ALL", "--security-opt no-new-privileges=true", "--publish 127.0.0.1:11435:11435/tcp", "--memory 2147483648", "--cpus 1.000", "--pull=never", s.Repository + "@" + digest} {
		if !strings.Contains(joined, required) {
			t.Fatalf("missing %q: %v", required, args)
		}
	}
	for _, op := range []string{"exec", "run", "build", "volume rm"} {
		if _, err := DockerArguments(op, s, p); err == nil {
			t.Fatalf("accepted %s", op)
		}
	}
	for _, change := range []func(*DockerSpec){func(s *DockerSpec) { s.Repository = "evil.example/image" }, func(s *DockerSpec) { s.Digest = "latest" }, func(s *DockerSpec) { s.Digest = "sha256:" + strings.Repeat("b", 64) }, func(s *DockerSpec) { s.ModelVolume = "/var/run/docker.sock" }, func(s *DockerSpec) { s.Name = "--privileged" }, func(s *DockerSpec) { s.MemoryBytes = 8 << 30 }} {
		bad := s
		change(&bad)
		if _, err := DockerArguments("create", bad, p); err == nil {
			t.Fatalf("accepted %+v", bad)
		}
	}
}
