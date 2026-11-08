package contracts

import "errors"

// NodeTestClient 保留订阅里的单个 mihomo 代理条目。
type NodeTestClient struct {
	Proxies []map[string]any `json:"proxies"`
	Target  string           `json:"target"`
}

// Loopback 只重定向网络端点，保留证书名称、插件与协议参数。
func (c NodeTestClient) Loopback(port int) error {
	if len(c.Proxies) != 1 || c.Target == "" || port < 1 || port > 65535 {
		return errors.New("测试客户端缺少节点")
	}
	p := c.Proxies[0]
	if p["name"] != c.Target || p["server"] == nil {
		return errors.New("测试客户端网络端点无效")
	}
	p["server"], p["port"] = "127.0.0.1", port
	return nil
}

func (c NodeTestClient) Config(port int) map[string]any {
	return map[string]any{"mixed-port": port, "bind-address": "127.0.0.1", "allow-lan": false, "mode": "rule", "log-level": "warning", "ipv6": true,
		"proxies": c.Proxies, "rules": []string{"MATCH," + c.Target}}
}
