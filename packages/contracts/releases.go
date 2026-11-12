package contracts

import (
	"encoding/hex"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type ReleaseAsset struct {
	Name      string `json:"name"`
	URL       string `json:"browser_download_url"`
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
	Downloads int64  `json:"download_count"`
}
type ReleaseRecord struct {
	Tag        string         `json:"tag_name"`
	Name       string         `json:"name"`
	Draft      bool           `json:"draft"`
	Prerelease bool           `json:"prerelease"`
	Published  time.Time      `json:"published_at"`
	Body       string         `json:"body"`
	HTMLURL    string         `json:"html_url"`
	Assets     []ReleaseAsset `json:"assets"`
}
type ReleaseQueryResult struct {
	Repository string          `json:"repository"`
	Releases   []ReleaseRecord `json:"releases"`
}

var releaseRepoSegment = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,99}$`)

func (q ReleaseQueryTask) Validate() error {
	parts := strings.Split(q.Repository, "/")
	if len(parts) != 2 || !releaseRepoSegment.MatchString(parts[0]) || !releaseRepoSegment.MatchString(parts[1]) || q.PerPage < 0 || q.PerPage > 100 || q.PerPage > 0 && q.Tag != "" || len(q.Tag) > 200 || strings.ContainsAny(q.Tag, "\r\n\x00") || q.MaxBytes < 1 || q.MaxBytes > 10<<20 {
		return errors.New("release_query_invalid")
	}
	return nil
}

func releaseURL(raw, repo, kind string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host == "github.com" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && strings.HasPrefix(u.EscapedPath(), "/"+repo+"/releases/"+kind+"/")
}

func (result ReleaseQueryResult) Validate(q ReleaseQueryTask) error {
	if err := q.Validate(); err != nil {
		return err
	}
	count := q.PerPage
	if count == 0 {
		count = 1
	}
	if result.Repository != q.Repository || len(result.Releases) > count || q.PerPage == 0 && len(result.Releases) != 1 {
		return errors.New("release_result_ownership_invalid")
	}
	seen := map[string]bool{}
	for _, r := range result.Releases {
		if r.Tag == "" || len(r.Tag) > 200 || q.Tag != "" && r.Tag != q.Tag || seen[r.Tag] || len(r.Name) > 1000 || len(r.Body) > 1<<20 || len(r.Assets) > 1000 || !releaseURL(r.HTMLURL, q.Repository, "tag") || r.Published.After(time.Now().Add(5*time.Minute)) {
			return errors.New("release_result_invalid")
		}
		if q.PerPage == 0 && q.Tag == "" && (r.Draft || r.Prerelease) {
			return errors.New("release_latest_invalid")
		}
		seen[r.Tag] = true
		for _, a := range r.Assets {
			if a.Name == "" || len(a.Name) > 500 || strings.ContainsAny(a.Name, "/\\\x00\r\n") || a.Size < 0 || a.Size > 16<<30 || a.Downloads < 0 || !releaseURL(a.URL, q.Repository, "download") {
				return errors.New("release_asset_invalid")
			}
			if a.Digest != "" {
				digest, err := hex.DecodeString(strings.TrimPrefix(a.Digest, "sha256:"))
				if err != nil || len(digest) != 32 || !strings.HasPrefix(a.Digest, "sha256:") {
					return errors.New("release_digest_invalid")
				}
			}
		}
	}
	return nil
}
