package center

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/node"
	"github.com/shusfun/weir/packages/contracts"
)

// 节点能不能从外面连上：需要放行的端口（协议决定 TCP / UDP）、Gate 本机防火墙有没有放行、
// 中心通过 mihomo 完成协议连接；中心与 Gate 在同一台机器时结果为未知。云厂商安全组由用户管理。

type portNeed struct {
	Port     int    `json:"port"`
	Proto    string `json:"proto"`    // tcp / udp
	Firewall string `json:"firewall"` // open（未启用或已放行）/ blocked / unknown
	Why      string `json:"why,omitempty"`
}

// nodeProtos 是节点监听的协议：QUIC 类与 WireGuard 用 UDP，Shadowsocks 同时用 TCP 与 UDP，其他 TCP。
func nodeProtos(protocol string) []string {
	switch protocol {
	case "hysteria2", "tuic", "wireguard":
		return []string{"udp"}
	case "shadowsocks", "socks":
		return []string{"tcp", "udp"}
	}
	return []string{"tcp"}
}

// nodePorts 列出节点要放行的端口，并对照 Gate 上报的本机防火墙。
func nodePorts(n *ent.Node, g *ent.Gate) []portNeed {
	var out []portNeed
	for _, proto := range nodeProtos(n.Protocol) {
		p := portNeed{Port: n.Port, Proto: proto}
		p.Firewall, p.Why = firewallAllows(g, n.Port, proto)
		out = append(out, p)
	}
	return out
}

// firewallAllows 判断本机防火墙是否放行某端口；读不到防火墙时为 unknown。
func firewallAllows(g *ent.Gate, port int, proto string) (string, string) {
	if g == nil || g.Firewall == nil {
		return "unknown", "VPS 没有上报防火墙状态"
	}
	tool, _ := g.Firewall["tool"].(string)
	active, _ := g.Firewall["active"].(bool)
	if e, _ := g.Firewall["error"].(string); e != "" {
		return "unknown", tool + "：" + e
	}
	if tool == "none" {
		return "open", "本机没有防火墙"
	}
	if !active {
		return "open", tool + " 未启用"
	}
	rules, _ := g.Firewall["rules"].([]any)
	for _, r := range rules {
		if ruleCovers(fmt.Sprint(r), port, proto) {
			return "open", tool + " 已放行"
		}
	}
	return "blocked", fmt.Sprintf("%s 没有放行 %d/%s", tool, port, proto)
}

// ruleCovers：规则形如 443、443/tcp、20000:20100/udp。
func ruleCovers(rule string, port int, proto string) bool {
	ports, p, hasProto := strings.Cut(rule, "/")
	if hasProto && p != proto {
		return false
	}
	lo, hi, isRange := strings.Cut(ports, ":")
	a, err := strconv.Atoi(lo)
	if err != nil {
		return false
	}
	b := a
	if isRange {
		if b, err = strconv.Atoi(hi); err != nil {
			return false
		}
	}
	return port >= a && port <= b
}

// reachResult 是中心从外部连接节点端口的结果；保存在 reach:node:<id>。
// LatencyMS 是从中心所在位置建立 TCP 连接用的时间，只在节点状况里显示；代理页排序看节点自己统计的速率，不看它。
type reachResult struct {
	Revision  int64     `json:"revision"`
	Host      string    `json:"host"`
	Port      int       `json:"port"`
	Status    string    `json:"status"` // ok / failed / unknown
	Why       string    `json:"why,omitempty"`
	LatencyMS int       `json:"latency_ms,omitempty"`
	At        time.Time `json:"at"`
	Binding   string    `json:"binding"`
	TraceID   string    `json:"trace_id,omitempty"`
}

func reachKey(id int) string { return "reach:node:" + itoa(id) }

func (s *Server) loadReach(ctx context.Context, id int) (reachResult, bool) {
	raw, ok, _ := s.Store.KV.Get(ctx, reachKey(id))
	var r reachResult
	if !ok || json.Unmarshal([]byte(raw), &r) != nil {
		return reachResult{}, false
	}
	n, err := s.Store.Ent.Node.Get(ctx, id)
	if err != nil {
		return reachResult{}, false
	}
	g, err := s.Store.Ent.Gate.Get(ctx, n.GateID)
	if err != nil {
		return reachResult{}, false
	}
	binding, err := s.probeFingerprint(ctx, n, g)
	if err != nil || r.Binding == "" || r.Binding != binding {
		return reachResult{}, false
	}
	return r, true
}

// nodeReadiness 使用与下发相同的凭据与额度规则，避免把未启用入站误判为网络故障。
func (s *Server) nodeReadiness(ctx context.Context, n *ent.Node, g *ent.Gate) (string, string) {
	if g == nil {
		return "unknown", "VPS 状态未知"
	}
	cfg := contracts.GateConfig{Budgets: map[string]contracts.GateBudget{}}
	_, err := s.addCredentials(ctx, g, &cfg, []int{n.ID})
	if err != nil {
		return "unknown", "无法读取节点配置状态"
	}
	now := time.Now()
	active := false
	for _, c := range cfg.Credentials {
		if cr, err := s.Store.Ent.Credential.Get(ctx, int(c.ID)); err != nil || cr.ProbeParentID != nil || cr.Purpose == "probe" {
			continue
		}
		if c.InboundID != int64(n.ID) || !c.InWindow(now) {
			continue
		}
		b := cfg.Budgets[c.UserKey]
		if c.Internal || b.Unlimited || b.Allocated > b.Used {
			active = true
			break
		}
	}
	if !active {
		return "credentials", "没有有效使用凭据，请分配用户并创建可用订阅；同时检查到期时间和额度"
	}
	if g.AppliedRevision < g.ConfigRevision {
		return "pending", "等待 VPS 应用配置"
	}
	if g.KernelsRunning == nil {
		return "unknown", "尚未收到内核运行状态"
	}
	if g.KernelsRunning[n.Kernel] == "" {
		return "kernel", "节点内核未运行，请查看内核诊断"
	}
	for _, p := range g.ListeningPorts {
		if fmt.Sprint(p["port"]) == strconv.Itoa(n.Port) {
			return "ready", ""
		}
	}
	return "listener", "内核没有监听节点端口，请查看内核诊断"
}

type nodeReachChecks struct {
	sync.Mutex
	pending map[int]bool
}

func (s *Server) refreshGateReachability(gid int) {
	s.reachChecks.Lock()
	if s.reachChecks.pending == nil {
		s.reachChecks.pending = map[int]bool{}
	}
	if _, exists := s.reachChecks.pending[gid]; exists {
		s.reachChecks.pending[gid] = true
		s.reachChecks.Unlock()
		return
	}
	s.reachChecks.pending[gid] = false
	s.reachChecks.Unlock()
	go func() {
		s.state.RLock()
		defer s.state.RUnlock()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		select {
		case <-s.events.stopped():
			cancel()
		default:
		}
		go func() {
			select {
			case <-s.events.stopped():
				cancel()
			case <-ctx.Done():
			}
		}()
		for {
			if ctx.Err() == nil {
				ids, err := s.Store.Ent.Node.Query().Where(node.GateIDEQ(gid), node.EnabledEQ(true)).IDs(ctx)
				if err == nil {
					var stale []int
					for _, id := range ids {
						result, valid := s.loadReach(ctx, id)
						if !valid || time.Since(result.At) >= 10*time.Minute {
							stale = append(stale, id)
						}
					}
					// 内部测试授权会推进应用版本；只有业务配置改变才需要再次检查，避免循环发放凭据。
					if len(stale) > 0 {
						s.checkReachabilities(ctx, stale)
					}
				}
				s.notifyConsole("gate", itoa(gid))
			}
			s.reachChecks.Lock()
			if ctx.Err() == nil && s.reachChecks.pending[gid] {
				s.reachChecks.pending[gid] = false
				s.reachChecks.Unlock()
				continue
			}
			delete(s.reachChecks.pending, gid)
			s.reachChecks.Unlock()
			return
		}
	}()
}

// colocated：Gate 经本机回环连接中心（单机），从中心测不出外部能不能连上。
func colocated(g *ent.Gate) bool {
	return g.CenterURL != "" && loopbackURL(g.CenterURL)
}

func loopbackURL(raw string) bool {
	host := raw
	if i := strings.Index(raw, "://"); i >= 0 {
		host = raw[i+3:]
	}
	host, _, _ = strings.Cut(host, "/")
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return host == "localhost" || ip != nil && ip.IsLoopback()
}

// checkAfterApplied 等这台 VPS 应用了包含新节点的配置（最多 60 秒）再检查外部连接：配置还没生效时端口没在监听，
// 马上检查只会得到「连不上」。建节点向导在结果出来之前一直刷新。
func (s *Server) checkAfterApplied(ctx context.Context, nodeID, gateID int) {
	st, stop := s.Store, s.events.stopped()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		select {
		case <-stop:
			cancel()
		case <-ctx.Done():
		}
	}()

	if g, err := st.Ent.Gate.Get(ctx, gateID); err == nil {
		want := g.ConfigRevision
		for i := 0; i < 30 && g.AppliedRevision < want && g.FailedRevision < want; i++ {
			select {
			case <-stop:
				return
			case <-time.After(2 * time.Second):
			}
			if g, err = st.Ent.Gate.Get(ctx, gateID); err != nil {
				break
			}
		}
	}
	// 持读锁：中心停止时等这次检查（最多 6 秒）结束再关闭存储
	s.state.RLock()
	defer s.state.RUnlock()
	select {
	case <-stop:
		return
	default:
	}
	if s.Store == st {
		go s.resumePendingNodeProbes(context.WithoutCancel(ctx))
		go s.resumePendingExits(context.WithoutCancel(ctx))
		s.checkReachability(ctx, nodeID)
	}
}

// 单节点检查与定时批量检查使用同一条协议实测路径。
func (s *Server) checkReachability(ctx context.Context, id int) {
	s.checkReachabilities(ctx, []int{id})
}

var reachRunning sync.Mutex

// CheckReachabilityAll 每 10 分钟检查全部启用的节点（维护任务调用；后台进行，不占维护锁）。
func (s *Server) CheckReachabilityAll(ctx context.Context, now time.Time) {
	if now.Minute()%10 != 0 || !reachRunning.TryLock() {
		return
	}
	ids, err := s.Store.Ent.Node.Query().Where(node.EnabledEQ(true)).IDs(ctx)
	if err != nil {
		reachRunning.Unlock()
		return
	}
	go func() {
		defer reachRunning.Unlock()
		s.state.RLock()
		defer s.state.RUnlock()
		if s.Store == nil {
			return
		}
		bg, cancel := context.WithCancel(context.WithoutCancel(ctx))
		defer cancel()
		go func() {
			select {
			case <-s.events.stopped():
				cancel()
			case <-bg.Done():
			}
		}()
		s.checkReachabilities(bg, ids)
	}()
}

// publicHostHint 是单机安装时建议的公网地址：本机网卡上的公网 IP（Gate 经回环连接中心，记录不到来源地址）。
func publicHostHint(g *ent.Gate) string {
	if g.PublicHost != "" || !colocated(g) {
		return ""
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil && publicAddr(ipn.IP) {
			return ipn.IP.String()
		}
	}
	return ""
}
