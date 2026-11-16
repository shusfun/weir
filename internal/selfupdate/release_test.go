package selfupdate

import (
	"strings"
	"testing"

	"github.com/shusfun/weir/internal/ghrelease"
)

func TestValidateCompleteRelease(t *testing.T) {
	complete := func() ghrelease.Release {
		r := ghrelease.Release{Tag: "v0.1.18"}
		for _, name := range []string{PrefetchName("0.1.18", "amd64"), PrefetchName("0.1.18", "arm64"), "checksums.txt"} {
			r.Assets = append(r.Assets, ghrelease.Asset{Name: name, State: "uploaded", Size: 1, Digest: "sha256:" + strings.Repeat("a", 64)})
		}
		return r
	}
	if err := ValidateRelease(complete(), "0.1.18"); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*ghrelease.Release){
		"missing":    func(r *ghrelease.Release) { r.Assets = r.Assets[:2] },
		"duplicate":  func(r *ghrelease.Release) { r.Assets[1] = r.Assets[0] },
		"uploading":  func(r *ghrelease.Release) { r.Assets[0].State = "starter" },
		"empty":      func(r *ghrelease.Release) { r.Assets[0].Size = 0 },
		"digest":     func(r *ghrelease.Release) { r.Assets[0].Digest = "sha256:" + strings.Repeat("z", 64) },
		"draft":      func(r *ghrelease.Release) { r.Draft = true },
		"prerelease": func(r *ghrelease.Release) { r.Prerelease = true },
		"tag":        func(r *ghrelease.Release) { r.Tag = "v0.1.17" },
	} {
		t.Run(name, func(t *testing.T) {
			r := complete()
			change(&r)
			if ValidateRelease(r, "0.1.18") == nil {
				t.Fatal("invalid release accepted")
			}
		})
	}
}
