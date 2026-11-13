//go:build linux

package layahost

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/shusfun/weir/internal/lockfile"
	"github.com/shusfun/weir/packages/laya"
)

// Prepare is an explicit root operation with fixed paths, image and resources.
// An existing approval is never silently replaced by a new image resolution.
func Prepare(ctx context.Context, target string, progress ...func(string)) (Deployment, error) {
	ctx, cancel := context.WithTimeout(ctx, 25*time.Minute)
	defer cancel()
	var empty Deployment
	stage := func(name string) {
		for _, report := range progress {
			report(name)
		}
	}
	stage("preflight")
	if os.Geteuid() != 0 {
		return empty, errors.New("root_required")
	}
	dir, name, port := "/opt/weir/center-laya", "weir-center-laya", 11435
	if target == "gate" {
		dir, name, port = "/opt/weir/laya", "weir-laya", 11436
	} else if target != "center" {
		return empty, errors.New("invalid_laya_target")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return empty, err
	}
	if err := trustedDirectoryPath(dir); err != nil {
		return empty, err
	}
	unlock, err := lockfile.TryAcquire(filepath.Join(dir, "prepare.lock"))
	if err != nil {
		return empty, err
	}
	defer unlock()
	if _, err := os.Lstat(filepath.Join(dir, "runtime.json")); err == nil {
		doc, err := ReadDeployment(dir)
		if err == nil {
			err = doc.Validate()
		}
		return doc, err
	} else if !os.IsNotExist(err) {
		return empty, err
	}
	if _, err := exec.LookPath("docker"); err != nil {
		stage("docker.install")
		if _, err := exec.LookPath("apt-get"); err == nil {
			if exec.CommandContext(ctx, "apt-get", "update").Run() != nil || exec.CommandContext(ctx, "apt-get", "install", "-y", "docker.io").Run() != nil {
				return empty, errors.New("laya_docker_install_failed")
			}
		} else {
			return empty, errors.New("laya_docker_required")
		}
	}
	if err := exec.CommandContext(ctx, "systemctl", "start", "docker").Run(); err != nil {
		return empty, errors.New("laya_docker_unavailable")
	}
	const repository = "ghcr.io/ollaya-dev/ollaya"
	image := repository + ":" + strings.TrimPrefix(laya.OllayaVersion, "v")
	stage("image.pull")
	if _, err := ExecuteDocker(ctx, []string{"pull", image}); err != nil {
		return empty, err
	}
	stage("image.verify")
	raw, err := ExecuteDocker(ctx, []string{"image", "inspect", "--format", "{{json .RepoDigests}}", image})
	if err != nil {
		return empty, err
	}
	var digests []string
	if json.Unmarshal(raw, &digests) != nil {
		return empty, errors.New("laya_image_digest_unknown")
	}
	digest := ""
	for _, ref := range digests {
		if strings.HasPrefix(ref, repository+"@sha256:") {
			digest = strings.TrimPrefix(ref, repository+"@")
			break
		}
	}
	doc := Deployment{Schema: 1, Version: laya.OllayaVersion, Model: laya.DefaultModel,
		Spec:   laya.DockerSpec{Name: name, Repository: repository, Digest: digest, ModelVolume: name + "-models", UID: 1000, GID: 1000, HostPort: port, MemoryBytes: 2 << 30, CPUMillis: 2000},
		Policy: laya.DockerPolicy{Repositories: []string{repository}, Digests: []string{digest}, MaxMemoryBytes: 2 << 30, MaxCPUMillis: 2000}}
	if err := doc.Validate(); err != nil {
		return empty, err
	}
	b, _ := json.MarshalIndent(doc, "", "  ")

	stage("approval.save")
	f, err := os.CreateTemp(dir, ".runtime-*")
	if err != nil {
		return empty, err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0644); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(f.Name(), filepath.Join(dir, "runtime.json"))
	}
	if err != nil {
		return empty, err
	}
	return doc, nil
}
