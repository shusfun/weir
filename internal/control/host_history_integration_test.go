package control

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/imzyb/MiGate/ent/resource"
)

func testHostHistory(t *testing.T, s *Server, request func(string, string, any, string, string) *httptest.ResponseRecorder, admin, alice string) {
	t.Helper()
	ctx := context.Background()
	at := time.Now().UTC().Add(-5 * time.Minute).Truncate(time.Minute)
	d, e := s.Store.Device.Create().SetName("host-history-isolated").SetPublicKey(randomToken(32)).SetPairExpires(at).SetStatus("approved").SetLastSeen(at).Save(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Store.Device.DeleteOne(d).Exec(ctx)
	defer s.Store.Resource.Delete().Where(resource.KindEQ("host-sample"), resource.OwnerIDEQ(d.ID)).Exec(ctx)
	fixture := func(when time.Time, cpu any) map[string]any {
		return map[string]any{"source": "linux-procfs-statfs", "observed_at": when.Format(time.RFC3339), "cpu_percent": cpu, "cpu_interval_seconds": 15, "memory_total": 1000, "memory_available": 250, "disk_total": 2000, "disk_available": 1000, "load": []float64{1, 2, 3}, "private": "never-publish"}
	}
	for i := 0; i < 3; i++ {
		now := at.Add(time.Duration(i) * time.Minute)
		host := fixture(now, 25.0)
		if i == 1 {
			host = fixture(at.Add(-time.Second), 25.0)
		}
		if i == 2 {
			host = fixture(now, nil)
		}
		updated, e := s.Store.Device.UpdateOne(d).SetLastSeen(now).SetMetadata(map[string]any{"host_status": host}).Save(ctx)
		if e != nil {
			t.Fatal(e)
		}
		sample := hostHistorySample(updated, now)
		if i == 0 && (sample.CPU == nil || *sample.CPU != 25 || sample.Memory == nil || *sample.Memory != 75) {
			t.Fatal("fresh projection wrong")
		}
		if i == 1 && (sample.CPU != nil || sample.Quality != "unknown") {
			t.Fatal("stale host promoted")
		}
		if i == 2 && (sample.CPU != nil || sample.Memory == nil) {
			t.Fatal("missing CPU filled or memory lost")
		}
		if e = s.sampleHosts(ctx, now); e != nil {
			t.Fatal(e)
		}
		if e = s.sampleHosts(ctx, now); e != nil {
			t.Fatal(e)
		}
	}
	base := fmt.Sprintf("/api/v1/admin/devices/%d/host-history", d.ID)
	w := request("GET", base, nil, "", admin)
	if w.Code != 200 || strings.Contains(w.Body.String(), "never-publish") {
		t.Fatal("history projection or response failed")
	}
	var reply struct {
		Items []hostSample `json:"items"`
	}
	if json.Unmarshal(w.Body.Bytes(), &reply) != nil || len(reply.Items) != 3 {
		t.Fatal("minute idempotence failed")
	}
	if reply.Items[1].Quality != "unknown" || reply.Items[1].CPU != nil {
		t.Fatal("unknown history became CPU")
	}
	for path, status := range map[string]int{base + "?minutes=5": 400, "/api/v1/admin/devices/0/host-history": 400, "/api/v1/admin/devices/999999/host-history": 404} {
		if request("GET", path, nil, "", admin).Code != status {
			t.Fatal("history validation wrong")
		}
	}
	if request("GET", base, nil, "", alice).Code != 403 || request("GET", base, nil, "", "").Code != 401 {
		t.Fatal("host history ownership failed")
	}
	h := sanitizeHostObservation(fixture(time.Now(), 101.0), time.Now())
	if h.CPUPercent != nil || h.CPUInterval != nil {
		t.Fatal("invalid CPU accepted")
	}
	// Offline contact invalidates even a fresh payload.
	d.LastSeen = at
	d.Metadata = map[string]any{"host_status": fixture(time.Now(), 10.0)}
	if hostHistorySample(d, time.Now()).CPU != nil {
		t.Fatal("offline device used cached host sample")
	}
}
