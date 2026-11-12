// Package diagnostic prepares bounded, non-executable evidence for code agents.
package diagnostic

import (
	"net/url"
	"regexp"
	"strings"
	"unicode"
)

const Removed = "[redacted]"

var (
	pem           = regexp.MustCompile(`(?s)-----BEGIN [^-]*PRIVATE KEY-----.*?(?:-----END [^-]*PRIVATE KEY-----|$)`)
	uuid          = regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`)
	address       = regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9+.-]*://[^\s<>"']+`)
	assignment    = regexp.MustCompile(`(?i)(?:["']?)(?:[a-z0-9_-]*(?:password|passwd|private[_-]?key|preshared[_-]?key|token|secret|api[_-]?key|uuid)|密码|令牌|私钥)(?:["']?)\s*[:=：]\s*(?:"[^"\r\n]*"|'[^'\r\n]*'|[^\s,;，；}]+)`)
	authorization = regexp.MustCompile(`(?i)\b(?:bearer|basic)\s+[a-z0-9+/_=.\-]+`)
	opaque        = regexp.MustCompile(`[A-Za-z0-9+/_=\-]{32,}`)
)

// Text is a second boundary after field allowlisting. URL paths and queries
// are omitted because subscription credentials can occupy arbitrary segments.
func Text(text string) string {
	text = pem.ReplaceAllString(text, Removed)
	text = address.ReplaceAllStringFunc(text, func(raw string) string {
		u, err := url.Parse(raw)
		if err != nil || u.Hostname() == "" {
			return Removed
		}
		switch strings.ToLower(u.Scheme) {
		case "http", "https", "socks", "socks5", "socks5h":
			return u.Scheme + "://" + u.Host + "/" + Removed
		default:
			// Proxy URIs may encode their entire credential in the authority.
			return Removed
		}
	})
	text = assignment.ReplaceAllString(text, Removed)
	text = authorization.ReplaceAllString(text, Removed)
	text = uuid.ReplaceAllString(text, Removed)
	text = opaque.ReplaceAllString(text, Removed)
	text = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, text)
	r := []rune(text)
	if len(r) > 8192 {
		return string(r[:8192]) + "\n[truncated]"
	}
	return text
}
