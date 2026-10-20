package subscription

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

func wireGuardEndpointInventory(p map[string]any) (string, error) {
	if e := requireKeys(p, "type", "tag", "system", "address", "private_key", "mtu", "peers"); e != nil {
		return "", e
	}
	if p["type"] != "wireguard" || p["system"] != nil && p["system"] != false {
		return "", fmt.Errorf("unsupported endpoint")
	}
	peers, ok := p["peers"].([]any)
	if !ok || len(peers) != 1 {
		return "", fmt.Errorf("exactly one WireGuard peer required")
	}
	peer, e := objectValue(peers[0])
	if e != nil {
		return "", e
	}
	if e = requireKeys(peer, "address", "port", "public_key", "pre_shared_key", "allowed_ips", "reserved", "persistent_keepalive_interval"); e != nil {
		return "", e
	}
	clash := map[string]any{"type": "wireguard", "name": p["tag"], "server": peer["address"], "port": peer["port"], "private-key": p["private_key"], "public-key": peer["public_key"], "ip": p["address"]}
	for field, key := range map[string]string{"pre_shared_key": "preshared-key", "allowed_ips": "allowed-ips", "reserved": "reserved", "persistent_keepalive_interval": "persistent-keepalive"} {
		if v, ok := peer[field]; ok {
			clash[key] = v
		}
	}
	if v, ok := p["mtu"]; ok {
		clash["mtu"] = v
	}
	return extendedInventory(clash, "clash")
}

func extendedInventory(p map[string]any, format string) (string, error) {
	kind := stringValue(p["type"])
	if format != "clash" {
		return "", fmt.Errorf("extended inventory format not adapted")
	}
	allowed := []string{"name", "type", "server", "port"}
	if kind == "snell" {
		if v, ok := p["version"]; ok {
			copy := map[string]any{}
			for k, value := range p {
				copy[k] = value
			}
			copy["version"] = fmt.Sprint(v)
			p = copy
		}
		allowed = append(allowed, "psk", "version", "reuse", "tfo", "udp", "ecn", "obfs-opts")
	} else {
		allowed = append(allowed, "private-key", "public-key", "preshared-key", "ip", "ipv6", "allowed-ips", "reserved", "mtu", "dns", "persistent-keepalive", "remote-dns-resolve", "udp")
	}
	if e := requireKeys(p, allowed...); e != nil {
		return "", e
	}
	q := url.Values{}
	credential := ""
	if kind == "snell" {
		credential = stringValue(p["psk"])
		for _, key := range []string{"version", "reuse", "tfo", "ecn"} {
			if v, ok := p[key]; ok {
				q.Set(key, fmt.Sprint(v))
			}
		}
		if v, ok := p["udp"]; ok {
			q.Set("udp-relay", fmt.Sprint(v))
		}
		if raw, ok := p["obfs-opts"]; ok {
			obfs, e := objectValue(raw)
			if e != nil {
				return "", e
			}
			if e = requireKeys(obfs, "mode", "host"); e != nil {
				return "", e
			}
			for field, key := range map[string]string{"mode": "obfs", "host": "obfs-host"} {
				if v, ok := obfs[field]; ok {
					t, valid := v.(string)
					if !valid {
						return "", fmt.Errorf("invalid Snell obfuscation")
					}
					q.Set(key, t)
				}
			}
		}
	} else {
		credential = stringValue(p["private-key"])
		for field, key := range map[string]string{"public-key": "public-key", "preshared-key": "preshared-key", "mtu": "mtu", "persistent-keepalive": "keepalive"} {
			if v, ok := p[field]; ok {
				q.Set(key, fmt.Sprint(v))
			}
		}
		addresses, e := stringList(p["ip"])
		if e != nil {
			return "", e
		}
		v6, e := stringList(p["ipv6"])
		if e != nil {
			return "", e
		}
		for _, v := range v6 {
			found := false
			for _, a := range addresses {
				found = found || a == v
			}
			if !found {
				addresses = append(addresses, v)
			}
		}
		q.Set("address", strings.Join(addresses, ","))
		for field, key := range map[string]string{"allowed-ips": "allowed-ips", "dns": "dns"} {
			if v, ok := p[field]; ok {
				list, e := stringList(v)
				if e != nil {
					return "", e
				}
				q.Set(key, strings.Join(list, ","))
			}
		}
		if v, ok := p["reserved"]; ok {
			b, _ := json.Marshal(v)
			var ns []int
			if json.Unmarshal(b, &ns) != nil {
				return "", fmt.Errorf("invalid reserved bytes")
			}
			values := []string{}
			for _, n := range ns {
				values = append(values, strconv.Itoa(n))
			}
			q.Set("reserved", strings.Join(values, ","))
		}
		for _, field := range []string{"udp", "remote-dns-resolve"} {
			if v, ok := p[field]; ok && v != true {
				return "", fmt.Errorf("unsupported WireGuard option")
			}
		}
	}
	port, e := strconv.Atoi(fmt.Sprint(p["port"]))
	if e != nil {
		return "", fmt.Errorf("invalid port")
	}
	u := url.URL{Scheme: kind, User: url.User(credential), Host: net.JoinHostPort(stringValue(p["server"]), strconv.Itoa(port)), RawQuery: q.Encode(), Fragment: stringValue(p["name"])}
	link := u.String()
	if _, e := ParseLink(link); e != nil {
		return "", e
	}
	return link, nil
}
