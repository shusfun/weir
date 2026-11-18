package geoip

import (
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCountrySnapshotWithMMDB(t *testing.T) {
	fixture := os.Getenv("WEIR_TEST_COUNTRY_MMDB")
	if fixture == "" {
		t.Skip("WEIR_TEST_COUNTRY_MMDB not set")
	}
	dir := t.TempDir()
	data, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, cityFile)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	db := Open(dir)
	t.Cleanup(func() {
		if db.city != nil {
			_ = db.city.Close()
		}
	})
	prefixes, initial, err := db.CountryNetworks("CN")
	if err != nil || len(prefixes) == 0 {
		t.Fatalf("snapshot ready=%v prefixes=%d: %v", initial.Ready, len(prefixes), err)
	}
	updated := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	if err = os.Chtimes(path, updated, updated); err != nil {
		t.Fatal(err)
	}
	var reads sync.WaitGroup
	for range 4 {
		reads.Go(func() {
			for range 8 {
				got, status, err := db.CountryNetworks("CN")
				if err != nil || len(got) != len(prefixes) || (!status.Fetched.Equal(initial.Fetched) && !status.Fetched.Equal(updated)) {
					t.Errorf("unexpected snapshot version=%s prefixes=%d: %v", status.Fetched, len(got), err)
				}
			}
		})
	}
	err = db.reload()
	reads.Wait()
	if err != nil || !db.Status().Fetched.Equal(updated) {
		t.Fatalf("snapshot replacement: %v, %s", err, db.Status().Fetched)
	}
	failure := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(failure.Close)
	db.BaseURL, db.HTTP = failure.URL, failure.Client()
	if err := db.Refresh(t.Context(), updated.Add(36*24*time.Hour)); err == nil {
		t.Fatal("refresh should fail")
	}
	if got, status, err := db.CountryNetworks("CN"); err != nil || len(got) != len(prefixes) || !status.Fetched.Equal(updated) {
		t.Fatalf("failed refresh lost active snapshot: %+v %v", status, err)
	}
}

// 下载的不是有效 mmdb 时不替换、不就绪，原因写进状态；6 小时内不重试。
func TestRefreshRejectsInvalidDatabase(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		gz := gzip.NewWriter(w)
		_, _ = gz.Write([]byte("not an mmdb"))
		_ = gz.Close()
	}))
	defer srv.Close()
	dir := t.TempDir()
	db := Open(dir)
	db.BaseURL, db.HTTP = srv.URL, srv.Client()
	if db.Status().Ready {
		t.Fatal("empty dir must not be ready")
	}
	now := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	if err := db.Refresh(context.Background(), now); err == nil || !strings.Contains(err.Error(), "mmdb") {
		t.Fatalf("refresh = %v", err)
	}
	if st := db.Status(); st.Ready || !strings.Contains(st.Error, "下载失败") {
		t.Fatalf("status = %+v", st)
	}
	if _, err := os.Stat(filepath.Join(dir, cityFile)); !os.IsNotExist(err) {
		t.Fatal("invalid file must not replace the database")
	}
	before := hits
	_ = db.Refresh(context.Background(), now.Add(time.Hour))
	if hits != before {
		t.Fatal("must not retry within 6 hours")
	}
	if _, ok := db.Lookup("8.8.8.8"); ok {
		t.Fatal("lookup without database must report unknown")
	}
}
