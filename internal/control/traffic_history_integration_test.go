package control

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/imzyb/MiGate/ent/resource"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func testTrafficHistoryIntegration(t *testing.T, s *Server, request func(string, string, any, string, string) *httptest.ResponseRecorder, admin, alice string) {
	t.Helper()
	ctx := context.Background()
	start := time.Now().UTC().Add(-10 * time.Minute).Truncate(time.Minute)
	d, err := s.Store.Device.Create().SetName("traffic-history-fixture").SetPublicKey(randomToken(32)).SetPairExpires(time.Now()).SetStatus("approved").SetLastSeen(start).SetMetadata(map[string]any{"traffic_status": "fresh"}).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Store.Device.DeleteOne(d).Exec(ctx)
	defer s.Store.Resource.Delete().Where(resource.KindIn("traffic-sample", "meter-observation"), resource.OwnerIDEQ(d.ID)).Exec(ctx)
	u, err := s.Store.User.Query().First(ctx)
	if err != nil {
		t.Fatal(err)
	}
	g, err := s.Store.Grant.Create().SetDeviceID(d.ID).SetUserID(u.ID).SetInboundID(777777).SetCredential(map[string]any{"password": "must-not-appear"}).SetUsed(100).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Store.Grant.DeleteOne(g).Exec(ctx)
	report := func(at time.Time) {
		t.Helper()
		if _, e := s.Store.Device.UpdateOne(d).SetLastSeen(at).Save(ctx); e != nil {
			t.Fatal(e)
		}
		raw, _ := json.Marshal(map[string]any{"at": at})
		if _, e := s.DB.ExecContext(ctx, `INSERT INTO migate.resources(kind,key,owner_id,data,created_at) VALUES('meter-observation',$1,$2,$3,now()) ON CONFLICT(kind,key) DO UPDATE SET data=excluded.data`, strconv.Itoa(d.ID), d.ID, raw); e != nil {
			t.Fatal(e)
		}
	}
	sample := func(at time.Time) {
		t.Helper()
		if e := s.sampleTraffic(ctx, at); e != nil {
			t.Fatal(e)
		}
	}
	report(start)
	sample(start)
	if _, err = s.Store.Grant.UpdateOne(g).SetUsed(164).Save(ctx); err != nil {
		t.Fatal(err)
	}
	report(start.Add(time.Minute))
	sample(start.Add(time.Minute))
	sample(start.Add(time.Minute + 15*time.Second))
	path := fmt.Sprintf("/api/v1/admin/devices/%d/traffic-history", d.ID)
	w := request("GET", path, nil, "", admin)
	var view struct {
		Items  []trafficSample `json:"items"`
		Source string          `json:"source"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &view) != nil || len(view.Items) != 2 {
		t.Fatal("history/duplicate sampling incorrect", w.Code, len(view.Items))
	}
	if view.Items[0].Delta != nil || view.Items[0].Quality != "unknown" || view.Items[1].Delta == nil || *view.Items[1].Delta != "64" || view.Source != "accepted_grant_settlements" {
		t.Fatal("first baseline or real delta incorrect")
	}
	if strings.Contains(w.Body.String(), "identity") || strings.Contains(w.Body.String(), "baseline_fresh") || strings.Contains(w.Body.String(), "must-not-appear") {
		t.Fatal("history private fields leaked")
	}
	sample(start.Add(2*time.Minute + 30*time.Second))
	report(start.Add(5 * time.Minute))
	sample(start.Add(5 * time.Minute))
	w = request("GET", path, nil, "", admin)
	json.Unmarshal(w.Body.Bytes(), &view)
	if view.Items[2].Delta != nil || view.Items[2].Reason != "device_offline" || view.Items[3].Delta != nil || view.Items[3].Reason != "sample_gap" {
		t.Fatal("missing interval bridged or reported zero")
	}
	report(start.Add(6 * time.Minute))
	sample(start.Add(6 * time.Minute))
	w = request("GET", path, nil, "", admin)
	json.Unmarshal(w.Body.Bytes(), &view)
	last := view.Items[len(view.Items)-1]
	if last.Delta == nil || *last.Delta != "0" || last.Quality != "fresh" {
		t.Fatal("real zero after recovered baseline not represented")
	}
	if request("GET", path, nil, "", alice).Code != 403 || request("GET", path+"?minutes=999999", nil, "", admin).Code != 400 {
		t.Fatal("history boundary failed")
	}
	// A newly observed authorization set must start a new baseline.
	if _, err = s.Store.Grant.UpdateOne(g).SetUsed(1).Save(ctx); err != nil {
		t.Fatal(err)
	}
	report(start.Add(7 * time.Minute))
	sample(start.Add(7 * time.Minute))
	w = request("GET", path, nil, "", admin)
	json.Unmarshal(w.Body.Bytes(), &view)
	if view.Items[len(view.Items)-1].Delta != nil || view.Items[len(view.Items)-1].Reason != "counter_reset" {
		t.Fatal("counter decrease became traffic")
	}
	t.Log("real PG traffic snapshots: one/minute, exact accepted delta, first baseline, offline/gap unknown, recovery zero, reset rejection, API source/time and admin isolation passed; controlled counters, not network traffic")
}
