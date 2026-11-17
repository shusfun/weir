package center

import (
	"context"
	"strings"
	"testing"

	"github.com/shusfun/weir/internal/geoip"
	"github.com/shusfun/weir/internal/kernelconf"
	"github.com/shusfun/weir/internal/nodeprobe"
	"github.com/shusfun/weir/packages/contracts"
)

type idleGeo struct{}

func (idleGeo) Lookup(string) (geoip.Location, bool) { return geoip.Location{}, false }
func (idleGeo) Status() geoip.Status                 { return geoip.Status{} }

func TestExitProbeReportsLandingAndRate(t *testing.T) {
	s, admin, g := setupGate(t)
	s.GeoDB = idleGeo{}
	s.origin.value, s.origin.loaded = "https://center.example.test", true
	ex := mustJSON(t, admin, "POST", "/api/v1/admin/exits", map[string]any{"kind": "socks5", "address": "203.0.113.9", "port": 1080, "password": "secret"}, 200)
	id := int(ex["id"].(float64))
	oldMeasure, oldBin := measureExitProxy, deployedProbeBinary
	t.Cleanup(func() { measureExitProxy, deployedProbeBinary = oldMeasure, oldBin })
	deployedProbeBinary = func(*Server, context.Context, string) (string, kernelconf.Driver, error) {
		return "mihomo", kernelconf.Driver{}, nil
	}
	measureExitProxy = func(context.Context, nodeprobe.MeasureOptions) (nodeprobe.MeasureResult, error) {
		return nodeprobe.MeasureResult{LatencyMS: 20, ExitIP: "198.51.100.20", Bytes: int64(nodeprobe.PayloadSize), DownloadMS: 1000}, nil
	}
	body := admin.do("GET", "/api/v1/probe/payload", nil, nil)
	if body.Code != 200 || body.Body.Len() != nodeprobe.PayloadSize || body.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("payload = %d %d %s", body.Code, body.Body.Len(), body.Header().Get("Cache-Control"))
	}
	probed := mustJSON(t, admin, "POST", "/api/v1/admin/exits/"+itoa(id)+"/probe", map[string]any{"location": "center"}, 200)
	place, _ := probed["place"].(map[string]any)
	if probed["location"] != "center" || probed["exit_ip"] != "198.51.100.20" || probed["rate_bps"] != float64(nodeprobe.PayloadSize) || place["known"] != false || place["why"] == "" {
		t.Fatalf("center probe = %v", probed)
	}
	if n := s.Store.Ent.ExitBinding.Query().CountX(t.Context()); n != 0 {
		t.Fatalf("center probe created %d bindings", n)
	}
	s.GeoDB = fakeGeo{"198.51.100.20": {IP: "198.51.100.20", City: "东京", Country: "JP"}}
	probed = mustJSON(t, admin, "POST", "/api/v1/admin/exits/"+itoa(id)+"/probe", map[string]any{"location": "center"}, 200)
	place, _ = probed["place"].(map[string]any)
	if place["known"] != true || place["city"] != "东京" {
		t.Fatalf("placed probe = %v", probed["place"])
	}
	measureExitProxy = func(context.Context, nodeprobe.MeasureOptions) (nodeprobe.MeasureResult, error) {
		return nodeprobe.MeasureResult{LatencyMS: 20, ExitIP: "198.51.100.20", DownloadError: "下载返回 HTTP 404"}, nil
	}
	probed = mustJSON(t, admin, "POST", "/api/v1/admin/exits/"+itoa(id)+"/probe", map[string]any{"location": "center"}, 200)
	if probed["ok"] != true || probed["exit_ip"] != "198.51.100.20" || probed["rate_bps"] != nil || probed["download_error"] == "" {
		t.Fatalf("download failure = %v", probed)
	}
	var seen map[string]any
	g.serveActions(t, s, map[string]func(contracts.Action) contracts.ActionResult{
		contracts.ActionChainProbe: func(a contracts.Action) contracts.ActionResult {
			seen = a.Payload
			return contracts.ActionResult{OK: true, Result: map[string]any{"latency_ms": 9, "exit_ip": "198.51.100.9", "download_bytes": nodeprobe.PayloadSize, "download_ms": 500}}
		},
	})
	gateProbe := mustJSON(t, admin, "POST", "/api/v1/admin/exits/"+itoa(id)+"/probe", map[string]any{"gate_id": int(g.id)}, 200)
	if gateProbe["location"] != "gate" || int(gateProbe["gate_id"].(float64)) != int(g.id) || gateProbe["exit_ip"] != "198.51.100.9" || gateProbe["rate_bps"] != float64(nodeprobe.PayloadSize*1000/500) {
		t.Fatalf("gate probe = %v", gateProbe)
	}
	if seen["download"] != "https://center.example.test/api/v1/probe/payload" || !strings.HasSuffix(seen["target"].(string), "/api/v1/ip") {
		t.Fatalf("gate payload = %v", seen)
	}
	if n := s.Store.Ent.ExitBinding.Query().CountX(t.Context()); n != 0 {
		t.Fatalf("gate probe created %d bindings", n)
	}
	if w := admin.do("POST", "/api/v1/admin/exits/"+itoa(id)+"/probe", map[string]any{"location": "center", "gate_id": int(g.id)}, nil); w.Code != 400 || !strings.Contains(w.Body.String(), "invalid_probe_location") {
		t.Fatalf("mixed location = %d %s", w.Code, w.Body.String())
	}
	s.origin.value = ""
	measureExitProxy = func(context.Context, nodeprobe.MeasureOptions) (nodeprobe.MeasureResult, error) {
		return nodeprobe.MeasureResult{LatencyMS: 8}, nil
	}
	probed = mustJSON(t, admin, "POST", "/api/v1/admin/exits/"+itoa(id)+"/probe", map[string]any{"location": "center"}, 200)
	place, _ = probed["place"].(map[string]any)
	if probed["ok"] != true || probed["exit_ip"] != nil || probed["rate_bps"] != nil || !strings.Contains(place["why"].(string), "公网地址") || !strings.Contains(probed["download_error"].(string), "公网地址") {
		t.Fatalf("origin missing = %v", probed)
	}
}
