package control

// Adapted interaction from MiSub 4952c16 github-proxy-handler.js (MIT).
// Only catalog-owned repository metadata is fetched; never client URLs or tokens.
import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

var releaseAPIPath = regexp.MustCompile(`^/(?:repos/[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+|repositories/[0-9]{1,20})/releases/latest$`)

func releaseRedirect(req *http.Request, via []*http.Request) error {
	u := req.URL
	if len(via) >= 3 || u.Scheme != "https" || u.Host != "api.github.com" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || !releaseAPIPath.MatchString(u.EscapedPath()) {
		return http.ErrUseLastResponse
	}
	return nil
}

type clientRelease struct {
	Status      string `json:"status"`
	Source      string `json:"source"`
	ObservedAt  string `json:"observed_at"`
	Version     string `json:"version,omitempty"`
	URL         string `json:"url,omitempty"`
	PublishedAt string `json:"published_at,omitempty"`
}

func releaseCacheKey(repo string) string { return "migate:client-release:" + digest(repo) }
func fetchClientRelease(ctx context.Context, client *http.Client, repo string, now time.Time) clientRelease {
	result := clientRelease{Status: "unknown", Source: "github-releases", ObservedAt: now.UTC().Format(time.RFC3339)}
	if !catalogRepo.MatchString(repo) {
		return result
	}
	req, e := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/repos/"+repo+"/releases/latest", nil)
	if e != nil {
		return result
	}
	req.Header.Set("User-Agent", "MiGate-client-metadata")
	req.Header.Set("Accept", "application/vnd.github+json")
	response, e := client.Do(req)
	if e != nil {
		return result
	}
	defer response.Body.Close()
	if response.StatusCode == 404 {
		result.Status = "not_found"
		return result
	}
	if response.StatusCode == 403 || response.StatusCode == 429 {
		result.Status = "rate_limited"
		return result
	}
	if response.StatusCode != 200 {
		return result
	}
	raw, e := io.ReadAll(io.LimitReader(response.Body, 512*1024+1))
	if e != nil || len(raw) > 512*1024 {
		return result
	}
	var wire struct {
		Tag        string `json:"tag_name"`
		URL        string `json:"html_url"`
		Published  string `json:"published_at"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
	}
	if json.Unmarshal(raw, &wire) != nil || wire.Draft || wire.Prerelease || !catalogText(wire.Tag, 120, true) {
		return result
	}
	published, e := time.Parse(time.RFC3339, wire.Published)
	if e != nil || published.After(now.Add(time.Minute)) {
		return result
	}
	u, e := url.Parse(wire.URL)
	if e != nil || u == nil {
		return result
	}
	// A canonical repository may differ after an official GitHub rename redirect.
	// Without that validated redirect, retain exact original repository ownership.
	canonicalRepo := repo
	if response.Request != nil && response.Request.URL.String() != req.URL.String() {
		final := response.Request.URL
		if final.Scheme != "https" || final.Host != "api.github.com" || final.User != nil || final.RawQuery != "" || final.Fragment != "" || !releaseAPIPath.MatchString(final.EscapedPath()) {
			return result
		}
		parts := strings.Split(u.EscapedPath(), "/")
		if len(parts) < 6 || !catalogRepo.MatchString(parts[1]+"/"+parts[2]) {
			return result
		}
		canonicalRepo = parts[1] + "/" + parts[2]
	}
	if e != nil || u.Scheme != "https" || u.Host != "github.com" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || !strings.HasPrefix(u.EscapedPath(), "/"+canonicalRepo+"/releases/tag/") {
		return result
	}
	result.Status = "current"
	result.Version = wire.Tag
	result.URL = u.String()
	result.PublishedAt = published.UTC().Format(time.RFC3339)
	return result
}
func validCachedRelease(r clientRelease, now time.Time) bool {
	t, e := time.Parse(time.RFC3339, r.ObservedAt)
	return e == nil && !t.After(now.Add(time.Minute)) && now.Sub(t) < 24*time.Hour && r.Source == "github-releases" && catalogChoice(r.Status, "current", "unknown", "not_found", "rate_limited")
}
func (s *Server) getClientRelease(c *gin.Context) {
	d, e := s.readClientCatalog(c.Request.Context())
	if e != nil {
		failure(c, 503, "client_catalog_unavailable")
		return
	}
	var item *catalogClient
	for i := range d.Clients {
		v := &d.Clients[i]
		if v.ID == c.Param("id") && (v.Enabled || c.GetString("role") == "admin") {
			item = v
			break
		}
	}
	if item == nil {
		failure(c, 404, "client_not_found")
		return
	}
	now := time.Now().UTC()
	if item.Repo == "" {
		c.JSON(200, clientRelease{Status: "not_configured", Source: "github-releases", ObservedAt: now.Format(time.RFC3339)})
		return
	}
	key := releaseCacheKey(item.Repo)
	ctx := c.Request.Context()
	// Cache failure does not trigger uncontrolled upstream requests.
	cached, e := s.Redis.Get(ctx, key).Result()
	if e == nil {
		var result clientRelease
		if json.Unmarshal([]byte(cached), &result) == nil && validCachedRelease(result, now) {
			c.JSON(200, result)
			return
		}
	}
	if e != nil && !errors.Is(e, redis.Nil) {
		failure(c, 503, "client_release_unavailable")
		return
	}
	// Shared bounded lease prevents duplicate fetches across processes and users.
	locked, e := s.Redis.SetNX(ctx, key+":fetch", "1", 15*time.Second).Result()
	if e != nil {
		failure(c, 503, "client_release_unavailable")
		return
	}
	if !locked {
		c.JSON(200, clientRelease{Status: "pending", Source: "github-releases", ObservedAt: now.Format(time.RFC3339)})
		return
	}
	// Keep the lease until TTL; no blind deletion can remove a later owner's lease.
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	defer transport.CloseIdleConnections()
	client := &http.Client{Timeout: 8 * time.Second, Transport: transport, CheckRedirect: releaseRedirect}
	result := fetchClientRelease(ctx, client, item.Repo, now)
	ttl := 24 * time.Hour
	if result.Status != "current" {
		ttl = 5 * time.Minute
	}
	raw, _ := json.Marshal(result)
	if s.Redis.Set(ctx, key, raw, ttl).Err() != nil {
		failure(c, 503, "client_release_unavailable")
		return
	}
	c.JSON(200, result)
}
