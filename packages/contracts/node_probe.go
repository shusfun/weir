package contracts

import (
	"errors"
)

// NodeTestClient 保留订阅客户端的完整依赖图；Target 是需要实测的节点标签。
type NodeTestClient struct {
	Outbounds []map[string]any `json:"outbounds"`
	Endpoints []map[string]any `json:"endpoints,omitempty"`
	Target    string           `json:"target"`
}

// Loopback 只重定向实际连接节点的网络端点，保留 detour 标签和 WireGuard peer 的其他属性。
func (c NodeTestClient) Loopback(port int) error {
	if c.Target == "" {
		return errors.New("test target missing")
	}
	items := append(append([]map[string]any{}, c.Outbounds...), c.Endpoints...)
	byTag := map[string]map[string]any{}
	for _, o := range items {
		tag, _ := o["tag"].(string)
		if tag == "" || byTag[tag] != nil {
			return errors.New("invalid test client tags")
		}
		byTag[tag] = o
	}
	seen := map[string]bool{}
	tag := c.Target
	for {
		if seen[tag] {
			return errors.New("test client detour cycle")
		}
		seen[tag] = true
		o := byTag[tag]
		if o == nil {
			return errors.New("test client dependency missing")
		}
		if next, _ := o["detour"].(string); next != "" {
			tag = next
			continue
		}
		if o["type"] == "wireguard" {
			peers, _ := o["peers"].([]any)
			if len(peers) != 1 {
				return errors.New("test WireGuard peer missing")
			}
			peer, ok := peers[0].(map[string]any)
			if !ok {
				return errors.New("invalid test WireGuard peer")
			}
			peer["address"], peer["port"] = "127.0.0.1", port
		} else {
			if _, ok := o["server"]; !ok {
				return errors.New("test network endpoint missing")
			}
			o["server"], o["server_port"] = "127.0.0.1", port
		}
		return nil
	}
}
