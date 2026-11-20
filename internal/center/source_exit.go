package center

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/shusfun/weir/internal/kernelconf"
	"github.com/shusfun/weir/internal/kernelconf/model"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/exitbinding"
	entnode "github.com/shusfun/weir/internal/store/ent/node"
	"github.com/shusfun/weir/internal/store/ent/subgrant"
	"github.com/shusfun/weir/internal/subscription"
	"github.com/shusfun/weir/packages/contracts"
)

// 订阅源里的节点作出口（分流规则的出口、建节点的出口、用户自建链式代理共用）：
//   - 单个节点：稳定连接身份，名称只用于旧数据首次绑定；
//   - 一组：名称正则筛选，全部成员都下发给 Gate，由内核按延迟自动选、出故障自动切换（中心不替内核挑节点），最多 50 个；
//     内核版本不支持时保存报错，不悄悄退回单个节点。
// 订阅源刷新后成员随之更新；刷新失败或一时没有匹配时保留上一次可用的；从没解析成功过时丢弃流量，不改成直连。
// 管理员可以选任意订阅源（source_id）；用户自建只能选分配给他的订阅组（profile_id）里出现的节点。
// 能作出口的协议与参数由 linkOutbound 决定；匹配到但不能作出口的节点跳过并写明原因。

type sourceSpec struct {
	SourceID           int      `json:"source_id,omitempty"`
	ProfileID          int      `json:"profile_id,omitempty"`
	Mode               string   `json:"mode"`
	Match              string   `json:"match"`
	Identity           string   `json:"identity,omitempty"`
	InsecureIdentities []string `json:"insecure_identities,omitempty"`
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
	if sp.Mode != "node" && sp.Mode != "filter" || strings.TrimSpace(sp.Match) == "" && sp.Identity == "" || len(sp.Match) > 200 || len(sp.InsecureIdentities) > contracts.MaxGroupMembers || sp.Identity != "" && (sp.Mode != "node" || !validSourceIdentity(sp.Identity)) {
		return errors.New("请选择单个节点或按名称筛选一组，并填写名称")
	}
	for _, id := range sp.InsecureIdentities {
		if !validSourceIdentity(id) {
			return errors.New("证书确认的节点身份无效")
		}
	}
	if sp.Mode == "filter" {
		if _, err := regexp.Compile(sp.Match); err != nil {
			return errors.New("筛选条件不是有效的正则")
		}
	}
	return nil
}

// specLinks 是订阅源（缓存的节点）或订阅组（处理后的节点）里的节点链接。
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
// publicOnly 为真（用户自建）时只接受公网地址的节点；kernels 是会用到这个出口的内核（入口节点的，或这台 VPS 上的全部），
// 有一个内核连不上的成员跳过并写明原因（否则整份 Gate 配置会被拒绝）。
func (s *Server) resolveSpec(ctx context.Context, g *ent.Gate, kernels []string, sp sourceSpec, publicOnly bool) (resolvedExit, error) {
	sp, err := s.bindSourceIdentity(ctx, sp)
	if err != nil {
		return resolvedExit{}, err
	}
	links, err := s.specLinks(ctx, sp)
	if err != nil {
		return resolvedExit{}, err
	}
	allowed := s.outboundFilter(ctx, g, kernels)
	var re *regexp.Regexp
	if sp.Mode == "filter" {
		if re, err = regexp.Compile(sp.Match); err != nil {
			return resolvedExit{}, errors.New("筛选条件不是有效的正则")
		}
	}
	parsed, _ := subscription.ParseLinks(links)
	var ex resolvedExit
	var members []contracts.Outbound
	seen := map[string]bool{}
	for _, n := range parsed.Nodes {
		if sp.Mode == "filter" && !re.MatchString(n.Remark) || sp.Mode != "filter" && (sp.Identity != "" && subscription.InventoryIdentity(n.RawLink) != sp.Identity || sp.Identity == "" && n.Remark != sp.Match) {
			continue
		}
		identity := subscription.InventoryIdentity(n.RawLink)
		if seen[identity] {
			continue
		}
		seen[identity] = true
		if publicOnly && (literalPrivate(n.Address) || strings.EqualFold(n.Address, "localhost")) {
			ex.Skipped = append(ex.Skipped, n.Remark+"：不是公网地址")
			continue
		}
		o, err := confirmedLinkOutbound(n, sp.InsecureIdentities)
		if err == nil {
			err = allowed(o)
		}
		if err != nil {
			ex.Skipped = append(ex.Skipped, n.Remark+"："+err.Error())
			continue
		}
		if supported, known := outboundKnownUDP(o); known {
			if o.Settings == nil {
				o.Settings = map[string]any{}
			}
			o.Settings["udp"] = supported
		}
		members = append(members, o)
		ex.Names, ex.Links = append(ex.Names, n.Remark), append(ex.Links, n.RawLink)
		if sp.Mode != "filter" {
			break
		}
	}
	switch {
	case len(members) == 0 && len(ex.Skipped) > 0:
		return resolvedExit{}, fmt.Errorf("匹配「%s」的节点都不能作出口（%s）", sp.Match, strings.Join(firstN(ex.Skipped, 3), "；"))
	case len(members) == 0:
		return resolvedExit{}, fmt.Errorf("没有匹配「%s」的节点", sp.Match)
	case len(members) > contracts.MaxGroupMembers:
		return resolvedExit{}, fmt.Errorf("匹配到 %d 个节点，一组最多 %d 个，请收紧筛选条件", len(members), contracts.MaxGroupMembers)
	case sp.Mode == "filter":
		udp := udpMembers(members)
		settings := map[string]any{"udp_members": len(udp)}
		if len(members) > 0 && allUDPKnown(members) && len(udp) == 0 {
			settings["udp_supported"] = false
		}
		ex.Outbound = contracts.Outbound{Protocol: "group", Members: members, Settings: settings}
	default:
		ex.Outbound = members[0]
	}
	return ex, nil
}

// outboundFilter 返回一个检查：这些内核能否作客户端连接某个出口。读不到驱动时不过滤（编译 Gate 配置时会如实报错）。
func (s *Server) outboundFilter(ctx context.Context, g *ent.Gate, kernels []string) func(contracts.Outbound) error {
	if g != nil && s.bridgeSupport(ctx, g) {
		return func(o contracts.Outbound) error {
			for _, kernel := range kernels {
				if _, err := s.outboundExecutor(ctx, g, kernel, o); err != nil {
					return err
				}
			}
			return nil
		}
	}
	var drivers []kernelconf.Driver
	if g != nil && len(kernels) > 0 {
		need := map[string]bool{}
		for _, k := range kernels {
			need[k] = true
		}
		if _, profiles, err := s.gateKernels(ctx, g, need); err == nil {
			for k := range need {
				if d, err := kernelconf.ParseDriver(profiles[k]); err == nil {
					drivers = append(drivers, d)
				}
			}
		}
	}
	return func(o contracts.Outbound) error {
		network, _ := o.Settings["network"].(string)
		for _, d := range drivers {
			if err := d.OutboundAllows(model.NormalizeOutboundProtocol(o.Protocol), network); err != nil {
				return err
			}
		}
		return nil
	}
}

func (s *Server) gateExitKernels(ctx context.Context, gid int) ([]string, error) {
	nodes, err := s.Store.Ent.Node.Query().Where(entnode.GateIDEQ(gid)).All(ctx)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []string
	for _, n := range nodes {
		if n.Kernel != "" && !seen[n.Kernel] {
			seen[n.Kernel] = true
			out = append(out, n.Kernel)
		}
	}
	return out, nil
}

func firstN(list []string, n int) []string {
	if len(list) > n {
		return list[:n]
	}
	return list
}

// owner 包含出口身份与测试位置；命名空间仅在这里追加。
func sourceExitKey(owner string) string { return "source-exit:" + owner }

type storedExit struct {
	Outbound contracts.Outbound `json:"outbound"`
	Names    []string           `json:"names"`
	Skipped  []string           `json:"skipped,omitempty"` // 匹配到但不能作出口的节点及原因
	At       time.Time          `json:"at"`
}

// refreshSourceExit 解析并保存；结果有变化时让 Gate 重新下发。解析失败保留上一次可用的。
// probe 为真时（定时刷新用户出口）成员变化后先经 Gate 实测：订阅源里的域名可能已改指内网，字面检查拦不住。
// 实测不过保留上一次可用的出口，和保存时的规则一致。保存路径已经实测过，传 false。
func (s *Server) refreshSourceExit(ctx context.Context, owner string, gid int, sp sourceSpec, publicOnly, probe bool) error {
	sp, err := s.bindStoredSourceIdentity(ctx, owner, gid, sp)
	if err != nil {
		return err
	}
	var g *ent.Gate
	var kernels []string
	if gid > 0 {
		g, err = s.Store.Ent.Gate.Get(ctx, gid)
		if err != nil {
			return err
		}
		kernels, err = s.gateExitKernels(ctx, gid)
		if err != nil {
			return err
		}
	}
	ex, err := s.resolveSpec(ctx, g, kernels, sp, publicOnly)
	if err != nil {
		return err
	}
	if g != nil && ex.Outbound.Protocol == "group" {
		if err := s.checkGroupSupport(ctx, g, kernels...); err != nil {
			return err
		}
	}
	cacheOwner := owner
	if gid > 0 {
		cacheOwner = sourceExitGateOwner(owner, gid)
	}
	prev, _ := s.loadExit(ctx, cacheOwner)
	var binding *ent.ExitBinding
	if gid > 0 {
		binding, err = s.exitBinding(ctx, atoiOr(strings.TrimPrefix(owner, "exit:")), gid)
		if err != nil {
			return err
		}
		if binding != nil && len(binding.Resolved) > 0 {
			if err := json.Unmarshal(binding.Resolved, &prev); err != nil {
				return fmt.Errorf("读取已验证订阅快照：%w", err)
			}
		}
	}
	a, _ := json.Marshal(ex.Outbound)
	b, _ := json.Marshal(prev.Outbound)
	if string(a) == string(b) && slices.Equal(ex.Skipped, prev.Skipped) && slices.Equal(ex.Names, prev.Names) {
		return nil
	}
	var measured *exitProbe
	if probe && gid > 0 && string(a) != string(b) {
		if measured, err = s.probeExit(ctx, g, ex, publicOnly); err != nil {
			return fmt.Errorf("订阅源出口成员变化后实测未通过，保留上一次可用的出口：%w", err)
		}
	}
	raw, err := json.Marshal(storedExit{Outbound: ex.Outbound, Names: ex.Names, Skipped: ex.Skipped, At: time.Now().UTC()})
	if err != nil {
		return err
	}
	if binding != nil {
		tx, err := s.Store.Ent.Tx(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if err = tx.ExitBinding.UpdateOneID(binding.ID).SetResolved(raw).SetSource(sp.toMap()).Exec(ctx); err != nil {
			return err
		}
		if err = tx.Gate.UpdateOneID(gid).AddConfigRevision(1).Exec(ctx); err != nil {
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
		s.recordRevision(ctx, gid, g.ConfigRevision, g.ConfigRevision+1)
		s.waiters.notify(gid)
		if err := s.Store.Notifications.Publish(ctx, "gate:"+itoa(gid)); err != nil {
			s.tasks.note("source-notify-"+itoa(gid), err)
		}
	}
	if err := s.Store.KV.Set(ctx, sourceExitKey(cacheOwner), string(raw), 0); err != nil {
		return err
	}
	location := "center"
	if gid > 0 {
		location = "gate"
	}
	if err := s.Store.KV.Del(ctx, exitProbeKey(exitProbeLocationOwner(owner, location, gid))); err != nil {
		return err
	}
	if err := s.invalidateExitDependents(ctx, atoiOr(strings.TrimPrefix(owner, "exit:"))); err != nil {
		return err
	}
	if measured != nil {
		return s.saveLocatedProbe(ctx, owner, location, gid, measured)
	}
	return nil
}

func outboundKnownUDP(o contracts.Outbound) (supported, known bool) {
	var measured *bool
	if o.Settings != nil {
		if v, ok := o.Settings["udp"].(bool); ok {
			measured = &v
		}
	}
	return udpKind(o.Protocol, measured)
}

func (s *Server) loadExit(ctx context.Context, owner string) (storedExit, bool) {
	raw, ok, _ := s.Store.KV.Get(ctx, sourceExitKey(owner))
	var v storedExit
	if !ok || json.Unmarshal([]byte(raw), &v) != nil || v.Outbound.Protocol == "" {
		return storedExit{}, false
	}
	return v, true
}

func (s *Server) loadGateExit(ctx context.Context, exitID, gateID int) (storedExit, bool) {
	b, err := s.exitBinding(ctx, exitID, gateID)
	if err != nil || b == nil || b.Kind != exitbinding.KindSource {
		return storedExit{}, false
	}
	var v storedExit
	if json.Unmarshal(b.Resolved, &v) != nil || v.Outbound.Protocol == "" {
		return storedExit{}, false
	}
	return v, true
}

// exitView 给页面显示当前用的是哪些节点。
func (s *Server) exitView(ctx context.Context, owner string) map[string]any {
	v, ok := s.loadExit(ctx, owner)
	if !ok {
		return map[string]any{"resolved": nil}
	}
	return storedExitView(v)
}

func storedExitView(v storedExit) map[string]any {
	return map[string]any{"resolved": v.Names, "group": v.Outbound.Protocol == "group", "skipped": v.Skipped, "at": v.At}
}

// groupFeatures 是「一组」在各内核上需要的特性。
var groupFeatures = map[string][]string{"xray": {"balancer", "observatory"}, "sing-box": {"urltest"}}

// checkGroupSupport 确认这台 Gate 上用到的内核版本支持出口自动切换；kernels 为空时检查这台 Gate 上节点用到的全部内核。
// 不支持时报错（保存时），不悄悄退回单个节点。
func (s *Server) checkGroupSupport(ctx context.Context, g *ent.Gate, kernels ...string) error {
	need := map[string]bool{}
	for _, k := range kernels {
		if k != "" {
			need[k] = true
		}
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
		p, err := kernelconf.ParseDriver(profiles[k])
		if err != nil {
			return err
		}
		for _, f := range groupFeatures[k] {
			if !p.Has(f) {
				return fmt.Errorf("%s %s 不支持出口自动切换（驱动没有 %s），请改选单个节点或升级内核", k, p.MinVersion, f)
			}
		}
	}
	return nil
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

func (s *Server) checkSourceAccess(ctx context.Context, sp sourceSpec, cc chainCtx) error {
	if err := sp.check(); err != nil {
		return fmt.Errorf("invalid_chain: %v", err)
	}
	if sp.SourceID > 0 && !cc.admin {
		return errors.New("exit_not_allowed: 只能选择分配给你的订阅组里的节点")
	}
	if sp.ProfileID > 0 && cc.userID > 0 {
		granted, err := s.grantedProfiles(ctx, cc.userID)
		if err != nil {
			return err
		}
		if !granted[sp.ProfileID] {
			return errors.New("exit_not_allowed: 这个订阅组没有分配给该用户")
		}
	}
	return nil
}

// RefreshSourceExits 在订阅源刷新后更新全部订阅源节点出口（维护任务每分钟调用；失败的保持上一次）。
func (s *Server) RefreshSourceExits(ctx context.Context) {
	bindings, err := s.Store.Ent.ExitBinding.Query().Where(exitbinding.KindEQ(exitbinding.KindSource)).All(ctx)
	if err != nil {
		s.tasks.note("refresh-source-exits", err)
		return
	}
	for _, binding := range bindings {
		e, err := s.Store.Ent.Exit.Get(ctx, binding.ExitID)
		if err != nil {
			s.tasks.note("source-exit-"+itoa(binding.ExitID), err)
			continue
		}
		if !e.Enabled {
			continue
		}
		// 刷新该 VPS 正在使用的来源；未通过的新期望配置不能借周期刷新替换旧快照。
		s.tasks.note("source-exit-"+itoa(e.ID)+"-"+itoa(binding.GateID), s.refreshSourceExit(ctx, exitOwner(e.ID), binding.GateID, specOf(binding.Source), e.OwnerUserID != nil, true))
	}
}

// chainSources 列出链式代理出口可以选的订阅节点：
//   - 管理员（/admin/chain-sources，建节点的出口与分流规则）：全部订阅源及其节点名；
//   - 用户自建（/me/chain-sources，或管理员代建 /admin/users/:uid/chain-sources）：只有分配给该用户的订阅组及组里的节点名。
func (s *Server) chainSources(c *gin.Context, admin bool) {
	ctx := c.Request.Context()
	type option struct {
		Kind  string             `json:"kind"` // source / profile
		ID    int                `json:"id"`
		Name  string             `json:"name"`
		Nodes []string           `json:"nodes"`
		Items []sourceNodeChoice `json:"node_items"`
	}
	choice := func(kind string, id int, name string, links []string) option {
		v := option{Kind: kind, ID: id, Name: name, Nodes: []string{}, Items: sourceNodeChoices(links)}
		for _, item := range v.Items {
			v.Nodes = append(v.Nodes, item.Name)
		}
		return v
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
			failInternal(c, 503, "query_failed", err)
			return
		}
		for _, src := range srcs {
			links, _ := kvCache{s}.Load(ctx, src.ID)
			out = append(out, choice("source", src.ID, firstNonEmpty(src.Name, "订阅源 #"+itoa(src.ID)), links))
		}
		c.JSON(200, gin.H{"items": out})
		return
	}
	granted, err := s.grantedProfiles(ctx, uid)
	if err != nil {
		failInternal(c, 503, "query_failed", err)
		return
	}
	for id := range granted {
		p, err := s.Store.Ent.SubProfile.Get(ctx, id)
		if err != nil {
			continue
		}
		links, _ := s.specLinks(ctx, sourceSpec{ProfileID: id})
		out = append(out, choice("profile", id, p.Name, links))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	c.JSON(200, gin.H{"items": out})
}
