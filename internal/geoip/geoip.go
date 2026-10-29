// Package geoip 按 IP 查地理位置与运营商，数据用 DB-IP 的免费 Lite 库（CC BY 4.0，页面上要注明来源）。
// 中心每月下载一次 City Lite 与 ASN Lite 的 mmdb，缓存在数据目录里；Gate 不下载。
// DB-IP 不提供摘要，下载后以「能按 mmdb 打开、库类型对得上」校验，校验不过不替换旧文件。
package geoip

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/oschwald/maxminddb-golang/v2"
)

// Location 是一次查询的结果；数据库里没有的字段留空，页面显示「未知」。
type Location struct {
	IP      string  `json:"ip,omitempty"`
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
	City    string  `json:"city,omitempty"`
	Region  string  `json:"region,omitempty"`
	Country string  `json:"country,omitempty"` // ISO 3166 两位代码
	ASN     uint    `json:"asn,omitempty"`
	Org     string  `json:"org,omitempty"`
}

// Status 是数据库的状态：Ready 为假时 Error 写明原因。
type Status struct {
	Ready   bool      `json:"ready"`
	Built   time.Time `json:"built,omitempty"`
	Error   string    `json:"error,omitempty"`
	Fetched time.Time `json:"fetched,omitempty"`
}

const (
	cityFile = "dbip-city-lite.mmdb"
	asnFile  = "dbip-asn-lite.mmdb"
	maxAge   = 35 * 24 * time.Hour
	retry    = 6 * time.Hour
	maxSize  = 512 << 20
)

// DB 是查询器；Dir 下放两个 mmdb。零值不可用，用 Open。
type DB struct {
	Dir  string
	HTTP *http.Client
	// BaseURL 默认 https://download.db-ip.com/free（测试中替换）。
	BaseURL string

	mu      sync.RWMutex
	city    *maxminddb.Reader
	asn     *maxminddb.Reader
	status  Status
	tried   time.Time
	fetchMu sync.Mutex
}

// Open 打开已下载的数据库（没有也返回可用的 DB，Lookup 返回未就绪）。
func Open(dir string) *DB {
	d := &DB{Dir: dir, BaseURL: "https://download.db-ip.com/free", HTTP: &http.Client{Timeout: 10 * time.Minute}}
	d.reload()
	return d
}

func (d *DB) reload() {
	city, err := maxminddb.Open(filepath.Join(d.Dir, cityFile))
	if err != nil {
		d.mu.Lock()
		d.status = Status{Error: "IP 定位数据还没有下载", Fetched: d.status.Fetched}
		d.mu.Unlock()
		return
	}
	asn, _ := maxminddb.Open(filepath.Join(d.Dir, asnFile))
	d.mu.Lock()
	if d.city != nil {
		_ = d.city.Close()
	}
	if d.asn != nil {
		_ = d.asn.Close()
	}
	d.city, d.asn = city, asn
	// 是否过期看本机下载的时间（文件修改时间）：DB-IP 当月的文件可能是上个月构建的。
	fetched := d.status.Fetched
	if fi, err := os.Stat(filepath.Join(d.Dir, cityFile)); err == nil {
		fetched = fi.ModTime()
	}
	d.status = Status{Ready: true, Built: city.Metadata.BuildTime(), Fetched: fetched}
	d.mu.Unlock()
}

// Status 返回数据库状态。
func (d *DB) Status() Status {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.status
}

type cityRecord struct {
	City struct {
		Names map[string]string `maxminddb:"names"`
	} `maxminddb:"city"`
	Country struct {
		ISOCode string            `maxminddb:"iso_code"`
		Names   map[string]string `maxminddb:"names"`
	} `maxminddb:"country"`
	Subdivisions []struct {
		Names map[string]string `maxminddb:"names"`
	} `maxminddb:"subdivisions"`
	Location struct {
		Latitude  float64 `maxminddb:"latitude"`
		Longitude float64 `maxminddb:"longitude"`
	} `maxminddb:"location"`
}

type asnRecord struct {
	Number uint   `maxminddb:"autonomous_system_number"`
	Org    string `maxminddb:"autonomous_system_organization"`
}

func name(m map[string]string) string {
	for _, k := range []string{"zh-CN", "en"} {
		if v := m[k]; v != "" {
			return v
		}
	}
	for _, v := range m {
		return v
	}
	return ""
}

// Lookup 查询一个 IP；数据库未就绪或查不到时 ok 为假。
func (d *DB) Lookup(ip string) (Location, bool) {
	addr, err := netip.ParseAddr(strings.Trim(ip, "[]"))
	if err != nil {
		return Location{}, false
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.city == nil {
		return Location{}, false
	}
	var rec cityRecord
	res := d.city.Lookup(addr.Unmap())
	if !res.Found() || res.Decode(&rec) != nil || rec.Location.Latitude == 0 && rec.Location.Longitude == 0 {
		return Location{}, false
	}
	loc := Location{IP: addr.Unmap().String(), Lat: rec.Location.Latitude, Lon: rec.Location.Longitude, City: name(rec.City.Names), Country: rec.Country.ISOCode}
	if len(rec.Subdivisions) > 0 {
		loc.Region = name(rec.Subdivisions[0].Names)
	}
	if d.asn != nil {
		var a asnRecord
		if r := d.asn.Lookup(addr.Unmap()); r.Found() && r.Decode(&a) == nil {
			loc.ASN, loc.Org = a.Number, a.Org
		}
	}
	return loc, true
}

// Refresh 在数据缺失或超过 35 天时下载（失败 6 小时内不重试）；成功后立即生效。
func (d *DB) Refresh(ctx context.Context, now time.Time) error {
	if !d.fetchMu.TryLock() {
		return nil
	}
	defer d.fetchMu.Unlock()
	st := d.Status()
	if st.Ready && now.Sub(st.Fetched) < maxAge || now.Sub(d.tried) < retry {
		return nil
	}
	d.tried = now
	err := d.download(ctx, now)
	d.mu.Lock()
	if err != nil {
		d.status.Error = "IP 定位数据下载失败：" + err.Error()
	}
	d.mu.Unlock()
	if err == nil {
		d.reload()
	}
	return err
}

// download 依次试本月与上月的文件（月初新文件可能还没发布）。
func (d *DB) download(ctx context.Context, now time.Time) error {
	if err := os.MkdirAll(d.Dir, 0o755); err != nil {
		return err
	}
	for _, f := range []struct{ kind, file, dbType string }{{"city-lite", cityFile, "City"}, {"asn-lite", asnFile, "ASN"}} {
		var last error
		for _, month := range []time.Time{now, now.AddDate(0, -1, 0)} {
			u := fmt.Sprintf("%s/dbip-%s-%s.mmdb.gz", strings.TrimRight(d.BaseURL, "/"), f.kind, month.Format("2006-01"))
			if last = d.fetch(ctx, u, filepath.Join(d.Dir, f.file), f.dbType); last == nil {
				break
			}
		}
		if last != nil {
			return last
		}
	}
	return nil
}

func (d *DB) fetch(ctx context.Context, u, dest, dbType string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	resp, err := d.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s 返回 %d", u, resp.StatusCode)
	}
	gz, err := gzip.NewReader(io.LimitReader(resp.Body, maxSize))
	if err != nil {
		return errors.New("不是 gzip 文件")
	}
	tmp := dest + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, io.LimitReader(gz, maxSize))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	r, err := maxminddb.Open(tmp)
	if err != nil {
		_ = os.Remove(tmp)
		return errors.New("下载的文件不是有效的 mmdb")
	}
	ok := strings.Contains(r.Metadata.DatabaseType, dbType)
	_ = r.Close()
	if !ok {
		_ = os.Remove(tmp)
		return fmt.Errorf("库类型不对：需要 %s", dbType)
	}
	return os.Rename(tmp, dest)
}
