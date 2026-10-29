package geoip

import (
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

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
