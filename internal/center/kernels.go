package center

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/shusfun/weir/internal/kernels"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/kerneldriver"
	"github.com/shusfun/weir/internal/store/ent/setting"
	"github.com/shusfun/weir/packages/contracts"
)

// dataDir 是中心的数据目录：内核缓存、版本档案覆盖等；未配置时放在配置文件同级的 data/ 下。
func (s *Server) dataDir() string {
	if s.Config.DataDir != "" {
		return s.Config.DataDir
	}
	return filepath.Join(filepath.Dir(s.Path), "data")
}

func (s *Server) kernelCache() kernels.Cache {
	return kernels.Cache{Dir: filepath.Join(s.dataDir(), "kernels")}
}

// drivers 合并内置驱动与保存的驱动（每个名称最新的一次保存）。保存驱动时清缓存。
func (s *Server) drivers(ctx context.Context) kernels.Drivers {
	s.driverCache.Lock()
	defer s.driverCache.Unlock()
	if s.driverCache.value != nil && time.Since(s.driverCache.at) < time.Minute {
		return *s.driverCache.value
	}
	d := kernels.Drivers{Saved: map[string]kernels.Saved{}}
	if s.Store != nil {
		rows, err := s.Store.Ent.KernelDriver.Query().Order(ent.Asc(kerneldriver.FieldRevision)).All(ctx)
		if err == nil {
			for _, r := range rows {
				d.Saved[r.Name] = kernels.Saved{Name: r.Name, Text: r.Text, Disabled: r.Disabled}
			}
		}
	}
	s.driverCache.value, s.driverCache.at = &d, time.Now()
	return d
}

func (s *Server) dropDriverCache() {
	s.driverCache.Lock()
	s.driverCache.value = nil
	s.driverCache.Unlock()
}

// defaultKernelVersions 是管理员设置的默认版本（设置键 kernel-defaults）；未设置时用缓存中最新的版本。
func (s *Server) defaultKernelVersion(ctx context.Context, kernel, arch string) string {
	if row, err := s.Store.Ent.Setting.Query().Where(setting.KeyEQ("kernel-defaults")).Only(ctx); err == nil {
		if v, ok := row.Value[kernel].(string); ok && v != "" {
			return v
		}
	}
	if list := s.kernelCache().Versions(kernel, arch); len(list) > 0 {
		return list[0]
	}
	if versions, err := s.kernelVersions(ctx, kernel); err == nil && len(versions) > 0 {
		return versions[0]
	}
	return ""
}

// gateKernels 为 Gate 选定 Xray / sing-box 版本（Gate 锁定的版本优先），并取对应驱动（已展开 extends 的全文）。
// New workers receive pinned archives; only legacy workers require a center
// binary cache. Missing release metadata keeps the last applied configuration.
func (s *Server) gateKernels(ctx context.Context, g *ent.Gate, need map[string]bool) (map[string]contracts.Kernel, map[string]string, error) {
	ks, profiles := map[string]contracts.Kernel{}, map[string]string{}
	direct, err := s.gateDownloadsKernels(ctx, g.ID)
	if err != nil {
		return nil, nil, err
	}
	for _, kernel := range []string{"xray", "sing-box", "mihomo"} {
		if !need[kernel] {
			continue
		}
		version := g.KernelPins[kernel]
		if version == "" {
			version = s.explicitDefault(ctx, kernel)
		}
		// 没有锁定也没有设置默认时，保持 Gate 正在运行的版本：下载新版本不会悄悄替换正在用的内核
		// （例如 Xray 26.10 的 REALITY 与 sing-box 1.14 客户端不兼容），换版本必须由管理员明确设置。
		if run := g.KernelsRunning[kernel]; version == "" && run != "" {
			if direct {
				version = run
			} else if _, _, err := s.kernelCache().Get(kernel, run, g.Arch); err == nil {
				version = run
			}
		}
		if version == "" {
			version = s.defaultKernelVersion(ctx, kernel, g.Arch)
		}
		if version == "" {
			// 这台 VPS 的架构还没有任何版本：用中心已有的最新版本（或默认版本）为它补下。
			if v := s.newestAnyArch(ctx, kernel); v != "" {
				s.autoKernelFetch(ctx, kernel, v)
				return nil, nil, kernelNotReady{Kernel: kernel, Detail: fmt.Sprintf("%s %s（%s）正在下载，完成后自动生效", kernel, v, g.Arch)}
			}
			s.autoKernelFetch(ctx, kernel, "")
			return nil, nil, kernelNotReady{Kernel: kernel, Detail: kernel + " 正在下载最新稳定版，完成后自动实测"}
		}
		ref := contracts.Kernel{Version: version}
		_, ref.SHA256, err = s.kernelCache().Get(kernel, version, g.Arch)
		if direct {
			artifact, artifactErr := s.kernelArtifact(ctx, kernel, version, g.Arch)
			if artifactErr == nil {
				ref.Artifact, err = &artifact, nil
			} else if !ent.IsNotFound(artifactErr) {
				return nil, nil, artifactErr
			}
		}
		if err != nil {
			s.autoKernelFetch(ctx, kernel, version)
			return nil, nil, kernelNotReady{Kernel: kernel, Detail: fmt.Sprintf("%s %s（%s）正在下载，完成后自动生效", kernel, version, g.Arch)}
		}
		_, text, err := s.drivers(ctx).Select(kernel, version)
		if err != nil {
			return nil, nil, err
		}
		ks[kernel], profiles[kernel] = ref, text
	}
	return ks, profiles, nil
}

// reveal 解开加密存储的出口密码；空值原样返回。
func (s *Server) reveal(sealed string) (string, error) {
	if sealed == "" {
		return "", nil
	}
	plain, err := s.Crypto.Decrypt(sealed)
	if err != nil {
		return "", errors.New("stored secret cannot be decrypted")
	}
	return plain, nil
}

// newestAnyArch includes approved manifests and offline binary imports.
func (s *Server) newestAnyArch(ctx context.Context, kernel string) string {
	versions, err := s.kernelVersions(ctx, kernel)
	if err != nil || len(versions) == 0 {
		return ""
	}
	return versions[0]
}

// explicitDefault 只返回管理员明确设置的默认版本（未设置为空）。
func (s *Server) explicitDefault(ctx context.Context, kernel string) string {
	if row, err := s.Store.Ent.Setting.Query().Where(setting.KeyEQ("kernel-defaults")).Only(ctx); err == nil {
		if v, ok := row.Value[kernel].(string); ok {
			return v
		}
	}
	return ""
}

type kernelNotReady struct{ Kernel, Detail string }

func (e kernelNotReady) Error() string { return e.Detail }
func missingMihomo(err error) bool {
	var unavailable kernelNotReady
	return errors.As(err, &unavailable) && unavailable.Kernel == "mihomo"
}
