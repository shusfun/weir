package cert

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestDurableCloudflareOwnership(t *testing.T) {
	expected := OwnedTXT{ID: strings.Repeat("b", 32), Type: "TXT", Name: "_acme-challenge.example.com", Content: "test-proof", Comment: "migate-dns01:test-owned-random-marker"}
	deleted := 0
	mode := "normal"
	p := CloudflareDNS{Token: "private-test-only", ZoneID: strings.Repeat("a", 32), Client: &http.Client{Transport: dnsRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.cloudflare.com" || r.Header.Get("Authorization") != "Bearer private-test-only" {
			t.Fatal("credential destination changed")
		}
		status := 200
		var body any
		switch {
		case mode == "rejected":
			status = 403
			body = map[string]any{"secret": "private-test-only"}
		case r.Method == "POST":
			var data map[string]any
			json.NewDecoder(r.Body).Decode(&data)
			if data["comment"] != expected.Comment || data["type"] != "TXT" {
				t.Fatal("missing owned marker")
			}
			body = map[string]any{"success": true, "result": expected}
		case r.Method == "DELETE":
			deleted++
			if mode == "delete-missing" {
				status = 404
			}
			body = map[string]any{"success": true}
		case r.URL.RawQuery != "":
			other := expected
			other.ID = strings.Repeat("c", 32)
			other.Comment = "other-application"
			pages := 1
			if mode == "excessive-pages" {
				pages = 11
			}
			body = map[string]any{"success": true, "result": []OwnedTXT{other, expected}, "result_info": map[string]any{"total_pages": pages}}
		default:
			actual := expected
			if mode == "changed-owner" {
				actual.Comment = "other-application"
			}
			if mode == "get-missing" {
				status = 404
			}
			body = map[string]any{"success": true, "result": actual}
		}
		b, _ := json.Marshal(body)
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(string(b))), Header: http.Header{}}, nil
	})}}
	ctx := context.Background()
	if id, err := p.CreateOwnedTXT(ctx, expected); err != nil || id != expected.ID {
		t.Fatal("owned creation failed")
	}
	ids, err := p.FindOwnedTXT(ctx, expected)
	if err != nil || len(ids) != 1 || ids[0] != expected.ID {
		t.Fatal("recovery included another application's TXT")
	}
	mode = "changed-owner"
	if err = p.DeleteOwnedTXT(ctx, expected); err == nil || deleted != 0 {
		t.Fatal("changed ownership deleted")
	}
	mode = "normal"
	if err = p.DeleteOwnedTXT(ctx, expected); err != nil || deleted != 1 {
		t.Fatal("matching owned record not deleted")
	}
	mode = "get-missing"
	if err = p.DeleteOwnedTXT(ctx, expected); err != nil || deleted != 1 {
		t.Fatal("missing record not idempotent")
	}
	mode = "delete-missing"
	if err = p.DeleteOwnedTXT(ctx, expected); err != nil || deleted != 2 {
		t.Fatal("concurrent removal not idempotent")
	}
	mode = "excessive-pages"
	if _, err = p.FindOwnedTXT(ctx, expected); err == nil {
		t.Fatal("incomplete inventory accepted")
	}
	mode = "rejected"
	if err = p.DeleteOwnedTXT(ctx, expected); err == nil || strings.Contains(err.Error(), p.Token) {
		t.Fatal("provider rejection leaked credentials")
	}
	invalid := expected
	invalid.Comment = ""
	if _, err = p.CreateOwnedTXT(ctx, invalid); err == nil {
		t.Fatal("unowned record created")
	}
}
