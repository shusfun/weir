package center

import (
	"github.com/gin-gonic/gin"
)

// 链路页：把 VPS、出口画在平铺的世界地图上，连线是「入口 VPS → 出口」，每条线上是经过它的路径（用户 × 入口节点 × 出口）。
// 管理员看全部；普通用户只看自己链接里的节点和自己建的链式代理，管理员配的出口只显示类型与地区，不显示地址。
// 结构（快照）与实时速率（SSE）分开：结构变了推一个 topology 事件，页面重新取快照。

type mapGate struct {
	ID        int               `json:"id"`
	Name      string            `json:"name"`
	Online    bool              `json:"online"`
	Place     geoPlace          `json:"place"`
	Host      map[string]any    `json:"host,omitempty"`
	State     string            `json:"state,omitempty"`
	LastError string            `json:"last_error,omitempty"`
	Kernels   map[string]string `json:"kernels,omitempty"`
	Version   string            `json:"version,omitempty"`
	LastSeen  any               `json:"last_seen_at,omitempty"`
	Entry     bool              `json:"entry"` // 有入口节点（否则只是别的链的出口落点）
	Nodes     []mapNode         `json:"nodes"`
}

type mapNode struct {
	ID       int          `json:"id"`
	Name     string       `json:"name"`
	Protocol string       `json:"protocol"`
	Network  string       `json:"network"`
	Security string       `json:"security"`
	Port     int          `json:"port"`
	Kernel   string       `json:"kernel,omitempty"`
	Enabled  bool         `json:"enabled"`
	Ports    []portNeed   `json:"ports,omitempty"`
	Reach    *reachResult `json:"reach,omitempty"`
	Users    int          `json:"users,omitempty"`
}

// mapExit 是一个出口；同一台入口 VPS 到它的线就是这条记录（ID 已含入口）。
type mapExit struct {
	ID       string     `json:"id"`
	Kind     string     `json:"kind"` // direct / socks5 / http / https / link / node / source / out / block
	Label    string     `json:"label"`
	Sub      string     `json:"sub,omitempty"`
	FromGate int        `json:"from_gate"`
	ToGate   int        `json:"to_gate,omitempty"` // 另一台 VPS 的节点作出口
	Owner    string     `json:"owner"`             // admin / self
	Address  string     `json:"address,omitempty"`
	Place    geoPlace   `json:"place"`
	Probe    *exitProbe `json:"probe,omitempty"`
	Members  []string   `json:"members,omitempty"`
	Skipped  int        `json:"skipped,omitempty"`
	Link     string     `json:"link,omitempty"`  // 点击后去的地方
	Group    bool       `json:"group,omitempty"` // 一组：成员由内核自动切换
	tag      string     // Gate 配置里的出口标签（与 compileRoutes 一致），实时计数据此对应
}

type mapPath struct {
	ID         string `json:"id"`
	UserID     int    `json:"user_id"`
	User       string `json:"user"`
	NodeID     int    `json:"node_id"`
	Node       string `json:"node"`
	GateID     int    `json:"gate_id"`
	ChainID    int    `json:"chain_id,omitempty"`
	Chain      string `json:"chain,omitempty"`
	Route      string `json:"route"` // chain：用户自建链式代理；nodechain：节点的链式代理；direct：直接出去
	ExitID     string `json:"exit_id"`
	TodayBytes *int64 `json:"today_bytes"`
}

type mapRule struct {
	ID     int    `json:"id"`
	GateID int    `json:"gate_id"`
	Label  string `json:"label"`
	ExitID string `json:"exit_id"`
	NodeID *int   `json:"node_id,omitempty"`
	UserID *int   `json:"user_id,omitempty"`
}

type mapUser struct {
	ID    int      `json:"id"`
	Name  string   `json:"name"`
	Paths []string `json:"paths"`
}

type linkMap struct {
	Admin   bool       `json:"admin"`
	Version string     `json:"version"`
	Geo     any        `json:"geo"`
	Gates   []*mapGate `json:"gates"`
	Exits   []*mapExit `json:"exits"`
	Paths   []*mapPath `json:"paths"`
	Rules   []mapRule  `json:"rules,omitempty"`
	Users   []mapUser  `json:"users,omitempty"`
	Hidden  int        `json:"hidden_rules,omitempty"` // 普通用户：入口上有多少条管理员分流规则（不列出内容）
	index   liveIndex
}

// liveIndex 把实时计数对应到页面上的对象：凭据 → 路径，出口标签 → 出口（一组的成员按顺序）。
type liveIndex struct {
	keys    map[int]map[string]string   // Gate → 统计键 → 路径
	tags    map[int]map[string]string   // Gate → 出口标签 → 出口（只管理员用）
	members map[int]map[string][]string // Gate → 出口 → 成员出口标签
	gates   []int
}

func (s *Server) linkMapRoutes(auth, admin *gin.RouterGroup) {
	admin.GET("/linkmap", func(c *gin.Context) { s.linkMapHandler(c, true) })
	admin.GET("/linkmap/live", func(c *gin.Context) { s.linkMapLive(c, true) })
	auth.GET("/me/linkmap", func(c *gin.Context) { s.linkMapHandler(c, false) })
	auth.GET("/me/linkmap/live", func(c *gin.Context) { s.linkMapLive(c, false) })
}

func (s *Server) linkMapHandler(c *gin.Context, admin bool) {
	viewer := 0
	if !admin {
		viewer = c.GetInt("user_id")
	}
	m, err := s.buildLinkMap(c.Request.Context(), viewer)
	if err != nil {
		failInternal(c, 503, "query_failed", err)
		return
	}
	c.JSON(200, m)
}
