package kernels

import (
	"bytes"
	"context"
	"crypto/sha256"
	"debug/elf"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/shusfun/weir/internal/fetch"
	"github.com/shusfun/weir/packages/contracts"
)

// ResolveArtifact reads only release metadata; it never downloads an archive.
func (f Fetcher) ResolveArtifact(ctx context.Context, kernel, version, arch string) (contracts.KernelArtifact, error) {
	var out contracts.KernelArtifact
	src, ok := sources[kernel]
	if !ok || !safeVersion.MatchString(version) || !safeArch.MatchString(arch) {
		return out, errors.New("invalid kernel reference")
	}
	release, err := f.gh().Tag(ctx, src.repo, src.tagPrefix+version)
	if err != nil {
		return out, err
	}
	if release.Tag != src.tagPrefix+version || release.Draft {
		return out, errors.New("kernel release identity mismatch")
	}
	asset, ok := release.Asset(src.asset(version, arch))
	if !ok || asset.SHA256() == "" {
		return out, fmt.Errorf("%s %s (%s) has no verified release asset", kernel, version, arch)
	}
	urls, err := f.gh().URLs(asset.URL)
	if err != nil {
		return out, err
	}
	out = contracts.KernelArtifact{Schema: contracts.KernelArtifactVersion, Kernel: kernel, Version: version, Arch: arch, URLs: urls, SHA256: asset.SHA256(), Size: asset.Size}
	return out, out.Validate(kernel, version, arch)
}

// ExtractArtifact verifies the pinned archive and a regular executable member.
// The binary stays in memory; archive paths are never written to the filesystem.
func ExtractArtifact(a contracts.KernelArtifact, archive []byte) ([]byte, error) {
	if err := a.Validate(a.Kernel, a.Version, a.Arch); err != nil {
		return nil, err
	}
	sum := sha256.Sum256(archive)
	if int64(len(archive)) != a.Size || hex.EncodeToString(sum[:]) != a.SHA256 {
		return nil, errors.New("kernel_archive_digest_mismatch")
	}
	binary, err := sources[a.Kernel].extract(archive, a.Version, a.Arch)
	if err != nil {
		return nil, err
	}
	if err := ValidateLinuxBinary(binary, a.Arch); err != nil {
		return nil, err
	}
	return binary, nil
}

// FetchArtifact consumes an already approved manifest without re-querying a
// mutable release between dispatch and download.
func (f Fetcher) FetchArtifact(ctx context.Context, artifact contracts.KernelArtifact) (string, error) {
	if err := artifact.Validate(artifact.Kernel, artifact.Version, artifact.Arch); err != nil {
		return "", err
	}
	src := sources[artifact.Kernel]
	var relay []fetch.Source
	if f.Relay != nil {
		host := f.DownloadHost
		if host == "" {
			host = "github.com"
		}
		canonical := "https://" + host + "/" + src.repo + "/releases/download/" + src.tagPrefix + artifact.Version + "/" + src.asset(artifact.Version, artifact.Arch)
		relay = f.Relay(canonical, artifact.SHA256, artifact.Size, maxDownload)
	}
	destination := filepath.Join(f.Cache.Dir, ".downloads", artifact.SHA256)
	if err := fetch.Download(ctx, fetch.Request{URLs: artifact.URLs, Sources: relay, Dest: destination, SHA256: artifact.SHA256, MaxSize: artifact.Size, HTTP: f.HTTP, OnProgress: f.Progress, OnStep: f.Step, Log: f.Log}); err != nil {
		return "", err
	}
	archive, err := os.ReadFile(destination)
	if err != nil {
		return "", err
	}
	binary, err := ExtractArtifact(artifact, archive)
	if err != nil {
		return "", err
	}
	sum, err := f.Cache.Put(artifact.Kernel, artifact.Version, artifact.Arch, binary)
	if err == nil {
		_ = os.Remove(destination)
	}
	return sum, err
}

func ValidateLinuxBinary(binary []byte, arch string) error {
	f, err := elf.NewFile(bytes.NewReader(binary))
	if err != nil {
		return errors.New("kernel_binary_not_elf")
	}
	defer f.Close()
	if f.Class != elf.ELFCLASS64 || f.Data != elf.ELFDATA2LSB || f.Type != elf.ET_EXEC && f.Type != elf.ET_DYN || arch == "amd64" && f.Machine != elf.EM_X86_64 || arch == "arm64" && f.Machine != elf.EM_AARCH64 || arch != "amd64" && arch != "arm64" || len(f.Progs) == 0 {
		return errors.New("kernel_binary_architecture_mismatch")
	}
	return nil
}
