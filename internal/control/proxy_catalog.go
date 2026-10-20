package control

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/imzyb/MiGate/ent/resource"
	"github.com/imzyb/MiGate/internal/subscription"
	"github.com/imzyb/MiGate/packages/contracts"
	"github.com/redis/go-redis/v9"
)

// Fixed official path replaces the upstream's third-party SOCKS download mirror.
// No URL, repository, headers or credentials may be supplied by a browser.
var proxyCatalogPaths = map[string]string{
	"socks": "https://raw.githubusercontent.com/EDT-Pages/Proxy-List/refs/heads/main/data/socks5.json",
	"http":  "https://raw.githubusercontent.com/EDT-Pages/Proxy-List/refs/heads/main/data/http.json",
	"https": "https://raw.githubusercontent.com/EDT-Pages/Proxy-List/refs/heads/main/data/https.json",
}

type proxyCandidate struct {
	Key          string `json:"key"`
	Protocol     string `json:"protocol"`
	Address      string `json:"address"`
	Port         int    `json:"port"`
	CountryCode  string `json:"country_code"`
	Country      string `json:"country"`
	City         string `json:"city"`
	Organization string `json:"organization"`
	Username     string `json:"username,omitempty"`
	Password     string `json:"password,omitempty"`
}
type proxyCatalog struct {
	ObservedAt time.Time        `json:"observed_at"`
	Items      []proxyCandidate `json:"items"`
	Rejected   int              `json:"rejected"`
}

func (s *Server) proxyCatalogRoutes(admin *gin.RouterGroup) {
	admin.GET("/proxy-catalog/:protocol", s.listProxyCatalog)
	admin.POST("/devices/:id/proxy-catalog/import", s.importProxyCatalog)
	admin.POST("/devices/:id/proxy-catalog/probe", s.probeProxyCatalog)
}
func publicCatalogIP(host string) bool {
	a, e := netip.ParseAddr(host)
	if e != nil {
		return false
	}
	a = a.Unmap()
	if !a.IsGlobalUnicast() || a.IsPrivate() || a.IsLoopback() || a.IsLinkLocalUnicast() {
		return false
	}
	for _, cidr := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001:db8::/32"} {
		if netip.MustParsePrefix(cidr).Contains(a) {
			return false
		}
	}
	return true
}
func parseProxyCatalog(body []byte, protocol string) (proxyCatalog, error) {
	result := proxyCatalog{ObservedAt: time.Now().UTC(), Items: []proxyCandidate{}}
	if proxyCatalogPaths[protocol] == "" || len(body) > subscription.MaxBodyBytes {
		return result, errors.New("invalid_proxy_catalog")
	}
	var raw []map[string]json.RawMessage
	if json.Unmarshal(body, &raw) != nil || len(raw) > 10000 {
		return result, errors.New("invalid_proxy_catalog")
	}
	seen := map[string]bool{}
	for _, item := range raw {
		textField := func(keys ...string) string {
			for _, key := range keys {
				var value string
				if json.Unmarshal(item[key], &value) == nil && value != "" {
					return value
				}
			}
			return ""
		}
		host := textField("ip", "address", "host", "server")
		port := 0
		json.Unmarshal(item["port"], &port)
		user, password := "", ""
		if rawURL := textField("proxy", "url", "uri"); rawURL != "" {
			u, e := url.Parse(rawURL)
			if e != nil || u.Hostname() == "" || u.Path != "" && u.Path != "/" || u.RawQuery != "" || u.Fragment != "" {
				result.Rejected++
				continue
			}
			want := protocol
			if want == "socks" {
				want = "socks5"
			}
			if u.Scheme != want {
				result.Rejected++
				continue
			}
			n, e := strconv.Atoi(u.Port())
			if e != nil || host != "" && host != u.Hostname() || port != 0 && port != n {
				result.Rejected++
				continue
			}
			host, port = u.Hostname(), n
			if u.User != nil {
				user = u.User.Username()
				password, _ = u.User.Password()
			}
		}
		if !publicCatalogIP(host) || port < 1 || port > 65535 || !catalogText(user, 256, false) || !catalogText(password, 256, false) {
			result.Rejected++
			continue
		}
		code := strings.ToUpper(textField("country_code", "country"))
		if len(code) != 2 || code[0] < 'A' || code[0] > 'Z' || code[1] < 'A' || code[1] > 'Z' {
			code = "ZZ"
		}
		p := proxyCandidate{Protocol: protocol, Address: host, Port: port, CountryCode: code, Country: textField("country_cn", "country_en"), City: textField("city"), Organization: textField("asOrganization", "organization"), Username: user, Password: password}
		if !catalogText(p.Country, 120, false) || !catalogText(p.City, 120, false) || !catalogText(p.Organization, 240, false) {
			result.Rejected++
			continue
		}
		b, _ := json.Marshal([]any{protocol, host, port, user, password})
		p.Key = digest(string(b))
		if seen[p.Key] {
			continue
		}
		seen[p.Key] = true
		result.Items = append(result.Items, p)
	}
	if len(result.Items) == 0 {
		return result, errors.New("empty_proxy_catalog")
	}
	return result, nil
}
func fetchProxyCatalog(ctx context.Context, protocol string) (proxyCatalog, error) {
	path := proxyCatalogPaths[protocol]
	if path == "" {
		return proxyCatalog{}, errors.New("invalid_proxy_catalog")
	}
	f := subscription.HTTPFetcher{Client: &http.Client{Timeout: 12 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("proxy_catalog_redirect_refused") }}}
	b, e := f.Fetch(ctx, path, false)
	if e != nil {
		return proxyCatalog{}, errors.New("proxy_catalog_fetch_failed")
	}
	return parseProxyCatalog(b, protocol)
}
func (s *Server) cachedProxyCatalog(ctx context.Context, protocol string) (proxyCatalog, error) {
	var result proxyCatalog
	if proxyCatalogPaths[protocol] == "" {
		return result, errors.New("invalid_proxy_catalog")
	}
	key := "migate:proxy-catalog:" + protocol
	sealed, e := s.Redis.Get(ctx, key).Result()
	if e == nil {
		plain, e := s.Crypto.Decrypt(sealed)
		prefix := "proxy-catalog:" + protocol + ":"
		if e != nil || !strings.HasPrefix(plain, prefix) || json.Unmarshal([]byte(strings.TrimPrefix(plain, prefix)), &result) != nil || len(result.Items) > 10000 || time.Since(result.ObservedAt) > 15*time.Minute {
			return result, errors.New("proxy_catalog_unavailable")
		}
		return result, nil
	}
	if !errors.Is(e, redis.Nil) {
		return result, errors.New("proxy_catalog_unavailable")
	}
	lease, e := s.Redis.SetNX(ctx, key+":refresh", randomToken(16), 20*time.Second).Result()
	if e != nil || !lease {
		return result, errors.New("proxy_catalog_refresh_pending")
	}
	// Let the bounded lease expire; never delete a possibly newer owner's lease.
	result, e = fetchProxyCatalog(ctx, protocol)
	if e != nil {
		return result, e
	}
	b, _ := json.Marshal(result)
	sealed, e = s.Crypto.Encrypt("proxy-catalog:" + protocol + ":" + string(b))
	if e != nil {
		return result, e
	}
	if e = s.Redis.Set(ctx, key, sealed, 15*time.Minute).Err(); e != nil {
		return result, errors.New("proxy_catalog_unavailable")
	}
	return result, nil
}
func (s *Server) listProxyCatalog(c *gin.Context) {
	page, e := strconv.Atoi(c.DefaultQuery("page", "1"))
	if e != nil || page < 1 || page > 1000 {
		failure(c, 400, "invalid_page")
		return
	}
	for key := range c.Request.URL.Query() {
		if key != "page" && key != "country" && key != "search" {
			failure(c, 400, "invalid_catalog_filter")
			return
		}
	}
	search := strings.ToLower(c.Query("search"))
	if len(search) > 120 {
		failure(c, 400, "invalid_catalog_filter")
		return
	}
	data, e := s.cachedProxyCatalog(c.Request.Context(), c.Param("protocol"))
	if e != nil {
		failure(c, 503, "proxy_catalog_unavailable")
		return
	}
	regions := map[string]int{}
	names := map[string]string{}
	items := []gin.H{}
	for _, p := range data.Items {
		regions[p.CountryCode]++
		names[p.CountryCode] = p.Country
		if country := c.Query("country"); country != "" && country != p.CountryCode {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(p.Address+" "+p.Country+" "+p.City+" "+p.Organization), search) {
			continue
		}
		items = append(items, gin.H{"key": p.Key, "protocol": p.Protocol, "address": p.Address, "port": p.Port, "country_code": p.CountryCode, "country": p.Country, "city": p.City, "organization": p.Organization, "has_credentials": p.Username != "" || p.Password != ""})
	}
	regionList := []gin.H{}
	for code, count := range regions {
		regionList = append(regionList, gin.H{"code": code, "name": names[code], "count": count})
	}
	sort.Slice(regionList, func(i, j int) bool { return regionList[i]["code"].(string) < regionList[j]["code"].(string) })
	total := len(items)
	start := (page - 1) * 20
	if start > total {
		start = total
	}
	end := start + 20
	if end > total {
		end = total
	}
	c.JSON(200, gin.H{"items": items[start:end], "total": total, "page": page, "page_size": 20, "regions": regionList, "observed_at": data.ObservedAt, "rejected": data.Rejected, "source": "EDT-Pages/Proxy-List"})
}
func (s *Server) importProxyCatalog(c *gin.Context) {
	var req struct {
		Protocol string `json:"protocol"`
		Key      string `json:"key"`
		Confirm  bool   `json:"confirm"`
	}
	if !bind(c, &req) {
		return
	}
	if !req.Confirm || len(req.Key) != 64 {
		failure(c, 400, "proxy_catalog_confirmation_required")
		return
	}
	data, e := s.cachedProxyCatalog(c.Request.Context(), req.Protocol)
	if e != nil {
		failure(c, 503, "proxy_catalog_unavailable")
		return
	}
	var found *proxyCandidate
	for i := range data.Items {
		if data.Items[i].Key == req.Key {
			found = &data.Items[i]
			break
		}
	}
	if found == nil {
		failure(c, 409, "proxy_catalog_changed")
		return
	}
	if !s.rate(c, "proxy-import", 10) {
		return
	}
	id, _ := strconv.Atoi(c.Param("id"))
	rows, e := s.Store.Resource.Query().Where(resource.KindEQ("device-outbound"), resource.OwnerIDEQ(id)).All(c.Request.Context())
	if e != nil {
		failure(c, 503, "query_failed")
		return
	}
	tag := "public-" + req.Protocol + "-" + req.Key[:16]
	for _, r := range rows {
		if r.Data["tag"] == tag {
			failure(c, 409, "proxy_already_imported")
			return
		}
	}
	// Use the existing capability/transport/async task path; no direct resource CRUD.
	payload := map[string]any{"id": 0, "tag": tag, "remark": strings.TrimSpace(found.Country + " " + found.City + " · 公共代理"), "protocol": found.Protocol, "address": found.Address, "port": found.Port, "username": found.Username, "password": found.Password, "enabled": true, "transport_options": map[string]any{}}
	if found.Protocol == "https" {
		payload["transport_options"] = map[string]any{"tls": true, "server_name": found.Address, "alpn": []string{"http/1.1"}}
	}
	b, _ := json.Marshal(map[string]any{"operation": "outbound.upsert", "confirm": true, "payload": payload})
	c.Request.Body = io.NopCloser(bytes.NewReader(b))
	c.Request.ContentLength = int64(len(b))
	s.createTask(c)
}

func (s *Server) probeProxyCatalog(c *gin.Context) {
	var req struct {
		Protocol string `json:"protocol"`
		Key      string `json:"key"`
		Confirm  bool   `json:"confirm"`
	}
	if !bind(c, &req) {
		return
	}
	if !req.Confirm || len(req.Key) != 64 {
		failure(c, 400, "proxy_catalog_confirmation_required")
		return
	}
	data, e := s.cachedProxyCatalog(c.Request.Context(), req.Protocol)
	if e != nil {
		failure(c, 503, "proxy_catalog_unavailable")
		return
	}
	var found *proxyCandidate
	for i := range data.Items {
		if data.Items[i].Key == req.Key {
			found = &data.Items[i]
			break
		}
	}
	if found == nil {
		failure(c, 409, "proxy_catalog_changed")
		return
	}
	payload := contracts.CatalogProbe{Address: found.Address, Port: found.Port, Key: found.Key}
	c.Set("proxy_catalog_trusted", true)
	b, _ := json.Marshal(map[string]any{"operation": "outbound.catalog.probe", "confirm": true, "payload": payload})
	c.Request.Body = io.NopCloser(bytes.NewReader(b))
	c.Request.ContentLength = int64(len(b))
	s.createTask(c)
}
