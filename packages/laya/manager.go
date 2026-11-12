package laya

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type RuntimeState string

const (
	StateUnknown  RuntimeState = "unknown"
	StateStopped  RuntimeState = "stopped"
	StateStarting RuntimeState = "starting"
	StateRunning  RuntimeState = "running"
	StateFailed   RuntimeState = "failed"
)

type RuntimeStatus struct {
	State     RuntimeState
	Version   string
	Model     string
	Endpoint  string
	ErrorCode string
}

// RuntimeAdapter owns OS-specific service, identity and resource-limit operations.
type RuntimeAdapter interface {
	Ensure(context.Context, RuntimeConfig) error
	Remove(context.Context) error
	Status(context.Context) (RuntimeStatus, error)
}

type RuntimeConfig struct {
	Directory      string
	Identity       string
	Namespace      string
	Endpoint       string
	Model          string
	MaxMemoryBytes uint64
	MaxCPUSeconds  uint64
}

type DownloadRequest struct {
	URL, Destination, SHA256 string
	Size                     int64
}
type DownloadFunc func(context.Context, DownloadRequest) error

type Installer struct {
	Download  DownloadFunc
	Observer  Observer
	Resources func(context.Context) (Resources, error)
}

type Resources struct{ AvailableMemory, AvailableDisk uint64 }

// InstallModel verifies the entire set before publication. With a manifest,
// blobs are immutable and replacing the manifest is the atomic activation.
func (in Installer) InstallModel(ctx context.Context, dir string, model Model) (err error) {
	started := time.Now().UTC()
	defer func() { emit(in.Observer, ctx, "laya.install_model", started, err) }()
	if !filepath.IsAbs(dir) || len(model.Files) == 0 || len(model.Files) > 128 {
		return errors.New("invalid model destination or manifest")
	}
	var requiredDisk uint64
	names := map[string]bool{}
	for _, artifact := range model.Files {
		digest, e := hex.DecodeString(artifact.SHA256)
		if !safeModelPath(artifact.Name) || names[artifact.Name] || artifact.Size <= 0 || e != nil || len(digest) != sha256.Size || artifact.URL == "" {
			return errors.New("invalid model artifact")
		}
		names[artifact.Name] = true
		if uint64(artifact.Size) > ^uint64(0)-requiredDisk {
			return errors.New("model size overflow")
		}
		requiredDisk += uint64(artifact.Size)
	}
	if len(model.Manifest) > 0 && (!safeModelPath(model.ManifestPath) || names[model.ManifestPath]) {
		return errors.New("invalid manifest destination")
	}
	if model.MinimumMemory < 0 {
		return errors.New("invalid model memory requirement")
	}
	if err := validateModelManifest(model); err != nil {
		return err
	}
	if in.Resources != nil {
		resources, e := in.Resources(ctx)
		if e != nil {
			return e
		}
		if resources.AvailableMemory < uint64(model.MinimumMemory) || resources.AvailableDisk < requiredDisk {
			return errors.New("insufficient model resources")
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	stage, err := os.MkdirTemp(dir, ".laya-stage-")
	if err != nil {
		return err
	}
	stageName := filepath.Base(stage)
	defer root.RemoveAll(stageName)
	download := in.Download
	if download == nil {
		download = Download
	}
	for _, artifact := range model.Files {
		if err := ctx.Err(); err != nil {
			return err
		}
		tmpName := filepath.Join(stage, artifact.Name)
		if err := root.MkdirAll(filepath.Join(stageName, filepath.Dir(artifact.Name)), 0o700); err != nil {
			return err
		}
		if err = download(ctx, DownloadRequest{URL: artifact.URL, Destination: tmpName, SHA256: artifact.SHA256, Size: artifact.Size}); err != nil {
			_ = os.Remove(tmpName)
			return err
		}
		info, e := root.Lstat(filepath.Join(stageName, artifact.Name))
		if e != nil {
			return e
		}
		if !info.Mode().IsRegular() {
			return errors.New("artifact is not a regular file")
		}
		f, err := os.Open(tmpName)
		if err != nil {
			return err
		}
		h := sha256.New()
		n, copyErr := io.Copy(h, io.LimitReader(f, artifact.Size+1))
		closeErr := f.Close()
		if copyErr != nil || closeErr != nil || n != artifact.Size || !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), artifact.SHA256) {
			_ = os.Remove(tmpName)
			return fmt.Errorf("artifact verification failed: %s", filepath.Base(artifact.Name))
		}
	}
	// Verify the entire staged set before making any file visible. The
	// manifest is the activation point and is replaced only after all blobs.
	for _, artifact := range model.Files {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := root.MkdirAll(filepath.Dir(artifact.Name), 0700); err != nil {
			return err
		}
		if err := root.Rename(filepath.Join(stageName, artifact.Name), artifact.Name); err != nil {
			return err
		}
	}
	if len(model.Manifest) != 0 {
		if err := root.MkdirAll(filepath.Dir(model.ManifestPath), 0o700); err != nil {
			return err
		}
		stagedManifest := filepath.Join(stageName, ".manifest")
		if err := root.WriteFile(stagedManifest, model.Manifest, 0600); err != nil {
			return err
		}
		if err := root.Rename(stagedManifest, model.ManifestPath); err != nil {
			return err
		}
	}
	return nil
}

func safeModelPath(name string) bool {
	return name != "." && filepath.IsLocal(name) && !strings.ContainsAny(name, "\\:") && filepath.ToSlash(filepath.Clean(name)) == name
}

type Manager struct {
	Config        RuntimeConfig
	Runtime       RuntimeAdapter
	Observer      Observer
	Compatibility *Compatibility
}

func (m Manager) Ensure(ctx context.Context) (err error) {
	started := time.Now().UTC()
	defer func() { emit(m.Observer, ctx, "laya.ensure", started, err) }()
	if m.Runtime == nil || m.Config.Directory == "" || m.Config.Identity == "" || m.Config.Namespace == "" || m.Config.Model == "" {
		return errors.New("incomplete laya runtime configuration")
	}
	if m.Config.MaxMemoryBytes == 0 || m.Config.MaxCPUSeconds == 0 {
		return errors.New("resource limits are required")
	}
	if m.Compatibility != nil {
		decision, err := CheckCompatibility(*m.Compatibility)
		if err != nil {
			return err
		}
		if !decision.EnableBase {
			return errors.New("laya runtime is outside its verified compatibility range")
		}
	}
	if err = m.Runtime.Ensure(ctx, m.Config); err != nil {
		return err
	}
	return nil
}

func (m Manager) Remove(ctx context.Context) (err error) {
	started := time.Now().UTC()
	defer func() { emit(m.Observer, ctx, "laya.remove", started, err) }()
	if m.Runtime == nil {
		return errors.New("runtime adapter is required")
	}
	return m.Runtime.Remove(ctx)
}

func (m Manager) Status(ctx context.Context) (RuntimeStatus, error) {
	if m.Runtime == nil {
		return RuntimeStatus{State: StateUnknown}, errors.New("runtime adapter is required")
	}
	return m.Runtime.Status(ctx)
}

func (m Manager) Classify(ctx context.Context, client Client, req Request) (Response, error) {
	return client.Classify(ctx, req)
}

func emit(observer Observer, ctx context.Context, operation string, started time.Time, err error) {
	if observer == nil {
		return
	}
	done := time.Now().UTC()
	status, code := "succeeded", ""
	if err != nil {
		status, code = "failed", "operation_failed"
		var detail *APIError
		if errors.As(err, &detail) {
			code = detail.Code
		}
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		status, code = "cancelled", "context_cancelled"
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		status, code = "timeout", "deadline_exceeded"
	}
	observer(ctx, Event{Operation: operation, TraceID: traceID(ctx), Started: started, Finished: done, Status: status, Code: code})
}
