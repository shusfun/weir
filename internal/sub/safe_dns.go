package sub

import (
	"strings"

	js "github.com/shusfun/weir/internal/sub/jscompat"
)

// 移植自 functions/modules/subscription/safe-dns.js

const (
	DNSModeClean     = "clean"
	DNSModePolluted  = "polluted"
	DNSProxyGroup    = "🌐 DNS 出口"
	SingboxCNRuleSet = "geosite-cn"
)

var (
	defaultDomesticDNS = []string{"223.5.5.5", "119.29.29.29"}
	defaultForeignDNS  = []string{"udp://8.8.8.8:53", "udp://1.1.1.1:53"}
	defaultPollutedDNS = []string{"https://8.8.8.8/dns-query", "https://1.1.1.1/dns-query"}
	safeDNSFields      = []string{"cache-algorithm", "fake-ip-range", "fake-ip-filter-mode", "fake-ip-ttl", "use-hosts", "use-system-hosts"}
	reDNSHost          = js.MustRegExp(`^(?:(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}|(?:\d{1,3}\.){3}\d{1,3}|\[[0-9a-f:]+\])$`, "i")
	reDNSScheme        = js.MustRegExp(`^(?:udp|tcp|tls|https):\/\/`, "i")
	reLoopbackPrefix   = js.MustRegExp(`^127\.`, "")
	reZeroPrefix       = js.MustRegExp(`^0\.`, "")
)

// cloneValue 对应 safe-dns.js 的 clone（深拷贝对象与数组）。
func cloneValue(v any) any {
	switch x := v.(type) {
	case *js.Array:
		out := js.Arr()
		for _, item := range x.Items {
			out.Push(cloneValue(item))
		}
		return out
	case *js.Object:
		out := js.NewObject()
		for _, kv := range x.Entries() {
			out.Set(kv.Key, cloneValue(kv.Value))
		}
		return out
	}
	return v
}

func parseDNSOverride(raw any) *js.Object {
	if o, ok := raw.(*js.Object); ok {
		return o
	}
	s, ok := raw.(string)
	if !ok || js.Trim(s) == "" {
		return js.NewObject()
	}
	parsed, err := js.YAMLLoad(js.Trim(s))
	if err != nil {
		return js.NewObject()
	}
	o, ok := parsed.(*js.Object)
	if !ok {
		return js.NewObject()
	}
	if dns, ok := o.Get("dns").(*js.Object); ok {
		return dns
	}
	return o
}

func normalizeDNSMode(v any) string {
	if js.ToLower(js.Trim(strOr(v))) == DNSModePolluted {
		return DNSModePolluted
	}
	return DNSModeClean
}

func resolverHost(value any) string {
	raw := js.Trim(strOr(value))
	if raw == "" || strings.Contains(raw, "#") || raw == "system" {
		if raw == "system" {
			return raw
		}
		return ""
	}
	candidate := raw
	if !reDNSScheme.Test(raw) {
		candidate = "udp://" + raw
	}
	u, err := js.ParseURL(candidate)
	if err != nil {
		return ""
	}
	host := reBracketTrim.Replace(u.Hostname(), "")
	if !reDNSHost.Test(u.Hostname()) {
		return ""
	}
	if host == "localhost" || host == "::1" || reLoopbackPrefix.Test(host) || reZeroPrefix.Test(host) {
		return ""
	}
	return raw
}

func resolverList(value any, fallback []string) []string {
	var values []any
	switch x := value.(type) {
	case *js.Array:
		values = x.Items
	case string:
		values = []any{x}
	}
	var out []string
	for _, v := range values {
		if h := resolverHost(v); h != "" {
			out = append(out, h)
		}
	}
	if len(out) > 0 {
		return out
	}
	return append([]string(nil), fallback...)
}

func plainResolver(value string) string {
	raw := js.Trim(value)
	if raw == "" || raw == "system" {
		return ""
	}
	candidate := raw
	if !reDNSScheme.Test(raw) {
		candidate = "udp://" + raw
	}
	u, err := js.ParseURL(candidate)
	if err != nil {
		return ""
	}
	host := reBracketTrim.Replace(u.Hostname(), "")
	if !reDNSHost.Test(u.Hostname()) {
		return ""
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return "udp://" + host + ":53"
}

func policyInput(override *js.Object) *js.Object {
	if p, ok := override.Get("policy").(*js.Object); ok {
		return override.Clone().Assign(p)
	}
	return override
}

// DNSPolicy 对应 resolveDnsPolicy 的结果。
type DNSPolicy struct {
	Mode                        string
	Domestic, Foreign, Polluted []string
}

// Value 转换为 JS 对象。
func (p DNSPolicy) Value() *js.Object {
	return js.Obj("mode", p.Mode, "domestic", js.StrArr(p.Domestic), "foreign", js.StrArr(p.Foreign), "polluted", js.StrArr(p.Polluted))
}

// ResolveDNSPolicy 对应 resolveDnsPolicy。
func ResolveDNSPolicy(raw any, mode any) DNSPolicy {
	input := policyInput(parseDNSOverride(raw))
	m := normalizeDNSMode(js.Or(mode, input.Get("mode"), input.Get("dns-mode")))
	foreign := resolverList(js.Or(input.Get("foreign"), input.Get("foreignNameservers"), input.Get("foreign-nameserver"), input.Get("nameserver")), defaultForeignDNS)
	plainForeign := foreign
	if m == DNSModeClean {
		plainForeign = nil
		for _, f := range foreign {
			if p := plainResolver(f); p != "" {
				plainForeign = append(plainForeign, p)
			}
		}
	}
	if len(plainForeign) == 0 {
		plainForeign = append([]string(nil), defaultForeignDNS...)
	}
	return DNSPolicy{
		Mode:     m,
		Domestic: resolverList(js.Or(input.Get("domestic"), input.Get("domesticNameservers"), input.Get("domestic-nameserver"), input.Get("default-nameserver")), defaultDomesticDNS),
		Foreign:  plainForeign,
		Polluted: resolverList(js.Or(input.Get("polluted"), input.Get("pollutedNameservers"), input.Get("polluted-nameserver"), input.Get("fallback")), defaultPollutedDNS),
	}
}

func withProxy(value, proxyGroup string) string {
	raw := js.Trim(value)
	if raw == "" || raw == "system" {
		return raw
	}
	return raw + "#" + proxyGroup
}

func withProxyAll(values []string, proxyGroup string) *js.Array {
	a := js.Arr()
	for _, v := range values {
		a.Push(withProxy(v, proxyGroup))
	}
	return a
}

// DefaultDNSConfig 对应 DEFAULT_DNS_CONFIG。
func DefaultDNSConfig() *js.Object {
	return js.Obj(
		"enable", true,
		"ipv6", false,
		"enhanced-mode", "fake-ip",
		"fake-ip-range", "198.18.0.1/16",
		"fake-ip-filter-mode", "blacklist",
		"fake-ip-filter", strs2("geosite:private", "geosite:category-ntp", "*.lan", "*.local", "localhost", "*.arpa"),
		"use-hosts", true,
		"use-system-hosts", true,
		"respect-rules", true,
		"default-nameserver", js.StrArr(defaultDomesticDNS),
		"nameserver", withProxyAll(defaultForeignDNS, DNSProxyGroup),
		"nameserver-policy", js.Obj(
			"geosite:private", js.StrArr(defaultDomesticDNS),
			"geosite:cn", js.StrArr(defaultDomesticDNS),
			"geosite:geolocation-!cn", withProxyAll(defaultForeignDNS, DNSProxyGroup),
		),
		"proxy-server-nameserver", js.StrArr(defaultDomesticDNS),
		"direct-nameserver", js.StrArr(defaultDomesticDNS),
		"direct-nameserver-follow-policy", true,
		"fallback", js.Arr(),
		"fallback-filter", js.Obj("geoip", true, "geoip-code", "CN", "ipcidr", strs2("240.0.0.0/4", "0.0.0.0/32", "127.0.0.0/8", "100.64.0.0/10")),
	)
}

// IsExplicitDNSBlock 对应 isExplicitDnsBlock（按布尔值使用）。
func IsExplicitDNSBlock(override any) bool { return js.Truthy(isExplicitDNSBlockValue(override)) }

// isObjectValue 对应 safe-dns.js 的 isObject：`value && typeof value === 'object' && !Array.isArray(value)`。
func isObjectValue(v any) any {
	if !js.Truthy(v) {
		return v
	}
	_, ok := v.(*js.Object)
	return ok
}

// isExplicitDNSBlockValue 返回与上游完全一致的值（某些情况下为 undefined）。
func isExplicitDNSBlockValue(override any) any {
	o, ok := override.(*js.Object)
	if !ok {
		return false
	}
	if o.Get("enable") != nil || o.Get("enhanced-mode") != nil || o.Get("proxy-server-nameserver") != nil {
		return true
	}
	if !js.IsArray(o.Get("nameserver")) {
		return false
	}
	return isObjectValue(o.Get("nameserver-policy"))
}

// SafeDNSOptions 对应 resolveSafeDnsConfig / buildSingboxDnsConfig 的 options。
type SafeDNSOptions struct {
	Mode                    any
	ProxyGroup              string
	SubscriptionDNSPolicies []any
}

// ResolveSafeDNSConfig 对应 resolveSafeDnsConfig：没有显式 DNS 配置时生成防泄漏的默认 DNS。
func ResolveSafeDNSConfig(raw any, options SafeDNSOptions) *js.Object {
	rawOverride := parseDNSOverride(raw)
	if IsExplicitDNSBlock(rawOverride) {
		return cloneValue(rawOverride).(*js.Object)
	}
	policy := ResolveDNSPolicy(raw, options.Mode)
	proxyGroup := options.ProxyGroup
	if proxyGroup == "" {
		proxyGroup = DNSProxyGroup
	}
	foreign := policy.Foreign
	if policy.Mode == DNSModePolluted {
		foreign = policy.Polluted
	}
	dns := DefaultDNSConfig()
	dns.Set("default-nameserver", js.StrArr(policy.Domestic))
	dns.Set("nameserver", withProxyAll(foreign, proxyGroup))
	np := js.Obj("geosite:private", js.StrArr(policy.Domestic), "geosite:cn", js.StrArr(policy.Domestic))
	for _, item := range options.SubscriptionDNSPolicies {
		if p, ok := objGet(item, "policy").(*js.Object); ok && js.Truthy(item) {
			np.Assign(p)
		} else if js.Truthy(item) && js.Truthy(objGet(item, "domains")) && js.Truthy(objGet(item, "nameservers")) {
			np.Set(js.ToString(objGet(item, "domains")), objGet(item, "nameservers"))
		}
	}
	np.Set("geosite:geolocation-!cn", withProxyAll(foreign, proxyGroup))
	dns.Set("nameserver-policy", np)
	dns.Set("proxy-server-nameserver", js.StrArr(policy.Domestic))
	dns.Set("direct-nameserver", js.StrArr(policy.Domestic))
	if policy.Mode == DNSModePolluted {
		dns.Set("fallback", withProxyAll(foreign, proxyGroup))
	} else {
		dns.Set("fallback", js.Arr())
	}
	override := policyInput(rawOverride)
	for _, key := range safeDNSFields {
		v := override.Get(key)
		if v == nil {
			continue
		}
		if key == "fake-ip-filter-mode" {
			if s := js.ToString(v); s != "blacklist" && s != "whitelist" && s != "rule" {
				continue
			}
		}
		if key == "use-hosts" || key == "use-system-hosts" {
			dns.Set(key, js.Truthy(v))
			continue
		}
		if js.IsString(v) || js.IsNumber(v) {
			dns.Set(key, v)
		}
	}
	dns.Set("enable", true)
	dns.Set("ipv6", false)
	dns.Set("enhanced-mode", "fake-ip")
	dns.Set("respect-rules", true)
	return dns
}

func parseSingboxResolver(value, tag, detour string) *js.Object {
	raw := js.Trim(value)
	candidate := raw
	if !reDNSScheme.Test(raw) {
		candidate = "udp://" + raw
	}
	u, err := js.ParseURL(candidate)
	if err != nil {
		panic(err) // 上游在这里直接抛出异常
	}
	typ := strings.TrimSuffix(u.Protocol(), ":")
	server := reBracketTrim.Replace(u.Hostname(), "")
	port := js.ToNumber(u.PortString())
	if !js.Truthy(port) {
		switch typ {
		case "https":
			port = 443
		case "tls":
			port = 853
		default:
			port = 53
		}
	}
	result := js.Obj("tag", tag, "type", typ, "server", server, "server_port", port, "detour", detour)
	if typ == "https" {
		result.Set("path", js.Or(u.Pathname(), "/dns-query"))
	}
	if typ == "tls" {
		result.Set("tls", js.Obj("enabled", true, "server_name", server))
	}
	return result
}

// BuildSingboxDNSConfig 对应 buildSingboxDnsConfig。
func BuildSingboxDNSConfig(raw any, options SafeDNSOptions) *js.Object {
	policy := ResolveDNSPolicy(raw, options.Mode)
	proxyGroup := options.ProxyGroup
	if proxyGroup == "" {
		proxyGroup = DNSProxyGroup
	}
	foreign := policy.Foreign
	if policy.Mode == DNSModePolluted {
		foreign = policy.Polluted
	}
	servers := js.Arr()
	domesticTag, foreignTag := "dns-cn-1", "dns-foreign-1"
	for i, v := range policy.Domestic {
		s := parseSingboxResolver(v, "dns-cn-"+js.NumberToString(float64(i+1)), "DIRECT")
		if i == 0 {
			domesticTag = js.ToString(s.Get("tag"))
		}
		servers.Push(s)
	}
	for i, v := range foreign {
		s := parseSingboxResolver(v, "dns-foreign-"+js.NumberToString(float64(i+1)), proxyGroup)
		if i == 0 {
			foreignTag = js.ToString(s.Get("tag"))
		}
		servers.Push(s)
	}
	return js.Obj(
		"strategy", "prefer_ipv4",
		"servers", servers,
		"rules", js.Arr(
			js.Obj("rule_set", strs2(SingboxCNRuleSet), "action", "route", "server", domesticTag),
			js.Obj("domain_suffix", strs2(".cn", ".lan", ".local"), "action", "route", "server", domesticTag),
		),
		"final", foreignTag,
	)
}

// CloneDNSPolicy 对应 cloneDnsPolicy。
func CloneDNSPolicy(raw any, mode any) *js.Object { return ResolveDNSPolicy(raw, mode).Value() }
