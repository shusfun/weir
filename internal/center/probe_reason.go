package center

func probeReasonText(code string) string {
	switch code {
	case "":
		return ""
	case "probe_certificate_failed":
		return "证书校验失败"
	case "probe_authentication_failed":
		return "节点认证失败"
	case "probe_port_refused":
		return "节点端口拒绝连接"
	case "probe_timeout":
		return "连接超时；不能据此判定封锁或防火墙原因"
	case "probe_address_rejected":
		return "测试地址不满足公网访问要求"
	case "probe_execution_failed":
		return "测试客户端执行失败，请查看任务环节记录"
	default:
		return "经节点连接失败"
	}
}
