package kernelconf

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"

	"github.com/shusfun/weir/internal/kernelconf/model"
	"github.com/shusfun/weir/packages/contracts"
)

// NativeOutbound includes the model's constraints as well as the selected
// driver's templates. A canonical client may carry parameters absent in that model.
func NativeOutbound(d Driver, o contracts.Outbound) bool {
	if o.Settings["udp_over_tcp"] == true && (d.Kernel != "sing-box" || !d.Has("udp-over-tcp")) {
		return false
	}
	if o.ClientOnly || !model.OutboundSupportsCore(model.Outbound{Protocol: o.Protocol}, d.Kernel) {
		return false
	}
	network, _ := o.Settings["network"].(string)
	return d.OutboundAllows(model.NormalizeOutboundProtocol(o.Protocol), network) == nil
}

func ClientOutbound(d Driver, o contracts.Outbound) bool {
	if d.Kernel != "mihomo" || !d.Has("hosted-outbounds") || len(o.Client) == 0 {
		return false
	}
	network, _ := o.Settings["network"].(string)
	return d.OutboundAllows(model.NormalizeOutboundProtocol(o.Protocol), network) == nil
}

// ExpandedOutbounds uses the same deterministic group member tags as Build.
func ExpandedOutbounds(cfg contracts.GateConfig) ([]contracts.Outbound, error) {
	expanded, _, err := expandGroups(cfg)
	return expanded.Outbounds, err
}

type bridgeView struct {
	plan   contracts.OutboundBridge
	target contracts.Outbound
	tag    string
}

func bridgeViews(cfg contracts.GateConfig) ([]bridgeView, error) {
	outs := map[string]contracts.Outbound{}
	usedPorts := map[int]bool{cfg.Ports.XrayStats: true, cfg.Ports.SingboxStats: true}
	for _, in := range cfg.Inbounds {
		usedPorts[in.Port] = true
	}
	for _, o := range cfg.Outbounds {
		outs[o.Tag] = o
	}
	seen := map[string]bool{}
	var views []bridgeView
	for i, p := range cfg.Bridges {
		key := p.FromKernel + ":" + p.OutboundTag
		o, ok := outs[p.OutboundTag]
		if !ok || seen[key] || (p.FromKernel != "xray" && p.FromKernel != "sing-box") ||
			(p.Kernel != "sing-box" && p.Kernel != "mihomo") || p.FromKernel == p.Kernel ||
			p.Port < 1024 || p.Port > 65535 || usedPorts[p.Port] || len(p.Username) < 8 || len(p.Password) < 24 {
			return nil, errors.New("invalid or conflicting outbound bridge")
		}
		if p.FromKernel == "sing-box" && p.Kernel != "mihomo" {
			return nil, errors.New("outbound bridge cycle")
		}
		d, err := DriverFor(cfg, p.Kernel)
		if err != nil {
			return nil, err
		}
		if d == nil {
			return nil, fmt.Errorf("%s auxiliary driver missing", p.Kernel)
		}
		if p.Kernel == "sing-box" && (!d.Has("hosted-outbounds") || !NativeOutbound(*d, o)) ||
			p.Kernel == "mihomo" && !ClientOutbound(*d, o) {
			return nil, fmt.Errorf("%s cannot express bridge target %q", p.Kernel, o.Tag)
		}
		usedPorts[p.Port], seen[key] = true, true
		views = append(views, bridgeView{p, o, fmt.Sprintf("weir-bridge-%d", i+1)})
	}
	return views, nil
}

func bridgedConfig(cfg contracts.GateConfig, kernel string, bridges []bridgeView) contracts.GateConfig {
	next := cfg
	next.Outbounds = append([]contracts.Outbound(nil), cfg.Outbounds...)
	for i, o := range next.Outbounds {
		for _, b := range bridges {
			if b.plan.FromKernel == kernel && b.plan.OutboundTag == o.Tag {
				next.Outbounds[i] = contracts.Outbound{RuleID: o.RuleID, ID: o.ID, Tag: o.Tag, Protocol: "socks", Address: "127.0.0.1", Port: b.plan.Port, Username: b.plan.Username, Password: b.plan.Password}
				break
			}
		}
	}
	return next
}

func bridgeVars(b bridgeView) map[string]any {
	return map[string]any{"tag": b.tag, "port": b.plan.Port, "username": b.plan.Username, "password": b.plan.Password, "target": b.tag + "-target"}
}

// appendSingboxBridges uses the driver's dedicated templates. These routes go
// first so generic user routes cannot intercept traffic from the helper listener.
func appendSingboxBridges(v *view, bridges []bridgeView) error {
	var routes []map[string]any
	for _, b := range bridges {
		if b.plan.Kernel != "sing-box" {
			continue
		}
		targetCfg := contracts.GateConfig{Outbounds: []contracts.Outbound{b.target}}
		outs, _, err := toOutbounds(targetCfg)
		if err != nil {
			return err
		}
		target, err := buildView("sing-box", nil, outs[1:], nil, nil)
		if err != nil {
			return err
		}
		if len(target.Outbounds) != 1 {
			return errors.New("auxiliary target was omitted")
		}
		ob := target.Outbounds[0]
		ob["tag"] = b.tag + "-target"
		v.Outbounds = append(v.Outbounds, ob)
		v.Bridges = append(v.Bridges, bridgeVars(b))
		routes = append(routes, map[string]any{"outbound": b.tag + "-target", "inbound": b.tag})
	}
	v.Routes = append(routes, v.Routes...)
	return nil
}

func renderMihomoBridges(cfg contracts.GateConfig, bridges []bridgeView, opt Options) ([]byte, KernelArgs, error) {
	var items []map[string]any
	for _, b := range bridges {
		if b.plan.Kernel != "mihomo" {
			continue
		}
		m := bridgeVars(b)
		// The client converter owns the proxy schema. Never mutate the shared
		// canonical object when assigning this listener's private target name.
		client := map[string]any{}
		for k, value := range b.target.Client {
			client[k] = value
		}
		client["name"] = b.tag + "-target"
		m["client"] = client
		items = append(items, m)
	}
	if len(items) == 0 {
		return nil, KernelArgs{}, nil
	}
	d, err := DriverFor(cfg, "mihomo")
	if err != nil || d == nil {
		return nil, KernelArgs{}, errors.New("mihomo auxiliary driver missing")
	}
	ports := map[int]bool{}
	for _, b := range bridges {
		if b.plan.Kernel == "mihomo" {
			ports[b.plan.Port] = true
		}
	}
	observer, err := observerVars(*d, cfg.Observers["mihomo"])
	if err != nil {
		return nil, KernelArgs{}, err
	}
	if o := cfg.Observers["mihomo"]; o.Port > 0 {
		ports[o.Port] = true
	}
	body, err := render(*d, view{Bridges: items, Observer: observer}, 0, ports, opt)
	if err != nil {
		return nil, KernelArgs{}, err
	}
	var doc map[string]any
	if err = validateObserverPlan(body, "mihomo", cfg.Observers["mihomo"]); err != nil {
		return nil, KernelArgs{}, err
	}
	if err = json.Unmarshal(body, &doc); err != nil {
		return nil, KernelArgs{}, err
	}
	expectedProxies, expectedListeners := []any{}, []any{}
	for _, item := range items {
		expectedProxies = append(expectedProxies, item["client"])
		expectedListeners = append(expectedListeners, map[string]any{"name": item["tag"], "type": "socks", "listen": "127.0.0.1", "port": item["port"], "udp": true, "users": []any{map[string]any{"username": item["username"], "password": item["password"]}}, "proxy": item["target"]})
	}
	if !sameJSON(doc["proxies"], expectedProxies) || !sameJSON(doc["listeners"], expectedListeners) {
		return nil, KernelArgs{}, errors.New("mihomo auxiliary driver changed the requested target or listener")
	}
	return body, KernelArgs{Run: d.Run, Check: d.Check}, nil
}

func sameJSON(a, b any) bool {
	ab, ae := json.Marshal(a)
	bb, be := json.Marshal(b)
	return ae == nil && be == nil && string(ab) == string(bb)
}

func validateMihomoBridgeConfig(root map[string]any, ports map[int]bool) error {
	// No DNS listener, TUN, provider, persistence, or implicit direct selection.
	allowed := map[string]bool{"mode": true, "allow-lan": true, "bind-address": true, "log-level": true, "ipv6": true, "listeners": true, "proxies": true, "rules": true, "external-controller": true, "secret": true}
	for key := range root {
		if !allowed[key] {
			return fmt.Errorf("mihomo auxiliary field %s is not allowed", key)
		}
	}
	if root["allow-lan"] != false || root["bind-address"] != "127.0.0.1" {
		return errors.New("mihomo must bind loopback only")
	}
	proxies, _ := root["proxies"].([]any)
	names := map[string]bool{}
	for _, value := range proxies {
		p, ok := value.(map[string]any)
		if !ok {
			return errors.New("invalid auxiliary proxy")
		}
		name, _ := p["name"].(string)
		typ, _ := p["type"].(string)
		if name == "" || names[name] || typ == "direct" || typ == "reject" {
			return errors.New("invalid auxiliary target")
		}
		for _, k := range []string{"dialer-proxy", "interface-name", "routing-mark", "certificate", "private-key-file"} {
			if _, ok := p[k]; ok {
				return fmt.Errorf("auxiliary proxy field %s is not allowed", k)
			}
		}
		names[name] = true
	}
	listeners, _ := root["listeners"].([]any)
	seen := map[int]bool{}
	for _, value := range listeners {
		l, _ := value.(map[string]any)
		port, _ := num(l["port"])
		proxy, _ := l["proxy"].(string)
		users, _ := l["users"].([]any)
		if l["type"] != "socks" || l["listen"] != "127.0.0.1" || !ports[port] || seen[port] || !names[proxy] || len(users) != 1 {
			return errors.New("invalid auxiliary listener")
		}
		u, _ := users[0].(map[string]any)
		username, _ := u["username"].(string)
		password, _ := u["password"].(string)
		if len(username) < 8 || len(password) < 24 {
			return errors.New("auxiliary listener requires authentication")
		}
		seen[port] = true
	}
	if listen, ok := root["external-controller"]; ok {
		if err := validateObserverAddress(listen, root["secret"], ports); err != nil {
			return err
		}
		_, p, _ := net.SplitHostPort(fmt.Sprint(listen))
		port, _ := strconv.Atoi(p)
		if seen[port] {
			return errors.New("native observer port conflict")
		}
		seen[port] = true
	} else if root["secret"] != nil {
		return errors.New("native observer controller missing")
	}
	if len(seen) != len(ports) {
		return errors.New("auxiliary listener omitted")
	}
	rules, _ := root["rules"].([]any)
	if len(rules) != 1 || rules[0] != "MATCH,REJECT" {
		return errors.New("auxiliary fallback must reject")
	}
	return nil
}

func validateBridgeListeners(body []byte, bridges []bridgeView) error {
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		return err
	}
	ins, _ := doc["inbounds"].([]any)
	route, _ := doc["route"].(map[string]any)
	rules, _ := route["rules"].([]any)
	outs, _ := doc["outbounds"].([]any)
	ruleIndex := 0
	// This non-routing action observes metadata without intercepting the helper.
	if len(rules) > 0 && sameJSON(rules[0], observerSniffRule()) {
		ruleIndex++
	}
	for _, b := range bridges {
		if b.plan.Kernel != "sing-box" {
			continue
		}
		if ruleIndex >= len(rules) || !sameJSON(rules[ruleIndex], map[string]any{"inbound": []any{b.tag}, "outbound": b.tag + "-target"}) {
			return errors.New("sing-box auxiliary route missing or intercepted")
		}
		ruleIndex++
		var target map[string]any
		for _, item := range outs {
			o, _ := item.(map[string]any)
			if o["tag"] == b.tag+"-target" {
				if target != nil {
					return errors.New("duplicate auxiliary target")
				}
				target = o
			}
		}
		if target == nil || target["type"] == "direct" || target["type"] == "block" {
			return errors.New("sing-box auxiliary target missing")
		}
		port, _ := num(target["server_port"])
		if target["server"] != b.target.Address || port != b.target.Port {
			return errors.New("sing-box auxiliary driver changed the requested server")
		}
		for _, key := range []string{"detour", "bind_interface", "routing_mark"} {
			if _, ok := target[key]; ok {
				return errors.New("sing-box auxiliary target redirects its connection")
			}
		}
		found := false
		for _, item := range ins {
			in, _ := item.(map[string]any)
			if in["tag"] != b.tag {
				continue
			}
			port, _ := num(in["listen_port"])
			users, _ := in["users"].([]any)
			if in["listen"] != "127.0.0.1" || port != b.plan.Port || in["type"] != "socks" || len(users) != 1 {
				return errors.New("sing-box auxiliary listener must be authenticated and loopback only")
			}
			u, _ := users[0].(map[string]any)
			if u["username"] != b.plan.Username || u["password"] != b.plan.Password {
				return errors.New("sing-box auxiliary authentication mismatch")
			}
			if found {
				return errors.New("duplicate sing-box auxiliary listener")
			}
			found = true
		}
		if !found {
			return errors.New("sing-box auxiliary listener missing")
		}
	}
	return nil
}
