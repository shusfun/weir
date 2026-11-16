package gate

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type appliedFile struct {
	path   string
	body   []byte
	exists bool
}
type kernelSnapshot struct {
	name    string
	running bool
	files   []appliedFile
}

func (h *Helper) snapshotKernels() ([]kernelSnapshot, error) {
	var out []kernelSnapshot
	running := h.Sup.Running()
	for _, name := range helperKernels {
		s := kernelSnapshot{name: name, running: running[name] > 0}
		for _, path := range []string{h.Sup.config(name), filepath.Join(h.P.CoresDir, name+".args.json"), filepath.Join(h.P.KernelsDir, name+".version")} {
			body, err := os.ReadFile(path)
			if err != nil && !os.IsNotExist(err) {
				return nil, err
			}
			s.files = append(s.files, appliedFile{path: path, body: body, exists: err == nil})
		}
		out = append(out, s)
	}
	return out, nil
}

func (h *Helper) restoreKernels(before []kernelSnapshot, touched map[string]bool) error {
	var errs []error
	// Stop every changed member before restoring any dependency.
	for i := len(before) - 1; i >= 0; i-- {
		s := before[i]
		if touched[s.name] {
			h.Sup.Stop(s.name)
		}
	}
	for _, s := range before {
		if !touched[s.name] {
			continue
		}
		for _, f := range s.files {
			if !f.exists {
				if err := os.Remove(f.path); err != nil && !os.IsNotExist(err) {
					errs = append(errs, err)
				}
				continue
			}
			if current, err := os.ReadFile(f.path); err == nil && bytes.Equal(current, f.body) {
				continue
			}
			if err := writeAtomic(f.path, f.body); err != nil {
				errs = append(errs, err)
			}
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	for _, s := range before {
		if touched[s.name] && s.running {
			if err := h.Sup.Start(s.name); err != nil {
				errs = append(errs, fmt.Errorf("%s 回滚启动失败：%w", s.name, err))
				break
			}
			if ports := h.savedAuxiliaryHealth(s.name); len(ports) > 0 && !h.healthyPorts(s.name, ports) {
				errs = append(errs, fmt.Errorf("%s 回滚健康检查失败", s.name))
				break
			}
		}
	}
	if len(errs) > 0 {
		h.stopManagedKernels()
	}
	return errors.Join(errs...)
}
