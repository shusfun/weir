package center

import (
	"encoding/json"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shusfun/weir/internal/geoip"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/packages/contracts"
)

func TestHotspotWindow(t *testing.T) {
	var metrics hotspotMetrics
	now := time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)
	for sample := range 5 {
		metrics.record("worker.maintenance", "trace", now.Add(time.Duration(sample)*time.Second), time.Duration(sample)*time.Second, sample > 1)
	}
	start, items := metrics.snapshot(now.Add(time.Minute))
	item := items[0]
	if !start.Equal(now) || item.Calls != 5 || item.Slow != 3 || item.Failures != 3 || item.P95MS == nil || *item.P95MS != 4000 || item.Trace != "trace" {
		t.Fatalf("start=%s item=%+v", start, item)
	}
	_, items = metrics.snapshot(now.Add(15 * time.Minute))
	if items[0].Calls != 0 || items[0].P95MS != nil || items[0].Trace != "" {
		t.Fatalf("expired samples reported: %+v", items[0])
	}
}

type hotspotGeoFixture struct{ status geoip.Status }

func (fixture hotspotGeoFixture) Status() geoip.Status         { return fixture.status }
func (hotspotGeoFixture) Lookup(string) (geoip.Location, bool) { return geoip.Location{}, false }
func (fixture hotspotGeoFixture) CountryNetworks(string) ([]netip.Prefix, geoip.Status, error) {
	return []netip.Prefix{netip.MustParsePrefix("203.0.113.0/24")}, fixture.status, nil
}

func TestHotspotCountrySnapshotConcurrent(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	server := &Server{GeoDB: hotspotGeoFixture{geoip.Status{Ready: true, Built: now, Fetched: now}}}
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("gate", &ent.Gate{ID: 1}) })
	router.GET("/country", server.agentCountryNetworks)
	var requests sync.WaitGroup
	for range 4 {
		requests.Go(func() {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest("GET", "/country", nil))
			var result contracts.CountryNetworks
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || response.Code != 200 || !result.Fetched.Equal(now) || len(result.Prefixes) != 1 {
				t.Errorf("country response: %d %+v %v", response.Code, result, err)
			}
		})
	}
	requests.Wait()
	server.GeoDB = hotspotGeoFixture{geoip.Status{Ready: true, Fetched: now.Add(-46 * 24 * time.Hour)}}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("GET", "/country", nil))
	if response.Code != 503 {
		t.Fatalf("expired snapshot returned %d", response.Code)
	}
	_, metrics := server.hotspots.snapshot(time.Now())
	if metrics[3].Calls != 5 || metrics[3].Failures != 1 {
		t.Fatalf("country snapshot telemetry: %+v", metrics[3])
	}
}

func TestHotspotTaskSummarySkipsHistoricalPayloads(t *testing.T) {
	server, admin := newInstalledServer(t)
	ctx := t.Context()
	now := time.Now()
	id := "historical-release"
	large := `{"sealed":"` + strings.Repeat("x", 3<<20) + `"}`
	_, err := server.Store.DB.ExecContext(ctx, `INSERT INTO worker_tasks(id,kind,version,trace_id,idem,payload_hash,body,requirements,candidates,deadline,created_at,status,result) VALUES($1,$2,1,$3,$1,'hash',$4,'{}','[1]',$5,$5,'succeeded',$6)`, id, contracts.TaskReleaseQuery, randomHex(16), "sealed payload", now.UnixMilli(), large)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	var oldBody, oldResult string
	if err := server.Store.DB.QueryRowContext(ctx, `SELECT body,result FROM worker_tasks ORDER BY created_at DESC,id LIMIT 1`).Scan(&oldBody, &oldResult); err != nil {
		t.Fatal(err)
	}
	oldElapsed := time.Since(started)
	started = time.Now()
	items, err := server.Store.Tasks.List(ctx, 200)
	newElapsed := time.Since(started)
	if err != nil || len(items) != 1 || items[0].Ciphertext != "" || len(items[0].Result) != 0 || items[0].Task.LeaseToken != "" {
		t.Fatalf("summary includes protected fields: count=%d, err=%v", len(items), err)
	}
	ids, err := server.Store.Tasks.PendingProjections(ctx, now)
	if err != nil || len(ids) != 0 {
		t.Fatalf("historical release is a projection candidate: %v %v", ids, err)
	}
	t.Logf("one-row historical payload over DB connection: %d selected bytes in %s; summary payload bytes 0 in %s", len(oldBody)+len(oldResult), oldElapsed, newElapsed)
	_ = mustJSON(t, admin, "GET", "/api/v1/admin/workers/tasks", nil, 200)
	response := mustJSON(t, admin, "GET", "/api/v1/admin/ops/hotspots", nil, 200)
	for _, value := range response["items"].([]any) {
		item := value.(map[string]any)
		if item["operation"] == "worker.tasks.list" && item["calls"] == float64(1) {
			return
		}
	}
	t.Fatalf("task list trace missing from hotspots: %v", response)
}
