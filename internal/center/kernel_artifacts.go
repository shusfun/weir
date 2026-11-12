package center

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"runtime"
	"sort"

	"github.com/shusfun/weir/internal/kernels"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/gate"
	"github.com/shusfun/weir/internal/store/ent/resource"
	"github.com/shusfun/weir/packages/contracts"
)

const kernelArtifactKind = "kernel-artifact"

func centerNeedsKernel(kernel, arch string) bool {
	return (kernel == "mihomo" || kernel == "sing-box") && runtime.GOOS == "linux" && runtime.GOARCH == arch
}

func kernelArtifactKey(kernel, version, arch string) string {
	return kernel + ":" + version + ":" + arch
}

func (s *Server) kernelArtifact(ctx context.Context, kernel, version, arch string) (contracts.KernelArtifact, error) {
	var artifact contracts.KernelArtifact
	row, err := s.Store.Ent.Resource.Query().Where(resource.KindEQ(kernelArtifactKind), resource.KeyEQ(kernelArtifactKey(kernel, version, arch))).Only(ctx)
	if err != nil {
		return artifact, err
	}
	if err = decodeStrict(row.Data, &artifact); err == nil {
		err = artifact.Validate(kernel, version, arch)
	}
	return artifact, err
}

func (s *Server) saveKernelArtifact(ctx context.Context, artifact contracts.KernelArtifact) error {
	if err := artifact.Validate(artifact.Kernel, artifact.Version, artifact.Arch); err != nil {
		return err
	}
	key := kernelArtifactKey(artifact.Kernel, artifact.Version, artifact.Arch)
	return s.Store.Lock.Do(ctx, "kernel-artifact:"+key, func(ctx context.Context) error {
		previous, err := s.kernelArtifact(ctx, artifact.Kernel, artifact.Version, artifact.Arch)
		if err == nil && (previous.SHA256 != artifact.SHA256 || previous.Size != artifact.Size) {
			return errors.New("kernel_release_digest_changed")
		}
		if err != nil && !ent.IsNotFound(err) {
			return err
		}
		return s.Store.Ent.Resource.Create().SetKind(kernelArtifactKind).SetKey(key).SetData(toMap(artifact)).OnConflictColumns(resource.FieldKind, resource.FieldKey).UpdateNewValues().Exec(ctx)
	})
}

func (s *Server) kernelVersions(ctx context.Context, kernel string) ([]string, error) {
	set := map[string]bool{}
	for _, arch := range []string{"amd64", "arm64"} {
		for _, version := range s.kernelCache().Versions(kernel, arch) {
			set[version] = true
		}
	}
	rows, err := s.Store.Ent.Resource.Query().Where(resource.KindEQ(kernelArtifactKind), resource.KeyHasPrefix(kernel+":")).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		var artifact contracts.KernelArtifact
		if err := decodeStrict(row.Data, &artifact); err != nil || artifact.Validate(kernel, artifact.Version, artifact.Arch) != nil || row.Key != kernelArtifactKey(kernel, artifact.Version, artifact.Arch) {
			return nil, errors.New("kernel_artifact_invalid")
		}
		set[artifact.Version] = true
	}
	out := make([]string, 0, len(set))
	for version := range set {
		out = append(out, version)
	}
	sort.Slice(out, func(i, j int) bool { return kernels.CompareVersions(out[i], out[j]) > 0 })
	return out, nil
}

func (s *Server) gateDownloadsKernels(ctx context.Context, id int) (bool, error) {
	caps, err := s.Store.Tasks.Capabilities(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil && caps.Tasks[contracts.TaskCoreInstall] == contracts.TaskVersion, err
}

func (s *Server) kernelBinaryRequired(ctx context.Context, kernel, arch string) (bool, error) {
	if centerNeedsKernel(kernel, arch) {
		return true, nil
	}
	gates, err := s.Store.Ent.Gate.Query().Where(gate.StatusEQ(gate.StatusActive)).All(ctx)
	if err != nil {
		return false, err
	}
	for _, g := range gates {
		if g.Arch != arch {
			continue
		}
		capable, err := s.gateDownloadsKernels(ctx, g.ID)
		if err != nil {
			return false, err
		}
		if !capable {
			return true, nil
		}
	}
	return false, nil
}

func (s *Server) kernelPrepared(ctx context.Context, kernel, version, arch string) error {
	if _, _, err := s.kernelCache().Get(kernel, version, arch); err == nil {
		return nil
	}
	required, err := s.kernelBinaryRequired(ctx, kernel, arch)
	if err != nil {
		return err
	}
	if required {
		return fmt.Errorf("%s %s (%s) binary is not cached", kernel, version, arch)
	}
	_, err = s.kernelArtifact(ctx, kernel, version, arch)
	return err
}
