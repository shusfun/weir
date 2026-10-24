package center

import (
	"errors"
	"fmt"
	"strings"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/packages/contracts"
)

func nodeSetting(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return strings.TrimSpace(v)
}

func firstOf(list string) string {
	for _, part := range strings.Split(list, ",") {
		if p := strings.TrimSpace(part); p != "" {
			return p
		}
	}
	return ""
}

func nodeTag(id int) string { return fmt.Sprintf("node-%d", id) }

// socksExit 生成家宽 SOCKS5 出口。
func socksExit(tag, address string, port int, username, password string) contracts.Outbound {
	return contracts.Outbound{Tag: tag, Protocol: "socks", Address: address, Port: port, Username: username, Password: password}
}

// nodeExit 生成「经另一台 Gate 的节点出去」的出口：入口 Gate 以内部凭据作为客户端连接出口节点。
// 出口节点的参数（REALITY 公钥、SNI、传输）与该节点的入站完全一致。
func nodeExit(tag string, exit *ent.Node, host string, cred *ent.Credential) (contracts.Outbound, error) {
	if host == "" {
		return contracts.Outbound{}, errors.New("exit gate has no public address")
	}
	st := exit.Settings
	o := contracts.Outbound{Tag: tag, Protocol: exit.Protocol, Address: host, Port: exit.Port}
	set := map[string]any{"network": exit.Network}
	switch exit.Network {
	case "ws":
		set["path"], set["host"] = nodeSetting(st, "ws_path"), nodeSetting(st, "ws_host")
	case "grpc":
		set["service_name"] = nodeSetting(st, "grpc_service_name")
	case "xhttp":
		set["path"] = nodeSetting(st, "xhttp_path")
	}
	switch exit.Security {
	case "reality":
		set["security"], set["reality"] = "reality", true
		set["pbk"], set["sid"] = nodeSetting(st, "reality_public_key"), firstOf(nodeSetting(st, "reality_short_id"))
		set["sni"], set["fp"] = firstOf(nodeSetting(st, "reality_server_names")), "chrome"
		if exit.Protocol == "vless" && exit.Network != "xhttp" {
			set["flow"] = "xtls-rprx-vision"
		}
	case "tls":
		set["security"], set["tls"] = "tls", true
		set["sni"] = nodeSetting(st, "tls_sni")
	}
	switch exit.Protocol {
	case "vless", "vmess":
		o.Username = cred.UUID
	case "trojan":
		o.Password = cred.Password
	default:
		return contracts.Outbound{}, fmt.Errorf("节点协议 %s 不能作为链式代理出口", exit.Protocol)
	}
	o.Settings = set
	return o, nil
}
