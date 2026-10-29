package center

import (
	"context"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/shusfun/weir/internal/geoip"
)

// IP 定位：链路页把 VPS 与出口放到地图上。数据来自 DB-IP Lite（中心每月下载一次）；
// 域名先解析再查；查不到时如实显示「位置未知」，不放到默认坐标上。

type geoLocator interface {
	Lookup(ip string) (geoip.Location, bool)
	Status() geoip.Status
}

func (s *Server) geoDB() geoLocator {
	if s.GeoDB != nil {
		return s.GeoDB
	}
	s.geoOnce.Do(func() { s.geo = geoip.Open(filepath.Join(s.dataDir(), "geoip")) })
	return s.geo
}

// RefreshGeoIP 在数据缺失或过期时后台下载（维护任务每分钟调用，失败 6 小时内不重试）。
func (s *Server) RefreshGeoIP(ctx context.Context, now time.Time) {
	if s.GeoDB != nil {
		return
	}
	db := s.geoDB().(*geoip.DB)
	go func() { _ = db.Refresh(context.WithoutCancel(ctx), now) }()
}

// geoPlace 是地图上的一个位置；Known 为假时 Why 写明原因。
type geoPlace struct {
	Known bool `json:"known"`
	geoip.Location
	Why string `json:"why,omitempty"`
}

var resolveCache = struct {
	sync.Mutex
	m map[string]resolved
}{m: map[string]resolved{}}

type resolved struct {
	ip string
	at time.Time
}

// resolveHost 把域名解析成第一个公网 IP（缓存 10 分钟）；IP 原样返回。
func resolveHost(ctx context.Context, host string) string {
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if host == "" || net.ParseIP(host) != nil {
		return host
	}
	resolveCache.Lock()
	r, ok := resolveCache.m[host]
	resolveCache.Unlock()
	if ok && time.Since(r.at) < 10*time.Minute {
		return r.ip
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	ip := ""
	if ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host); err == nil {
		for _, x := range ips {
			if publicAddr(x) {
				ip = x.String()
				break
			}
		}
	}
	resolveCache.Lock()
	resolveCache.m[host] = resolved{ip: ip, at: time.Now()}
	resolveCache.Unlock()
	return ip
}

// locate 查询一个地址（IP 或域名）的位置。
func (s *Server) locate(ctx context.Context, host string) geoPlace {
	if strings.TrimSpace(host) == "" {
		return geoPlace{Why: "没有地址"}
	}
	db := s.geoDB()
	if st := db.Status(); !st.Ready {
		return geoPlace{Why: firstNonEmpty(st.Error, "IP 定位数据还没有下载")}
	}
	ip := resolveHost(ctx, host)
	if ip == "" {
		return geoPlace{Why: host + " 解析不到公网地址"}
	}
	if !publicAddr(net.ParseIP(ip)) {
		return geoPlace{Why: ip + " 不是公网地址"}
	}
	loc, ok := db.Lookup(ip)
	if !ok {
		return geoPlace{Location: geoip.Location{IP: ip}, Why: "定位数据里没有 " + ip}
	}
	return geoPlace{Known: true, Location: loc}
}
