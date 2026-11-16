package kernelconf

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/shusfun/weir/packages/contracts"
)

// NativeInboundNames shares the exact tags used by the selected server templates.
func NativeInboundNames(cfg contracts.GateConfig, engine string) map[string]int64 {
	result := map[string]int64{}
	inbounds, _, _, err := toInbounds(cfg, Options{Now: time.Now()})
	if err != nil {
		return result
	}
	for _, in := range inbounds {
		if in.Core != engine {
			continue
		}
		tag := statsTag(in)
		result[tag] = in.ID
		if in.Protocol == "shadowtls" {
			result[tag+"-ss"] = in.ID
		}
	}
	return result
}

func observerVars(d Driver, plan contracts.ConnectionObserver) (map[string]any, error) {
	if plan.Port == 0 && plan.Secret == "" {
		return nil, nil
	}
	if !d.Has("native-connections") || (d.Kernel != "sing-box" && d.Kernel != "mihomo") || plan.Port < 1 || plan.Port > 65535 || len(plan.Secret) < 32 {
		return nil, errors.New("unsupported or invalid native connection observer")
	}
	return map[string]any{"listen": fmt.Sprintf("127.0.0.1:%d", plan.Port), "secret": plan.Secret}, nil
}

func validateObserverAddress(listen, secret any, ports map[int]bool) error {
	host, p, err := net.SplitHostPort(fmt.Sprint(listen))
	port, _ := strconv.Atoi(p)
	s, _ := secret.(string)
	if err != nil || host != "127.0.0.1" || !ports[port] || len(s) < 32 {
		return errors.New("native observer requires allocated loopback port and authentication")
	}
	return nil
}

func validateObserverPlan(body []byte, engine string, plan contracts.ConnectionObserver) error {
	if engine == "xray" {
		return nil
	}
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		return err
	}
	var listen, secret any
	if engine == "sing-box" {
		exp, _ := doc["experimental"].(map[string]any)
		api, _ := exp["clash_api"].(map[string]any)
		if len(api) > 0 {
			if len(api) != 2 {
				return errors.New("native observer permits only controller and secret")
			}
			listen, secret = api["external_controller"], api["secret"]
		}
	} else {
		listen, secret = doc["external-controller"], doc["secret"]
	}
	if plan.Port == 0 {
		if listen != nil || secret != nil {
			return errors.New("unrequested native observer")
		}
		return nil
	}
	if listen != fmt.Sprintf("127.0.0.1:%d", plan.Port) || secret != plan.Secret {
		return errors.New("driver changed native observer plan")
	}
	if engine == "sing-box" {
		route, _ := doc["route"].(map[string]any)
		rules, _ := route["rules"].([]any)
		if len(rules) == 0 || !sameJSON(rules[0], observerSniffRule()) {
			return errors.New("native observer requires sniffing without destination overrides")
		}
	}
	return nil
}

func observerSniffRule() map[string]any {
	return map[string]any{"action": "sniff", "sniffer": []any{"http", "tls", "quic"}}
}
