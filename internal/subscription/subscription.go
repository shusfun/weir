package subscription

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/imzyb/MiGate/internal/db"
)

const (
	MaxBodyBytes = 2 << 20
	UserAgent    = "MiGate outbound-subscription/1.0"
)

type ParsedLink struct {
	// Transformation provenance is internal; it never changes endpoint credentials.
	OriginalName string
	SourceGroup  string
	SourceName   string
	Identity     string
	Remark       string
	Protocol     string
	Address      string
	Port         int
	Username     string
	Password     string
	RawLink      string
	SettingsJSON string
}

type SkippedLink struct {
	Raw      string `json:"raw"`
	Reason   string `json:"reason"`
	Protocol string `json:"protocol,omitempty"`
}

type ParseResult struct {
	Nodes   []ParsedLink  `json:"nodes"`
	Skipped []SkippedLink `json:"skipped"`
}

type Settings struct {
	Snell             *SnellSettings     `json:"snell,omitempty"`
	WireGuard         *WireGuardSettings `json:"wireguard,omitempty"`
	Security          string             `json:"security,omitempty"`
	Network           string             `json:"network,omitempty"`
	TLS               bool               `json:"tls,omitempty"`
	Reality           bool               `json:"reality,omitempty"`
	SNI               string             `json:"sni,omitempty"`
	Host              string             `json:"host,omitempty"`
	Path              string             `json:"path,omitempty"`
	Flow              string             `json:"flow,omitempty"`
	Fingerprint       string             `json:"fp,omitempty"`
	PublicKey         string             `json:"pbk,omitempty"`
	ShortID           string             `json:"sid,omitempty"`
	ALPN              []string           `json:"alpn,omitempty"`
	Method            string             `json:"method,omitempty"`
	ServiceName       string             `json:"service_name,omitempty"`
	AllowInsecure     bool               `json:"allow_insecure,omitempty"`
	VmessUnsupported  bool               `json:"vmess_unsupported,omitempty"`
	UnsupportedFields []string           `json:"unsupported_fields,omitempty"`
	Obfs              string             `json:"obfs,omitempty"`
	ObfsPassword      string             `json:"obfs_password,omitempty"`
	CongestionControl string             `json:"congestion_control,omitempty"`
	ZeroRTT           bool               `json:"zero_rtt,omitempty"`
	SPKI              string             `json:"spki,omitempty"`
	CertificatePin    string             `json:"certificate_pin,omitempty"`
}

type HTTPFetcher struct {
	Client    *http.Client
	UserAgent string
}

func ValidateUserAgent(value string) error {
	if len(value) > 512 {
		return errors.New("invalid subscription user agent")
	}
	for _, c := range value {
		if c < 32 || c > 126 {
			return errors.New("invalid subscription user agent")
		}
	}
	return nil
}

func (f HTTPFetcher) Fetch(ctx context.Context, rawURL string, allowPrivate bool) ([]byte, error) {
	body, _, err := f.fetch(ctx, rawURL, allowPrivate)
	return body, err
}

func (f HTTPFetcher) FetchInventory(ctx context.Context, rawURL string, allowPrivate bool) (FetchedInventory, error) {
	body, header, err := f.fetch(ctx, rawURL, allowPrivate)
	if err != nil {
		return FetchedInventory{}, err
	}
	return FetchedInventory{Body: body, Usage: ParseUsageMetadata(header.Values("Subscription-Userinfo"))}, nil
}

func (f HTTPFetcher) fetch(ctx context.Context, rawURL string, allowPrivate bool) ([]byte, http.Header, error) {
	if err := ValidateUserAgent(f.UserAgent); err != nil {
		return nil, nil, err
	}
	parsed, err := validateFetchURL(ctx, rawURL, allowPrivate)
	if err != nil {
		return nil, nil, err
	}
	client := safeHTTPClient(allowPrivate)
	if f.Client != nil {
		if f.Client.Timeout > 0 {
			client.Timeout = f.Client.Timeout
		}
		client.Jar = f.Client.Jar
	}
	baseCheckRedirect := client.CheckRedirect
	if f.Client != nil && f.Client.CheckRedirect != nil {
		baseCheckRedirect = f.Client.CheckRedirect
	}
	copyClient := *client
	copyClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if baseCheckRedirect != nil {
			if err := baseCheckRedirect(req, via); err != nil {
				return err
			}
		}
		if _, err := validateFetchURL(req.Context(), req.URL.String(), allowPrivate); err != nil {
			return err
		}
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", UserAgent)
	if f.UserAgent != "" {
		req.Header.Set("User-Agent", f.UserAgent)
	}
	resp, err := copyClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, nil, fmt.Errorf("subscription upstream returned %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxBodyBytes+1))
	if err != nil {
		return nil, nil, err
	}
	if len(body) > MaxBodyBytes {
		return nil, nil, fmt.Errorf("subscription body exceeds %d bytes", MaxBodyBytes)
	}
	return body, resp.Header.Clone(), nil
}

func safeHTTPClient(allowPrivate bool) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		target, err := resolveDialTarget(ctx, host, allowPrivate)
		if err != nil {
			return nil, err
		}
		return dialer.DialContext(ctx, network, net.JoinHostPort(target.String(), port))
	}
	return &http.Client{Timeout: 10 * time.Second, Transport: transport}
}

func DecodeBody(body []byte) []string {
	text := strings.TrimSpace(string(body))
	if text == "" {
		return nil
	}
	if decoded, ok := tryDecodeBase64(text); ok {
		text = strings.TrimSpace(decoded)
	}
	lines := strings.FieldsFunc(text, func(r rune) bool { return r == '\n' || r == '\r' })
	links := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.Contains(line, "://") {
			links = append(links, line)
		}
	}
	return links
}

func ParseBody(body []byte) ([]ParsedLink, error) {
	result, err := ParseLinks(DecodeBody(body))
	if err != nil {
		return nil, err
	}
	return result.Nodes, nil
}

func ParseLinks(links []string) (ParseResult, error) {
	result := ParseResult{Nodes: make([]ParsedLink, 0, len(links))}
	parsed := make([]ParsedLink, 0, len(links))
	for _, raw := range links {
		item, err := ParseLink(raw)
		if err != nil {
			result.Skipped = append(result.Skipped, SkippedLink{Raw: raw, Reason: err.Error(), Protocol: linkProtocol(raw)})
			continue
		}
		parsed = append(parsed, item)
	}
	if len(parsed) == 0 && len(links) > 0 {
		return result, fmt.Errorf("no supported subscription links found")
	}
	result.Nodes = parsed
	return result, nil
}

func ParseLink(raw string) (ParsedLink, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ParsedLink{}, errors.New("empty link")
	}
	lower := strings.ToLower(raw)
	switch {
	case strings.HasPrefix(lower, "vless://"):
		return parseURLLink(raw, "vless")
	case strings.HasPrefix(lower, "trojan://"):
		return parseURLLink(raw, "trojan")
	case strings.HasPrefix(lower, "ss://"):
		return parseSSLink(raw)
	case strings.HasPrefix(lower, "vmess://"):
		return parseVMessLink(raw)
	case strings.HasPrefix(lower, "hysteria2://"), strings.HasPrefix(lower, "hy2://"):
		return parseURLLink(raw, "hysteria2")
	case strings.HasPrefix(lower, "tuic://"):
		return parseURLLink(raw, "tuic")
	case strings.HasPrefix(lower, "anytls://"):
		return parseURLLink(raw, "anytls")
	case strings.HasPrefix(lower, "snell://"):
		return parseExtendedLink(raw, "snell")
	case strings.HasPrefix(lower, "wireguard://"):
		return parseExtendedLink(raw, "wireguard")
	case strings.HasPrefix(lower, "http://"), strings.HasPrefix(lower, "https://"):
		return parseURLLink(raw, "http")
	case strings.HasPrefix(lower, "socks5://"), strings.HasPrefix(lower, "socks://"), strings.HasPrefix(lower, "socks5-tls://"):
		return parseURLLink(raw, "socks5")
	default:
		return ParsedLink{}, fmt.Errorf("unsupported link")
	}
}

func linkProtocol(raw string) string {
	raw = strings.TrimSpace(raw)
	idx := strings.Index(raw, "://")
	if idx <= 0 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(raw[:idx]))
}

func StableIdentity(raw string, parsed ParsedLink) string {
	base := strings.ToLower(strings.TrimSpace(parsed.Protocol)) + "|" + strings.TrimSpace(parsed.Address) + "|" + strconv.Itoa(parsed.Port) + "|" + strings.TrimSpace(parsed.Username) + "|" + strings.TrimSpace(parsed.Password)
	sum := sha256.Sum256([]byte(base))
	return hex.EncodeToString(sum[:])
}

func Materialize(subscriptionID int64, links []ParsedLink, existing []db.Outbound, tagPrefix string) ([]db.MaterializedSubscriptionOutbound, []string) {
	byIdentity := map[string]db.Outbound{}
	subscriptionOutbounds := []db.Outbound{}
	usedTags := map[string]bool{}
	for _, outbound := range existing {
		if strings.TrimSpace(outbound.Tag) != "" {
			usedTags[outbound.Tag] = true
		}
		if outbound.SubscriptionID == subscriptionID && outbound.Source == db.OutboundSourceSubscription {
			subscriptionOutbounds = append(subscriptionOutbounds, outbound)
			if strings.TrimSpace(outbound.SubscriptionIdentity) != "" {
				byIdentity[outbound.SubscriptionIdentity] = outbound
			}
		}
	}
	sort.Slice(subscriptionOutbounds, func(i, j int) bool {
		if subscriptionOutbounds[i].Sort == subscriptionOutbounds[j].Sort {
			return subscriptionOutbounds[i].ID < subscriptionOutbounds[j].ID
		}
		return subscriptionOutbounds[i].Sort < subscriptionOutbounds[j].Sort
	})
	nodes := make([]db.MaterializedSubscriptionOutbound, 0, len(links))
	identities := make([]string, 0, len(links))
	for i, link := range links {
		identity := strings.TrimSpace(link.Identity)
		if identity == "" {
			identity = StableIdentity(link.RawLink, link)
		}
		identities = append(identities, identity)
		var existingID int64
		tag := ""
		if old, ok := byIdentity[identity]; ok {
			existingID = old.ID
			tag = old.Tag
		} else if i < len(subscriptionOutbounds) {
			old := subscriptionOutbounds[i]
			existingID = old.ID
			tag = old.Tag
		}
		if strings.TrimSpace(tag) == "" {
			tag = uniqueTag(tagPrefix+slug(link.Remark), usedTags)
		}
		usedTags[tag] = true
		nodes = append(nodes, db.MaterializedSubscriptionOutbound{
			ID: existingID, Tag: tag, Remark: firstNonEmpty(link.Remark, link.Address), Protocol: link.Protocol, Address: link.Address, Port: link.Port,
			Username: link.Username, Password: link.Password, SubscriptionIdentity: identity, RawLink: link.RawLink, SettingsJSON: link.SettingsJSON, Position: i,
		})
	}
	return nodes, identities
}

func parseURLLink(raw string, protocol string) (ParsedLink, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return ParsedLink{}, err
	}
	address := strings.TrimSpace(u.Hostname())
	port, _ := strconv.Atoi(u.Port())
	if port == 0 && u.Port() == "" && protocol == "http" {
		port = 80
		if strings.EqualFold(u.Scheme, "https") {
			port = 443
		}
	}
	if address == "" || port <= 0 || port > 65535 {
		return ParsedLink{}, fmt.Errorf("invalid endpoint")
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return ParsedLink{}, errors.New("invalid query encoding")
	}
	settings, err := settingsFromQuery(query, address)
	if err != nil {
		return ParsedLink{}, err
	}
	if u.Path != "" && u.Path != "/" {
		settings.UnsupportedFields = append(settings.UnsupportedFields, "uri_path")
	}
	username := ""
	password := ""
	switch protocol {
	case "vless":
		username = strings.TrimSpace(u.User.Username())
	case "trojan":
		if settings.Security == "" {
			settings.Security = "tls"
			settings.TLS = true
		}
		password, _ = u.User.Password()
		if password == "" {
			password = u.User.Username()
		}
	case "hysteria2":
		password = userInfoString(u.User)
		settings.Security = "tls"
		settings.TLS = true
	case "tuic":
		username = u.User.Username()
		password, _ = u.User.Password()
		settings.Security = "tls"
		settings.TLS = true
	case "anytls":
		password = userInfoString(u.User)
		settings.Security = "tls"
		settings.TLS = true
	case "http", "socks5":
		username = u.User.Username()
		password, _ = u.User.Password()
		if strings.EqualFold(u.Scheme, "https") || strings.EqualFold(u.Scheme, "socks5-tls") {
			settings.Security = "tls"
			settings.TLS = true
		}
	}
	if protocol != "http" && protocol != "socks5" && (username == "" && password == "" || protocol == "tuic" && (username == "" || password == "")) {
		return ParsedLink{}, errors.New("missing credentials")
	}
	if settings.TLS && settings.SNI == "" {
		settings.SNI = address
	}
	settingsJSON, _ := json.Marshal(settings)
	remark := u.Fragment
	item := ParsedLink{
		Remark: strings.TrimSpace(remark), Protocol: protocol, Address: address, Port: port, Username: username, Password: password,
		RawLink: raw, SettingsJSON: string(settingsJSON),
	}
	item.Identity = StableIdentity(raw, item)
	return item, nil
}

func parseSSLink(raw string) (ParsedLink, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return ParsedLink{}, err
	}
	remark := u.Fragment
	address := strings.TrimSpace(u.Hostname())
	port, _ := strconv.Atoi(u.Port())
	userInfo := ""
	if u.User != nil {
		userInfo = userInfoString(u.User)
	}
	if address == "" || port == 0 {
		payload := strings.TrimPrefix(raw, "ss://")
		if idx := strings.Index(payload, "#"); idx >= 0 {
			payload = payload[:idx]
		}
		if idx := strings.Index(payload, "?"); idx >= 0 {
			payload = payload[:idx]
		}
		if decoded, ok := tryDecodeBase64(payload); ok {
			if parsed, err := url.Parse("ss://" + decoded); err == nil {
				address = parsed.Hostname()
				port, _ = strconv.Atoi(parsed.Port())
				if parsed.User != nil {
					userInfo = userInfoString(parsed.User)
				}
			}
		}
	}
	if address == "" || port <= 0 || port > 65535 || userInfo == "" {
		return ParsedLink{}, fmt.Errorf("invalid ss link")
	}
	if decoded, ok := tryDecodeBase64(userInfo); ok {
		userInfo = decoded
	}
	parts := strings.SplitN(userInfo, ":", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return ParsedLink{}, fmt.Errorf("invalid ss credentials")
	}
	if err := validateSS2022(strings.TrimSpace(parts[0]), parts[1]); err != nil {
		return ParsedLink{}, err
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return ParsedLink{}, errors.New("invalid query encoding")
	}
	settings, err := settingsFromQuery(query, address)
	if err != nil {
		return ParsedLink{}, err
	}
	settings.Method = strings.TrimSpace(parts[0])
	settingsJSON, _ := json.Marshal(settings)
	item := ParsedLink{
		Remark: strings.TrimSpace(remark), Protocol: "shadowsocks", Address: address, Port: port, Username: strings.TrimSpace(parts[0]), Password: parts[1],
		RawLink: raw, SettingsJSON: string(settingsJSON),
	}
	item.Identity = StableIdentity(raw, item)
	return item, nil
}

func userInfoString(user *url.Userinfo) string {
	if user == nil {
		return ""
	}
	username := user.Username()
	password, hasPassword := user.Password()
	if hasPassword {
		return username + ":" + password
	}
	return username
}

func settingsFromQuery(values url.Values, address string) (Settings, error) {
	// url.ParseQuery has already decoded each value. Reject ambiguity instead of
	// allowing aliases or repeated keys to silently select different credentials.
	for _, entries := range values {
		if len(entries) != 1 {
			return Settings{}, errors.New("duplicate subscription parameter")
		}
	}
	for _, aliases := range [][]string{
		{"security", "tls"}, {"type", "network"}, {"sni", "servername", "serverName", "peer"},
		{"fp", "fingerprint"}, {"pbk", "publicKey"}, {"sid", "shortId"},
		{"serviceName", "service_name"}, {"obfs-password", "obfs_password"},
		{"allowInsecure", "allow_insecure", "skip-cert-verify", "insecure"},
	} {
		var previous string
		seen := false
		for _, alias := range aliases {
			if entries, ok := values[alias]; ok {
				if seen && entries[0] != previous {
					return Settings{}, errors.New("conflicting subscription aliases")
				}
				seen, previous = true, entries[0]
			}
		}
	}
	for _, key := range []string{"allowInsecure", "allow_insecure", "skip-cert-verify", "insecure", "zero_rtt_handshake"} {
		if entries, ok := values[key]; ok {
			switch strings.ToLower(entries[0]) {
			case "", "0", "false", "no", "off", "1", "true", "yes", "on":
			default:
				return Settings{}, errors.New("invalid subscription boolean")
			}
		}
	}
	security := strings.ToLower(firstQuery(values, "security", "tls"))
	network := strings.ToLower(firstQuery(values, "type", "network"))
	settings := Settings{
		Security:      security,
		Network:       network,
		SNI:           firstQuery(values, "sni", "servername", "serverName", "peer"),
		Host:          firstQuery(values, "host"),
		Path:          firstQuery(values, "path"),
		Flow:          firstQuery(values, "flow"),
		Fingerprint:   firstQuery(values, "fp", "fingerprint"),
		PublicKey:     firstQuery(values, "pbk", "publicKey"),
		ShortID:       firstQuery(values, "sid", "shortId"),
		ServiceName:   firstQuery(values, "serviceName", "service_name"),
		AllowInsecure: truthy(firstQuery(values, "allowInsecure", "allow_insecure", "skip-cert-verify", "insecure")),
		Obfs:          firstQuery(values, "obfs"), ObfsPassword: firstQuery(values, "obfs-password", "obfs_password"), CongestionControl: firstQuery(values, "congestion_control"), ZeroRTT: truthy(firstQuery(values, "zero_rtt_handshake")), SPKI: firstQuery(values, "spki"), CertificatePin: firstQuery(values, "pinSHA256"),
	}
	for key, value := range values {
		if len(value) == 0 || value[0] == "" {
			continue
		}
		switch key {
		case "security", "tls", "type", "network", "sni", "servername", "serverName", "peer", "host", "path", "flow", "fp", "fingerprint", "pbk", "publicKey", "sid", "shortId", "serviceName", "service_name", "allowInsecure", "allow_insecure", "skip-cert-verify", "insecure", "alpn", "obfs", "obfs-password", "obfs_password", "congestion_control", "zero_rtt_handshake", "spki", "pinSHA256":
		case "encryption":
			if value[0] != "none" {
				settings.UnsupportedFields = append(settings.UnsupportedFields, key)
			}
		case "headerType":
			if value[0] != "none" {
				settings.UnsupportedFields = append(settings.UnsupportedFields, key)
			}
		default:
			settings.UnsupportedFields = append(settings.UnsupportedFields, key)
		}
	}
	sort.Strings(settings.UnsupportedFields)
	if settings.SNI == "" && security != "" && security != "none" {
		settings.SNI = address
	}
	if security == "tls" {
		settings.TLS = true
	}
	if security == "reality" {
		settings.Reality = true
	}
	if alpn := firstQuery(values, "alpn"); alpn != "" {
		settings.ALPN = splitList(alpn)
	}
	return settings, nil
}

func validateFetchURL(ctx context.Context, rawURL string, allowPrivate bool) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return nil, err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("subscription url must be http or https")
	}
	host := parsed.Hostname()
	if host == "" {
		return nil, fmt.Errorf("subscription url host cannot be empty")
	}
	if _, err := resolveDialTarget(ctx, host, allowPrivate); err != nil {
		return nil, err
	}
	return parsed, nil
}

func resolveDialTarget(ctx context.Context, host string, allowPrivate bool) (netip.Addr, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return netip.Addr{}, fmt.Errorf("subscription url host cannot be empty")
	}
	if addr, err := netip.ParseAddr(strings.Trim(host, "[]")); err == nil {
		if !allowPrivate && !isPublicAddr(addr) {
			return netip.Addr{}, fmt.Errorf("subscription url resolves to private or local address")
		}
		return addr, nil
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return netip.Addr{}, err
	}
	if len(ips) == 0 {
		return netip.Addr{}, fmt.Errorf("subscription url host has no addresses")
	}
	for _, ipAddr := range ips {
		addr, ok := netip.AddrFromSlice(ipAddr.IP)
		if !ok {
			continue
		}
		if allowPrivate || isPublicAddr(addr) {
			return addr, nil
		}
	}
	return netip.Addr{}, fmt.Errorf("subscription url resolves to private or local address")
}

func isPublicAddr(addr netip.Addr) bool {
	if addr.Is4In6() {
		addr = addr.Unmap()
	}
	return addr.IsGlobalUnicast() && !addr.IsPrivate() && !addr.IsLoopback() && !addr.IsLinkLocalUnicast() && !addr.IsLinkLocalMulticast() && !addr.IsMulticast() && !addr.IsUnspecified()
}

func tryDecodeBase64(s string) (string, bool) {
	clean := strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\n', '\r', '\t':
			return -1
		default:
			return r
		}
	}, strings.TrimSpace(s))
	if clean == "" {
		return "", false
	}
	if b, err := base64.StdEncoding.DecodeString(padBase64(clean)); err == nil {
		return string(b), true
	}
	if b, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(clean, "=")); err == nil {
		return string(b), true
	}
	if b, err := base64.RawStdEncoding.DecodeString(strings.TrimRight(clean, "=")); err == nil {
		return string(b), true
	}
	return "", false
}

func padBase64(s string) string {
	if rem := len(s) % 4; rem != 0 {
		s += strings.Repeat("=", 4-rem)
	}
	return s
}

func firstQuery(values url.Values, keys ...string) string {
	for _, key := range keys {
		if value := values.Get(key); value != "" {
			return value
		}
	}
	return ""
}

func truthy(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func splitList(value string) []string {
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == '|' || r == ' ' })
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func slug(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "node"
	}
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(value) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case r > 127:
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
		if b.Len() >= 48 {
			break
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "node"
	}
	return out
}

func uniqueTag(base string, used map[string]bool) string {
	base = strings.TrimSpace(base)
	if base == "" {
		base = "sub-node"
	}
	tag := base
	for i := 2; used[tag]; i++ {
		tag = fmt.Sprintf("%s-%d", base, i)
	}
	return tag
}
