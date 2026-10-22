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
	ActionOutboundProbe  = "outbound.probe"
	ActionDiagnostics    = "core.diagnostics"
	ActionCoreRestart    = "core.restart"
	ActionUninstall      = "gate.uninstall"
)

type ActionResult struct {
	ID     int64          `json:"id"`
	OK     bool           `json:"ok"`
	Error  string         `json:"error,omitempty"`
	Result map[string]any `json:"result,omitempty"`
}
