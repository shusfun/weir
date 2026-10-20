package control

import (
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/imzyb/MiGate/internal/subscription"
)

// Pinned MiSub node-cache-service.js: at least ten cached nodes and below 30%.
// Enabled per source. A refused response never replaces last-good data or usage.
func suspiciousSourceDrop(previous, next int) bool {
	return previous >= 10 && next < previous && next*10 < previous*3
}

func sourceRequestAddress(data map[string]any, now time.Time) string {
	address := text(data["url"])
	if data["node_cache"] != true {
		return address
	}
	u, err := url.Parse(address)
	if err != nil {
		return address
	} // validated at save; fetch still validates.
	q := u.Query()
	q.Set("_t", strconv.FormatInt(now.UnixMilli(), 10))
	u.RawQuery = q.Encode()
	return u.String()
}

func realSourceNodes(nodes []subscription.ParsedLink) []subscription.ParsedLink {
	out := make([]subscription.ParsedLink, 0, len(nodes))
	for _, n := range nodes {
		host := strings.Trim(strings.ToLower(n.Address), "[]")
		ip := net.ParseIP(host)
		if host == "localhost" || (ip != nil && (ip.IsLoopback() || ip.IsUnspecified())) {
			continue
		}
		out = append(out, n)
	}
	return out
}
