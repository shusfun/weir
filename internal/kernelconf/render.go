// Package kernelconf 把中心下发的 GateConfig 按内核驱动生成为 Xray / sing-box 的配置文件。
// 中心和 Gate 使用同一份代码：中心用于预览与检查，Gate 用于实际应用（可离线重建）。
// 配置怎么写由驱动 YAML 决定（driver.go、tmpl.go）；引擎只负责变量（view.go）与生成后的强制检查。
package kernelconf

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/shusfun/weir/internal/db"
	"github.com/shusfun/weir/packages/contracts"
)

// Options 控制生成过程。Exhausted 是本 Gate 上额度已用尽的用户键集合。
type Options struct {
	CertDir   string
	Now       time.Time
	Exhausted map[string]bool
}

// Output 是生成结果；某个内核没有需要运行的入站时对应字段为 nil。Args 是各内核的运行与自检参数（来自驱动）。
type Output struct {
	Xray    []byte
	Singbox []byte
	Args    map[string]KernelArgs
}

// KernelArgs 是一个内核的运行与自检参数（{config} 待替换）。
type KernelArgs struct {
	Run   []string `json:"run"`
	Check []string `json:"check"`
}

// DriverFor 解析 GateConfig 里某内核的驱动（已在中心展开）。
func DriverFor(cfg contracts.GateConfig, kernel string) (*Driver, error) {
	text, ok := cfg.Drivers[kernel]
	if !ok || text == "" {
		return nil, nil
	}
	d, err := ParseDriver(text)
	if err != nil {
		return nil, err
	}
	if d.Kernel != kernel {
		return nil, fmt.Errorf("driver for %s declares kernel %s", kernel, d.Kernel)
	}
	return &d, nil
}

// Build 生成两个内核的配置：先按驱动检查每个入站组合，再渲染驱动模板，最后做强制检查。
func Build(cfg contracts.GateConfig, opt Options) (Output, error) {
	if opt.Now.IsZero() {
		opt.Now = time.Now()
	}
	out := Output{Args: map[string]KernelArgs{}}
	cfg, groups, err := expandGroups(cfg)
	if err != nil {
		return out, err
	}
	inbounds, tagToID, keyToCred, err := toInbounds(cfg, opt)
	if err != nil {
		return out, err
	}
	outbounds, outByTag, err := toOutbounds(cfg)
	if err != nil {
		return out, err
	}
	routes, err := toRoutes(cfg, tagToID, keyToCred, outByTag)
	if err != nil {
		return out, err
	}
	var xrayIn, singIn []db.Inbound
	for _, in := range inbounds {
		switch in.Core {
		case "xray":
			xrayIn = append(xrayIn, in)
		case "sing-box":
			// sing-box 不接受没有用户的入站；该入站此刻没有有效凭据时跳过。
			if len(in.Clients) > 0 {
				singIn = append(singIn, in)
			}
		default:
			return out, fmt.Errorf("inbound %d: unknown kernel %q", in.ID, in.Core)
		}
	}
	ports := map[int]bool{}
	for _, in := range cfg.Inbounds {
		ports[in.Port] = true
	}
	for _, k := range []struct {
		name string
		list []db.Inbound
	}{{"xray", xrayIn}, {"sing-box", singIn}} {
		kernel, list := k.name, k.list
		if len(list) == 0 {
			continue
		}
		d, err := DriverFor(cfg, kernel)
		if err != nil {
			return out, err
		}
		if d == nil {
			return out, fmt.Errorf("%s 没有驱动，无法生成配置", kernel)
		}
		for _, in := range list {
			if err := d.Allows(in.Protocol, in.Network, in.Security); err != nil {
				return out, fmt.Errorf("inbound %d: %w", in.ID, err)
			}
		}
		stats := cfg.Ports.XrayStats
		if kernel == "sing-box" {
			stats = cfg.Ports.SingboxStats
		}
		if stats <= 0 {
			return out, fmt.Errorf("%s stats port not allocated", kernel)
		}
		if len(groups) > 0 {
			need := []string{"balancer", "observatory"}
			if kernel == "sing-box" {
				need = []string{"urltest"}
			}
			for _, f := range need {
				if !d.Has(f) {
					return out, fmt.Errorf("%s %s 不支持出口自动切换（驱动没有 %s）", kernel, d.MinVersion, f)
				}
			}
		}
		v, err := buildView(kernel, list, outbounds, routes, groups)
		if err != nil {
			return out, err
		}
		body, err := render(*d, v, stats, ports, opt)
		if err != nil {
			return out, err
		}
		if kernel == "xray" {
			out.Xray = body
		} else {
			out.Singbox = body
		}
		out.Args[kernel] = KernelArgs{Run: d.Run, Check: d.Check}
	}
	return out, nil
}

// render 渲染驱动模板，按 strip 删字段，做强制检查，输出缩进的 JSON。
func render(d Driver, v view, stats int, ports map[int]bool, opt Options) ([]byte, error) {
	doc, err := renderTemplate(d.Templates, "root", v.vars(stats))
	if err != nil {
		return nil, fmt.Errorf("%s 驱动 %s：%w", d.Kernel, d.Name, err)
	}
	// 经过一次 JSON 往返：数字统一、键排序，也保证输出一定是合法 JSON。
	b, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	var tree any
	if err = json.Unmarshal(b, &tree); err != nil {
		return nil, err
	}
	for _, path := range d.Strip {
		strip(tree, splitPath(path))
	}
	if err = enforce(d.Kernel, tree, stats, ports, opt.CertDir); err != nil {
		return nil, fmt.Errorf("%s 驱动 %s 生成的配置不安全：%w", d.Kernel, d.Name, err)
	}
	return json.MarshalIndent(tree, "", "  ")
}

// —— 生成后的强制检查：驱动写错也绕不过 ——

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

func num(v any) (int, bool) {
	switch x := v.(type) {
	case float64:
		return int(x), x == float64(int(x))
	case string:
		n, err := strconv.Atoi(x)
		return n, err == nil
	}
	return 0, false
}

// enforce：统计 / API 只监听回环且用分配的端口；对外监听只用分配给节点的端口；证书只用托管目录；
// 不允许写日志文件或打开其他管理接口。
func enforce(kernel string, tree any, stats int, ports map[int]bool, certDir string) error {
	root, ok := tree.(map[string]any)
	if !ok {
		return errors.New("配置顶层必须是对象")
	}
	if err := checkPaths(root, certDir); err != nil {
		return err
	}
	inbounds, _ := root["inbounds"].([]any)
	switch kernel {
	case "xray":
		if log, ok := root["log"].(map[string]any); ok {
			for _, k := range []string{"access", "error"} {
				if s, _ := log[k].(string); s != "" && s != "none" {
					return errors.New("不允许把日志写到文件")
				}
			}
		}
		for _, x := range inbounds {
			in, _ := x.(map[string]any)
			port, _ := num(in["port"])
			listen, _ := in["listen"].(string)
			if port == stats {
				if !isLoopback(listen) {
					return fmt.Errorf("统计端口 %d 必须只监听回环地址", port)
				}
				continue
			}
			if port < 1 || port > 65535 {
				return fmt.Errorf("invalid inbound port: %v", in["port"])
			}
			if !ports[port] {
				return fmt.Errorf("入站监听了没有分配的端口 %v", in["port"])
			}
		}
		if api, ok := root["api"].(map[string]any); ok {
			if l, _ := api["listen"].(string); l != "" {
				host, _, _ := net.SplitHostPort(l)
				if !isLoopback(host) {
					return errors.New("Xray API 只能监听回环地址")
				}
			}
		}
	case "sing-box":
		if log, ok := root["log"].(map[string]any); ok {
			if s, _ := log["output"].(string); s != "" {
				return errors.New("不允许把日志写到文件")
			}
		}
		for _, x := range inbounds {
			in, _ := x.(map[string]any)
			port, _ := num(in["listen_port"])
			listen, _ := in["listen"].(string)
			if isLoopback(listen) && port == 0 {
				continue // 只在回环上的内部入站（如 ShadowTLS 后面的 Shadowsocks）
			}
			if port < 1 || port > 65535 {
				return fmt.Errorf("invalid inbound port: %v", in["listen_port"])
			}
			if !ports[port] {
				return fmt.Errorf("入站监听了没有分配的端口 %v", in["listen_port"])
			}
			if t, ok := in["tls"].(map[string]any); ok && t["enabled"] == true {
				cp, _ := t["certificate_path"].(string)
				kp, _ := t["key_path"].(string)
				if cp == "" || kp == "" {
					return errors.New("sing-box TLS inbound requires a managed certificate")
				}
			}
		}
		eps, _ := root["endpoints"].([]any)
		for _, x := range eps {
			ep, _ := x.(map[string]any)
			if port, _ := num(ep["listen_port"]); !ports[port] {
				return fmt.Errorf("endpoint 监听了没有分配的端口 %v", ep["listen_port"])
			}
		}
		exp, _ := root["experimental"].(map[string]any)
		for k := range exp {
			if k != "v2ray_api" {
				return fmt.Errorf("experimental 只允许 v2ray_api，不允许 %s", k)
			}
		}
		api, _ := exp["v2ray_api"].(map[string]any)
		if api == nil {
			return errors.New("sing-box stats api missing")
		}
		host, port, err := net.SplitHostPort(fmt.Sprint(api["listen"]))
		if err != nil || !isLoopback(host) || port != strconv.Itoa(stats) {
			return fmt.Errorf("v2ray_api 必须只监听 127.0.0.1:%d", stats)
		}
	}
	return nil
}

// checkPaths 遍历整个配置：证书与密钥文件只能在托管的证书目录里。
func checkPaths(node any, certDir string) error {
	switch n := node.(type) {
	case map[string]any:
		for k, v := range n {
			switch k {
			case "certificateFile", "keyFile", "certificate_path", "key_path":
				s, _ := v.(string)
				if s == "" {
					continue
				}
				if certDir == "" || !strings.HasPrefix(filepath.Clean(s), filepath.Clean(certDir)+string(filepath.Separator)) {
					return fmt.Errorf("证书文件 %s 不在托管目录里", s)
				}
			default:
				if err := checkPaths(v, certDir); err != nil {
					return err
				}
			}
		}
	case []any:
		for _, v := range n {
			if err := checkPaths(v, certDir); err != nil {
				return err
			}
		}
	}
	return nil
}

// strip 按点分路径删除字段；路径不存在时忽略。
func strip(doc any, path []string) {
	if len(path) == 0 {
		return
	}
	switch node := doc.(type) {
	case map[string]any:
		if path[0] == "*" {
			for k := range node {
				if len(path) == 1 {
					delete(node, k)
				} else {
					strip(node[k], path[1:])
				}
			}
			return
		}
		if len(path) == 1 {
			delete(node, path[0])
			return
		}
		strip(node[path[0]], path[1:])
	case []any:
		if path[0] == "*" {
			for _, item := range node {
				strip(item, path[1:])
			}
		}
	}
}

func splitPath(path string) []string {
	var parts []string
	for _, p := range strings.Split(path, ".") {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return parts
}

// —— 「一组」出口：全部成员都下发，由内核按延迟自动选、出故障自动切换 ——

// groupMemberBase 是成员出口的 ID 起点（与其他出口不冲突）。
const groupMemberBase int64 = 1 << 30

// group 是一个组：组本身与各成员出口的 ID。
type group struct {
	id      int64
	members []int64
}

// expandGroups 把组展开成成员出口；组本身留一个丢弃出口占位（规则照常指向它，模板再换成 balancer / urltest）。
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
