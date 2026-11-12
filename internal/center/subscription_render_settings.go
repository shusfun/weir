package center

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/shusfun/weir/internal/sub"
	js "github.com/shusfun/weir/internal/sub/jscompat"
	"github.com/shusfun/weir/packages/contracts"
)

func checkSubscriptionRendering(mode, dns string, regions []any) error {
	if mode != "" && mode != "clean" && mode != "polluted" {
		return errors.New("invalid_dns_mode")
	}
	if len(dns) > 32<<10 {
		return errors.New("custom_dns_too_large")
	}
	if strings.TrimSpace(dns) != "" {
		value, err := js.YAMLLoad(dns)
		if _, ok := value.(*js.Object); err != nil || !ok {
			return errors.New("invalid_custom_dns")
		}
	}
	if len(regions) > 64 {
		return errors.New("too_many_region_overrides")
	}
	for i, raw := range regions {
		var rule struct {
			Pattern string `json:"pattern"`
			Region  string `json:"region"`
			Flags   string `json:"flags"`
		}
		b, err := json.Marshal(raw)
		if err != nil || contracts.DecodeStrict(b, &rule, 2048) != nil || len(rule.Pattern) == 0 || len(rule.Pattern) > 512 || strings.TrimSpace(rule.Region) == "" || len(rule.Region) > 80 {
			return fmt.Errorf("invalid_region_override:%d", i+1)
		}
		if rule.Flags == "" {
			rule.Flags = "i"
		}
		if _, err := sub.CompileRegionPattern(rule.Pattern, rule.Flags); err != nil {
			return fmt.Errorf("invalid_region_pattern:%d", i+1)
		}
	}
	return nil
}

func subscriptionRenderingField(err error) string {
	switch code := err.Error(); {
	case strings.Contains(code, "dns_mode"):
		return "dns_mode"
	case strings.Contains(code, "dns"):
		return "custom_dns"
	default:
		return "region_overrides"
	}
}
