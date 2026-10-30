package center

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/link"
	"github.com/shusfun/weir/internal/store/ent/subpull"
	"github.com/shusfun/weir/internal/store/ent/user"
)

// 订阅管理：统计每条链接被拉取的情况（次数、最后一次、用的什么客户端、从哪里来）与用量。
// 拉取记录保留 30 天，只给链接的主人与管理员看。

const pullKeep = 30 * 24 * time.Hour

// clientNames 按 User-Agent 认客户端（先匹配的优先，长的写在前面）。
var clientNames = [][2]string{
	{"clash-verge", "Clash Verge"}, {"clash verge", "Clash Verge"}, {"mihomo.party", "Mihomo Party"}, {"mihomo-party", "Mihomo Party"}, {"clash party", "Clash Party"},
	{"flclash", "FlClash"}, {"stash", "Stash"}, {"nyanpasu", "Clash Nyanpasu"}, {"clashmetaforandroid", "Clash Meta for Android"}, {"clash.meta", "Clash Meta"},
	{"mihomo", "mihomo"}, {"clash", "Clash"}, {"hiddify", "Hiddify"}, {"sfa", "sing-box (Android)"}, {"sfi", "sing-box (iOS)"}, {"sfm", "sing-box (macOS)"},
	{"sing-box", "sing-box"}, {"karing", "Karing"}, {"shadowrocket", "Shadowrocket"}, {"quantumult", "Quantumult X"}, {"surge", "Surge"}, {"loon", "Loon"},
	{"egern", "Egern"}, {"v2rayng", "v2rayNG"}, {"v2rayn", "v2rayN"}, {"nekobox", "NekoBox"}, {"nekoray", "NekoRay"}, {"streisand", "Streisand"},
	{"curl", "curl"}, {"wget", "wget"},
}

func clientName(ua string) string {
	low := strings.ToLower(ua)
	for _, c := range clientNames {
		if strings.Contains(low, c[0]) {
			return c[1]
		}
	}
	return ""
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	// 按字节截断时不切断多字节字符
	for n > 0 && n < len(s) && s[n]&0xC0 == 0x80 {
		n--
	}
	return s[:n]
}

// recordPull 记一次拉取；写入失败不影响订阅本身。
func (s *Server) recordPull(c *gin.Context, l *ent.Link, target, status string) {
	ua := c.GetHeader("User-Agent")
	_ = s.Store.Ent.SubPull.Create().SetLinkID(l.ID).SetUserID(l.UserID).SetAt(time.Now().UTC()).SetTarget(truncate(target, 16)).
		SetClient(clientName(ua)).SetUserAgent(truncate(ua, 256)).SetIP(truncate(c.ClientIP(), 64)).SetStatus(status).Exec(context.WithoutCancel(c.Request.Context()))
}

func (s *Server) prunePulls(ctx context.Context, now time.Time) error {
	_, err := s.Store.Ent.SubPull.Delete().Where(subpull.AtLT(now.Add(-pullKeep))).Exec(ctx)
	return err
}

func (s *Server) pullRoutes(auth, admin *gin.RouterGroup) {
	auth.GET("/me/link-stats", func(c *gin.Context) { s.linkStats(c, false) })
	admin.GET("/link-stats", func(c *gin.Context) { s.linkStats(c, true) })
	auth.GET("/me/links/:lid/pulls", func(c *gin.Context) { s.linkPulls(c, false) })
	admin.GET("/users/:uid/links/:lid/pulls", func(c *gin.Context) { s.linkPulls(c, true) })
}

type clientCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// linkStats：普通用户是自己的全部链接；管理员是全部用户的链接，可用 ?user= 只看一个用户。
func (s *Server) linkStats(c *gin.Context, admin bool) {
	uid := 0
	if !admin {
		uid = c.MustGet("account").(*ent.User).ID
	} else if id, err := strconv.Atoi(c.Query("user")); err == nil && id > 0 {
		uid = id
	}
	out, err := s.collectLinkStats(c.Request.Context(), uid, true)
	if err != nil {
		failInternal(c, 503, "query_failed", err)
		return
	}
	c.JSON(200, gin.H{"items": out, "keep_days": int(pullKeep.Hours() / 24)})
}

// collectLinkStats 汇总链接最近 30 天的拉取；uid 为 0 是全部用户。withSource 为假时（给 dsh）最近一次拉取不带来源 IP、
// 位置、运营商与 User-Agent，只留时间、客户端、格式与结果。
func (s *Server) collectLinkStats(ctx context.Context, uid int, withSource bool) ([]gin.H, error) {
	q := s.Store.Ent.User.Query().Order(ent.Asc(user.FieldID))
	if uid > 0 {
		q = q.Where(user.IDEQ(uid))
	}
	users, err := q.All(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	day, month := now.Add(-24*time.Hour), now.Add(-pullKeep)
	out := []gin.H{}
	for _, u := range users {
		links, err := s.Store.Ent.Link.Query().Where(link.UserIDEQ(u.ID)).Order(ent.Asc(link.FieldID)).All(ctx)
		if err != nil {
			return nil, err
		}
		for _, l := range links {
			pulls, err := s.Store.Ent.SubPull.Query().Where(subpull.LinkIDEQ(l.ID), subpull.AtGTE(month)).Order(ent.Desc(subpull.FieldAt)).All(ctx)
			if err != nil {
				return nil, err
			}
			counts := map[string]int{}
			ips := map[string]bool{}
			last24, refused := 0, 0
			for _, p := range pulls {
				if p.At.After(day) {
					last24++
				}
				if p.Status != "ok" {
					refused++
				}
				counts[firstNonEmpty(p.Client, "其他")]++
				ips[p.IP] = true
			}
			clients := make([]clientCount, 0, len(counts))
			for name, n := range counts {
				clients = append(clients, clientCount{name, n})
			}
			sort.Slice(clients, func(i, j int) bool {
				if clients[i].Count != clients[j].Count {
					return clients[i].Count > clients[j].Count
				}
				return clients[i].Name < clients[j].Name
			})
			start, end := linkWindow(u, l)
			v := gin.H{"id": l.ID, "name": l.Name, "user_id": u.ID, "user": firstNonEmpty(u.Name, u.Email), "enabled": l.Enabled,
				"state": windowState(u, l, now), "starts_at": start, "ends_at": end, "used": l.Used,
				"pulls_24h": last24, "pulls_30d": len(pulls), "refused_30d": refused, "sources_30d": len(ips), "clients": clients, "last_used_at": l.LastUsedAt}
			if len(pulls) > 0 {
				if withSource {
					v["last_pull"] = s.pullView(ctx, pulls[0])
				} else {
					p := pulls[0]
					v["last_pull"] = gin.H{"at": p.At, "target": p.Target, "client": p.Client, "status": p.Status}
				}
			}
			out = append(out, v)
		}
	}
	return out, nil
}

func (s *Server) pullView(ctx context.Context, p *ent.SubPull) gin.H {
	v := gin.H{"at": p.At, "target": p.Target, "client": p.Client, "user_agent": p.UserAgent, "ip": p.IP, "status": p.Status}
	if p.IP != "" {
		if place := s.locate(ctx, p.IP); place.Known {
			v["place"] = strings.Join(nonEmpty(place.Country, place.Region, place.City), " · ")
			v["org"] = place.Org
		}
	}
	return v
}

func nonEmpty(v ...string) []string {
	out := v[:0:0]
	for _, s := range v {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// linkPulls 是一条链接最近的拉取记录（最多 200 条，新的在前）。
func (s *Server) linkPulls(c *gin.Context, admin bool) {
	u, ok := s.subject(c, admin)
	if !ok {
		return
	}
	l, ok := s.linkParam(c, u)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	pulls, err := s.Store.Ent.SubPull.Query().Where(subpull.LinkIDEQ(l.ID)).Order(ent.Desc(subpull.FieldAt)).Limit(200).All(ctx)
	if err != nil {
		failInternal(c, 503, "query_failed", err)
		return
	}
	out := make([]gin.H, 0, len(pulls))
	for _, p := range pulls {
		out = append(out, s.pullView(ctx, p))
	}
	c.JSON(200, gin.H{"items": out})
}
