package kernelconf

import (
	"github.com/shusfun/weir/internal/db"
	"github.com/shusfun/weir/packages/contracts"
)

// RuleOutboundNames only includes the consumer engine. A loopback helper is
// part of the same path and must not add a second copy of its bytes.
func RuleOutboundNames(cfg contracts.GateConfig) map[string]map[string]int64 {
	expanded, err := ExpandedOutbounds(cfg)
	if err != nil {
		return nil
	}
	byTag := map[string]int64{}
	for _, o := range cfg.Outbounds {
		if o.RuleID > 0 {
			byTag[o.Tag] = o.RuleID
		}
	}
	wanted := map[string]map[int64]bool{}
	for _, r := range cfg.Routes {
		id := byTag[r.OutboundTag]
		if id == 0 {
			continue
		}
		for _, in := range cfg.Inbounds {
			if r.InboundTag != "" && r.InboundTag != in.Tag {
				continue
			}
			if r.StatsKey != "" {
				matches := false
				for _, c := range cfg.Credentials {
					if c.StatsKey == r.StatsKey && c.InboundID == in.ID {
						matches = true
						break
					}
				}
				if !matches {
					continue
				}
			}
			if wanted[in.Kernel] == nil {
				wanted[in.Kernel] = map[int64]bool{}
			}
			wanted[in.Kernel][id] = true
		}
	}
	out := map[string]map[string]int64{}
	for engine, ids := range wanted {
		out[engine] = map[string]int64{}
		for _, o := range expanded {
			if !ids[o.RuleID] {
				continue
			}
			core := engine
			if core == "sing-box" {
				core = db.CoreSingbox
			}
			out[engine][db.GeneratedOutboundTag(core, o.ID, o.Tag)] = o.RuleID
		}
	}
	return out
}
