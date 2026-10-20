package subscription

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUsageMetadataUnknownAndStrictValues(t *testing.T) {
	for _, headers := range [][]string{
		{"upload=-1"}, {"download=1.2"}, {"total=9223372036854775808"},
		{"expire=1;expire=2"}, {"upload=0", "download=1"}, {"upload=1;total=unknown"},
		{"total=+12"}, {"expire"}, {"upload="},
	} {
		m := ParseUsageMetadata(headers)
		if m.Status != "invalid" || m.Upload != nil || m.Download != nil || m.Total != nil || m.Expire != nil {
			t.Fatal("invalid header produced partial or apparently normal observations")
		}
	}
	for _, headers := range [][]string{nil, {"private=must-not-store"}} {
		m := ParseUsageMetadata(headers)
		if m.Status != "absent" || m.Total != nil {
			t.Fatal("absent values inferred as zero")
		}
	}
	m := ParseUsageMetadata([]string{"upload=0; download=9007199254740993; expire=0; private=must-not-store"})
	b, _ := json.Marshal(m)
	if m.Status != "available" || m.Upload == nil || *m.Upload != "0" || *m.Download != "9007199254740993" || m.Total != nil || strings.Contains(string(b), "must-not-store") {
		t.Fatal("whitelist, unknown values or exact counters lost")
	}
}

func TestFetchInventoryRetainsOnlyWhitelistedMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Subscription-Userinfo", "upload=128;download=256;total=1024;expire=4102444800;private=do-not-store")
		w.Header().Set("Authorization", "do-not-store")
		w.Write([]byte("synthetic body"))
	}))
	defer server.Close()
	result, err := (HTTPFetcher{}).FetchInventory(context.Background(), server.URL, true)
	if err != nil || string(result.Body) != "synthetic body" || result.Usage.Status != "available" || *result.Usage.Download != "256" {
		t.Fatal("HTTP metadata fetch failed", err)
	}
	b, _ := json.Marshal(result)
	if strings.Contains(string(b), "do-not-store") {
		t.Fatal("unrelated response headers retained")
	}
}
