package selfupdate

import (
	"fmt"

	"github.com/shusfun/weir/internal/ghrelease"
)

// ValidateRelease uses the same complete package set for discovery and installation.
// A withdrawn package invalidates the release even on the other architecture.
func ValidateRelease(r ghrelease.Release, version string) error {
	if r.Tag != "v"+version || r.Draft || r.Prerelease {
		return fmt.Errorf("v%s 不是可用的正式发布", version)
	}
	expected := map[string]bool{
		PrefetchName(version, "amd64"): false,
		PrefetchName(version, "arm64"): false,
		"checksums.txt":                false,
	}
	for _, a := range r.Assets {
		found, ok := expected[a.Name]
		if !ok || found {
			return fmt.Errorf("v%s 发布附件重复或不符合发布契约", version)
		}
		if a.State != "uploaded" || a.Size <= 0 || a.SHA256() == "" {
			return fmt.Errorf("v%s 的 %s 未上传完成或缺少有效大小、SHA256 摘要", version, a.Name)
		}
		expected[a.Name] = true
	}
	for _, name := range []string{PrefetchName(version, "amd64"), PrefetchName(version, "arm64"), "checksums.txt"} {
		if !expected[name] {
			return fmt.Errorf("v%s 缺少 %s，发布不可用", version, name)
		}
	}
	return nil
}
