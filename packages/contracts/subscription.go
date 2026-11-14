package contracts

import "time"

// SubscriptionMember references database IDs; its position is the output order.
type SubscriptionMember struct {
	Kind string `json:"kind"`
	ID   int    `json:"id"`
}

// ProxyPolicy 是每个用户的客户端选择与有序清单。
type ProxyPolicy struct {
	Version            int          `json:"version"`
	Revision           int          `json:"revision"`
	HiddenLines        []string     `json:"hidden_lines,omitempty"`
	Disabled           []string     `json:"disabled,omitempty"`
	Auto               AutoPolicy   `json:"auto"`
	Groups             []ProxyGroup `json:"groups,omitempty"`
	NextGroupID        int          `json:"next_group_id,omitempty"`
	TargetMigration    []string     `json:"target_migration,omitempty"`
	MigrationConfirmed bool         `json:"migration_confirmed"`
	Custom             CustomRules  `json:"custom"`
}

const MaxProxyGroups = 20

// ProxyGroup 的创建顺序与 Members 顺序稳定，不受观测变化影响。
type ProxyGroup struct {
	ID              int      `json:"id"`
	Name            string   `json:"name"`
	Kind            string   `json:"kind"`
	Members         []string `json:"members"`
	IntervalSeconds int      `json:"interval_seconds"`
	ToleranceMS     int      `json:"tolerance_ms"`
}

// Revision 记录清单的生效版本，写入使用 ProxyPolicy.Revision 校验并发。
type CustomRules struct {
	Version  int              `json:"version"`
	Revision int              `json:"revision"`
	Entries  []CustomEntry    `json:"entries"`
	Pending  *CustomCandidate `json:"pending,omitempty"`
	Source   string           `json:"source,omitempty"`
	Check    *CustomCheck     `json:"check,omitempty"`
}
type CustomEntry struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Value   string `json:"value"`
	Target  string `json:"target"`
	Enabled bool   `json:"enabled"`
	Notice  string `json:"notice,omitempty"`
}
type CustomCandidate struct {
	Entries []CustomEntry `json:"entries"`
	Source  string        `json:"source,omitempty"`
}
type CustomCheck struct {
	Status string    `json:"status"`
	Detail string    `json:"detail,omitempty"`
	At     time.Time `json:"at"`
}
type AutoPolicy struct {
	RefreshMinutes int `json:"refresh_minutes"`
}

const (
	MinRefreshMinutes     = 30
	DefaultRefreshMinutes = 30
)

func (p ProxyPolicy) Normalized() ProxyPolicy {
	if p.Auto.RefreshMinutes < 30 {
		p.Auto.RefreshMinutes = 30
	}
	if p.Auto.RefreshMinutes > 1440 {
		p.Auto.RefreshMinutes = 1440
	}
	return p
}

// WeirSubscription 是 ?target=weir 的输出：给以后的 Weir 客户端用，带版本号的结构化订阅。
// 节点仍是标准节点链接；分组与规则和其他格式由同一份计划生成，客户端不必再解析 Clash 文本。
type WeirSubscription struct {
	Version   int               `json:"version"`
	Name      string            `json:"name"`
	Generated time.Time         `json:"generated_at"`
	Usage     WeirUsage         `json:"usage"`
	Window    WeirWindow        `json:"window"`
	Refresh   int               `json:"refresh_minutes"`
	Auto      AutoPolicy        `json:"auto"`
	Nodes     []WeirNode        `json:"nodes"`
	Groups    []WeirGroup       `json:"groups"`
	Rules     []WeirRule        `json:"rules"`
	Types     map[string]string `json:"types"` // 类型 key → 显示名称
}

// WeirSubscriptionVersion 在结构有不兼容变化时加一。
const WeirSubscriptionVersion = 2

type WeirUsage struct {
	Used      int64 `json:"used"`
	Quota     int64 `json:"quota"`
	Unlimited bool  `json:"unlimited"`
}

type WeirWindow struct {
	StartsAt *time.Time `json:"starts_at,omitempty"`
	EndsAt   *time.Time `json:"ends_at,omitempty"`
}

// WeirNode：Rank 是中心排序后的名次（从 1 起，0 为不可用、没有参与排序）；Health 是 ok / degraded / unknown。
// RateBps 是最近 24 小时已完成且下载至少 1 MiB 的有效分钟速度的第 90 百分位（字节/秒），没有数据时为 0 且 RateMinutes 为 0；
// RateFrom 是 self（这个用户自己）或 all（全部用户，他自己没有数据时补上）。
type WeirNode struct {
	Ref         string `json:"ref"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	URI         string `json:"uri"`
	Rank        int    `json:"rank,omitempty"`
	Health      string `json:"health"`
	RateBps     int64  `json:"rate_bps,omitempty"`
	RateMinutes int    `json:"rate_minutes,omitempty"`
	RateFrom    string `json:"rate_from,omitempty"`
	Auto        bool   `json:"auto"`          // 是否在「自动选择」组里
	UDP         *bool  `json:"udp,omitempty"` // nil means the exit capability has not been measured.
}

type WeirGroup struct {
	Ref             string   `json:"ref"`
	Name            string   `json:"name"`
	Kind            string   `json:"kind"` // select / url-test / fallback
	Members         []string `json:"members"`
	MemberRefs      []string `json:"member_refs"`
	ProbeURL        string   `json:"probe_url,omitempty"`
	IntervalSeconds int      `json:"interval_seconds,omitempty"`
	ToleranceMS     int      `json:"tolerance_ms,omitempty"`
	Hidden          bool     `json:"hidden"`
}

// WeirRule：Kind 是 domain / domain_suffix / domain_keyword / ip_cidr / geoip / final；Values 为空时只有 final。
type WeirRule struct {
	Kind   string   `json:"kind"`
	Values []string `json:"values,omitempty"`
	Policy string   `json:"policy"`
}
