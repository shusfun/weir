package center

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/shusfun/weir/internal/kernelconf"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/chain"
	entnode "github.com/shusfun/weir/internal/store/ent/node"
	"github.com/shusfun/weir/internal/store/ent/outbound"
	"github.com/shusfun/weir/internal/store/ent/subgrant"
	"github.com/shusfun/weir/internal/subscription"
	"github.com/shusfun/weir/packages/contracts"
)

// 订阅源里的节点作出口（分流规则的出口、链式代理第 ③ 步、用户自建链式代理共用）：
//   - 单个节点：名称完全一致；
//   - 一组：名称正则筛选，全部成员都下发给 Gate，由内核按延迟自动选、出故障自动切换（中心不替内核挑节点），最多 50 个；
//     内核版本不支持时保存报错，不悄悄退回单个节点。
// 订阅源刷新后成员随之更新；刷新失败或一时没有匹配时保留上一次可用的；从没解析成功过时丢弃流量，不改成直连。
// 管理员可以选任意订阅源（source_id）；用户自建只能选分配给他的订阅组（profile_id）里出现的节点。

var sourceExitProtocols = map[string]string{"vless": "vless", "vmess": "vmess", "trojan": "trojan", "socks5": "socks", "http": "http"}

type sourceSpec struct {
	SourceID  int    `json:"source_id,omitempty"`
	ProfileID int    `json:"profile_id,omitempty"`
	Mode      string `json:"mode"`
	Match     string `json:"match"`
}

func specOf(m map[string]any) sourceSpec {
	b, _ := json.Marshal(m)
	var sp sourceSpec
	_ = json.Unmarshal(b, &sp)
	return sp
}

func (sp sourceSpec) toMap() map[string]any {
	b, _ := json.Marshal(sp)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m
}

func (sp sourceSpec) check() error {
	if (sp.SourceID > 0) == (sp.ProfileID > 0) {
		return errors.New("请选择一个订阅源或订阅组")
	}
	if sp.Mode != "node" && sp.Mode != "filter" || strings.TrimSpace(sp.Match) == "" || len(sp.Match) > 200 {
		return errors.New("请选择单个节点或按名称筛选一组，并填写名称")
	}
	if sp.Mode == "filter" {
		if _, err := regexp.Compile(sp.Match); err != nil {
			return errors.New("筛选条件不是有效的正则")
		}
	}
	return nil
}

// specLinks 是订阅源（缓存的节点）或订阅组（处理后的节点）里的分享链接。
func (s *Server) specLinks(ctx context.Context, sp sourceSpec) ([]string, error) {
	if sp.ProfileID > 0 {
		p, err := s.Store.Ent.SubProfile.Get(ctx, sp.ProfileID)
		if err != nil {
			return nil, errors.New("订阅组不存在")
		}
		list, _, _, _, err := s.groupNodes(ctx, p, "clash-meta/2.5.0", false)
		if err != nil {
			return nil, err
		}
		return strings.Split(strings.TrimSpace(list), "\n"), nil
	}
	links, ok := kvCache{s}.Load(ctx, sp.SourceID)
	if !ok || len(links) == 0 {
		return nil, errors.New("订阅源还没有成功拉取过节点")
	}
	return links, nil
}

// resolveSpec 找出匹配的节点并转成出口：单个节点得到普通出口，一组得到 group 出口（全部成员）。
// publicOnly 为真（用户自建）时只接受公网地址的节点。返回出口与成员名称。
func (s *Server) resolveSpec(ctx context.Context, sp sourceSpec, publicOnly bool) (contracts.Outbound, []string, error) {
	links, err := s.specLinks(ctx, sp)
	if err != nil {
		return contracts.Outbound{}, nil, err
	}
	var re *regexp.Regexp
	if sp.Mode == "filter" {
		if re, err = regexp.Compile(sp.Match); err != nil {
			return contracts.Outbound{}, nil, errors.New("筛选条件不是有效的正则")
		}
	}
	parsed, _ := subscription.ParseLinks(links)
	var members []contracts.Outbound
	var names []string
	for _, n := range parsed.Nodes {
		if sp.Mode == "filter" && !re.MatchString(n.Remark) || sp.Mode != "filter" && n.Remark != sp.Match {
			continue
		}
		proto, ok := sourceExitProtocols[n.Protocol]
		if !ok || publicOnly && (literalPrivate(n.Address) || strings.EqualFold(n.Address, "localhost")) {
			continue
		}
		set := map[string]any{}
		_ = json.Unmarshal([]byte(n.SettingsJSON), &set)
		members = append(members, contracts.Outbound{Protocol: proto, Address: n.Address, Port: n.Port, Username: n.Username, Password: n.Password, Settings: set})
		names = append(names, n.Remark)
		if sp.Mode != "filter" {
			break
		}
	}
	switch {
	case len(members) == 0:
		return contracts.Outbound{}, nil, fmt.Errorf("没有匹配「%s」的可用节点（支持 VLESS / VMess / Trojan / SOCKS5 / HTTP）", sp.Match)
	case len(members) > contracts.MaxGroupMembers:
		return contracts.Outbound{}, nil, fmt.Errorf("匹配到 %d 个节点，一组最多 %d 个，请收紧筛选条件", len(members), contracts.MaxGroupMembers)
	case sp.Mode == "filter":
		return contracts.Outbound{Protocol: "group", Members: members}, names, nil
	}
	return members[0], names, nil
}

// 解析结果按所属对象单独保存：分流出口 out:<id>、节点链式代理 node:<id>、用户自建 chain:<id>。
func sourceExitKey(owner string) string { return "source-exit:" + owner }

type storedExit struct {
	Outbound contracts.Outbound `json:"outbound"`
	Names    []string           `json:"names"`
	At       time.Time          `json:"at"`
}

// refreshSourceExit 解析并保存；结果有变化时让 Gate 重新下发。解析失败保留上一次可用的。
func (s *Server) refreshSourceExit(ctx context.Context, owner string, gid int, sp sourceSpec, publicOnly bool) error {
	ob, names, err := s.resolveSpec(ctx, sp, publicOnly)
	if err != nil {
		return err
	}
	prev, _ := s.loadExit(ctx, owner)
	a, _ := json.Marshal(ob)
	b, _ := json.Marshal(prev.Outbound)
	if string(a) == string(b) {
		return nil
	}
	raw, _ := json.Marshal(storedExit{Outbound: ob, Names: names, At: time.Now().UTC()})
	if err = s.Store.KV.Set(ctx, sourceExitKey(owner), string(raw), 0); err != nil {
		return err
	}
	_, err = s.bumpGate(ctx, gid)
	return err
}

func (s *Server) loadExit(ctx context.Context, owner string) (storedExit, bool) {
	raw, ok, _ := s.Store.KV.Get(ctx, sourceExitKey(owner))
	var v storedExit
	if !ok || json.Unmarshal([]byte(raw), &v) != nil || v.Outbound.Protocol == "" {
		return storedExit{}, false
	}
	return v, true
}

// exitFromStore 是编译 Gate 配置时用的出口：上一次解析成功的结果；从没成功过则为丢弃。
func (s *Server) exitFromStore(ctx context.Context, owner, tag string, id int64) (contracts.Outbound, string) {
	v, ok := s.loadExit(ctx, owner)
	if !ok {
		return contracts.Outbound{ID: id, Tag: tag, Protocol: "blackhole"}, "订阅源节点出口还没有解析成功，暂时丢弃经这个出口的流量"
	}
	v.Outbound.ID, v.Outbound.Tag = id, tag
	return v.Outbound, ""
}

// exitView 给页面显示当前用的是哪些节点。
func (s *Server) exitView(ctx context.Context, owner string) map[string]any {
	v, ok := s.loadExit(ctx, owner)
	if !ok {
		return map[string]any{"resolved": nil}
	}
	return map[string]any{"resolved": v.Names, "group": v.Outbound.Protocol == "group", "at": v.At}
}

// groupFeatures 是「一组」在各内核上需要的特性。
var groupFeatures = map[string][]string{"xray": {"balancer", "observatory"}, "sing-box": {"urltest"}}

// checkGroupSupport 确认这台 Gate 上用到的内核版本支持出口自动切换；kernels 为空时检查这台 Gate 上节点用到的全部内核。
// 不支持时报错（保存时），不悄悄退回单个节点。
func (s *Server) checkGroupSupport(ctx context.Context, g *ent.Gate, kernels ...string) error {
	need := map[string]bool{}
	for _, k := range kernels {
		need[k] = true
	}
	if len(need) == 0 {
		nodes, err := s.Store.Ent.Node.Query().Where(entnode.GateIDEQ(g.ID), entnode.EnabledEQ(true)).All(ctx)
		if err != nil {
			return err
		}
		for _, n := range nodes {
			need[n.Kernel] = true
		}
	}
	if len(need) == 0 {
		return nil
	}
	_, profiles, err := s.gateKernels(ctx, g, need)
	if err != nil {
		return fmt.Errorf("无法确认内核是否支持出口自动切换：%v", err)
	}
	for k := range need {
		p, err := kernelconf.ParseProfile(profiles[k])
		if err != nil {
			return err
		}
		for _, f := range groupFeatures[k] {
			if !p.Has(f) {
				return fmt.Errorf("%s %s 不支持出口自动切换（版本档案没有 %s），请改选单个节点或升级内核", k, p.MinVersion, f)
			}
		}
	}
	return nil
}

// probeSpec 由入口 Gate 实测：单个节点必须连得上；一组至少有一个成员连得上（最多试 5 个）。
func (s *Server) probeSpec(ctx context.Context, entry *ent.Gate, ob contracts.Outbound, publicOnly bool) error {
	members := []contracts.Outbound{ob}
	if ob.Protocol == "group" {
		members = ob.Members
	}
	var last error
	for i, m := range members {
		if i >= 5 {
			break
		}
		_, err := s.gateProbe(ctx, entry, contracts.ActionChainProbe, map[string]any{"kind": "node", "address": m.Address, "port": m.Port, "public_only": publicOnly}, 30*time.Second)
		if err == nil {
			return nil
		}
		last = err
	}
	return fmt.Errorf("chain_probe_failed: %w", last)
}

// grantedProfiles 是分配给用户的订阅组（用户自建链式代理只能选这些组里出现的节点）。
func (s *Server) grantedProfiles(ctx context.Context, uid int) (map[int]bool, error) {
	grants, err := s.Store.Ent.SubGrant.Query().Where(subgrant.UserIDEQ(uid)).All(ctx)
	if err != nil {
		return nil, err
	}
	out := map[int]bool{}
	for _, g := range grants {
		out[g.ProfileID] = true
	}
	return out, nil
}

// RefreshSourceExits 在订阅源刷新后更新全部订阅源节点出口（维护任务每分钟调用；失败的保持上一次）。
func (s *Server) RefreshSourceExits(ctx context.Context) {
	if outs, err := s.Store.Ent.Outbound.Query().Where(outbound.ProtocolEQ("source")).All(ctx); err == nil {
		for _, o := range outs {
			_ = s.refreshSourceExit(ctx, "out:"+itoa(o.ID), o.GateID, specOf(o.Settings), false)
		}
	}
	if nodes, err := s.Store.Ent.Node.Query().Where(entnode.ChainKindEQ(entnode.ChainKindSource)).All(ctx); err == nil {
		for _, n := range nodes {
			_ = s.refreshSourceExit(ctx, "node:"+itoa(n.ID), n.GateID, specOf(n.ChainSource), false)
		}
	}
	if chains, err := s.Store.Ent.Chain.Query().Where(chain.ExitKindEQ(chain.ExitKindSource)).All(ctx); err == nil {
		for _, ch := range chains {
			if n, err := s.Store.Ent.Node.Get(ctx, ch.EntryNodeID); err == nil {
				_ = s.refreshSourceExit(ctx, "chain:"+itoa(ch.ID), n.GateID, specOf(ch.ExitSource), true)
			}
		}
	}
}

// chainSources 列出链式代理出口可以选的订阅节点：
//   - 管理员（/admin/chain-sources，建节点第 ③ 步与分流规则）：全部订阅源及其节点名；
//   - 用户自建（/me/chain-sources，或管理员代建 /admin/users/:uid/chain-sources）：只有分配给该用户的订阅组及组里的节点名。
func (s *Server) chainSources(c *gin.Context, admin bool) {
	ctx := c.Request.Context()
	type option struct {
		Kind  string   `json:"kind"` // source / profile
		ID    int      `json:"id"`
		Name  string   `json:"name"`
		Nodes []string `json:"nodes"`
	}
	names := func(links []string) []string {
		out := []string{}
		parsed, _ := subscription.ParseLinks(links)
		for _, n := range parsed.Nodes {
			if _, ok := sourceExitProtocols[n.Protocol]; ok {
				out = append(out, n.Remark)
			}
		}
		return out
	}
	out := []option{}
	uid := 0
	if c.Param("uid") != "" {
		u, ok := s.subject(c, true)
		if !ok {
			return
		}
		uid = u.ID
	} else if !admin {
		uid = c.GetInt("user_id")
	}
	if uid == 0 {
		srcs, err := s.Store.Ent.SubSource.Query().Order(ent.Asc("position"), ent.Asc("id")).All(ctx)
		if err != nil {
			failure(c, 503, "query_failed")
			return
		}
		for _, src := range srcs {
			links, _ := kvCache{s}.Load(ctx, src.ID)
			out = append(out, option{Kind: "source", ID: src.ID, Name: firstNonEmpty(src.Name, src.URL), Nodes: names(links)})
		}
		c.JSON(200, gin.H{"items": out})
		return
	}
	granted, err := s.grantedProfiles(ctx, uid)
	if err != nil {
		failure(c, 503, "query_failed")
		return
	}
	for id := range granted {
		p, err := s.Store.Ent.SubProfile.Get(ctx, id)
		if err != nil {
			continue
		}
		links, _ := s.specLinks(ctx, sourceSpec{ProfileID: id})
		out = append(out, option{Kind: "profile", ID: id, Name: p.Name, Nodes: names(links)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	c.JSON(200, gin.H{"items": out})
}
