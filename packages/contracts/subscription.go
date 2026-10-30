package contracts

import "time"

// ProxyPolicy 是代理页的设置：按类型开关、按网站类别分流到哪一组、自动选择怎么做。
// 存在用户上（每个用户一份，作用于他的全部链接）；类型的出口是单独的链式代理（chain.policy_type），不在这里。
type ProxyPolicy struct {
	// Disabled 是不放进订阅的类型；新出现的类型默认放进去。
	Disabled []string `json:"disabled,omitempty"`
	// Rules：类别（RuleCategories 的 key）→ 目标。目标是 auto（自动选择）、select（节点选择）、direct（直连）、
	// reject（拒绝），或一个类型的 key（走这一类节点）。没写的类别用默认目标。
	Rules map[string]string `json:"rules,omitempty"`
	Auto  AutoPolicy        `json:"auto"`
}

// AutoPolicy：latency 由客户端按延迟测速自动选；ranked 由中心按各节点自己统计的下载速率（最近 24 小时有流量的
// 分钟的第 90 百分位）排序，订阅里只放 Top 个节点：前 Top−1 个按速率，留 1 个轮换位给还没有速率数据的节点，
// 客户端在其中再按延迟选。RefreshMinutes 是客户端刷新节点列表的间隔。
type AutoPolicy struct {
	Mode           string `json:"mode,omitempty"` // latency / ranked，空为 latency
	Top            int    `json:"top,omitempty"`
	RefreshMinutes int    `json:"refresh_minutes,omitempty"`
}

const (
	AutoLatency = "latency"
	AutoRanked  = "ranked"
	// MinRefreshMinutes 是节点列表刷新的最短间隔：太频繁会让中心压力变大、客户端来回切换。
	MinRefreshMinutes     = 30
	DefaultRefreshMinutes = 30
	DefaultRankedTop      = 3
)

// Normalized 补上默认值并把越界的值收回范围。
func (p ProxyPolicy) Normalized() ProxyPolicy {
	if p.Auto.Mode != AutoRanked {
		p.Auto.Mode = AutoLatency
	}
	if p.Auto.RefreshMinutes < MinRefreshMinutes {
		p.Auto.RefreshMinutes = DefaultRefreshMinutes
	}
	if p.Auto.RefreshMinutes > 24*60 {
		p.Auto.RefreshMinutes = 24 * 60
	}
	if p.Auto.Top <= 0 {
		p.Auto.Top = DefaultRankedTop
	}
	if p.Auto.Top > 20 {
		p.Auto.Top = 20
	}
	if p.Rules == nil {
		p.Rules = map[string]string{}
	}
	return p
}

// WeirSubscription 是 ?target=weir 的输出：给以后的 Weir 客户端用，带版本号的结构化订阅。
// 节点仍是标准分享链接；分组与规则和其他格式由同一份计划生成，客户端不必再解析 Clash 文本。
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
const WeirSubscriptionVersion = 1

type WeirUsage struct {
	Used  int64 `json:"used"`
	Quota int64 `json:"quota"` // 0 为不限
}

type WeirWindow struct {
	StartsAt *time.Time `json:"starts_at,omitempty"`
	EndsAt   *time.Time `json:"ends_at,omitempty"`
}

// WeirNode：Rank 是中心排序后的名次（从 1 起，0 为不可用、没有参与排序）；Health 是 ok / degraded / unknown。
// RateBps 是最近 24 小时下载速率的第 90 百分位（字节/秒），没有数据时为 0 且 RateMinutes 为 0；
// RateFrom 是 self（这个用户自己）或 all（全部用户，他自己没有数据时补上）。Exploring：这次是轮换位。
type WeirNode struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	URI         string `json:"uri"`
	Rank        int    `json:"rank,omitempty"`
	Health      string `json:"health"`
	RateBps     int64  `json:"rate_bps,omitempty"`
	RateMinutes int    `json:"rate_minutes,omitempty"`
	RateFrom    string `json:"rate_from,omitempty"`
	Auto        bool   `json:"auto"` // 是否在「自动选择」组里
	Exploring   bool   `json:"exploring,omitempty"`
}

type WeirGroup struct {
	Name    string   `json:"name"`
	Kind    string   `json:"kind"` // select / url-test
	Members []string `json:"members"`
}

// WeirRule：Kind 是 domain_suffix / ip_cidr / geoip / final；Values 为空时只有 final。
type WeirRule struct {
	Kind   string   `json:"kind"`
	Values []string `json:"values,omitempty"`
	Policy string   `json:"policy"`
}
