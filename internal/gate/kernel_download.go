package gate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/shusfun/weir/internal/fetch"
	"github.com/shusfun/weir/internal/kernels"
	"github.com/shusfun/weir/packages/contracts"
)

type kernelReceipt struct {
	ArchiveSHA256 string `json:"archive_sha256"`
	BinarySHA256  string `json:"binary_sha256"`
}

func (a *Agent) kernelReceiptPath(name, version string) string {
	return filepath.Join(a.P.Data, "kernel-receipts", name+"-"+version+"-"+runtime.GOARCH+".json")
}

func (a *Agent) installedKernelMatches(name string, k contracts.Kernel) bool {
	data, err := readBoundedKernel(filepath.Join(a.P.KernelsDir, name+"-"+k.Version))
	if err != nil {
		return false
	}
	if k.Artifact == nil {
		return k.SHA256 != "" && sumOf(data) == k.SHA256
	}
	var receipt kernelReceipt
	if err = readJournal(a.kernelReceiptPath(name, k.Version), &receipt); err != nil {
		return false
	}
	return receipt.ArchiveSHA256 == k.Artifact.SHA256 && receipt.BinarySHA256 == sumOf(data) && (k.SHA256 == "" || receipt.BinarySHA256 == k.SHA256) && kernels.ValidateLinuxBinary(data, runtime.GOARCH) == nil
}

func readBoundedKernel(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Size() < 1 || st.Size() > contracts.MaxKernelArchiveBytes {
		return nil, errors.New("invalid_kernel_file")
	}
	body, err := io.ReadAll(io.LimitReader(f, contracts.MaxKernelArchiveBytes+1))
	if err != nil || len(body) > int(contracts.MaxKernelArchiveBytes) {
		return nil, errors.New("invalid_kernel_file")
	}
	return body, nil
}

func (a *Agent) downloadKernel(ctx context.Context, name string, k contracts.Kernel) (data []byte, err error) {
	finish := beginTaskStage(ctx, "kernel.download", "gate.downloadKernel")
	defer func() { finish(err) }()
	if k.Artifact == nil {
		if len(k.SHA256) != 64 {
			return nil, errors.New("kernel_digest_missing")
		}
		data, _, err = a.center.download(ctx, "/agent/kernels/"+name+"/"+k.Version, "X-Kernel-SHA256")
		if err == nil && sumOf(data) != k.SHA256 {
			err = errors.New("kernel_binary_digest_mismatch")
		}
		return data, err
	}
	artifact := *k.Artifact
	if err = artifact.Validate(name, k.Version, runtime.GOARCH); err != nil {
		return nil, err
	}
	urls, err := a.githubURLs(artifact.URLs)
	if err != nil {
		return nil, errors.New("kernel_artifact_source_invalid")
	}
	dir := filepath.Join(a.P.Data, "kernel-downloads")
	destination := filepath.Join(dir, artifact.SHA256)
	defer os.Remove(destination)
	// The only fallback is an authenticated center endpoint with the same
	// archive digest. The center cannot substitute an unrelated binary.
	fallback := fetch.Source{Name: "center-fallback", Unmetered: true, Fetch: func(ctx context.Context, dest string, _ fetch.Reporter) (err error) {
		finish := beginTaskStage(ctx, "kernel.download.center_fallback", "gate.downloadKernel")
		defer func() { finish(err) }()
		staged := destination + ".center"
		if err := a.center.downloadFile(ctx, "/agent/kernel-assets/"+name+"/"+k.Version, artifact.SHA256, artifact.Size, staged); err != nil {
			return err
		}
		return os.Rename(staged, dest)
	}}
	err = fetch.Download(ctx, fetch.Request{URLs: urls, Sources: []fetch.Source{fallback}, Dest: destination, SHA256: artifact.SHA256, MaxSize: artifact.Size, HTTP: a.externalHTTP()})
	if err != nil {
		return nil, fmt.Errorf("kernel_download_failed: %w", err)
	}
	archive, err := os.ReadFile(destination)
	if err != nil {
		return nil, err
	}
	verified := beginTaskStage(ctx, "kernel.verify", "kernels.ExtractArtifact")
	data, err = kernels.ExtractArtifact(artifact, archive)
	verified(err)
	if err == nil && k.SHA256 != "" && sumOf(data) != k.SHA256 {
		return nil, errors.New("kernel_binary_digest_mismatch")
	}
	return data, err
}

func (a *Agent) installKernelTask(ctx context.Context, req contracts.KernelInstallTask) (map[string]any, error) {
	if req.Arch != runtime.GOARCH || req.Artifact.Validate(req.Kernel, req.Version, req.Arch) != nil {
		return nil, errors.New("kernel_artifact_invalid")
	}
	k := contracts.Kernel{Version: req.Version, Artifact: &req.Artifact}
	if err := a.ensureKernels(ctx, contracts.GateConfig{Kernels: map[string]contracts.Kernel{req.Kernel: k}}); err != nil {
		return nil, err
	}
	var receipt kernelReceipt
	if err := readJournal(a.kernelReceiptPath(req.Kernel, req.Version), &receipt); err != nil {
		return nil, err
	}
	return map[string]any{"kernel": req.Kernel, "version": req.Version, "arch": req.Arch, "archive_sha256": req.Artifact.SHA256, "binary_sha256": receipt.BinarySHA256}, nil
}

func cleanupAbandonedAssetDownloads(dir string) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := strings.TrimSuffix(entry.Name(), ".part")
		if !entry.IsDir() && uploadID.MatchString(name) {
			if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}
