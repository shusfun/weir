//go:build e2e

package e2e

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/geoip"
	"github.com/shusfun/weir/internal/store/ent/resource"
)

// Geography is controlled fixture input; connection metadata and bytes must
// still come from the unmodified native API through the running Gate.
type observationGeography struct{}

func (observationGeography) Lookup(string) (geoip.Location, bool) { return geoip.Location{}, false }
func (observationGeography) Status() geoip.Status {
	return geoip.Status{Ready: true, Built: time.Now(), Fetched: time.Now()}
}
func (g observationGeography) CountryNetworks(string) ([]netip.Prefix, geoip.Status, error) {
	return []netip.Prefix{netip.MustParsePrefix("198.18.0.0/24")}, g.Status(), nil
}

func TestDomainObservationNativeGate(t *testing.T) {
	e := setup(t)
	e.s.GeoDB = observationGeography{}
	sb := e.seedSingbox()
	if err := addLoopIP("198.18.0.1"); err != nil {
		t.Fatal(err)
	}
	lab := startDNSLab(t)
	lab.alias("domestic.observation.test", net.ParseIP("198.18.0.1"))
	lab.alias("foreign.observation.test", net.ParseIP("8.8.8.8"))
	e.startGate("observed")
	e.s.Store.Ent.Gate.UpdateOneID(e.gateID).SetPublicHost("127.0.0.1").ExecX(e.ctx)
	n := e.post("/api/v1/admin/nodes", map[string]any{"gate_id": e.gateID, "preset": "ss2022", "skip_probe": true}, nil)
	u := e.quotaUser("observation@e2e.test")
	e.applied("native domain observer")
	clientPort := e.runSingboxClient(sb, e.subOutbound(int(u["id"].(float64)), int(n["id"].(float64))))
	proxy, err := url.Parse("socks5://127.0.0.1:" + strconv.Itoa(clientPort))
	if err != nil {
		t.Fatal(err)
	}
	transport := &http.Transport{Proxy: http.ProxyURL(proxy)}
	t.Cleanup(transport.CloseIdleConnections)
	client := &http.Client{Transport: transport, Timeout: 45 * time.Second}
	stream := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		flusher := w.(http.Flusher)
		tick := time.NewTicker(100 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-tick.C:
				if _, err := io.WriteString(w, strings.Repeat("x", 8192)); err != nil {
					return
				}
				flusher.Flush()
			}
		}
	}))
	stream.Listener.Close()
	stream.Listener, err = net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	stream.Start()
	t.Cleanup(stream.Close)
	_, port, _ := net.SplitHostPort(stream.Listener.Addr().String())
	// Real clients can connect to an IP and carry the HTTP host separately.
	// Native sniffing must preserve that IP while exposing the host. A domain
	// dial can omit destinationIP in the native API and remains partial coverage.
	for _, target := range []struct{ ip, domain string }{
		{"198.18.0.1", "domestic.observation.test"},
		{"8.8.8.8", "foreign.observation.test"},
		{"127.0.0.1", "private.observation.test"},
	} {
		req, err := http.NewRequest("GET", "http://"+net.JoinHostPort(target.ip, port), nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Host = target.domain
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { resp.Body.Close() })
		go func() { _, _ = io.Copy(io.Discard, resp.Body) }()
	}
	eventually(t, 30*time.Second, "native observed bytes reported", func() bool {
		rows := e.s.Store.Ent.Resource.Query().Where(resource.KindEQ("domain-observed-daily")).AllX(e.ctx)
		for _, row := range rows {
			if row.Data["domain"] != "domestic.observation.test" {
				t.Fatalf("unexpected persisted domain: %v", row.Data["domain"])
			}
			raw, _ := json.Marshal(row.Data)
			var value struct {
				Bytes int64 `json:"bytes,string"`
			}
			if err := json.Unmarshal(raw, &value); err != nil {
				t.Fatal(err)
			}
			if value.Bytes > 0 {
				evidence(t, "native-domain-observation", map[string]any{"observed_bytes": value.Bytes, "engine": "sing-box", "geography": "isolated fixture prefix", "coverage": "partial"})
				return true
			}
		}
		return false
	})
	coverage := e.s.Store.Ent.Resource.Query().Where(resource.KindEQ("domain-observed-coverage")).AllX(e.ctx)
	if len(coverage) == 0 {
		t.Fatal("missing native coverage")
	}
	for _, row := range coverage {
		if row.Data["engine"] == "sing-box" && row.Data["status"] != "partial" {
			t.Fatalf("native snapshots must retain partial coverage: %v", row.Data["status"])
		}
	}
}
