package kernelconf

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"time"

	"github.com/shusfun/weir/internal/db"
	"github.com/shusfun/weir/packages/contracts"
)

// active 判断凭据此刻是否应下发到内核：到期或所属用户额度用尽的凭据被剔除；
// 系统内部凭据（链式代理入口到出口那一跳）不受用户额度影响。
func active(c contracts.Credential, now time.Time, exhausted map[string]bool) bool {
	if c.ExpiresAt != nil && !now.Before(*c.ExpiresAt) {
		return false
	}
	return c.Internal || !exhausted[c.UserKey]
}

// toInbounds 把协议字段（settings，键名与构建器字段一致）展开成构建器使用的入站结构。
func toInbounds(cfg contracts.GateConfig, opt Options) ([]db.Inbound, map[string]int64, map[string]int64, error) {
	tagToID := map[string]int64{}
	keyToCred := map[string]int64{}
	clients := map[int64][]db.Client{}
	for _, c := range cfg.Credentials {
		if !active(c, opt.Now, opt.Exhausted) {
			continue
		}
		clients[c.InboundID] = append(clients[c.InboundID], db.Client{ID: c.ID, InboundID: c.InboundID, UUID: c.UUID, CredentialID: c.UUID, Password: c.Password, StatsKey: c.StatsKey, Email: c.StatsKey, Enabled: true})
		keyToCred[c.StatsKey] = c.ID
	}
	out := make([]db.Inbound, 0, len(cfg.Inbounds))
	for _, in := range cfg.Inbounds {
		var item db.Inbound
		if len(in.Settings) > 0 {
			b, err := json.Marshal(in.Settings)
			if err != nil {
				return nil, nil, nil, err
			}
			if err = json.Unmarshal(b, &item); err != nil {
				return nil, nil, nil, fmt.Errorf("inbound %d settings: %w", in.ID, err)
			}
		}
		item.ID, item.Protocol, item.Core, item.Port = in.ID, in.Protocol, in.Kernel, in.Port
		item.Network, item.Security, item.Enabled, item.Remark = in.Network, in.Security, true, in.Tag
		if in.CertID > 0 {
			item.CertificateID = in.CertID
			item.TLSCertFile = filepath.Join(opt.CertDir, fmt.Sprintf("%d.crt", in.CertID))
			item.TLSKeyFile = filepath.Join(opt.CertDir, fmt.Sprintf("%d.key", in.CertID))
		}
		item.Clients = clients[in.ID]
		tagToID[in.Tag] = in.ID
		out = append(out, item)
	}
	return out, tagToID, keyToCred, nil
}

// directID 是固定直连出口的保留 ID：构建器只按正数 ID 解析路由目标，指向 direct 的规则也能生效。
const directID int64 = 1<<31 - 1

// toOutbounds 在最前面固定放直连出口：新增出口不会悄悄替换默认直连，只有显式路由才选中它。
func toOutbounds(cfg contracts.GateConfig) ([]db.Outbound, map[string]int64, error) {
	outs := []db.Outbound{{ID: directID, Tag: "direct", Protocol: "freedom", Enabled: true}}
	byTag := map[string]int64{"direct": directID}
	for _, o := range cfg.Outbounds {
		if o.ID <= 0 {
			return nil, nil, fmt.Errorf("outbound %q has no id", o.Tag)
		}
		item := db.Outbound{ID: o.ID, Tag: o.Tag, Protocol: o.Protocol, Address: o.Address, Port: o.Port, Username: o.Username, Password: o.Password, Enabled: true}
		if len(o.Settings) > 0 {
			b, err := json.Marshal(o.Settings)
			if err != nil {
				return nil, nil, err
			}
			item.SettingsJSON = string(b)
		}
		outs = append(outs, item)
		byTag[o.Tag] = o.ID
	}
	return outs, byTag, nil
}

// toRoutes 按优先级排序；引用了不存在的入站 / 凭据 / 出口的规则直接报错，不静默丢弃。
// 指向被剔除（到期、超额）凭据的规则跳过：该用户此刻本就不能连接。
func toRoutes(cfg contracts.GateConfig, tagToID, keyToCred, outByTag map[string]int64) ([]db.RoutingRule, error) {
	known := map[string]bool{}
	for _, c := range cfg.Credentials {
		known[c.StatsKey] = true
	}
	routes := append([]contracts.Route(nil), cfg.Routes...)
	sort.SliceStable(routes, func(i, j int) bool {
		if routes[i].Priority != routes[j].Priority {
			return routes[i].Priority < routes[j].Priority
		}
		return routes[i].ID < routes[j].ID
	})
	out := make([]db.RoutingRule, 0, len(routes))
	for _, r := range routes {
		outID, ok := outByTag[r.OutboundTag]
		if !ok {
			return nil, fmt.Errorf("route %d targets missing outbound %q", r.ID, r.OutboundTag)
		}
		rule := db.RoutingRule{ID: r.ID, OutboundID: outID, OutboundTag: r.OutboundTag, Domain: r.Domain, IP: r.IP, Enabled: true, Sort: r.Priority}
		if r.InboundTag != "" {
			id, ok := tagToID[r.InboundTag]
			if !ok {
				return nil, fmt.Errorf("route %d references missing inbound %q", r.ID, r.InboundTag)
			}
			rule.InboundID = id
		}
		if r.StatsKey != "" {
			if !known[r.StatsKey] {
				return nil, fmt.Errorf("route %d references missing credential", r.ID)
			}
			id, ok := keyToCred[r.StatsKey]
			if !ok {
				continue
			}
			rule.ClientID = id
		}
		out = append(out, rule)
	}
	return out, nil
}
