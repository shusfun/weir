package subout

import (
	"encoding/json"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/imzyb/MiGate/internal/subscription"
)

// Ported from pinned MiSub's Surge/Loon/QuanX generators (MIT). Unsupported
// combinations fail, including fields that upstream would silently discard.
var iniNameSpace = regexp.MustCompile(`\s+`)

func iniNodeName(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		switch r {
		case ',', ';', '[', ']', '"', '\'':
			return ' '
		case '=':
			return '-'
		}
		return r
	}, s)
	s = strings.TrimSpace(iniNameSpace.ReplaceAllString(s, " "))
	if s == "" {
		return "Untitled"
	}
	return s
}
func iniValue(s string) (string, error) {
	if strings.IndexFunc(s, unicode.IsControl) >= 0 || strings.ContainsAny(s, "\\;#") {
		return "", fmt.Errorf("INI value cannot safely represent delimiter")
	}
	if strings.ContainsAny(s, ", \t\"=") {
		return "\"" + strings.ReplaceAll(s, "\"", "\\\"") + "\"", nil
	}
	return s, nil
}
func iniOutput(links []string, target string) ([]byte, error) {
	if target != "surge" && target != "loon" && target != "quanx" {
		return nil, fmt.Errorf("unsupported INI target")
	}
	parsed, err := subscription.ParseLinks(links)
	if err != nil || len(parsed.Skipped) > 0 {
		return nil, fmt.Errorf("invalid INI node")
	}
	names := []string{}
	lines := []string{}
	sections := []string{}
	used := map[string]bool{"PROXY": true, "DIRECT": true, "REJECT": true, "direct": true, "reject": true}
	for _, n := range parsed.Nodes {
		var s subscription.Settings
		if err = json.Unmarshal([]byte(n.SettingsJSON), &s); err != nil {
			return nil, err
		}
		if !supportedSubscriptionSettings(n.Protocol, s) || s.Network == "grpc" || s.SPKI != "" || s.CertificatePin != "" || s.Obfs != "" || s.CongestionControl != "" || s.ZeroRTT {
			return nil, fmt.Errorf("unsupported INI protocol fields")
		}
		if s.Fingerprint != "" && target != "loon" || len(s.ALPN) > 1 || len(s.ALPN) > 0 && target != "loon" || s.Reality && target != "loon" {
			return nil, fmt.Errorf("unsupported INI TLS fields")
		}
		if s.Flow != "" && target != "loon" {
			return nil, fmt.Errorf("unsupported INI flow")
		}
		if strings.ContainsAny(n.Address, "\r\n,;#[] ") {
			return nil, fmt.Errorf("invalid INI address")
		}
		name := uniqueNodeName(iniNodeName(n.Remark), used)
		names = append(names, name)
		if n.Protocol == "snell" {
			line, e := snellINI(n, s, name, target)
			if e != nil {
				return nil, e
			}
			lines = append(lines, line)
			continue
		}
		if n.Protocol == "wireguard" {
			line, section, e := wireGuardINI(n, s, name, target)
			if e != nil {
				return nil, e
			}
			lines = append(lines, line)
			if section != "" {
				sections = append(sections, section)
			}
			continue
		}
		values := []string{n.Username, n.Password, s.SNI, s.Host, s.Path, s.PublicKey, s.ShortID, s.Fingerprint, s.Flow, s.Method}
		quoted := make([]string, len(values))
		for i, v := range values {
			quoted[i], err = iniValue(v)
			if err != nil {
				return nil, err
			}
		}
		user, password, sni, host, path, pbk, sid, fp, flow, method := quoted[0], quoted[1], quoted[2], quoted[3], quoted[4], quoted[5], quoted[6], quoted[7], quoted[8], quoted[9]
		parts := []string{}
		kind := n.Protocol
		if target == "quanx" {
			// QuanX doesn't define a general quoting mechanism for secrets. Do
			// not emit ambiguous commas or encode a different password.
			for _, v := range values {
				if strings.ContainsAny(v, ", \t\"=") {
					return nil, fmt.Errorf("QuanX value cannot be represented losslessly")
				}
			}
			endpoint := net.JoinHostPort(n.Address, strconv.Itoa(n.Port))
			switch kind {
			case "shadowsocks":
				parts = []string{"shadowsocks=" + endpoint, "method=" + method, "password=" + password, "udp-relay=true"}
			case "vmess":
				if method == "auto" || method == "" {
					method = "none"
				}
				parts = []string{"vmess=" + endpoint, "method=" + method, "password=" + user}
			case "trojan":
				if !s.TLS && s.Security != "tls" {
					return nil, fmt.Errorf("QuanX Trojan requires TLS")
				}
				parts = []string{"trojan=" + endpoint, "password=" + password}
			case "http":
				parts = []string{"http=" + endpoint, "username=" + user, "password=" + password}
			case "anytls":
				parts = []string{"anytls=" + endpoint, "password=" + password}
			default:
				return nil, fmt.Errorf("protocol unsupported by QuanX conversion")
			}
			if s.Network == "ws" {
				if kind == "vmess" && sni != "" && host != "" && sni != host {
					return nil, fmt.Errorf("QuanX WS distinct SNI/Host cannot be preserved")
				}
				if kind != "vmess" && kind != "trojan" {
					return nil, fmt.Errorf("unsupported QuanX WS")
				}
				obfs := "ws"
				if s.TLS && kind != "trojan" {
					obfs = "wss"
				}
				parts = append(parts, "obfs="+obfs)
				if path != "" {
					parts = append(parts, "obfs-uri="+path)
				}
				if host != "" {
					parts = append(parts, "obfs-host="+host)
				} else if kind == "vmess" && sni != "" {
					parts = append(parts, "obfs-host="+sni)
				}
			} else if s.TLS {
				parts = append(parts, "over-tls=true")
			}
			if sni != "" && !(kind == "vmess" && s.Network == "ws") {
				parts = append(parts, "tls-host="+sni)
			}
			if s.AllowInsecure {
				parts = append(parts, "tls-verification=false")
			}
			if n.Protocol == "anytls" {
				if !s.AllowInsecure {
					parts = append(parts, "tls-verification=true")
				}
				parts = append(parts, "fast-open=false", "udp-relay=true")
			}
			parts = append(parts, "tag="+name)
			lines = append(lines, strings.Join(parts, ", "))
			continue
		}
		if kind == "shadowsocks" {
			kind = "ss"
			if target == "loon" {
				kind = "Shadowsocks"
			}
		}
		if kind == "vless" && target == "surge" {
			return nil, fmt.Errorf("Surge does not support VLESS")
		}
		if kind == "http" || kind == "socks5" {
			if target == "loon" {
				return nil, fmt.Errorf("Loon HTTP/SOCKS conversion not adapted")
			}
			if s.TLS {
				if kind == "http" {
					kind = "https"
				} else {
					kind = "socks5-tls"
				}
			}
		}
		parts = []string{name + " = " + kind, n.Address, strconv.Itoa(n.Port)}
		if target == "surge" {
			switch n.Protocol {
			case "shadowsocks":
				parts = append(parts, "encrypt-method="+method, "password="+password, "udp-relay=true")
			case "vmess":
				parts = append(parts, "username="+user)
				if method != "" && method != "auto" {
					parts = append(parts, "encrypt-method="+method)
				}
				if s.TLS {
					parts = append(parts, "tls=true")
				}
				parts = append(parts, "vmess-aead=true")
			case "trojan", "anytls", "hysteria2":
				parts = append(parts, "password="+password)
			case "http", "socks5":
				parts = append(parts, user, password)
			default:
				return nil, fmt.Errorf("unsupported Surge protocol")
			}
		} else {
			switch n.Protocol {
			case "shadowsocks":
				parts = append(parts, method, password, "udp-relay=true")
			case "vmess":
				parts = append(parts, method, strconv.Quote(n.Username), "alterId=0")
				if s.TLS {
					parts = append(parts, "over-tls=true")
				}
			case "vless":
				parts = append(parts, strconv.Quote(n.Username))
				if flow != "" {
					parts = append(parts, "flow="+flow)
				}
				network := s.Network
				if network == "" {
					network = "tcp"
				}
				parts = append(parts, "transport="+network)
				if s.TLS || s.Reality {
					parts = append(parts, "over-tls=true")
				}
				if s.Reality {
					parts = append(parts, "reality=true", "public-key="+pbk, "short-id="+sid)
				}
			case "trojan", "anytls", "hysteria2":
				parts = append(parts, password)
			default:
				return nil, fmt.Errorf("unsupported Loon protocol")
			}
		}
		if s.Network == "ws" {
			if n.Protocol != "vmess" && n.Protocol != "vless" && n.Protocol != "trojan" {
				return nil, fmt.Errorf("unsupported INI WS")
			}
			if target == "surge" {
				parts = append(parts, "ws=true")
				if path != "" {
					parts = append(parts, "ws-path="+path)
				}
				if host != "" {
					parts = append(parts, "ws-headers=Host:"+host)
				}
			} else {
				if n.Protocol != "vless" {
					parts = append(parts, "transport=ws")
				}
				if path != "" {
					parts = append(parts, "path="+path)
				}
				if host != "" {
					parts = append(parts, "host="+host)
				}
			}
		}
		if sni != "" {
			parts = append(parts, "sni="+sni)
		}
		if s.AllowInsecure {
			parts = append(parts, "skip-cert-verify=true")
		}
		if len(s.ALPN) == 1 {
			alpn, e := iniValue(s.ALPN[0])
			if e != nil {
				return nil, e
			}
			parts = append(parts, "alpn="+alpn)
		}
		if fp != "" {
			parts = append(parts, "client-fingerprint="+fp)
		}
		lines = append(lines, strings.Join(parts, ", "))
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("no eligible INI nodes")
	}
	if target == "quanx" {
		return []byte("[server_local]\n" + strings.Join(lines, "\n") + "\n\n[policy]\nstatic=PROXY, " + strings.Join(names, ", ") + "\n\n[filter_local]\nfinal, PROXY\n"), nil
	}
	return []byte("[General]\n\n[Proxy]\n" + strings.Join(lines, "\n") + "\n\n[Proxy Group]\nPROXY = select, " + strings.Join(names, ", ") + "\n\n[Rule]\nFINAL, PROXY\n\n" + strings.Join(sections, "\n\n")), nil
}
