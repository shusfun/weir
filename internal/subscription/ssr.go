package subscription

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type SSRSettings struct {
	Protocol      string `json:"protocol"`
	ProtocolParam string `json:"protocol_param,omitempty"`
	Obfs          string `json:"obfs"`
	ObfsParam     string `json:"obfs_param,omitempty"`
}

func decodeSSR(value string) (string, error) {
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(value, "="))
	if err != nil || !utf8.Valid(b) || len(b) > 8192 || strings.IndexFunc(string(b), unicode.IsControl) >= 0 {
		return "", errors.New("invalid SSR encoding")
	}
	return string(b), nil
}

func parseSSRLink(raw string) (ParsedLink, error) {
	if len(raw) > 16384 {
		return ParsedLink{}, errors.New("SSR URI too large")
	}
	body, err := decodeSSR(raw[len("ssr://"):])
	if err != nil {
		return ParsedLink{}, err
	}
	endpoint, query, _ := strings.Cut(body, "/?")
	// Split from the right: bracketed IPv6 addresses contain colons too.
	parts := make([]string, 6)
	for i := 5; i > 0; i-- {
		at := strings.LastIndexByte(endpoint, ':')
		if at < 0 {
			return ParsedLink{}, errors.New("invalid SSR endpoint")
		}
		parts[i], endpoint = endpoint[at+1:], endpoint[:at]
	}
	parts[0] = strings.Trim(endpoint, "[]")
	port, err := strconv.Atoi(parts[1])
	if err != nil || port < 1 || port > 65535 || parts[0] == "" || len(parts[0]) > 253 || strings.ContainsAny(parts[0], "/ @?#") || strings.Contains(parts[0], ":") && net.ParseIP(parts[0]) == nil {
		return ParsedLink{}, errors.New("invalid SSR address")
	}
	for _, value := range parts[2:5] {
		if value == "" || len(value) > 64 || strings.IndexFunc(value, func(r rune) bool {
			return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.')
		}) >= 0 {
			return ParsedLink{}, errors.New("invalid SSR protocol option")
		}
	}
	password, err := decodeSSR(parts[5])
	if err != nil || password == "" {
		return ParsedLink{}, errors.New("invalid SSR password")
	}
	q, err := url.ParseQuery(query)
	if err != nil {
		return ParsedLink{}, errors.New("invalid SSR parameters")
	}
	decoded := map[string]string{}
	for key, values := range q {
		if (key != "obfsparam" && key != "protoparam" && key != "remarks" && key != "group") || len(values) != 1 {
			return ParsedLink{}, errors.New("unsupported or ambiguous SSR parameter")
		}
		value, err := decodeSSR(values[0])
		if err != nil {
			return ParsedLink{}, err
		}
		decoded[key] = value
	}
	settings, _ := json.Marshal(Settings{Method: parts[3], SSR: &SSRSettings{Protocol: parts[2], ProtocolParam: decoded["protoparam"], Obfs: parts[4], ObfsParam: decoded["obfsparam"]}})
	return ParsedLink{Identity: InventoryIdentity(raw), Protocol: "ssr", Remark: decoded["remarks"], Address: parts[0], Port: port, Password: password, RawLink: raw, SettingsJSON: string(settings)}, nil
}
