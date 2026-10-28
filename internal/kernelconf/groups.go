package kernelconf

import (
	"fmt"

	"github.com/shusfun/weir/internal/db"
	"github.com/shusfun/weir/internal/singbox"
	"github.com/shusfun/weir/internal/xray"
	"github.com/shusfun/weir/packages/contracts"
)

// 「一组」出口：全部成员都下发，由内核按延迟自动选、出故障自动切换；中心不替内核挑节点。
// 生成时先把组本身放成一个丢弃出口（规则照常指向它；万一没被替换，流量被丢弃而不是改成直连），
// 内核配置生成后再替换为 Xray balancer + observatory 或 sing-box urltest。
// 构建器按出口 ID 生成内核里的标签（xray-out-<ID> / singbox-out-<ID>），所以这里按 ID 对应。

// groupMemberBase 是成员出口的 ID 起点（与其他出口不冲突）。
const groupMemberBase int64 = 1 << 30

// group 是一个组：组本身与各成员出口的 ID。
type group struct {
	id      int64
	members []int64
}

// expandGroups 把组展开成成员出口。
func expandGroups(cfg contracts.GateConfig) (contracts.GateConfig, []group, error) {
	var groups []group
	var outs []contracts.Outbound
	next := groupMemberBase
	for _, o := range cfg.Outbounds {
		if o.Protocol != "group" {
			outs = append(outs, o)
			continue
		}
		if len(o.Members) == 0 || len(o.Members) > contracts.MaxGroupMembers {
			return cfg, nil, fmt.Errorf("outbound %q: a group needs 1-%d members", o.Tag, contracts.MaxGroupMembers)
		}
		outs = append(outs, contracts.Outbound{ID: o.ID, Tag: o.Tag, Protocol: "blackhole"})
		g := group{id: o.ID}
		for i, m := range o.Members {
			m.ID, m.Tag = next, fmt.Sprintf("%s-m%d", o.Tag, i+1)
			next++
			outs = append(outs, m)
			g.members = append(g.members, m.ID)
		}
		groups = append(groups, g)
	}
	cfg.Outbounds = outs
	return cfg, groups, nil
}

// needFeatures 确认内核版本支持「一组」需要的特性；不支持时报错，不悄悄退回单个节点。
func needFeatures(cfg contracts.GateConfig, kernel string, features ...string) error {
	p, err := profileFor(cfg, kernel)
	if err != nil {
		return err
	}
	if p == nil {
		return fmt.Errorf("%s 没有版本档案，无法确认是否支持出口自动切换", kernel)
	}
	for _, f := range features {
		if !p.Has(f) {
			return fmt.Errorf("%s %s 不支持出口自动切换（版本档案没有 %s）", kernel, p.MinVersion, f)
		}
	}
	return nil
}

func tags(core string, ids []int64) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = db.GeneratedOutboundTag(core, id, "")
	}
	return out
}

// xrayGroups 把组的丢弃出口换成 balancer：规则改指向 balancer，observatory 探测成员延迟。
func xrayGroups(conf *xray.Config, groups []group) error {
	if len(groups) == 0 {
		return nil
	}
	if conf.Routing == nil {
		conf.Routing = &xray.RoutingConfig{DomainStrategy: "AsIs"}
	}
	placeholder := map[string]bool{}
	for _, g := range groups {
		placeholder[db.GeneratedOutboundTag(db.CoreXray, g.id, "")] = true
	}
	kept := conf.Outbounds[:0]
	for _, o := range conf.Outbounds {
		if !placeholder[o.Tag] {
			kept = append(kept, o)
		}
	}
	conf.Outbounds = kept
	var subjects []string
	for _, g := range groups {
		tag, members := db.GeneratedOutboundTag(db.CoreXray, g.id, ""), tags(db.CoreXray, g.members)
		conf.Routing.Balancers = append(conf.Routing.Balancers, xray.Balancer{Tag: tag, Selector: members, Strategy: &xray.BalancerStrategy{Type: "leastPing"}})
		subjects = append(subjects, members...)
		for i := range conf.Routing.Rules {
			if conf.Routing.Rules[i].OutboundTag == tag {
				conf.Routing.Rules[i].OutboundTag, conf.Routing.Rules[i].BalancerTag = "", tag
			}
		}
	}
	conf.Observatory = &xray.ObservatoryConfig{SubjectSelector: subjects, ProbeURL: contracts.GroupProbeURL, ProbeInterval: contracts.GroupProbeEvery, EnableConcurrency: true}
	return nil
}

// singboxGroups 把组的丢弃出口换成 urltest（规则本来就指向组的标签）。
func singboxGroups(conf *singbox.Config, groups []group) error {
	for _, g := range groups {
		tag, found := db.GeneratedOutboundTag(db.CoreSingbox, g.id, ""), false
		for i := range conf.Outbounds {
			if conf.Outbounds[i].Tag == tag {
				conf.Outbounds[i] = singbox.OutboundConfig{Type: "urltest", Tag: tag, Outbounds: tags(db.CoreSingbox, g.members), URL: contracts.GroupProbeURL, Interval: contracts.GroupProbeEvery}
				found = true
			}
		}
		if !found {
			return fmt.Errorf("outbound group %d was not generated for sing-box", g.id)
		}
	}
	return nil
}
