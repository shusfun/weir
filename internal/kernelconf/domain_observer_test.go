package kernelconf

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/shusfun/weir/packages/contracts"
)

func TestDomainObserverLoopbackAuthenticationAndTampering(t *testing.T) {
	for _, engine := range []string{"sing-box", "mihomo"} {
		t.Run(engine, func(t *testing.T) {
			cfg := bridgeConfig(t, engine)
			plan := contracts.ConnectionObserver{Port: 28001, Secret: strings.Repeat("s", 32)}
			cfg.Observers = map[string]contracts.ConnectionObserver{engine: plan}
			out, err := Build(cfg, Options{})
			if err != nil {
				t.Fatal(err)
			}
			body := out.Singbox
			if engine == "mihomo" {
				body = out.Mihomo
			}
			if !strings.Contains(string(body), "127.0.0.1:28001") || !strings.Contains(string(body), plan.Secret) {
				t.Fatal("observer omitted")
			}
			if err = validateObserverPlan(body, engine, plan); err != nil {
				t.Fatal(err)
			}
			if engine == "sing-box" {
				var doc struct {
					Route struct {
						Rules []map[string]any `json:"rules"`
					} `json:"route"`
				}
				if err = json.Unmarshal(body, &doc); err != nil {
					t.Fatal(err)
				}
				if len(doc.Route.Rules) == 0 || doc.Route.Rules[0]["action"] != "sniff" || doc.Route.Rules[0]["override_destination"] != nil {
					t.Fatal("observer must sniff without rewriting the destination")
				}
				for name, mutate := range map[string]func(map[string]any){
					"destination override": func(rule map[string]any) { rule["override_destination"] = true },
					"routing action":      func(rule map[string]any) { rule["action"] = "route" },
					"inbound filter":      func(rule map[string]any) { rule["inbound"] = []any{"other"} },
				} {
					var unsafe map[string]any
					if err = json.Unmarshal(body, &unsafe); err != nil {
						t.Fatal(err)
					}
					unsafeRoute := unsafe["route"].(map[string]any)
					unsafeRules := unsafeRoute["rules"].([]any)
					mutate(unsafeRules[0].(map[string]any))
					unsafeBody, marshalErr := json.Marshal(unsafe)
					if marshalErr != nil {
						t.Fatal(marshalErr)
					}
					if validateObserverPlan(unsafeBody, engine, plan) == nil {
						t.Fatalf("unsafe observer rule accepted: %s", name)
					}
				}
			}
			tampered := strings.ReplaceAll(string(body), "127.0.0.1:28001", "0.0.0.0:28001")
			if validateObserverPlan([]byte(tampered), engine, plan) == nil {
				t.Fatal("public observer accepted")
			}
			if validateObserverPlan(body, engine, contracts.ConnectionObserver{}) == nil {
				t.Fatal("unrequested observer accepted")
			}
			cfg.Observers[engine] = contracts.ConnectionObserver{Port: cfg.Bridges[0].Port, Secret: plan.Secret}
			if _, err = Build(cfg, Options{}); err == nil {
				t.Fatal("port collision accepted")
			}
		})
	}
}
