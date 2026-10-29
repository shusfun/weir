package contracts

import "time"

// PairRequest 是 Gate 首次配对。Token 为一次性预批准凭证（本机配对或「添加 Gate」生成）。
type PairRequest struct {
	Name      string `json:"name"`
	PublicKey string `json:"public_key"` // Ed25519，base64
	Token     string `json:"token"`
}

type PairResult struct {
	GateID int64 `json:"gate_id"`
}

// Heartbeat 每 30 秒上报一次；ListeningPorts 为本机实际在监听的端口（含其他程序）。
type Heartbeat struct {
	Version        string            `json:"version"`
	Hostname       string            `json:"hostname"`
	Arch           string            `json:"arch"`
	ListeningPorts []ListeningPort   `json:"listening_ports"`
	Kernels        map[string]string `json:"kernels"` // 实际运行的内核版本；缺失表示未运行
	Host           *HostSample       `json:"host,omitempty"`
	Center         string            `json:"center,omitempty"` // Gate 正在连接的中心地址
	Panel          *PanelStatus      `json:"panel,omitempty"`
	// PanelCerts 是本机 1Panel 证书的当前状态（证书 ID → 摘要与到期），1Panel 续签后中心据此更新。
	PanelCerts map[string]PanelCert `json:"panel_certs,omitempty"`
	// Firewall 是本机防火墙的状态（读不到时为空，中心显示「未知」）。
	Firewall *FirewallStatus `json:"firewall,omitempty"`
}

// PanelStatus 是 Gate 本机 1Panel 的状态。API 密钥只存在 Gate 本机（root 可读），从不上报。
type PanelStatus struct {
	Detected   bool   `json:"detected"`
	Configured bool   `json:"configured"`
	Version    string `json:"version,omitempty"`
	Error      string `json:"error,omitempty"`
}

// PanelCert 是 1Panel 管理的一张证书：证书正文（公开）、证书与私钥一起的摘要、到期时间。
type PanelCert struct {
	CertPEM  string    `json:"cert_pem,omitempty"`
	SHA256   string    `json:"sha256"`
	NotAfter time.Time `json:"not_after"`
}

// FirewallStatus 是 Gate 本机的防火墙：Tool 为 ufw / firewalld / iptables / none（没有防火墙）；
// Active 为假表示未启用（全部放行）；Rules 是放行的端口，形如 443/tcp、20000:20100/udp、8443（不分协议）。
type FirewallStatus struct {
	Tool   string   `json:"tool"`
	Active bool     `json:"active"`
	Rules  []string `json:"rules,omitempty"`
	Error  string   `json:"error,omitempty"`
}

// HeartbeatReply 是中心对心跳的回复。Center 为中心记录的公共地址；与 Gate 当前连接的不同时，
// Gate 先用新地址完成设备认证，成功后改用新地址（单机角色经回环连接，不切换）。
type HeartbeatReply struct {
	ConfigRevision int64  `json:"config_revision"`
	Center         string `json:"center,omitempty"`
}

type ListeningPort struct {
	Port    int    `json:"port"`
	Proto   string `json:"proto"` // tcp / udp
	Process string `json:"process,omitempty"`
}

// HostSample 只读主机观测；读取失败的字段保持为空，中心显示为「未知」。
type HostSample struct {
	ObservedAt time.Time `json:"observed_at"`
	CPUPercent *float64  `json:"cpu_percent,omitempty"`
	Load1      *float64  `json:"load1,omitempty"`
	MemTotal   *uint64   `json:"mem_total,omitempty"`
	MemUsed    *uint64   `json:"mem_used,omitempty"`
	DiskTotal  *uint64   `json:"disk_total,omitempty"`
	DiskUsed   *uint64   `json:"disk_used,omitempty"`
	NetRxBytes *uint64   `json:"net_rx_bytes,omitempty"`
	NetTxBytes *uint64   `json:"net_tx_bytes,omitempty"`
	UptimeSecs *uint64   `json:"uptime_secs,omitempty"`
}

// MeterReport 上报每个凭据自开机以来的累计字节数（单调递增，带纪元以识别内核重启）。
// 中心按差值记账；累计值回退或纪元变化时只按新纪元重新起算，不重复扣减。
type MeterReport struct {
	Revision int64         `json:"revision"`
	Epoch    string        `json:"epoch"`
	Seq      int64         `json:"seq"`
	Usage    []MeterSample `json:"usage"`
	Status   string        `json:"status"` // fresh / unavailable
}

type MeterSample struct {
	StatsKey string `json:"stats_key"`
	Up       int64  `json:"up"`
	Down     int64  `json:"down"`
}

// Action 是 Gate 需要执行的一次性动作（白名单）；与期望配置分开，按 ID 幂等。
type Action struct {
	ID        int64          `json:"id"`
	Kind      string         `json:"kind"`
	Payload   map[string]any `json:"payload,omitempty"`
	ExpiresAt time.Time      `json:"expires_at"`
}

// 允许的动作种类；Gate 收到其他种类一律拒绝。
const (
	ActionReleaseReplace = "release.replace"
	ActionRealityProbe   = "reality.probe"
	ActionChainProbe     = "chain.probe"
	// ActionLiveStart 让 Gate 在 seconds 秒内每 2 秒上报一次实时计数（有人打开链路页时才发）。
	ActionLiveStart     = "live.start"
	ActionOutboundProbe = "outbound.probe"
	ActionDiagnostics   = "core.diagnostics"
	ActionCoreRestart   = "core.restart"
	ActionUninstall     = "gate.uninstall"
	// ActionNodeTest 在 Gate 本机回环上实测一份候选入站：起服务端内核与 sing-box 客户端，经代理真实下载。
	ActionNodeTest = "node.test"
	// ActionACMEHTTP01 让 Gate 临时在 80 端口应答一次 ACME HTTP-01 验证（只回应验证路径，用完即关）。
	ActionACMEHTTP01 = "acme.http01"
	// ActionPanelConfigure 把管理员填写的 1Panel API 密钥交给 Gate（经一次性编号取回，中心不落库）。
	ActionPanelConfigure = "panel.configure"
	ActionPanelRemove    = "panel.remove"
	// ActionPanelCert 让 Gate 经本机 1Panel 申请（或找到已有的）本机 IP 证书。
	ActionPanelCert = "panel.cert"
)

type ActionResult struct {
	ID     int64          `json:"id"`
	OK     bool           `json:"ok"`
	Error  string         `json:"error,omitempty"`
	Result map[string]any `json:"result,omitempty"`
}

// LiveReport 是实时上报：各凭据与各出口（GateConfig 里的出口标签，两个内核相加）的累计计数，以及网卡收发累计；中心按相邻两次算速率。
// 只放在中心内存里，不写数据库；额度与用量仍以 MeterReport 为准。
type LiveReport struct {
	At        time.Time     `json:"at"`
	Users     []MeterSample `json:"users"`
	Outbounds []MeterSample `json:"outbounds"`
	NetRx     *uint64       `json:"net_rx,omitempty"`
	NetTx     *uint64       `json:"net_tx,omitempty"`
}
