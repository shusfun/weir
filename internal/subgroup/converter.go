package subgroup

import (
	"errors"
	"net/url"
	"strconv"
	"strings"

	"github.com/shusfun/weir/internal/publicnet"
)

// ConverterURL builds the subconverter-compatible /sub request. Inline nodes
// avoid giving a third party a reusable Weir subscription or callback URL.
func ConverterURL(endpoint, nodes, target string, options RenderOptions) (string, error) {
	u, err := publicnet.URL(endpoint)
	if err != nil || u.RawQuery != "" {
		return "", errors.New("converter_endpoint_invalid")
	}
	targets := map[string]string{"clash": "clash", "surge": "surge", "loon": "loon", "quanx": "quanx", "base64": "mixed", "v2ray": "v2ray", "trojan": "trojan"}
	backendTarget, ok := targets[target]
	if !ok {
		return "", errors.New("converter_target_unsupported")
	}
	if options.CustomDNS != "" || len(options.RegionOverrides) > 0 || options.DNSMode == "polluted" {
		return "", errors.New("converter_settings_unsupported")
	}
	q := url.Values{"target": {backendTarget}, "udp": {strconv.FormatBool(options.EnableUDP)}, "tfo": {strconv.FormatBool(options.EnableTFO)}, "scv": {strconv.FormatBool(options.SkipCertVerify)}, "emoji": {"false"}, "sort": {"false"}, "insert": {"false"}}
	if options.ExternalTemplate != "" {
		if !strings.HasPrefix(options.ExternalTemplate, "remote:") {
			return "", errors.New("converter_template_unsupported")
		}
		template := strings.TrimPrefix(options.ExternalTemplate, "remote:")
		if _, err := publicnet.URL(template); err != nil || !IsINIPath(template) {
			return "", errors.New("converter_template_unsupported")
		}
		q.Set("config", template)
	} else if options.RuleLevel != "none" {
		return "", errors.New("converter_template_required")
	} else {
		q.Set("list", "true")
	}
	if target == "clash" {
		q.Set("new_name", "true")
	}
	if target == "surge" {
		q.Set("ver", "4")
	}
	var links []string
	for _, link := range strings.Split(nodes, "\n") {
		link = strings.TrimSpace(link)
		if link == "" {
			continue
		}
		parsed, err := url.Parse(link)
		// URLs and data/file/script schemes would turn conversion into a
		// third-party fetch rather than converting the already resolved nodes.
		if err != nil || strings.ContainsAny(link, "|\r\x00") {
			return "", errors.New("converter_input_invalid")
		}
		switch parsed.Scheme {
		case "ss", "ssr", "vmess", "vless", "trojan", "socks", "socks5", "hysteria", "hysteria2", "hy2", "tuic", "snell", "anytls":
		default:
			return "", errors.New("converter_protocol_unsupported")
		}
		links = append(links, link)
	}
	if len(links) == 0 {
		return "", errors.New("converter_empty_input")
	}
	q.Set("url", strings.Join(links, "|"))
	u.RawQuery = q.Encode()
	if len(u.String()) > 64<<10 {
		return "", errors.New("converter_input_too_large")
	}
	return u.String(), nil
}
