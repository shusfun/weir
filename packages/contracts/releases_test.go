package contracts

import (
	"strings"
	"testing"
)

func TestReleaseQueryRejectsRepositoryAndModeEscapes(t *testing.T) {
	for _, repo := range []string{"../repo", "owner/..", "owner/repo/extra", "owner/repo?x=1", "owner%2frepo", "owner/repo\n"} {
		if err := (ReleaseQueryTask{Repository: repo, MaxBytes: 1024}).Validate(); err == nil {
			t.Fatalf("accepted %q", repo)
		}
	}
	if err := (ReleaseQueryTask{Repository: "owner/repo", Tag: "v1", PerPage: 10, MaxBytes: 1024}).Validate(); err == nil {
		t.Fatal("ambiguous mode accepted")
	}
}

func TestReleaseResultValidatesOwnershipAndDigests(t *testing.T) {
	q := ReleaseQueryTask{Repository: "owner/repo", Tag: "v1", MaxBytes: 1 << 20}
	valid := func() ReleaseQueryResult {
		return ReleaseQueryResult{Repository: q.Repository, Releases: []ReleaseRecord{{Tag: "v1", HTMLURL: "https://github.com/owner/repo/releases/tag/v1", Assets: []ReleaseAsset{{Name: "core.zip", URL: "https://github.com/owner/repo/releases/download/v1/core.zip", Digest: "sha256:" + strings.Repeat("a", 64), Size: 100}}}}}
	}
	if err := valid().Validate(q); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*ReleaseQueryResult){
		func(r *ReleaseQueryResult) { r.Repository = "other/repo" },
		func(r *ReleaseQueryResult) { r.Releases[0].Tag = "v2" },
		func(r *ReleaseQueryResult) { r.Releases[0].HTMLURL = "https://github.com/other/repo/releases/tag/v1" },
		func(r *ReleaseQueryResult) { r.Releases[0].Assets[0].URL = "http://127.0.0.1/core.zip" },
		func(r *ReleaseQueryResult) { r.Releases[0].Assets[0].Digest = "sha256:" + strings.Repeat("z", 64) },
		func(r *ReleaseQueryResult) { r.Releases[0].Assets[0].Size = -1 },
	} {
		r := valid()
		change(&r)
		if err := r.Validate(q); err == nil {
			t.Fatal("invalid result accepted")
		}
	}
}
