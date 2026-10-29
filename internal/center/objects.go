package center

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/shusfun/weir/internal/kernels"
	"github.com/shusfun/weir/internal/store/ent/resource"
)

// 对象目录：控制台里能被引用、能跳过去的对象类型。每类对象在控制台有固定地址（见 apps/console/src/lib/objects.ts），
// 对象链接、dsh 的界面导航、页面感知里的「正在看的对象」都用这一份类型表。这里只负责确认对象存在并给出名称与归属；
// 地址由控制台按类型生成。

// objectTypes 与控制台 lib/objects.ts 的 ObjectType 一一对应。
var objectTypes = []string{"gate", "node", "user", "link", "chain", "driver", "kernel", "source", "profile", "page", "cert", "audit"}

var (
	objectKey = regexp.MustCompile(`^[A-Za-z0-9._@:-]{1,80}$`)
	kernelKey = regexp.MustCompile(`^([a-z-]+)(?:@([0-9A-Za-z.+-]+))?$`)
)

// objectRef 是解析后的对象：Owner 是链接与链式代理所属的用户（它们在用户详情里打开）。
type objectRef struct {
	Type  string `json:"type"`
	ID    int    `json:"id,omitempty"`
	Key   string `json:"key,omitempty"`
	Name  string `json:"name"`
	Owner int    `json:"owner,omitempty"`
}

// gateRef 是引用一台 VPS 的最小信息（列表里同时给编号与名称，控制台据此生成链接）。
type gateRef struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func knownObjectType(t string) bool {
	for _, x := range objectTypes {
		if x == t {
			return true
		}
	}
	return false
}

// resolveObject 确认对象存在。数字类对象用 key 的数字形式查库；驱动、内核版本、公开页面按名称。
func (s *Server) resolveObject(ctx context.Context, typ, key string) (objectRef, bool) {
	if !knownObjectType(typ) || !objectKey.MatchString(key) {
		return objectRef{}, false
	}
	ref := objectRef{Type: typ}
	id, _ := strconv.Atoi(key)
	db := s.Store.Ent
	switch typ {
	case "gate":
		g, err := db.Gate.Get(ctx, id)
		if err != nil {
			return ref, false
		}
		ref.ID, ref.Name = g.ID, g.Name
	case "node":
		n, err := db.Node.Get(ctx, id)
		if err != nil {
			return ref, false
		}
		ref.ID, ref.Name = n.ID, n.Name
	case "user":
		u, err := db.User.Get(ctx, id)
		if err != nil {
			return ref, false
		}
		ref.ID, ref.Name = u.ID, u.Email
		if u.Name != "" {
			ref.Name = u.Name
		}
	case "link":
		l, err := db.Link.Get(ctx, id)
		if err != nil {
			return ref, false
		}
		ref.ID, ref.Name, ref.Owner = l.ID, l.Name, l.UserID
	case "chain":
		ch, err := db.Chain.Get(ctx, id)
		if err != nil {
			return ref, false
		}
		ref.ID, ref.Name, ref.Owner = ch.ID, ch.Name, ch.UserID
	case "source":
		v, err := db.SubSource.Get(ctx, id)
		if err != nil {
			return ref, false
		}
		ref.ID, ref.Name = v.ID, v.Name
	case "profile":
		v, err := db.SubProfile.Get(ctx, id)
		if err != nil {
			return ref, false
		}
		ref.ID, ref.Name = v.ID, v.Name
	case "cert":
		v, err := db.Certificate.Get(ctx, id)
		if err != nil {
			return ref, false
		}
		ref.ID, ref.Name = v.ID, v.Domain
	case "audit":
		v, err := db.Audit.Get(ctx, id)
		if err != nil {
			return ref, false
		}
		ref.ID, ref.Name = v.ID, v.Action
	case "page":
		v, err := db.Resource.Query().Where(resource.KindEQ(kindPage), resource.KeyEQ(key)).Only(ctx)
		if err != nil {
			return ref, false
		}
		ref.Key, ref.Name = v.Key, v.Key
	case "driver":
		for _, e := range s.drivers(ctx).List() {
			if e.Name == key {
				ref.Key, ref.Name = e.Name, e.Name
				return ref, true
			}
		}
		return ref, false
	case "kernel":
		// 「内核@版本」是某个版本，只写内核名是这个内核的卡片；版本不要求已下载（版本页里能补下）
		m := kernelKey.FindStringSubmatch(key)
		if m == nil || !kernelKnown(m[1]) {
			return ref, false
		}
		ref.Key, ref.Name = key, strings.TrimSpace(m[1]+" "+m[2])
	}
	if ref.Key == "" {
		ref.Key = strconv.Itoa(ref.ID)
	}
	return ref, true
}

func kernelKnown(name string) bool {
	for _, k := range kernels.Names {
		if k == name {
			return true
		}
	}
	return false
}

// getObject 给控制台查对象的名称与归属（例如审计里的「link:12」要先知道属于哪个用户才能打开）。
func (s *Server) getObject(c *gin.Context) {
	ref, ok := s.resolveObject(c.Request.Context(), c.Param("type"), c.Param("key"))
	if !ok {
		failure(c, 404, "object_not_found")
		return
	}
	c.JSON(200, gin.H{"object": ref})
}

func (s *Server) objectRoutes(admin *gin.RouterGroup) {
	admin.GET("/objects/:type/:key", s.getObject)
}
