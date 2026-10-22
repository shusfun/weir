package contracts

import "time"

// GateConfig 是中心下发给某台 Gate 的完整期望配置。Gate 只按版本号整份应用，
// 用同一程序内的构建器生成内核配置；中心离线时仍可据此在本地剔除到期 / 超额凭据。
type GateConfig struct {
	Revision    int64                 `json:"revision"`
	Version     string                `json:"version"`  // 中心程序版本；与 Gate 不一致时 Gate 先自更新
	Kernels     map[string]Kernel     `json:"kernels"`  // "xray" / "sing-box"
	Profiles    map[string]string     `json:"profiles"` // 内核 → 版本档案 YAML（已按锁定版本选好）
	Ports       PortPlan              `json:"ports"`
	Inbounds    []Inbound             `json:"inbounds"`
	Outbounds   []Outbound            `json:"outbounds"`
	Routes      []Route               `json:"routes"`
	Credentials []Credential          `json:"credentials"`
	Certs       []CertRef             `json:"certs"`
	Budgets     map[string]GateBudget `json:"budgets"` // 用户键 → 本 Gate 可用额度
	Meta        map[string]string     `json:"meta,omitempty"`
}

// Kernel 指定内核版本与官方（或 CI 编译）摘要；Gate 从中心下载并校验。
type Kernel struct {
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
}

// PortPlan 由中心按 Gate 上报的占用端口分配，只监听回环。
type PortPlan struct {
	XrayStats    int `json:"xray_stats"`
	SingboxStats int `json:"singbox_stats"`
}

// Inbound 对应一个节点。Settings 存放协议相关字段（与构建器字段同名）。
type Inbound struct {
	ID       int64          `json:"id"`
	Tag      string         `json:"tag"`
	Kernel   string         `json:"kernel"`
	Protocol string         `json:"protocol"`
	Port     int            `json:"port"`
	Network  string         `json:"network"`
	Security string         `json:"security"`
	CertID   int64          `json:"cert_id,omitempty"`
	Settings map[string]any `json:"settings,omitempty"`
}

// Outbound 是出口：直连、家宽 SOCKS5、另一台 Gate 或其他协议。
type Outbound struct {
	ID       int64          `json:"id"`
	Tag      string         `json:"tag"`
	Protocol string         `json:"protocol"`
	Address  string         `json:"address,omitempty"`
	Port     int            `json:"port,omitempty"`
	Username string         `json:"username,omitempty"`
	Password string         `json:"password,omitempty"`
	Settings map[string]any `json:"settings,omitempty"`
}

// Route 把某入站（可限定某凭据）的流量送往指定出口；Priority 小者优先。
type Route struct {
	ID          int64  `json:"id"`
	InboundTag  string `json:"inbound_tag,omitempty"`
	StatsKey    string `json:"stats_key,omitempty"`
	OutboundTag string `json:"outbound_tag"`
	Domain      string `json:"domain,omitempty"`
	IP          string `json:"ip,omitempty"`
	Priority    int    `json:"priority"`
}

// Credential 是某个链接在某个节点上的独立凭据。StatsKey 用作内核里的用户标识。
// Internal 为真时是系统内部凭据（如链式代理入口到出口那一跳），不计入用户额度。
type Credential struct {
	ID        int64      `json:"id"`
	InboundID int64      `json:"inbound_id"`
	StatsKey  string     `json:"stats_key"`
	UserKey   string     `json:"user_key"`
	UUID      string     `json:"uuid,omitempty"`
	Password  string     `json:"password,omitempty"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	Internal  bool       `json:"internal,omitempty"`
}

// GateBudget 是某用户在本 Gate 上累计可用的字节数；Unlimited 时不限。
// Used 是中心已记账的该用户在本 Gate 的累计用量：Gate 在此基础上叠加尚未上报的本地用量，
// 中心离线时仍能在用满 Allocated 时停用该用户。
type GateBudget struct {
	Allocated int64 `json:"allocated"`
	Used      int64 `json:"used"`
	Unlimited bool  `json:"unlimited,omitempty"`
}

// CertRef 引用一张证书；Gate 按 ID 下载 PEM 并核对摘要。
type CertRef struct {
	ID     int64  `json:"id"`
	SHA256 string `json:"sha256"`
}

// GateState 是 Gate 应用配置后的上报。
type GateState struct {
	AppliedRevision int64  `json:"applied_revision"`
	FailedRevision  int64  `json:"failed_revision,omitempty"`
	Error           string `json:"error,omitempty"`
	ConflictPort    int    `json:"conflict_port,omitempty"`
}
