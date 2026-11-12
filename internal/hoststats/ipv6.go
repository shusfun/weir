package hoststats

import (
	"net/netip"
	"os"
	"strings"
)

// ipv6Usable 只在非回环接口有全局单播地址且存在默认路由时返回可用。
// 只有 ::1、链路本地，或文件读不到、有地址但无法确认路由时，known 为假，调用方保持未知。
func ipv6Usable(procRoot string) (usable, known bool) {
	if procRoot == "" {
		procRoot = "/proc"
	}
	text, err := os.ReadFile(procRoot + "/net/if_inet6")
	if err != nil {
		return false, false
	}
	hasGlobal := false
	for _, line := range strings.Split(string(text), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 6 {
			continue
		}
		name := fields[len(fields)-1]
		if name == "lo" {
			continue
		}
		addr, ok := parseIfInet6(fields[0])
		if !ok || !addr.IsGlobalUnicast() || addr.IsPrivate() {
			continue
		}
		hasGlobal = true
		break
	}
	if !hasGlobal {
		return false, true
	}
	route, err := os.ReadFile(procRoot + "/net/ipv6_route")
	if err != nil {
		return false, false
	}
	for _, line := range strings.Split(string(route), "\n") {
		fields := strings.Fields(line)
		// 目的地址、前缀长度、下一跳、度量、接口。默认路由的目的是全 0 且前缀长度为 0。
		if len(fields) >= 2 && strings.Trim(fields[0], "0") == "" && fields[1] == "00" {
			return true, true
		}
	}
	return false, false
}

func parseIfInet6(hexAddr string) (netip.Addr, bool) {
	if len(hexAddr) != 32 {
		return netip.Addr{}, false
	}
	var raw [16]byte
	for i := 0; i < 16; i++ {
		value, ok := hexByte(hexAddr[i*2 : i*2+2])
		if !ok {
			return netip.Addr{}, false
		}
		raw[i] = value
	}
	return netip.AddrFrom16(raw), true
}

func hexByte(s string) (byte, bool) {
	var out byte
	for _, c := range s {
		out <<= 4
		switch {
		case c >= '0' && c <= '9':
			out |= byte(c - '0')
		case c >= 'a' && c <= 'f':
			out |= byte(c-'a') + 10
		case c >= 'A' && c <= 'F':
			out |= byte(c-'A') + 10
		default:
			return 0, false
		}
	}
	return out, true
}
