package center

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/geoip"
	"github.com/shusfun/weir/internal/store/ent"
)

func TestSubscriptionPlacesAndCandidateNames(t *testing.T) {
	for _, tc := range []struct {
		country string
		lon     float64
		known   bool
		want    string
	}{
		{"US", -104.01, true, "美西"}, {"US", -104, true, "美中"}, {"US", -87, true, "美中"}, {"US", -86.99, true, "美东"},
		{"hk", 0, true, "香港"}, {"ZZ", 0, true, "ZZ"}, {"JP", 0, false, "地区未知"}, {"", 0, true, "地区未知"},
	} {
		if got := subscriptionPlace(geoPlace{Known: tc.known, Location: geoip.Location{Country: tc.country, Lon: tc.lon}}); got != tc.want {
			t.Errorf("%+v: %q", tc, got)
		}
	}
}

func TestSubscriptionNameFallbacksAndOrder(t *testing.T) {
	s, _, g := setupGate(t)
	s.GeoDB = fakeGeo{"203.0.113.1": {Country: "US", Lon: -120}, "203.0.113.2": {Country: "JP"}}
	gate := &ent.Gate{ID: int(g.id), PublicHost: "203.0.113.1"}
	es := []subscriptionEntry{
		{gate: gate, exit: &ent.Exit{Kind: "freedom"}},
		{gate: gate, exit: &ent.Exit{Kind: "freedom"}},
		{Entry: Entry{Chained: true}, gate: gate, exit: &ent.Exit{Kind: "socks5", Remark: "备注", Address: "203.0.113.2"}, chain: &ent.Chain{Name: "手动名"}},
		{Entry: Entry{Chained: true}, gate: gate, exit: &ent.Exit{Kind: "socks5", Remark: "203.0.113.1", Address: "203.0.113.2"}, chain: &ent.Chain{Name: "手动名"}},
		{Entry: Entry{Chained: true}, gate: gate, exit: &ent.Exit{Kind: "socks5", Remark: "proxy.example:1080", Address: "203.0.113.2"}, chain: &ent.Chain{Name: "203.0.113.3"}},
		{Entry: Entry{Chained: true}, gate: gate, exit: &ent.Exit{Kind: "socks5", Address: "203.0.113.3", PolicyType: "vless"}, chain: &ent.Chain{Name: "不应使用"}},
	}
	s.nameSubscriptionEntries(t.Context(), es)
	want := []string{"[直连] 美西 01", "[直连] 美西 02", "[链式] 备注", "[链式] 203.0.113.1", "[链式] proxy.example:1080", "[链式] 地区未知"}
	for i, e := range es {
		if e.Name != want[i] {
			t.Errorf("entry %d = %q want %q", i, e.Name, want[i])
		}
	}
}

func TestSubscriptionInfoLinksUseRemainingAndEffectiveExpiry(t *testing.T) {
	userEnd := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	linkEnd := userEnd.Add(-24 * time.Hour)
	u := &ent.User{Quota: 1 << 30, Used: 2 << 30, ExpiresAt: &userEnd}
	l := &ent.Link{EndsAt: &linkEnd}
	links := subscriptionInfoLinks(u, l)
	for i, want := range []string{"剩余 0.0 GB", "到期 2026-11-30"} {
		v, err := url.Parse(links[i])
		if err != nil || v.Fragment != want || v.Host != "127.0.0.1:1" || v.User.String() != "weir-info" || v.RawQuery != "security=none&type=tcp" {
			t.Fatalf("info %d: %v %v", i, v, err)
		}
	}
	l.EndsAt = &userEnd
	u.Quota = 0
	u.Unlimited = true
	u.ExpiresAt = nil
	l.EndsAt = nil
	if v := strings.Join(subscriptionInfoLinks(u, l), "\n"); !strings.Contains(v, url.PathEscape("剩余 不限")) || !strings.Contains(v, url.PathEscape("到期 长期有效")) {
		t.Fatal(v)
	}
}
