package subscription

// Field mapping: pinned MiSub 4952c16 functions/utils/url-to-clash.js (MIT).
// Reject ambiguous aliases and invalid keys instead of silently losing fields.
import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"unicode"
)

type SnellSettings struct {
	Version  int    `json:"version,omitempty"`
	Reuse    *bool  `json:"reuse,omitempty"`
	TFO      *bool  `json:"tfo,omitempty"`
	UDP      *bool  `json:"udp,omitempty"`
	ECN      *bool  `json:"ecn,omitempty"`
	Obfs     string `json:"obfs,omitempty"`
	ObfsHost string `json:"obfs_host,omitempty"`
}
type WireGuardSettings struct {
	PublicKey    string   `json:"public_key"`
	PresharedKey string   `json:"preshared_key,omitempty"`
	Address      []string `json:"address"`
	AllowedIPs   []string `json:"allowed_ips,omitempty"`
	Reserved     []int    `json:"reserved,omitempty"`
	MTU          int      `json:"mtu,omitempty"`
	DNS          []string `json:"dns,omitempty"`
	Keepalive    int      `json:"keepalive,omitempty"`
}

func protocolKey(v string) bool {
	b, e := base64.StdEncoding.DecodeString(v)
	return e == nil && len(b) == 32
}
func parseExtendedLink(raw, protocol string) (ParsedLink, error) {
	if len(raw) > 16384 {
		return ParsedLink{}, errors.New("extended protocol URI too large")
	}
	u, e := url.Parse(raw)
	if e != nil || u.Opaque != "" || u.Hostname() == "" || u.Path != "" {
		return ParsedLink{}, errors.New("invalid extended protocol URI")
	}
	if len(u.Hostname()) > 253 || strings.IndexFunc(u.Hostname(), func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }) >= 0 {
		return ParsedLink{}, errors.New("invalid extended protocol host")
	}
	port, e := strconv.Atoi(u.Port())
	if e != nil || port < 1 || port > 65535 {
		return ParsedLink{}, errors.New("invalid endpoint")
	}
	q, e := url.ParseQuery(u.RawQuery)
	if e != nil {
		return ParsedLink{}, errors.New("invalid query")
	}
	for _, v := range q {
		if len(v) != 1 || len(v[0]) > 4096 || strings.IndexFunc(v[0], unicode.IsControl) >= 0 {
			return ParsedLink{}, errors.New("invalid protocol parameter")
		}
	}
	allowed := map[string]bool{}
	keys := []string{"version", "reuse", "tfo", "udp-relay", "ecn", "obfs", "obfs-host", "psk", "password"}
	if protocol == "wireguard" {
		keys = []string{"publickey", "public-key", "presharedkey", "preshared-key", "address", "allowedips", "allowed-ips", "reserved", "mtu", "dns", "keepalive"}
	}
	for _, key := range keys {
		allowed[key] = true
	}
	for key := range q {
		if !allowed[key] {
			return ParsedLink{}, errors.New("unsupported protocol parameter")
		}
	}
	alias := func(a, b string) (string, error) {
		if q.Has(a) && q.Has(b) {
			return "", errors.New("ambiguous protocol parameter")
		}
		if q.Has(a) {
			return q.Get(a), nil
		}
		return q.Get(b), nil
	}
	credential := ""
	if u.User != nil {
		credential = userInfoString(u.User)
	}
	s := Settings{}
	if protocol == "snell" {
		psk, err := alias("psk", "password")
		if err != nil {
			return ParsedLink{}, err
		}
		if psk != "" && credential != "" {
			return ParsedLink{}, errors.New("ambiguous Snell PSK")
		}
		if credential == "" {
			credential = psk
		}
		if credential == "" || len(credential) > 1024 || strings.IndexFunc(credential, unicode.IsControl) >= 0 {
			return ParsedLink{}, errors.New("missing Snell PSK")
		}
		v := &SnellSettings{Obfs: q.Get("obfs"), ObfsHost: q.Get("obfs-host")}
		if q.Has("version") {
			v.Version, e = strconv.Atoi(q.Get("version"))
			if e != nil || v.Version < 1 || v.Version > 5 {
				return ParsedLink{}, errors.New("invalid Snell version")
			}
		}
		for key, dst := range map[string]**bool{"reuse": &v.Reuse, "tfo": &v.TFO, "udp-relay": &v.UDP, "ecn": &v.ECN} {
			if q.Has(key) {
				var b bool
				switch q.Get(key) {
				case "true", "1":
					b = true
				case "false", "0":
					b = false
				default:
					return ParsedLink{}, errors.New("invalid Snell boolean")
				}
				*dst = &b
			}
		}
		if v.Obfs != "" && v.Obfs != "http" && v.Obfs != "tls" || v.Obfs == "" && v.ObfsHost != "" {
			return ParsedLink{}, errors.New("invalid Snell obfuscation")
		}
		s.Snell = v
	} else {
		pub, err := alias("publickey", "public-key")
		if err != nil {
			return ParsedLink{}, err
		}
		psk, err := alias("presharedkey", "preshared-key")
		if err != nil {
			return ParsedLink{}, err
		}
		ips, err := alias("allowedips", "allowed-ips")
		if err != nil {
			return ParsedLink{}, err
		}
		if !protocolKey(credential) || !protocolKey(pub) || psk != "" && !protocolKey(psk) {
			return ParsedLink{}, errors.New("invalid WireGuard key")
		}
		v := &WireGuardSettings{PublicKey: pub, PresharedKey: psk}
		for key, dst := range map[string]*[]string{"address": &v.Address, "allowed-ips": &v.AllowedIPs} {
			value := q.Get("address")
			if key == "allowed-ips" {
				value = ips
			}
			if value == "" {
				continue
			}
			for _, part := range strings.Split(value, ",") {
				part = strings.TrimSpace(part)
				if _, e := netip.ParsePrefix(part); e != nil {
					return ParsedLink{}, errors.New("invalid WireGuard CIDR")
				}
				*dst = append(*dst, part)
			}
		}
		if len(v.Address) == 0 || len(v.Address) > 8 || len(v.AllowedIPs) > 32 {
			return ParsedLink{}, errors.New("invalid WireGuard address list")
		}
		for _, cidr := range v.Address {
			p, _ := netip.ParsePrefix(cidr)
			if p.Addr().IsUnspecified() || p.Addr().IsMulticast() {
				return ParsedLink{}, errors.New("invalid WireGuard local address")
			}
		}
		if q.Has("reserved") {
			parts := strings.Split(q.Get("reserved"), ",")
			if len(parts) != 3 {
				return ParsedLink{}, errors.New("invalid WireGuard reserved bytes")
			}
			for _, part := range parts {
				n, e := strconv.Atoi(strings.TrimSpace(part))
				if e != nil || n < 0 || n > 255 {
					return ParsedLink{}, errors.New("invalid WireGuard reserved bytes")
				}
				v.Reserved = append(v.Reserved, n)
			}
		}
		for key, dst := range map[string]*int{"mtu": &v.MTU, "keepalive": &v.Keepalive} {
			if q.Has(key) {
				n, e := strconv.Atoi(q.Get(key))
				if e != nil || key == "mtu" && (n < 576 || n > 9000) || key == "keepalive" && (n < 0 || n > 65535) {
					return ParsedLink{}, errors.New("invalid WireGuard numeric option")
				}
				*dst = n
			}
		}
		if q.Has("dns") {
			for _, part := range strings.Split(q.Get("dns"), ",") {
				part = strings.TrimSpace(part)
				if _, e := netip.ParseAddr(part); e != nil {
					return ParsedLink{}, errors.New("invalid WireGuard DNS")
				}
				v.DNS = append(v.DNS, part)
			}
			if len(v.DNS) > 8 {
				return ParsedLink{}, errors.New("excessive WireGuard DNS")
			}
		}
		s.WireGuard = v
	}
	b, _ := json.Marshal(s)
	n := ParsedLink{Protocol: protocol, Remark: u.Fragment, Address: u.Hostname(), Port: port, Password: credential, SettingsJSON: string(b), RawLink: raw}
	n.Identity = InventoryIdentity(raw)
	return n, nil
}
