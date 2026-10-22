package center

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/imzyb/MiGate/internal/kernels"
	"github.com/imzyb/MiGate/internal/store/ent"
	"github.com/imzyb/MiGate/internal/store/ent/setting"
	"github.com/imzyb/MiGate/packages/contracts"
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

func (s *Server) profiles() kernels.Profiles {
	return kernels.Profiles{OverrideDir: filepath.Join(s.dataDir(), "profiles")}
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
	return ""
}

// gateKernels 为 Gate 选定 Xray / sing-box 版本（Gate 锁定的版本优先），并取对应版本档案。
// 需要的内核尚未下载时返回错误：Gate 保持上一份已生效配置，不会收到缺内核的配置。
func (s *Server) gateKernels(ctx context.Context, g *ent.Gate, need map[string]bool) (map[string]contracts.Kernel, map[string]string, error) {
	ks, profiles := map[string]contracts.Kernel{}, map[string]string{}
	for _, kernel := range []string{"xray", "sing-box"} {
		if !need[kernel] {
			continue
		}
		version := g.KernelPins[kernel]
		if version == "" {
			version = s.defaultKernelVersion(ctx, kernel, g.Arch)
		}
		if version == "" {
			return nil, nil, fmt.Errorf("%s 尚未下载，请先在内核页面拉取", kernel)
		}
		_, sum, err := s.kernelCache().Get(kernel, version, g.Arch)
		if err != nil {
			return nil, nil, err
		}
		text, err := s.profiles().Select(kernel, version)
		if err != nil {
			return nil, nil, err
		}
		ks[kernel], profiles[kernel] = contracts.Kernel{Version: version, SHA256: sum}, text
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
