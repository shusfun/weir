package control

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/imzyb/MiGate/ent/resource"
	certservice "github.com/imzyb/MiGate/internal/service/cert"
)

type cleanupRoundTrip func(*http.Request) (*http.Response, error)

func (f cleanupRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func testDurableDNSCleanup(t *testing.T, s *Server, request func(string, string, any, string, string) *httptest.ResponseRecorder, admin, alice string) {
	t.Helper()
	ctx := context.Background()
	zone := strings.Repeat("a", 32)
	deviceID := 898989
	sealed, _ := s.Crypto.Encrypt("dns-provider:" + zone + ":test-only-provider-secret")
	provider, err := s.Store.Resource.Create().SetKind("dns-provider").SetKey(randomToken(12)).SetData(map[string]any{"name": "cleanup test", "zone_id": zone, "token": sealed}).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Store.Resource.DeleteOne(provider).Exec(ctx)
	job, err := s.Store.Resource.Create().SetKind("certificate-order").SetKey(randomToken(12)).SetData(map[string]any{"device_id": deviceID, "provider_id": provider.ID, "status": "failed"}).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Store.Resource.DeleteOne(job).Exec(ctx)
	ids := []int{}
	defer func() {
		for _, id := range ids {
			s.Store.Resource.DeleteOneID(id).Exec(ctx)
		}
	}()
	records := map[string]certservice.OwnedTXT{}
	ambiguous := false
	rejectDelete := false
	posts := 0
	deletes := 0
	client := &http.Client{Transport: cleanupRoundTrip(func(r *http.Request) (*http.Response, error) {
		status := 200
		var body any
		if r.URL.Host != "api.cloudflare.com" || r.Header.Get("Authorization") != "Bearer test-only-provider-secret" {
			t.Fatal("credentials sent outside fixed DNS provider")
		}
		switch r.Method {
		case "POST":
			posts++
			var record certservice.OwnedTXT
			json.NewDecoder(r.Body).Decode(&record)
			count, e := s.Store.Resource.Query().Where(resource.KindEQ("dns-challenge"), resource.KeyEQ(strings.TrimPrefix(record.Comment, "migate-dns01:"))).Count(ctx)
			if e != nil || count != 1 {
				t.Fatal("DNS request preceded durable ownership intent")
			}
			record.ID = strings.Repeat(strconv.Itoa(posts), 32)
			records[record.ID] = record
			if ambiguous {
				return nil, errors.New("simulated response lost after provider commit")
			}
			body = map[string]any{"success": true, "result": record}
		case "GET":
			if r.URL.RawQuery != "" {
				list := []certservice.OwnedTXT{}
				for _, record := range records {
					list = append(list, record)
				}
				body = map[string]any{"success": true, "result": list, "result_info": map[string]any{"total_pages": 1}}
			} else {
				id := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
				record, found := records[id]
				if !found {
					status = 404
				}
				body = map[string]any{"success": true, "result": record}
			}
		case "DELETE":
			if rejectDelete {
				status = 403
				body = map[string]any{"credential": "test-only-provider-secret"}
			} else {
				id := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
				delete(records, id)
				deletes++
				body = map[string]any{"success": true}
			}
		default:
			t.Fatal("unexpected DNS operation")
		}
		b, _ := json.Marshal(body)
		return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(b)))}, nil
	})}
	durable := durableTXTProvider{server: s, job: job, provider: provider, client: client}
	cleanup, err := durable.Present(ctx, "_acme-challenge.example.com", "test-challenge-proof")
	if err != nil {
		t.Fatal(err)
	}
	item, err := s.Store.Resource.Query().Where(resource.KindEQ("dns-challenge"), resource.KeyEQ(strings.TrimPrefix(records[strings.Repeat("1", 32)].Comment, "migate-dns01:"))).First(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ids = append(ids, item.ID)
	if item.Data["value"] == "test-challenge-proof" || item.Data["token"] != sealed || item.Data["status"] != "presented" {
		t.Fatal("challenge ownership/sealing not persisted")
	}
	path := "/api/v1/admin/devices/" + strconv.Itoa(deviceID) + "/dns-challenges"
	if request("GET", path, nil, "", alice).Code != 403 {
		t.Fatal("ordinary user read DNS ledger")
	}
	view := request("GET", path, nil, "", admin)
	if view.Code != 200 || strings.Contains(view.Body.String(), "test-challenge-proof") || strings.Contains(view.Body.String(), sealed) || strings.Contains(view.Body.String(), "marker") {
		t.Fatal("ledger API exposed private material")
	}
	if request("DELETE", "/api/v1/admin/dns-providers/"+strconv.Itoa(provider.ID), map[string]any{"password": "Local1-testing-password"}, "", admin).Code != 409 {
		t.Fatal("pending cleanup credentials deleted")
	}
	lock, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = lock.ExecContext(ctx, `SELECT pg_advisory_xact_lock(69443000,$1)`, item.ID); e != nil {
		lock.Rollback()
		t.Fatal(e)
	}
	if e = s.cleanupDNSChallenge(ctx, item.ID, client); e == nil || deletes != 0 {
		lock.Rollback()
		t.Fatal("concurrent cleanup bypassed lock")
	}
	lock.Rollback()
	rejectDelete = true
	if err = cleanup(ctx); err == nil {
		t.Fatal("failed provider delete marked as clean")
	}
	item, _ = s.Store.Resource.Get(ctx, item.ID)
	if item.Data["status"] != "cleanup_failed" || text(item.Data["value"]) == "" {
		t.Fatal("failed cleanup lost recovery material")
	}
	retry := path + "/" + strconv.Itoa(item.ID) + "/retry"
	if request("POST", retry, nil, "", alice).Code != 403 || request("POST", retry, nil, "", admin).Code != 202 {
		t.Fatal("cleanup retry permission/status")
	}
	rejectDelete = false
	// A fresh server wrapper has no in-memory record ID; state lives in PostgreSQL.
	restarted := &Server{Store: s.Store, DB: s.DB, Crypto: s.Crypto}
	restarted.recoverDNSChallengesWithClient(ctx, client)
	item, _ = s.Store.Resource.Get(ctx, item.ID)
	if item.Data["status"] != "cleaned" || item.Data["value"] != nil || item.Data["token"] != nil {
		t.Fatal("recovered cleanup not confirmed or secrets retained")
	}
	beforeDeletes := deletes
	if err = cleanup(ctx); err != nil || deletes != beforeDeletes {
		t.Fatal("replayed cleanup repeated external mutation")
	}
	// Simulate process death after POST but before the response/ID is persisted.
	ambiguous = true
	if _, err = durable.Present(ctx, "_acme-challenge.example.com", "second-proof"); err == nil {
		t.Fatal("ambiguous create response accepted")
	}
	interrupted, err := s.Store.Resource.Query().Where(resource.KindEQ("dns-challenge"), resource.IDGT(item.ID)).First(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ids = append(ids, interrupted.ID)
	if text(interrupted.Data["record_id"]) != "" {
		t.Fatal("ambiguous request unexpectedly persisted ID")
	}
	unrelated := records[strings.Repeat("2", 32)]
	unrelated.ID = strings.Repeat("c", 32)
	unrelated.Comment = "another-application"
	records[unrelated.ID] = unrelated
	interrupted.Data["retry_after"] = time.Now().Add(-time.Minute).Unix()
	s.Store.Resource.UpdateOne(interrupted).SetData(interrupted.Data).Save(ctx)
	restarted.recoverDNSChallengesWithClient(ctx, client)
	interrupted, _ = s.Store.Resource.Get(ctx, interrupted.ID)
	if interrupted.Data["status"] != "cleaned" || len(records) != 1 || records[unrelated.ID].Comment != "another-application" {
		t.Fatal("marker recovery deleted unrelated TXT or missed owned record")
	}
	if request("POST", retry, nil, "", admin).Code != 409 {
		t.Fatal("cleaned record queued again")
	}
	if posts != 2 {
		t.Fatal("cleanup reissued a challenge")
	}
	if request("DELETE", "/api/v1/admin/dns-providers/"+strconv.Itoa(provider.ID), map[string]any{"password": "Local1-testing-password"}, "", admin).Code != 200 {
		t.Fatal("cleaned ledger still prevented credential deletion")
	}
	t.Log("durable DNS intent before POST, encrypted ledger, provider failure/retry, advisory exclusion, restart recovery without ID, unowned TXT preservation, API redaction and credential deletion guard passed; controlled provider, not public CA")
}
